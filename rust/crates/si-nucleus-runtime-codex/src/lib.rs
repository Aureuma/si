use std::collections::{BTreeMap, HashMap};
use std::io::{BufRead, BufReader, Write};
use std::process::{Child, ChildStderr, ChildStdin, ChildStdout, Command, Stdio};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::{Arc, Mutex, mpsc};
use std::thread;
use std::time::{Duration, Instant};

use anyhow::{Context, Result, anyhow};
use chrono::{DateTime, Local, TimeZone, Utc};
use serde::Deserialize;
use serde_json::{Value, json};
use si_nucleus_core::{
    CanonicalEventSource, CanonicalEventType, EventDataEnvelope, RunStatus, WorkerId, WorkerStatus,
};
use si_nucleus_runtime::{
    CanonicalEventDraft, NucleusRuntime, RunInputItem, RunTurnSpec, RuntimeCommand,
    RuntimeRunOutcome, RuntimeStatusSnapshot, SessionOpenResult, SessionOpenSpec, WorkerLaunchSpec,
    WorkerProbeResult, WorkerRuntimeView, WorkerStartResult,
};
use si_rs_codex::codex_profile_fort_runtime_env;
use si_rs_process::{CommandSpec, ProcessRunner, RunOptions, StdinBehavior};

const RESPONSE_TIMEOUT: Duration = Duration::from_secs(30);
const DEFAULT_TURN_TIMEOUT: Duration = Duration::from_secs(900);
const TURN_POLL_INTERVAL: Duration = Duration::from_secs(15);

fn is_managed_codex_runtime_env(key: &str) -> bool {
    matches!(key, "HOME" | "CODEX_HOME" | "FORT_TOKEN_PATH" | "FORT_REFRESH_TOKEN_PATH" | "TERM")
}

#[derive(Debug)]
struct ManagedCodexWorker {
    worker_id: WorkerId,
    pid: u32,
    started_at: DateTime<Utc>,
    request_seq: AtomicI64,
    request_tx: mpsc::Sender<String>,
    pending: Arc<Mutex<HashMap<i64, mpsc::Sender<Value>>>>,
    listeners: Arc<Mutex<Vec<mpsc::Sender<Value>>>>,
    stderr_lines: Arc<Mutex<Vec<String>>>,
    child: Arc<Mutex<Child>>,
}

impl ManagedCodexWorker {
    fn spawn(worker_id: WorkerId, command: RuntimeCommand) -> Result<Arc<Self>> {
        let mut child = Command::new(&command.program);
        child.args(&command.args);
        child.current_dir(&command.current_dir);
        child.stdin(Stdio::piped());
        child.stdout(Stdio::piped());
        child.stderr(Stdio::piped());
        for (key, value) in &command.env {
            child.env(key, value);
        }
        let mut child = child
            .spawn()
            .with_context(|| format!("spawn {} {:?}", command.program, command.args))?;
        let pid = child.id();
        let stdin = child.stdin.take().ok_or_else(|| anyhow!("missing codex stdin"))?;
        let stdout = child.stdout.take().ok_or_else(|| anyhow!("missing codex stdout"))?;
        let stderr = child.stderr.take().ok_or_else(|| anyhow!("missing codex stderr"))?;
        let child = Arc::new(Mutex::new(child));
        let pending = Arc::new(Mutex::new(HashMap::<i64, mpsc::Sender<Value>>::new()));
        let listeners = Arc::new(Mutex::new(Vec::<mpsc::Sender<Value>>::new()));
        let stderr_lines = Arc::new(Mutex::new(Vec::<String>::new()));
        let (request_tx, request_rx) = mpsc::channel::<String>();

        spawn_writer_thread(stdin, request_rx)?;
        spawn_stdout_thread(stdout, Arc::clone(&pending), Arc::clone(&listeners));
        spawn_stderr_thread(stderr, Arc::clone(&stderr_lines));

        Ok(Arc::new(Self {
            worker_id,
            pid,
            started_at: Utc::now(),
            request_seq: AtomicI64::new(1000),
            request_tx,
            pending,
            listeners,
            stderr_lines,
            child,
        }))
    }

    fn subscribe(&self) -> mpsc::Receiver<Value> {
        let (tx, rx) = mpsc::channel();
        let mut listeners = self.listeners.lock().expect("worker listeners lock");
        listeners.push(tx);
        rx
    }

    fn call(&self, method: &str, params: Value) -> Result<Value> {
        let request_id = self.request_seq.fetch_add(1, Ordering::SeqCst);
        let request = json!({
            "jsonrpc": "2.0",
            "id": request_id,
            "method": method,
            "params": params,
        });
        let (tx, rx) = mpsc::channel();
        {
            let mut pending = self.pending.lock().expect("worker pending lock");
            pending.insert(request_id, tx);
        }
        let payload = serde_json::to_string(&request)?;
        if self.request_tx.send(payload).is_err() {
            let mut pending = self.pending.lock().expect("worker pending lock");
            pending.remove(&request_id);
            anyhow::bail!("worker request channel closed");
        }
        let response = match rx.recv_timeout(RESPONSE_TIMEOUT) {
            Ok(response) => response,
            Err(mpsc::RecvTimeoutError::Timeout) => {
                let mut pending = self.pending.lock().expect("worker pending lock");
                pending.remove(&request_id);
                anyhow::bail!("worker request timed out: {method}");
            }
            Err(mpsc::RecvTimeoutError::Disconnected) => {
                anyhow::bail!("worker response channel closed")
            }
        };
        if let Some(error) = response.get("error") {
            let message = error
                .get("message")
                .and_then(Value::as_str)
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .unwrap_or("codex app-server request failed");
            anyhow::bail!("{message}");
        }
        Ok(response.get("result").cloned().unwrap_or(Value::Null))
    }

