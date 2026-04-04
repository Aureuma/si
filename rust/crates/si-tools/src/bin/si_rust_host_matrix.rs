use std::env;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode};
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
