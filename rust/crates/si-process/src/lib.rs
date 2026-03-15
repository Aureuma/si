use std::collections::{BTreeMap, BTreeSet};
use std::io::{Cursor, Read};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, ExitStatus, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use thiserror::Error;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CommandSpec {
    program: String,
    args: Vec<String>,
    current_dir: Option<PathBuf>,
    env: BTreeMap<String, String>,
    env_remove: BTreeSet<String>,
}

impl CommandSpec {
    pub fn new(program: impl Into<String>) -> Self {
        Self {
            program: program.into(),
            args: Vec::new(),
            current_dir: None,
            env: BTreeMap::new(),
            env_remove: BTreeSet::new(),
        }
    }

    pub fn arg(mut self, arg: impl Into<String>) -> Self {
        self.args.push(arg.into());
        self
    }

    pub fn args<I, S>(mut self, args: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        self.args.extend(args.into_iter().map(Into::into));
        self
    }

    pub fn current_dir(mut self, current_dir: impl Into<PathBuf>) -> Self {
        self.current_dir = Some(current_dir.into());
        self
    }

    pub fn env(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        let key = key.into();
        self.env_remove.remove(&key);
        self.env.insert(key, value.into());
        self
    }

    pub fn env_remove(mut self, key: impl Into<String>) -> Self {
        let key = key.into();
        self.env.remove(&key);
        self.env_remove.insert(key);
        self
    }

    pub fn program(&self) -> &str {
        &self.program
    }

    pub fn args_slice(&self) -> &[String] {
        &self.args
    }