    fn kill(&self) -> Result<()> {
        let mut child = self.child.lock().expect("worker child lock");
        child.kill().context("kill codex app-server")?;
        let _ = child.wait();
        Ok(())
    }

    fn stderr_summary(&self) -> String {
        let lines = self.stderr_lines.lock().expect("worker stderr lock");
        lines.join("\n")
    }

    fn runtime_view(&self, checked_at: DateTime<Utc>) -> WorkerRuntimeView {
        WorkerRuntimeView {
            worker_id: self.worker_id.clone(),
            runtime_name: "codex-app-server".to_owned(),
            pid: self.pid,
            started_at: self.started_at,
            checked_at,
        }
    }
}

fn spawn_writer_thread(stdin: ChildStdin, request_rx: mpsc::Receiver<String>) -> Result<()> {
    thread::Builder::new()
        .name("si-nucleus-codex-writer".to_owned())
        .spawn(move || writer_loop(stdin, request_rx))
        .context("spawn codex writer thread")?;
    Ok(())
}

fn writer_loop(mut stdin: ChildStdin, request_rx: mpsc::Receiver<String>) {
    for line in request_rx {
        if stdin.write_all(line.as_bytes()).is_err() {
            break;
        }
        if stdin.write_all(b"\n").is_err() {
            break;
        }
        if stdin.flush().is_err() {
            break;
        }
    }
}

fn spawn_stdout_thread(
    stdout: ChildStdout,
    pending: Arc<Mutex<HashMap<i64, mpsc::Sender<Value>>>>,
    listeners: Arc<Mutex<Vec<mpsc::Sender<Value>>>>,
) {
    let _ = thread::Builder::new()
        .name("si-nucleus-codex-stdout".to_owned())
        .spawn(move || stdout_loop(stdout, pending, listeners));
}

fn stdout_loop(
    stdout: ChildStdout,
    pending: Arc<Mutex<HashMap<i64, mpsc::Sender<Value>>>>,
    listeners: Arc<Mutex<Vec<mpsc::Sender<Value>>>>,
) {
    let reader = BufReader::new(stdout);
    for line in reader.lines().map_while(Result::ok) {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        let Ok(value) = serde_json::from_str::<Value>(trimmed) else {
            continue;
        };
        if let Some(id) = value.get("id").and_then(parse_request_id) {
            let tx = {
                let mut pending = pending.lock().expect("worker pending lock");
                pending.remove(&id)
            };
            if let Some(tx) = tx {
                let _ = tx.send(value);
            }
            continue;
        }
        let mut listeners = listeners.lock().expect("worker listeners lock");
        listeners.retain(|tx| tx.send(value.clone()).is_ok());
    }
}

fn spawn_stderr_thread(stderr: ChildStderr, stderr_lines: Arc<Mutex<Vec<String>>>) {
    let _ = thread::Builder::new()
        .name("si-nucleus-codex-stderr".to_owned())
        .spawn(move || stderr_loop(stderr, stderr_lines));
}

fn stderr_loop(stderr: ChildStderr, stderr_lines: Arc<Mutex<Vec<String>>>) {
    let reader = BufReader::new(stderr);
    for line in reader.lines().map_while(Result::ok) {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        let mut lines = stderr_lines.lock().expect("worker stderr lock");
        lines.push(trimmed.to_owned());
        if lines.len() > 64 {
            let drain = lines.len() - 64;
            lines.drain(0..drain);
        }
    }
}

#[derive(Debug, Default)]
pub struct CodexNucleusRuntime {
    workers: Mutex<HashMap<WorkerId, Arc<ManagedCodexWorker>>>,
}

impl CodexNucleusRuntime {
    pub fn new() -> Self {
        Self::default()
    }

    fn worker(&self, worker_id: &WorkerId) -> Result<Arc<ManagedCodexWorker>> {
        let workers = self.workers.lock().map_err(|_| anyhow!("runtime worker lock poisoned"))?;
        workers.get(worker_id).cloned().ok_or_else(|| anyhow!("worker not running: {worker_id}"))
    }

    fn initialize_live_worker(worker: &ManagedCodexWorker) -> Result<()> {
        let _ = worker.call(
            "initialize",
            json!({
                "clientInfo": {
                    "name": "si-nucleus",
                    "version": si_rs_core::version::current_version(),
                },
                "capabilities": {
                    "experimentalApi": false,
                },
            }),
        )?;
        Ok(())
    }

    fn probe_live_worker(
        worker: &ManagedCodexWorker,
        workdir: Option<&str>,
    ) -> Result<WorkerProbeResult> {
        let rate_limits = worker.call("account/rateLimits/read", Value::Null)?;
        let account = worker.call("account/read", json!({ "refreshToken": false }))?;
        let config = worker.call(
            "config/read",
            json!({
                "includeLayers": false,
                "cwd": workdir,
            }),
        )?;
        let snapshot = runtime_status_snapshot_from_values(&rate_limits, &account, &config)?;
        Ok(WorkerProbeResult { status: WorkerStatus::Ready, snapshot, checked_at: Utc::now() })
    }

