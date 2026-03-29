use std::env;
use std::fs;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Stdio};
use std::thread;

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    let root = repo_root()?;
    let fort_root = root.parent().unwrap_or(&root).join("fort");
    let surf_root = root.parent().unwrap_or(&root).join("surf");

    need_cmd("cargo")?;
    need_cmd("docker")?;
    need_cmd("python3")?;
    need_dir(&fort_root)?;
    need_dir(&surf_root)?;

    run_step("installer smoke-host", || {
        run_cargo(
            &root,
            &[
                "run",
                "--quiet",
                "--locked",
                "-p",
                "si-rs-cli",
                "--",
                "build",
                "installer",
                "smokehost",
            ],
        )
    })?;
    run_step("si cli integration", || {
        run_cargo(&root, &["test", "-p", "si-rs-cli", "--test", "cli", "--quiet"])
    })?;
    run_step("si vault package", || run_cargo(&root, &["test", "-p", "si-rs-vault", "--quiet"]))?;
    run_step("fort workspace", || {
        run_command(Command::new("cargo").args([
            "test",
            "--quiet",
            "--manifest-path",
            fort_root.join("Cargo.toml").to_str().unwrap_or_default(),
        ]))
    })?;
    run_step("surf workspace", || {
        run_command(Command::new("cargo").args([
            "test",
            "--workspace",
            "--quiet",
            "--manifest-path",
            surf_root.join("Cargo.toml").to_str().unwrap_or_default(),
        ]))
    })?;
    run_step("si fort live wrapper smoke", || run_fort_wrapper_smoke(&root))?;
    run_step("si dyad lifecycle smoke", || run_dyad_smoke(&root))?;

    println!("\n==> rust host matrix: ok");
    Ok(())
}

fn repo_root() -> Result<PathBuf, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("Cargo.toml").is_file() && cwd.join("rust").join("crates").join("si-cli").is_dir() {
        Ok(cwd)
    } else {
        Err("repo root not found. Run this command from the si workspace root.".to_owned())
    }
}

fn need_cmd(name: &str) -> Result<(), String> {
    let status = Command::new("sh")
        .args(["-lc", &format!("command -v {name} >/dev/null 2>&1")])
        .status()
        .map_err(|err| err.to_string())?;
    if status.success() { Ok(()) } else { Err(format!("missing required command: {name}")) }
}

fn need_dir(path: &Path) -> Result<(), String> {
    if path.is_dir() { Ok(()) } else { Err(format!("missing required repo: {}", path.display())) }
}

fn run_step<F>(name: &str, f: F) -> Result<(), String>
where
    F: FnOnce() -> Result<(), String>,
{
    println!("\n==> {name}");
    f()
}

fn run_cargo(root: &Path, args: &[&str]) -> Result<(), String> {
    run_command(Command::new("cargo").current_dir(root).args(args))
}

fn run_command(command: &mut Command) -> Result<(), String> {
    let status = command.status().map_err(|err| err.to_string())?;
    if status.success() { Ok(()) } else { Err(format!("command failed: {status}")) }
}

fn run_output(command: &mut Command) -> Result<String, String> {
    let output = command.output().map_err(|err| err.to_string())?;
    if !output.status.success() {
        return Err(format!("command failed: {}", output.status));
    }
    String::from_utf8(output.stdout).map_err(|err| err.to_string())
}