    pub fn current_dir_path(&self) -> Option<&Path> {
        self.current_dir.as_deref()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum StreamBehavior {
    Inherit,
    Capture,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum StdinBehavior {
    Null,
    Inherit,
    Bytes(Vec<u8>),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RunOptions {
    pub stdin: StdinBehavior,
    pub stdout: StreamBehavior,
    pub stderr: StreamBehavior,
    pub timeout: Option<Duration>,
}

impl Default for RunOptions {
    fn default() -> Self {
        Self {
            stdin: StdinBehavior::Null,
            stdout: StreamBehavior::Capture,
            stderr: StreamBehavior::Capture,
            timeout: None,
        }
    }
}

#[derive(Debug)]
pub struct CommandOutput {
    pub status: ExitStatus,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
}

impl CommandOutput {
    pub fn exit_code(&self) -> Option<i32> {
        self.status.code()
    }
}

#[derive(Debug, Error)]
pub enum ProcessError {
    #[error("spawn {program:?}: {source}")]
    Spawn {
        program: String,
        #[source]
        source: std::io::Error,
    },
    #[error("wait for {program:?}: {source}")]
    Wait {
        program: String,
        #[source]
        source: std::io::Error,
    },
    #[error("read {stream} from {program:?}: {source}")]
    Read {
        program: String,
        stream: &'static str,
        #[source]
        source: std::io::Error,
    },
    #[error("join {stream} reader for {program:?}")]
    Join { program: String, stream: &'static str },
    #[error("{program:?} timed out after {timeout:?}")]
    TimedOut { program: String, timeout: Duration },
    #[error("kill {program:?} after timeout: {source}")]
    Kill {
        program: String,
        #[source]
        source: std::io::Error,
    },
}

#[derive(Clone, Copy, Debug, Default)]
pub struct ProcessRunner;

impl ProcessRunner {
    pub fn run(
        &self,
        spec: &CommandSpec,
        options: &RunOptions,
    ) -> Result<CommandOutput, ProcessError> {
        let mut command = Command::new(&spec.program);
        command.args(&spec.args);
        command.stdin(match options.stdin {
            StdinBehavior::Null => Stdio::null(),
            StdinBehavior::Inherit => Stdio::piped(),
            StdinBehavior::Bytes(_) => Stdio::piped(),
        });

        if let Some(current_dir) = &spec.current_dir {
            command.current_dir(current_dir);
        }
        for (key, value) in &spec.env {
            command.env(key, value);
        }
        for key in &spec.env_remove {
            command.env_remove(key);
        }
        command.stdout(match options.stdout {
            StreamBehavior::Capture => Stdio::piped(),
            StreamBehavior::Inherit => Stdio::inherit(),
        });
        command.stderr(match options.stderr {
            StreamBehavior::Capture => Stdio::piped(),
            StreamBehavior::Inherit => Stdio::inherit(),
        });

        let mut child = spawn_command_with_retry(command, &spec.program)?;

        let stdout = spawn_reader(child.stdout.take(), &spec.program, "stdout");
        let stderr = spawn_reader(child.stderr.take(), &spec.program, "stderr");
        let stdin = spawn_stdin_writer(child.stdin.take(), &spec.program, options.stdin.clone());
        let status = wait_for_child(&mut child, &spec.program, options.timeout)?;
        let stdout = join_reader(stdout)?;
        let stderr = join_reader(stderr)?;
        join_stdin_writer(stdin)?;

        Ok(CommandOutput { status, stdout, stderr })
    }
}

fn spawn_command_with_retry(mut command: Command, program: &str) -> Result<Child, ProcessError> {
    const ETXTBSY_RETRIES: u32 = 5;
    const ETXTBSY_DELAY: Duration = Duration::from_millis(20);

    for attempt in 0..=ETXTBSY_RETRIES {
        match command.spawn() {
            Ok(child) => return Ok(child),
            Err(source) if source.raw_os_error() == Some(26) && attempt < ETXTBSY_RETRIES => {
                thread::sleep(ETXTBSY_DELAY);
            }
            Err(source) => {
                return Err(ProcessError::Spawn { program: program.to_owned(), source });
            }
        }
    }
    unreachable!("spawn retry loop should always return")
}

fn spawn_reader(
    pipe: Option<impl Read + Send + 'static>,
    program: &str,
    stream: &'static str,
) -> Option<thread::JoinHandle<Result<Vec<u8>, ProcessError>>> {
    pipe.map(|mut pipe| {
        let program = program.to_owned();
        thread::spawn(move || {
            let mut buffer = Vec::new();
            pipe.read_to_end(&mut buffer).map_err(|source| ProcessError::Read {
                program,
                stream,
                source,
            })?;
            Ok(buffer)
        })
    })
}

fn join_reader(
    handle: Option<thread::JoinHandle<Result<Vec<u8>, ProcessError>>>,
) -> Result<Vec<u8>, ProcessError> {
    match handle {
        Some(handle) => match handle.join() {
            Ok(result) => result,
            Err(_) => Err(ProcessError::Join { program: "process".to_owned(), stream: "capture" }),
        },
        None => Ok(Vec::new()),
    }
}

fn spawn_stdin_writer(
    pipe: Option<impl std::io::Write + Send + 'static>,
    program: &str,
    behavior: StdinBehavior,
) -> Option<thread::JoinHandle<Result<(), ProcessError>>> {
    match (pipe, behavior) {
        (Some(mut pipe), StdinBehavior::Bytes(bytes)) => {
            let program = program.to_owned();
            Some(thread::spawn(move || {
                std::io::copy(&mut Cursor::new(bytes), &mut pipe)
                    .map_err(|source| ProcessError::Read { program, stream: "stdin", source })?;
                Ok(())
            }))
        }
        (Some(mut pipe), StdinBehavior::Inherit) => {
            let program = program.to_owned();
            Some(thread::spawn(move || {
                std::io::copy(&mut std::io::stdin(), &mut pipe)
                    .map_err(|source| ProcessError::Read { program, stream: "stdin", source })?;
                Ok(())
            }))
        }
        _ => None,
    }
}

fn join_stdin_writer(
    handle: Option<thread::JoinHandle<Result<(), ProcessError>>>,
) -> Result<(), ProcessError> {
    match handle {
        Some(handle) => match handle.join() {
            Ok(result) => result,
            Err(_) => Err(ProcessError::Join { program: "process".to_owned(), stream: "stdin" }),
        },
        None => Ok(()),
    }
}

fn wait_for_child(
    child: &mut std::process::Child,
    program: &str,
    timeout: Option<Duration>,
) -> Result<ExitStatus, ProcessError> {
    match timeout {
        None => child
            .wait()
            .map_err(|source| ProcessError::Wait { program: program.to_owned(), source }),
        Some(timeout) => {
            let started_at = Instant::now();
            loop {
                match child
                    .try_wait()
                    .map_err(|source| ProcessError::Wait { program: program.to_owned(), source })?
                {
                    Some(status) => return Ok(status),
                    None if started_at.elapsed() >= timeout => {
                        child.kill().map_err(|source| ProcessError::Kill {
                            program: program.to_owned(),
                            source,
                        })?;
                        let _ = child.wait();
                        return Err(ProcessError::TimedOut {
                            program: program.to_owned(),
                            timeout,
                        });
                    }
                    None => thread::sleep(Duration::from_millis(10)),
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    fn sh(script: &str) -> CommandSpec {
        CommandSpec::new("sh").args(["-c", script])
    }

    #[test]
    fn captures_stdout_and_stderr() {
        let runner = ProcessRunner;
        let output = runner
            .run(&sh("printf 'hello'; printf 'warn' >&2"), &RunOptions::default())
            .expect("run process");

        assert!(output.status.success());
        assert_eq!(String::from_utf8_lossy(&output.stdout), "hello");
        assert_eq!(String::from_utf8_lossy(&output.stderr), "warn");
    }

    #[test]
    fn applies_env_and_current_dir() {
        let temp_dir = TempDir::new().expect("temp dir");
        let runner = ProcessRunner;
        let output = runner
            .run(
                &sh("pwd; printf '%s' \"$SI_SAMPLE\"")
                    .current_dir(temp_dir.path())
                    .env("SI_SAMPLE", "value"),
                &RunOptions::default(),
            )
            .expect("run process");

        let stdout = String::from_utf8_lossy(&output.stdout);
        assert!(stdout.contains(temp_dir.path().to_string_lossy().as_ref()));
        assert!(stdout.ends_with("value"));
    }

    #[test]
    fn removes_env_values() {
        let runner = ProcessRunner;
        let output = runner
            .run(
                &sh("printf '%s' \"${SI_SAMPLE:-missing}\"")
                    .env("SI_SAMPLE", "value")
                    .env_remove("SI_SAMPLE"),
                &RunOptions::default(),
            )
            .expect("run process");

        assert_eq!(String::from_utf8_lossy(&output.stdout), "missing");
    }

    #[test]
    fn retains_non_zero_exit_status() {
        let runner = ProcessRunner;
        let output = runner
            .run(&sh("printf 'boom' >&2; exit 7"), &RunOptions::default())
            .expect("run process");

        assert_eq!(output.exit_code(), Some(7));
        assert_eq!(String::from_utf8_lossy(&output.stderr), "boom");
    }

    #[test]
    fn reports_timeout() {
        let runner = ProcessRunner;
        let err = runner
            .run(
                &sh("sleep 1"),
                &RunOptions { timeout: Some(Duration::from_millis(20)), ..RunOptions::default() },
            )
            .expect_err("timeout");

        match err {
            ProcessError::TimedOut { timeout, .. } => {
                assert_eq!(timeout, Duration::from_millis(20));
            }
            other => panic!("unexpected error: {other:?}"),
        }
    }

    #[test]
    fn writes_static_stdin_bytes() {
        let runner = ProcessRunner;
        let output = runner
            .run(
                &sh("cat"),
                &RunOptions {
                    stdin: StdinBehavior::Bytes(b"hello".to_vec()),
                    ..RunOptions::default()
                },
            )
            .expect("run process");

        assert_eq!(String::from_utf8_lossy(&output.stdout), "hello");
    }
}