    fn turn_timeout(spec: &RunTurnSpec) -> Duration {
        spec.timeout_seconds.map(Duration::from_secs).unwrap_or(DEFAULT_TURN_TIMEOUT)
    }
}

impl NucleusRuntime for CodexNucleusRuntime {
    fn runtime_name(&self) -> &'static str {
        "codex-app-server"
    }

    fn build_worker_command(&self, spec: &WorkerLaunchSpec) -> RuntimeCommand {
        let mut env = BTreeMap::new();
        env.insert("HOME".to_owned(), spec.home_dir.display().to_string());
        env.insert("CODEX_HOME".to_owned(), spec.codex_home.display().to_string());
        env.insert("TERM".to_owned(), "xterm-256color".to_owned());
        env.extend(codex_profile_fort_runtime_env(&spec.codex_home));
        for (key, value) in &spec.extra_env {
            if is_managed_codex_runtime_env(key) {
                continue;
            }
            env.insert(key.clone(), value.clone());
        }
        RuntimeCommand {
            program: "codex".to_owned(),
            args: vec!["app-server".to_owned()],
            current_dir: spec.workdir.clone(),
            env,
        }
    }

    fn probe_worker(&self, spec: &WorkerLaunchSpec) -> Result<WorkerProbeResult> {
        if let Ok(worker) = self.worker(&spec.worker_id) {
            return Self::probe_live_worker(&worker, Some(&spec.workdir.display().to_string()));
        }
        let command = self.build_worker_command(spec);
        let process_spec = command_spec_from_runtime(&command);
        let runner = ProcessRunner;
        let options = RunOptions {
            stdin: StdinBehavior::Bytes(build_app_server_status_input(Some(
                spec.workdir.display().to_string(),
            ))),
            timeout: Some(Duration::from_secs(15)),
            ..RunOptions::default()
        };
        let output = runner.run(&process_spec, &options).map_err(anyhow::Error::new)?;
        let stdout = String::from_utf8_lossy(&output.stdout);
        let stderr = String::from_utf8_lossy(&output.stderr);
        let mut combined = stdout.trim().to_owned();
        if !stderr.trim().is_empty() {
            if !combined.is_empty() {
                combined.push('\n');
            }
            combined.push_str(stderr.trim());
        }
        if !output.status.success() {
            return Err(anyhow!(if combined.is_empty() {
                "codex app-server failed".to_owned()
            } else {
                combined
            }));
        }
        parse_app_server_status(&combined)
    }

    fn start_worker(&self, spec: &WorkerLaunchSpec) -> Result<WorkerStartResult> {
        if let Ok(worker) = self.worker(&spec.worker_id) {
            let probe =
                Self::probe_live_worker(&worker, Some(&spec.workdir.display().to_string()))?;
            return Ok(WorkerStartResult { runtime: worker.runtime_view(probe.checked_at), probe });
        }

        let command = self.build_worker_command(spec);
        let worker = ManagedCodexWorker::spawn(spec.worker_id.clone(), command)?;
        if let Err(error) = Self::initialize_live_worker(&worker) {
            let _ = worker.kill();
            return Err(error);
        }
        let probe =
            match Self::probe_live_worker(&worker, Some(&spec.workdir.display().to_string())) {
                Ok(probe) => probe,
                Err(error) => {
                    let stderr = worker.stderr_summary();
                    let _ = worker.kill();
                    if stderr.trim().is_empty() {
                        return Err(error);
                    }
                    return Err(error.context(stderr));
                }
            };
        {
            let mut workers =
                self.workers.lock().map_err(|_| anyhow!("runtime worker lock poisoned"))?;
            workers.insert(spec.worker_id.clone(), Arc::clone(&worker));
        }
        Ok(WorkerStartResult { runtime: worker.runtime_view(probe.checked_at), probe })
    }

    fn stop_worker(&self, worker_id: &WorkerId) -> Result<()> {
        let worker = {
            let mut workers =
                self.workers.lock().map_err(|_| anyhow!("runtime worker lock poisoned"))?;
            workers.remove(worker_id)
        };
        if let Some(worker) = worker {
            worker.kill()?;
        }
        Ok(())
    }

    fn inspect_worker(&self, worker_id: &WorkerId) -> Result<Option<WorkerRuntimeView>> {
        let worker = {
            let workers =
                self.workers.lock().map_err(|_| anyhow!("runtime worker lock poisoned"))?;
            workers.get(worker_id).cloned()
        };
        Ok(worker.map(|worker| worker.runtime_view(Utc::now())))
    }

    fn ensure_session(&self, spec: &SessionOpenSpec) -> Result<SessionOpenResult> {
        let worker = self.worker(&spec.worker_id)?;
        let result = if let Some(thread_id) = &spec.resume_thread_id {
            worker.call(
                "thread/resume",
                json!({
                    "threadId": thread_id,
                    "cwd": spec.workdir,
                    "persistExtendedHistory": false,
                }),
            )?
        } else {
            worker.call(
                "thread/start",
                json!({
                    "cwd": spec.workdir,
                    "approvalPolicy": "never",
                    "sandbox": "danger-full-access",
                    "serviceName": "si-nucleus",
                    "experimentalRawEvents": false,
                    "persistExtendedHistory": false,
                }),
            )?
        };
        let thread_id = result
            .get("thread")
            .and_then(|thread| thread.get("id"))
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .ok_or_else(|| anyhow!("thread id missing"))?;
        Ok(SessionOpenResult {
            thread_id: thread_id.to_owned(),
            created: spec.resume_thread_id.is_none(),
            opened_at: Utc::now(),
        })
    }

    fn execute_turn(
        &self,
        spec: &RunTurnSpec,
        on_event: &mut dyn FnMut(CanonicalEventDraft) -> Result<()>,
    ) -> Result<RuntimeRunOutcome> {
        let worker = self.worker(&spec.worker_id)?;
        let subscription = worker.subscribe();
        let input = spec
            .input
            .iter()
            .map(|item| match item {
                RunInputItem::Text { text } => json!({
                    "type": "text",
                    "text": text,
                    "text_elements": [],
                }),
            })
            .collect::<Vec<_>>();
        let response = worker.call(
            "turn/start",
            json!({
                "threadId": spec.thread_id,
                "input": input,
            }),
        )?;
        let turn_id = response
            .get("turn")
            .and_then(|turn| turn.get("id"))
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .ok_or_else(|| anyhow!("turn id missing"))?
            .to_owned();
        on_event(run_started_event(spec, &turn_id))?;

        let mut final_output = String::new();
        let started_at = Instant::now();
        let turn_timeout = Self::turn_timeout(spec);
        loop {
            let elapsed = started_at.elapsed();
            if elapsed >= turn_timeout {
                anyhow::bail!("turn timed out");
            }
            let poll_timeout = TURN_POLL_INTERVAL.min(turn_timeout.saturating_sub(elapsed));
            let notification = match subscription.recv_timeout(poll_timeout) {
                Ok(notification) => notification,
                Err(mpsc::RecvTimeoutError::Timeout) => continue,
                Err(mpsc::RecvTimeoutError::Disconnected) => {
                    anyhow::bail!("worker notification stream closed")
                }
            };
            let Some(method) = notification.get("method").and_then(Value::as_str) else {
                continue;
            };
            let params = notification.get("params").cloned().unwrap_or(Value::Null);
            match method {
                "item/agentMessage/delta" => {
                    if !matches_turn(&params, &spec.thread_id, &turn_id) {
                        continue;
                    }
                    let Some(delta) = params.get("delta").and_then(Value::as_str) else {
                        continue;
                    };
                    final_output.push_str(delta);
                    on_event(run_output_delta_event(spec, &turn_id, delta))?;
                }
                "item/completed" => {
                    if !matches_turn(&params, &spec.thread_id, &turn_id) {
                        continue;
                    }
                    let Some(item) = params.get("item") else {
                        continue;
                    };
                    if item.get("type").and_then(Value::as_str) != Some("agentMessage") {
                        continue;
                    }
                    if let Some(text) = item.get("text").and_then(Value::as_str) {
                        final_output = text.to_owned();
                    }
                }
                "turn/completed" => {
                    if params.get("threadId").and_then(Value::as_str)
                        != Some(spec.thread_id.as_str())
                    {
                        continue;
                    }
                    let Some(turn) = params.get("turn") else {
                        continue;
                    };
                    let completed_turn_id = turn
                        .get("id")
                        .and_then(Value::as_str)
                        .map(str::trim)
                        .filter(|value| !value.is_empty())
                        .unwrap_or_default();
                    if completed_turn_id != turn_id {
                        continue;
                    }
                    let status = turn.get("status").and_then(Value::as_str).unwrap_or("failed");
                    let error_message = turn
                        .get("error")
                        .and_then(|item| item.get("message"))
                        .and_then(Value::as_str)
                        .map(ToOwned::to_owned);
                    match status {
                        "completed" => {
                            on_event(run_completed_event(spec, &turn_id, &final_output, None))?;
                            return Ok(RuntimeRunOutcome {
                                turn_id,
                                status: RunStatus::Completed,
                                completed_at: Utc::now(),
                                final_output: if final_output.is_empty() {
                                    None
                                } else {
                                    Some(final_output)
                                },
                            });
                        }
                        "interrupted" => {
                            on_event(run_cancelled_event(
                                spec,
                                &turn_id,
                                error_message.as_deref(),
                            ))?;
                            return Ok(RuntimeRunOutcome {
                                turn_id,
                                status: RunStatus::Cancelled,
                                completed_at: Utc::now(),
                                final_output: if final_output.is_empty() {
                                    None
                                } else {
                                    Some(final_output)
                                },
                            });
                        }
                        _ => {
                            if is_auth_error(turn) {
                                on_event(run_requires_auth_event(
                                    spec,
                                    &turn_id,
                                    error_message.as_deref().unwrap_or("authentication required"),
                                ))?;
                                return Ok(RuntimeRunOutcome {
                                    turn_id,
                                    status: RunStatus::Blocked,
                                    completed_at: Utc::now(),
                                    final_output: if final_output.is_empty() {
                                        None
                                    } else {
                                        Some(final_output)
                                    },
                                });
                            }
                            on_event(run_failed_event(spec, &turn_id, error_message.as_deref()))?;
                            return Ok(RuntimeRunOutcome {
                                turn_id,
                                status: RunStatus::Failed,
                                completed_at: Utc::now(),
                                final_output: if final_output.is_empty() {
                                    None
                                } else {
                                    Some(final_output)
                                },
                            });
                        }
                    }
                }
                _ => {}
            }
        }
    }

    fn interrupt_turn(&self, worker_id: &WorkerId, thread_id: &str, turn_id: &str) -> Result<()> {
        let worker = self.worker(worker_id)?;
        let _ = worker.call(
            "turn/interrupt",
            json!({
                "threadId": thread_id,
                "turnId": turn_id,
            }),
        )?;
        Ok(())
    }

    fn probe_events(
        &self,
        spec: &WorkerLaunchSpec,
        probe: &WorkerProbeResult,
    ) -> Result<Vec<CanonicalEventDraft>> {
        let payload = self.status_payload(probe);
        Ok(vec![CanonicalEventDraft {
            event_type: match probe.status {
                WorkerStatus::Ready => CanonicalEventType::WorkerReady,
                WorkerStatus::Starting => CanonicalEventType::WorkerStarting,
                WorkerStatus::Degraded | WorkerStatus::Failed | WorkerStatus::Stopped => {
                    CanonicalEventType::WorkerFailed
                }
            },
            source: CanonicalEventSource::AppServer,
            data: EventDataEnvelope {
                task_id: None,
                worker_id: Some(spec.worker_id.clone()),
                session_id: None,
                run_id: None,
                profile: Some(spec.profile.clone()),
                payload,
            },
        }])
    }

    fn status_payload(&self, probe: &WorkerProbeResult) -> Value {
        json!({
            "source": probe.snapshot.source,
            "model": probe.snapshot.model,
            "reasoning_effort": probe.snapshot.reasoning_effort,
            "account_email": probe.snapshot.account_email,
            "account_plan": probe.snapshot.account_plan,
            "five_hour_left_pct": probe.snapshot.five_hour_left_pct,
            "five_hour_reset": probe.snapshot.five_hour_reset,
            "five_hour_remaining_minutes": probe.snapshot.five_hour_remaining_minutes,
            "weekly_left_pct": probe.snapshot.weekly_left_pct,
            "weekly_reset": probe.snapshot.weekly_reset,
            "weekly_remaining_minutes": probe.snapshot.weekly_remaining_minutes,
            "checked_at": probe.checked_at,
            "worker_status": probe.status,
        })
    }
}