fn run_fort_wrapper_smoke(root: &Path) -> Result<(), String> {
    let listener = TcpListener::bind(("127.0.0.1", 0)).map_err(|err| err.to_string())?;
    let port = listener.local_addr().map_err(|err| err.to_string())?.port();
    let server = thread::spawn(move || {
        if let Ok((mut stream, _)) = listener.accept() {
            let mut buffer = [0_u8; 2048];
            let _ = stream.read(&mut buffer);
            let req = String::from_utf8_lossy(&buffer);
            let path =
                req.lines().next().and_then(|line| line.split_whitespace().nth(1)).unwrap_or("/");
            let body = match path {
                "/v1/health" => r#"{"status":"ok"}"#,
                "/v1/ready" => r#"{"status":"ready"}"#,
                "/v1/whoami" => r#"{"actor":"matrix"}"#,
                _ => r#"{"error":"not found"}"#,
            };
            let status = if matches!(path, "/v1/health" | "/v1/ready" | "/v1/whoami") {
                "200 OK"
            } else {
                "404 Not Found"
            };
            let response = format!(
                "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            let _ = stream.write_all(response.as_bytes());
        }
    });

    let home = tempfile::tempdir().map_err(|err| err.to_string())?;
    let output = run_output(
        Command::new("cargo")
            .current_dir(root)
            .env("FORT_HOST", format!("http://127.0.0.1:{port}"))
            .args([
                "run",
                "--quiet",
                "--bin",
                "si-rs",
                "--",
                "fort",
                "--home",
                home.path().to_str().unwrap_or_default(),
                "--",
                "--json",
                "doctor",
            ]),
    )?;
    println!("{output}");
    let payload: serde_json::Value =
        serde_json::from_str(&output).map_err(|err| err.to_string())?;
    if payload["health_status"] != 200 || payload["ready_status"] != 200 {
        return Err(format!("unexpected fort payload: {payload}"));
    }
    server.join().map_err(|_| "fort mock server panicked".to_owned())?;
    Ok(())
}

fn run_dyad_smoke(root: &Path) -> Result<(), String> {
    let tmpdir = tempfile::tempdir().map_err(|err| err.to_string())?;
    let workspace = tmpdir.path().join("workspace");
    let configs = tmpdir.path().join("configs");
    let home_dir = tmpdir.path().join("home");
    let script_dir = tmpdir.path().join("bin");
    let state = tmpdir.path().join("state.txt");
    let docker_bin = script_dir.join("docker");
    fs::create_dir_all(&workspace).map_err(|err| err.to_string())?;
    fs::create_dir_all(&configs).map_err(|err| err.to_string())?;
    fs::create_dir_all(home_dir.join(".si")).map_err(|err| err.to_string())?;
    fs::create_dir_all(&script_dir).map_err(|err| err.to_string())?;
    let script = format!(
        "#!/bin/sh\nSTATE_FILE=\"{}\"\ncmd=\"$1\"\nshift\ncase \"$cmd\" in\n  run)\n    printf '%s\\n' 'running' > \"$STATE_FILE\"\n    printf '%s\\n' 'container-id'\n    ;;\n  ps)\n    state='missing'\n    if [ -f \"$STATE_FILE\" ]; then state=$(tr -d '\\n' < \"$STATE_FILE\"); fi\n    if [ \"$state\" = 'removed' ] || [ \"$state\" = 'missing' ]; then exit 0; fi\n    printf 'si-actor-alpha\\t%s\\tactor-id\\talpha\\tios\\tactor\\n' \"$state\"\n    printf 'si-critic-alpha\\t%s\\tcritic-id\\talpha\\tios\\tcritic\\n' \"$state\"\n    ;;\n  logs)\n    printf '%s\\n' 'critic logs'\n    ;;\n  start)\n    printf '%s\\n' 'running' > \"$STATE_FILE\"\n    printf '%s\\n' 'started'\n    ;;\n  stop)\n    printf '%s\\n' 'exited' > \"$STATE_FILE\"\n    printf '%s\\n' 'stopped'\n    ;;\n  rm)\n    printf '%s\\n' 'removed' > \"$STATE_FILE\"\n    printf '%s\\n' 'removed'\n    ;;\n  exec)\n    printf '%s\\n' 'exec-ok'\n    ;;\n  *)\n    printf 'unexpected docker command: %s\\n' \"$cmd\" >&2\n    exit 1\n    ;;\nesac\n",
        state.display()
    );
    fs::write(&docker_bin, script).map_err(|err| err.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&docker_bin).map_err(|err| err.to_string())?.permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&docker_bin, perms).map_err(|err| err.to_string())?;
    }

    run_cargo(
        root,
        &[
            "run",
            "--quiet",
            "--bin",
            "si-rs",
            "--",
            "dyad",
            "spawn",
            "start",
            "--name",
            "alpha",
            "--workspace",
            workspace.to_str().unwrap_or_default(),
            "--configs",
            configs.to_str().unwrap_or_default(),
            "--home",
            home_dir.to_str().unwrap_or_default(),
            "--docker-bin",
            docker_bin.to_str().unwrap_or_default(),
        ],
    )?;

    let status = run_output(Command::new("cargo").current_dir(root).args([
        "run",
        "--quiet",
        "--bin",
        "si-rs",
        "--",
        "dyad",
        "status",
        "alpha",
        "--format",
        "json",
        "--docker-bin",
        docker_bin.to_str().unwrap_or_default(),
    ]))?;
    println!("{status}");
    let payload: serde_json::Value =
        serde_json::from_str(&status).map_err(|err| err.to_string())?;
    if payload["found"] != true
        || payload["actor"]["status"] != "running"
        || payload["critic"]["status"] != "running"
    {
        return Err(format!("unexpected dyad status: {payload}"));
    }

    run_cargo(
        root,
        &[
            "run",
            "--quiet",
            "--bin",
            "si-rs",
            "--",
            "dyad",
            "logs",
            "alpha",
            "--member",
            "critic",
            "--tail",
            "10",
            "--docker-bin",
            docker_bin.to_str().unwrap_or_default(),
        ],
    )?;
    run_command(
        Command::new("cargo")
            .current_dir(root)
            .args([
                "run",
                "--quiet",
                "--bin",
                "si-rs",
                "--",
                "dyad",
                "exec",
                "alpha",
                "--member",
                "critic",
                "--tty=true",
                "--docker-bin",
                docker_bin.to_str().unwrap_or_default(),
                "--",
                "bash",
                "-lc",
                "echo hi",
            ])
            .stdout(Stdio::null())
            .stderr(Stdio::null()),
    )?;
    run_cargo(
        root,
        &[
            "run",
            "--quiet",
            "--bin",
            "si-rs",
            "--",
            "dyad",
            "stop",
            "alpha",
            "--docker-bin",
            docker_bin.to_str().unwrap_or_default(),
        ],
    )?;

    let stopped = run_output(Command::new("cargo").current_dir(root).args([
        "run",
        "--quiet",
        "--bin",
        "si-rs",
        "--",
        "dyad",
        "status",
        "alpha",
        "--format",
        "json",
        "--docker-bin",
        docker_bin.to_str().unwrap_or_default(),
    ]))?;
    let stopped_payload: serde_json::Value =
        serde_json::from_str(&stopped).map_err(|err| err.to_string())?;
    if stopped_payload["actor"]["status"] != "exited"
        || stopped_payload["critic"]["status"] != "exited"
    {
        return Err(format!("unexpected stopped dyad status: {stopped_payload}"));
    }

    run_cargo(
        root,
        &[
            "run",
            "--quiet",
            "--bin",
            "si-rs",
            "--",
            "dyad",
            "start",
            "alpha",
            "--docker-bin",
            docker_bin.to_str().unwrap_or_default(),
        ],
    )?;
    run_cargo(
        root,
        &[
            "run",
            "--quiet",
            "--bin",
            "si-rs",
            "--",
            "dyad",
            "remove",
            "alpha",
            "--docker-bin",
            docker_bin.to_str().unwrap_or_default(),
        ],
    )?;

    let final_status = run_output(Command::new("cargo").current_dir(root).args([
        "run",
        "--quiet",
        "--bin",
        "si-rs",
        "--",
        "dyad",
        "status",
        "alpha",
        "--format",
        "json",
        "--docker-bin",
        docker_bin.to_str().unwrap_or_default(),
    ]))?;
    println!("{final_status}");
    let final_payload: serde_json::Value =
        serde_json::from_str(&final_status).map_err(|err| err.to_string())?;
    if final_payload["found"] != false {
        return Err(format!("unexpected final dyad status: {final_payload}"));
    }

    Ok(())
}