fn command_spec_from_runtime(command: &RuntimeCommand) -> CommandSpec {
    let mut spec = CommandSpec::new(&command.program).current_dir(&command.current_dir);
    spec = spec.args(command.args.clone());
    for (key, value) in &command.env {
        spec = spec.env(key, value);
    }
    spec
}

fn build_app_server_request(id: i64, method: &str, params: Value) -> Value {
    json!({
        "jsonrpc": "2.0",
        "id": id,
        "method": method,
        "params": params,
    })
}

fn build_app_server_initialize_request(id: i64) -> Value {
    build_app_server_request(
        id,
        "initialize",
        json!({
            "clientInfo": {
                "name": "si-nucleus",
                "version": si_rs_core::version::current_version(),
            },
            "capabilities": {
                "experimentalApi": false,
            },
        }),
    )
}

fn serialize_app_server_requests(requests: &[Value]) -> Vec<u8> {
    let mut payload = Vec::new();
    for request in requests {
        payload.extend(serde_json::to_vec(request).expect("app server request json"));
        payload.push(b'\n');
    }
    payload
}

fn build_app_server_status_input(cwd: Option<String>) -> Vec<u8> {
    serialize_app_server_requests(&[
        build_app_server_initialize_request(1),
        build_app_server_request(2, "account/rateLimits/read", Value::Null),
        build_app_server_request(3, "account/read", json!({ "refreshToken": false })),
        build_app_server_request(
            4,
            "config/read",
            json!({
                "includeLayers": false,
                "cwd": cwd,
            }),
        ),
    ])
}

fn parse_app_server_status(raw: &str) -> Result<WorkerProbeResult> {
    let raw = raw.trim();
    if raw.is_empty() {
        anyhow::bail!("empty app-server output");
    }
    let mut rate_resp: Option<Value> = None;
    let mut account_resp: Option<Value> = None;
    let mut config_resp: Option<Value> = None;
    let mut rate_err: Option<String> = None;

    for line in raw.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let Ok(envelope) = serde_json::from_str::<AppServerEnvelope>(line) else {
            continue;
        };
        let Some(id) = parse_request_id(&envelope.id) else {
            continue;
        };
        if let Some(error) = envelope.error {
            if id == 2 {
                let message = error.message.trim();
                rate_err = Some(if message.is_empty() {
                    "rate limits request failed".to_owned()
                } else {
                    message.to_owned()
                });
            }
            continue;
        }
        match id {
            2 => rate_resp = Some(envelope.result),
            3 => account_resp = Some(envelope.result),
            4 => config_resp = Some(envelope.result),
            _ => {}
        }
    }

    if let Some(rate_err) = rate_err {
        anyhow::bail!(rate_err);
    }
    let rate_resp = rate_resp.ok_or_else(|| anyhow!("rate limits missing"))?;
    let account_resp = account_resp.unwrap_or(Value::Null);
    let config_resp = config_resp.unwrap_or(Value::Null);
    let snapshot = runtime_status_snapshot_from_values(&rate_resp, &account_resp, &config_resp)?;
    Ok(WorkerProbeResult { status: WorkerStatus::Ready, snapshot, checked_at: Utc::now() })
}

fn runtime_status_snapshot_from_values(
    rate: &Value,
    account: &Value,
    config: &Value,
) -> Result<RuntimeStatusSnapshot> {
    let rate_resp = serde_json::from_value::<AppRateLimitsResponse>(rate.clone())?;
    let account_resp =
        serde_json::from_value::<AppAccountResponse>(account.clone()).unwrap_or_default();
    let config_resp =
        serde_json::from_value::<AppConfigResponse>(config.clone()).unwrap_or_default();

    let total_limit_min = std::env::var("CODEX_PLAN_LIMIT_MINUTES")
        .ok()
        .and_then(|value| value.trim().parse::<i64>().ok())
        .filter(|value| *value > 0)
        .unwrap_or(300);
    let now = chrono::Local::now();
    let (five_hour_left_pct, five_hour_remaining_minutes, five_hour_reset) = rate_resp
        .rate_limits
        .primary
        .as_ref()
        .map(|window| window_usage(window, total_limit_min, now))
        .unwrap_or((None, None, None));
    let (weekly_left_pct, weekly_remaining_minutes, weekly_reset) = rate_resp
        .rate_limits
        .secondary
        .as_ref()
        .map(|window| window_usage(window, 0, now))
        .unwrap_or((None, None, None));

    Ok(RuntimeStatusSnapshot {
        source: "app-server".to_owned(),
        model: config_resp.config.model.filter(|value| !value.trim().is_empty()),
        reasoning_effort: config_resp
            .config
            .model_reasoning_effort
            .filter(|value| !value.trim().is_empty()),
        account_email: account_resp
            .account
            .as_ref()
            .filter(|account| account.account_type.eq_ignore_ascii_case("chatgpt"))
            .map(|account| account.email.trim().to_owned())
            .filter(|value| !value.is_empty()),
        account_plan: account_resp
            .account
            .as_ref()
            .filter(|account| account.account_type.eq_ignore_ascii_case("chatgpt"))
            .map(|account| account.plan_type.trim().to_owned())
            .filter(|value| !value.is_empty()),
        five_hour_left_pct,
        five_hour_reset,
        five_hour_remaining_minutes,
        weekly_left_pct,
        weekly_reset,
        weekly_remaining_minutes,
    })
}

fn parse_request_id(value: &Value) -> Option<i64> {
    match value {
        Value::Number(number) => number.as_i64(),
        Value::String(value) => value.trim().parse::<i64>().ok(),
        _ => None,
    }
}

fn matches_turn(params: &Value, thread_id: &str, turn_id: &str) -> bool {
    params.get("threadId").and_then(Value::as_str) == Some(thread_id)
        && params.get("turnId").and_then(Value::as_str) == Some(turn_id)
}

fn run_started_event(spec: &RunTurnSpec, turn_id: &str) -> CanonicalEventDraft {
    CanonicalEventDraft {
        event_type: CanonicalEventType::RunStarted,
        source: CanonicalEventSource::AppServer,
        data: EventDataEnvelope {
            task_id: spec.task_id.clone(),
            worker_id: Some(spec.worker_id.clone()),
            session_id: Some(spec.session_id.clone()),
            run_id: Some(spec.run_id.clone()),
            profile: Some(spec.profile.clone()),
            payload: json!({
                "thread_id": spec.thread_id,
                "turn_id": turn_id,
            }),
        },
    }
}

fn run_output_delta_event(spec: &RunTurnSpec, turn_id: &str, delta: &str) -> CanonicalEventDraft {
    CanonicalEventDraft {
        event_type: CanonicalEventType::RunOutputDelta,
        source: CanonicalEventSource::AppServer,
        data: EventDataEnvelope {
            task_id: spec.task_id.clone(),
            worker_id: Some(spec.worker_id.clone()),
            session_id: Some(spec.session_id.clone()),
            run_id: Some(spec.run_id.clone()),
            profile: Some(spec.profile.clone()),
            payload: json!({
                "thread_id": spec.thread_id,
                "turn_id": turn_id,
                "delta": delta,
            }),
        },
    }
}

fn run_completed_event(
    spec: &RunTurnSpec,
    turn_id: &str,
    final_output: &str,
    error: Option<&str>,
) -> CanonicalEventDraft {
    CanonicalEventDraft {
        event_type: CanonicalEventType::RunCompleted,
        source: CanonicalEventSource::AppServer,
        data: EventDataEnvelope {
            task_id: spec.task_id.clone(),
            worker_id: Some(spec.worker_id.clone()),
            session_id: Some(spec.session_id.clone()),
            run_id: Some(spec.run_id.clone()),
            profile: Some(spec.profile.clone()),
            payload: json!({
                "thread_id": spec.thread_id,
                "turn_id": turn_id,
                "final_output": final_output,
                "error": error,
            }),
        },
    }
}

fn run_cancelled_event(
    spec: &RunTurnSpec,
    turn_id: &str,
    error: Option<&str>,
) -> CanonicalEventDraft {
    CanonicalEventDraft {
        event_type: CanonicalEventType::RunCancelled,
        source: CanonicalEventSource::AppServer,
        data: EventDataEnvelope {
            task_id: spec.task_id.clone(),
            worker_id: Some(spec.worker_id.clone()),
            session_id: Some(spec.session_id.clone()),
            run_id: Some(spec.run_id.clone()),
            profile: Some(spec.profile.clone()),
            payload: json!({
                "thread_id": spec.thread_id,
                "turn_id": turn_id,
                "error": error,
            }),
        },
    }
}

fn run_failed_event(spec: &RunTurnSpec, turn_id: &str, error: Option<&str>) -> CanonicalEventDraft {
    CanonicalEventDraft {
        event_type: CanonicalEventType::RunFailed,
        source: CanonicalEventSource::AppServer,
        data: EventDataEnvelope {
            task_id: spec.task_id.clone(),
            worker_id: Some(spec.worker_id.clone()),
            session_id: Some(spec.session_id.clone()),
            run_id: Some(spec.run_id.clone()),
            profile: Some(spec.profile.clone()),
            payload: json!({
                "thread_id": spec.thread_id,
                "turn_id": turn_id,
                "error": error,
            }),
        },
    }
}

fn run_requires_auth_event(spec: &RunTurnSpec, turn_id: &str, error: &str) -> CanonicalEventDraft {
    CanonicalEventDraft {
        event_type: CanonicalEventType::RunRequiresAuth,
        source: CanonicalEventSource::AppServer,
        data: EventDataEnvelope {
            task_id: spec.task_id.clone(),
            worker_id: Some(spec.worker_id.clone()),
            session_id: Some(spec.session_id.clone()),
            run_id: Some(spec.run_id.clone()),
            profile: Some(spec.profile.clone()),
            payload: json!({
                "thread_id": spec.thread_id,
                "turn_id": turn_id,
                "error": error,
            }),
        },
    }
}

fn is_auth_error(turn: &Value) -> bool {
    let Some(message) =
        turn.get("error").and_then(|item| item.get("message")).and_then(Value::as_str)
    else {
        return false;
    };
    let normalized = message.to_ascii_lowercase();
    normalized.contains("auth")
        || normalized.contains("login")
        || normalized.contains("unauthorized")
}

fn window_usage(
    window: &AppRateLimitWindow,
    fallback_minutes: i64,
    now: chrono::DateTime<Local>,
) -> (Option<f64>, Option<i32>, Option<String>) {
    let used = window.used_percent as f64;
    if !(0.0..=100.0).contains(&used) {
        return (None, None, None);
    }
    let remaining_pct = 100.0 - used;
    let window_minutes = window.window_duration_mins.unwrap_or(fallback_minutes);
    let remaining_minutes = window
        .resets_at
        .and_then(|timestamp| Local.timestamp_opt(timestamp, 0).single())
        .filter(|reset_at| *reset_at > now)
        .map(|reset_at| ((reset_at - now).num_seconds() as f64 / 60.0).ceil() as i32)
        .filter(|value| *value > 0)
        .or_else(|| {
            if window_minutes > 0 {
                Some(((window_minutes as f64) * remaining_pct / 100.0).round() as i32)
            } else {
                None
            }
        });
    let reset = window
        .resets_at
        .and_then(|timestamp| Local.timestamp_opt(timestamp, 0).single())
        .map(format_reset_at);
    (Some(remaining_pct), remaining_minutes, reset)
}

fn format_reset_at(time: chrono::DateTime<Local>) -> String {
    time.format("%b %-d, %Y %-I:%M %p").to_string()
}

#[derive(Debug, Deserialize)]
struct AppServerEnvelope {
    id: Value,
    #[serde(default)]
    result: Value,
    error: Option<AppServerError>,
}

#[derive(Debug, Deserialize)]
struct AppServerError {
    message: String,
}

#[derive(Debug, Deserialize)]
struct AppRateLimitsResponse {
    #[serde(rename = "rateLimits")]
    rate_limits: AppRateLimitSnapshot,
}

#[derive(Debug, Deserialize)]
struct AppRateLimitSnapshot {
    primary: Option<AppRateLimitWindow>,
    secondary: Option<AppRateLimitWindow>,
}

#[derive(Debug, Deserialize)]
struct AppRateLimitWindow {
    #[serde(rename = "usedPercent")]
    used_percent: i32,
    #[serde(rename = "windowDurationMins")]
    window_duration_mins: Option<i64>,
    #[serde(rename = "resetsAt")]
    resets_at: Option<i64>,
}

#[derive(Debug, Default, Deserialize)]
struct AppAccountResponse {
    account: Option<AppAccount>,
}

#[derive(Debug, Deserialize)]
struct AppAccount {
    #[serde(rename = "type")]
    account_type: String,
    email: String,
    #[serde(rename = "planType")]
    plan_type: String,
}

#[derive(Debug, Default, Deserialize)]
struct AppConfigResponse {
    #[serde(default)]
    config: AppConfig,
}

#[derive(Debug, Default, Deserialize)]
struct AppConfig {
    model: Option<String>,
    #[serde(rename = "model_reasoning_effort")]
    model_reasoning_effort: Option<String>,
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;
    use std::path::PathBuf;

    use serde_json::json;
    use si_nucleus_core::{ProfileName, SessionId, WorkerId, WorkerStatus};
    use si_nucleus_runtime::{
        NucleusRuntime, RunInputItem, RunTurnSpec, SessionOpenSpec, WorkerLaunchSpec,
    };

    use super::{
        CodexNucleusRuntime, build_app_server_status_input, parse_app_server_status,
        run_started_event,
    };

    #[test]
    fn build_worker_command_shapes_env() {
        let runtime = CodexNucleusRuntime::new();
        let profile = ProfileName::new("america").expect("profile");
        let worker_id = WorkerId::generate();
        let spec = WorkerLaunchSpec {
            worker_id,
            profile,
            home_dir: PathBuf::from("/tmp/home"),
            codex_home: PathBuf::from("/tmp/home/.si/codex/profiles/america"),
            workdir: PathBuf::from("/tmp/work"),
            extra_env: BTreeMap::from([
                ("EXTRA_FLAG".to_owned(), "1".to_owned()),
                ("FORT_TOKEN_PATH".to_owned(), "/tmp/override.token".to_owned()),
            ]),
        };
        let command = runtime.build_worker_command(&spec);
        assert_eq!(command.program, "codex");
        assert_eq!(command.args, vec!["app-server".to_owned()]);
        assert!(!command.args.iter().any(|arg| arg == "exec"));
        assert_eq!(command.args.len(), 1);
        assert_eq!(
            command.env.get("CODEX_HOME").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/america")
        );
        assert_eq!(
            command.env.get("FORT_TOKEN_PATH").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/america/fort/access.token")
        );
        assert_eq!(
            command.env.get("FORT_REFRESH_TOKEN_PATH").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/america/fort/refresh.token")
        );
        assert_eq!(command.env.get("EXTRA_FLAG").map(String::as_str), Some("1"));
    }

    #[test]
    fn probe_events_emit_worker_ready_payload() {
        let runtime = CodexNucleusRuntime::new();
        let profile = ProfileName::new("america").expect("profile");
        let spec = WorkerLaunchSpec {
            worker_id: WorkerId::generate(),
            profile: profile.clone(),
            home_dir: PathBuf::from("/tmp/home"),
            codex_home: PathBuf::from("/tmp/codex-home"),
            workdir: PathBuf::from("/tmp/work"),
            extra_env: BTreeMap::new(),
        };
        let probe = si_nucleus_runtime::WorkerProbeResult {
            status: WorkerStatus::Ready,
            snapshot: si_nucleus_runtime::RuntimeStatusSnapshot {
                source: "app-server".to_owned(),
                model: Some("gpt-5.4".to_owned()),
                reasoning_effort: Some("medium".to_owned()),
                account_email: Some("agent@example.com".to_owned()),
                account_plan: Some("pro".to_owned()),
                five_hour_left_pct: Some(80.0),
                five_hour_reset: Some("Apr 5, 2026 4:00 PM".to_owned()),
                five_hour_remaining_minutes: Some(240),
                weekly_left_pct: Some(90.0),
                weekly_reset: Some("Apr 11, 2026 4:00 PM".to_owned()),
                weekly_remaining_minutes: Some(8000),
            },
            checked_at: chrono::Utc::now(),
        };
        let events = runtime.probe_events(&spec, &probe).expect("events");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, si_nucleus_core::CanonicalEventType::WorkerReady);
        assert_eq!(events[0].data.profile, Some(profile));
        assert_eq!(events[0].data.payload["model"], json!("gpt-5.4"));
    }

    #[test]
    fn status_input_uses_current_protocol_shapes() {
        let payload =
            String::from_utf8(build_app_server_status_input(Some("/tmp/work".to_owned())))
                .expect("utf8");
        assert!(payload.contains("\"account/read\""));
        assert!(payload.contains("\"refreshToken\":false"));
        assert!(payload.contains("\"config/read\""));
        assert!(payload.contains("\"includeLayers\":false"));
    }

    #[test]
    fn parse_status_uses_official_account_and_config_shapes() {
        let raw = [
            json!({"id":1,"result":{"userAgent":"si-nucleus/0.118.0"}}),
            json!({"id":2,"result":{"rateLimits":{"primary":{"usedPercent":20,"windowDurationMins":300,"resetsAt":1775448461},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1775660971}}}}),
            json!({"id":3,"result":{"account":{"type":"chatgpt","email":"agent@example.com","planType":"plus"},"requiresOpenaiAuth":false}}),
            json!({"id":4,"result":{"config":{"model":"gpt-5.4","model_reasoning_effort":"high"}}}),
        ]
        .into_iter()
        .map(|item| serde_json::to_string(&item).expect("line"))
        .collect::<Vec<_>>()
        .join("\n");
        let probe = parse_app_server_status(&raw).expect("probe");
        assert_eq!(probe.snapshot.model.as_deref(), Some("gpt-5.4"));
        assert_eq!(probe.snapshot.account_email.as_deref(), Some("agent@example.com"));
    }

    #[test]
    fn run_started_event_keeps_nucleus_ids_in_payload() {
        let spec = RunTurnSpec {
            run_id: si_nucleus_core::RunId::generate(),
            task_id: Some(si_nucleus_core::TaskId::generate()),
            worker_id: WorkerId::generate(),
            session_id: SessionId::generate(),
            profile: ProfileName::new("america").expect("profile"),
            thread_id: "thread-123".to_owned(),
            timeout_seconds: Some(7200),
            input: vec![RunInputItem::Text { text: "ping".to_owned() }],
        };
        let event = run_started_event(&spec, "turn-123");
        assert_eq!(event.data.run_id, Some(spec.run_id.clone()));
        assert_eq!(event.data.payload["turn_id"], json!("turn-123"));
    }

    #[test]
    fn session_spec_and_run_spec_are_constructible() {
        let _session = SessionOpenSpec {
            session_id: SessionId::generate(),
            worker_id: WorkerId::generate(),
            profile: ProfileName::new("america").expect("profile"),
            workdir: PathBuf::from("/tmp/work"),
            resume_thread_id: Some("thread-123".to_owned()),
        };
    }
}
