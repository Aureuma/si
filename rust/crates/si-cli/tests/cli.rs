use assert_cmd::Command;
use base64::{
    Engine as _,
    engine::general_purpose::{STANDARD as BASE64_STANDARD, URL_SAFE_NO_PAD},
};
use chrono::Utc;
use reqwest::blocking::Client as BlockingClient;
use serde_json::{Value, json};
use si_nucleus::{NucleusConfig, NucleusService};
use si_nucleus_core::{
    CanonicalEventSource, CanonicalEventType, EventDataEnvelope, RunStatus, WorkerId, WorkerStatus,
};
use si_nucleus_runtime::{
    CanonicalEventDraft, NucleusRuntime, RunTurnSpec, RuntimeCommand, RuntimeRunOutcome,
    RuntimeStatusSnapshot, SessionOpenResult, SessionOpenSpec, WorkerLaunchSpec, WorkerProbeResult,
    WorkerRuntimeView, WorkerStartResult,
};
use si_rs_fort::{PersistedSessionState, save_persisted_session_state};
use std::collections::{BTreeMap, HashMap, HashSet};
use std::fs;
use std::fs::File;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::Path;
use std::process::{Command as ProcessCommand, Stdio};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};
use tar::Archive;
use tempfile::tempdir;
use tungstenite::handshake::server::{Request as WsRequest, Response as WsResponse};
use tungstenite::http::Response as HttpResponse;
use tungstenite::stream::MaybeTlsStream;
use tungstenite::{Message as WsMessage, WebSocket, accept_hdr, connect};

fn cargo_bin() -> Command {
    Command::cargo_bin("si-rs").expect("si-rs binary should build")
}

#[allow(clippy::result_large_err)]
fn accept_test_ws_response(
    response: WsResponse,
) -> Result<WsResponse, HttpResponse<Option<String>>> {
    Ok(response)
}

fn expected_version_line() -> String {
    format!("v{}\n", env!("CARGO_PKG_VERSION"))
}

fn write_named_codex_profile_settings(
    home: &Path,
    active_profile: &str,
    profiles: &[(&str, &str, &str)],
) {
    let settings_dir = home.join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let mut source = String::from("schema_version = 1\n[codex]\n");
    source.push_str(&format!("profile = {:?}\n\n", active_profile));
    source.push_str("[codex.profiles]\n");
    source.push_str(&format!("active = {:?}\n\n", active_profile));
    for (profile, name, email) in profiles {
        source.push_str(&format!("[codex.profiles.entries.{profile}]\n"));
        source.push_str(&format!("name = \"{}\"\n", name));
        source.push_str(&format!("email = \"{}\"\n", email));
        source.push_str(&format!(
            "auth_path = {:?}\n\n",
            home.join(".si").join("codex").join("profiles").join(profile).join("auth.json")
        ));
    }
    fs::write(settings_dir.join("settings.toml"), source).expect("write named codex settings");
}

fn write_workspace_manifest(repo: &Path, version: &str) {
    fs::create_dir_all(repo.join("rust/crates/si-cli")).expect("mkdir cli crate");
    fs::write(
        repo.join("Cargo.toml"),
        format!(
            "[workspace]\nmembers = [\"rust/crates/si-cli\"]\nresolver = \"2\"\n\n[workspace.package]\nversion = \"{}\"\nedition = \"2024\"\nlicense = \"AGPL-3.0-only\"\nrepository = \"https://example.invalid/si\"\nrust-version = \"1.88\"\n",
            version.trim_start_matches('v')
        ),
    )
    .expect("write Cargo.toml");
}

fn spawn_single_response_server(status: &str, body: &str) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test listener");
    let addr = listener.local_addr().expect("listener addr");
    let status = status.to_owned();
    let body = body.to_owned();
    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept test connection");
        let mut buffer = [0_u8; 4096];
        let _ = stream.read(&mut buffer);
        let response = format!(
            "HTTP/1.1 {status}\r\nContent-Length: {}\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n{body}",
            body.len()
        );
        stream.write_all(response.as_bytes()).expect("write test response");
    });
    format!("http://{}", addr)
}

fn spawn_live_nucleus_service(state_dir: &Path) -> String {
    spawn_live_nucleus_service_with_options(state_dir, "127.0.0.1", "127.0.0.1", None, None)
}

fn spawn_live_nucleus_service_with_runtime(
    state_dir: &Path,
    runtime: Arc<dyn NucleusRuntime>,
) -> String {
    spawn_live_nucleus_service_with_options(
        state_dir,
        "127.0.0.1",
        "127.0.0.1",
        None,
        Some(runtime),
    )
}

fn spawn_live_nucleus_service_with_options(
    state_dir: &Path,
    bind_host: &str,
    client_host: &str,
    auth_token: Option<&str>,
    runtime: Option<Arc<dyn NucleusRuntime>>,
) -> String {
    let state_dir = state_dir.to_path_buf();
    let auth_token = auth_token.map(str::to_owned);
    let client = BlockingClient::builder()
        .timeout(Duration::from_millis(250))
        .build()
        .expect("build reqwest client");

    for attempt in 0..10 {
        let listener = TcpListener::bind(format!("{bind_host}:0")).expect("bind nucleus addr");
        let addr = listener.local_addr().expect("nucleus addr");
        drop(listener);

        let state_dir = state_dir.clone();
        let auth_token = auth_token.clone();
        let runtime = runtime.clone();
        thread::spawn(move || {
            let tokio_runtime = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .expect("tokio runtime");
            tokio_runtime.block_on(async move {
                let config = NucleusConfig { bind_addr: addr, state_dir, auth_token };
                let service = match runtime {
                    Some(runtime) => NucleusService::open_with_runtime(config, runtime),
                    None => NucleusService::open(config),
                }
                .expect("open nucleus service");
                service.serve().await.expect("serve nucleus service");
            });
        });

        let base_url = format!("http://{client_host}:{}", addr.port());
        for _ in 0..50 {
            if let Ok(response) = client.get(format!("{base_url}/status")).send()
                && response.status().is_success()
            {
                return base_url;
            }
            thread::sleep(Duration::from_millis(50));
        }

        eprintln!(
            "retrying nucleus test service startup after timeout on {base_url} (attempt {} of 10)",
            attempt + 1
        );
    }
    panic!("nucleus service did not become ready after 10 attempts");
}

#[derive(Clone)]
struct TestRuntimeConfig {
    run_delay: Duration,
    step_delay: Duration,
    output_deltas: Vec<String>,
    fail_execute: bool,
    fail_execute_prompts: Vec<String>,
    block_when_worker_missing: bool,
    fail_start_worker: bool,
    fail_ensure_session: bool,
}

impl Default for TestRuntimeConfig {
    fn default() -> Self {
        Self {
            run_delay: Duration::from_millis(0),
            step_delay: Duration::from_millis(0),
            output_deltas: vec!["nucleus-smoke".to_owned()],
            fail_execute: false,
            fail_execute_prompts: Vec::new(),
            block_when_worker_missing: false,
            fail_start_worker: false,
            fail_ensure_session: false,
        }
    }
}

#[derive(Default)]
struct TestRuntimeState {
    workers: HashMap<String, WorkerRuntimeView>,
    interrupted_turns: HashSet<String>,
    start_calls: usize,
    config: TestRuntimeConfig,
}

#[derive(Clone)]
struct TestRuntime {
    state: Arc<Mutex<TestRuntimeState>>,
}

impl Default for TestRuntime {
    fn default() -> Self {
        Self { state: Arc::new(Mutex::new(TestRuntimeState::default())) }
    }
}

impl TestRuntime {
    fn with_config(config: TestRuntimeConfig) -> Self {
        Self {
            state: Arc::new(Mutex::new(TestRuntimeState {
                workers: HashMap::new(),
                interrupted_turns: HashSet::new(),
                start_calls: 0,
                config,
            })),
        }
    }

    fn with_streaming_output(
        run_delay: Duration,
        step_delay: Duration,
        output_deltas: &[&str],
    ) -> Self {
        Self::with_config(TestRuntimeConfig {
            run_delay,
            step_delay,
            output_deltas: output_deltas.iter().map(|value| (*value).to_owned()).collect(),
            fail_execute: false,
            fail_execute_prompts: Vec::new(),
            block_when_worker_missing: false,
            fail_start_worker: false,
            fail_ensure_session: false,
        })
    }

    fn wait_for_interrupt_or_timeout(&self, turn_id: &str, delay: Duration) -> bool {
        if delay.is_zero() {
            return self.state.lock().expect("runtime state").interrupted_turns.remove(turn_id);
        }
        let start = Instant::now();
        while start.elapsed() < delay {
            if self.state.lock().expect("runtime state").interrupted_turns.remove(turn_id) {
                return true;
            }
            thread::sleep(Duration::from_millis(20));
        }
        false
    }

    fn worker_is_missing(&self, worker_id: &WorkerId) -> bool {
        !self.state.lock().expect("runtime state").workers.contains_key(worker_id.as_str())
    }

    fn set_fail_start_worker(&self, value: bool) {
        self.state.lock().expect("runtime state").config.fail_start_worker = value;
    }

    fn set_fail_ensure_session(&self, value: bool) {
        self.state.lock().expect("runtime state").config.fail_ensure_session = value;
    }

    fn start_call_count(&self) -> usize {
        self.state.lock().expect("runtime state").start_calls
    }

    fn block_run_for_missing_worker(
        &self,
        spec: &RunTurnSpec,
        turn_id: &str,
        block_when_worker_missing: bool,
        on_event: &mut dyn FnMut(CanonicalEventDraft) -> anyhow::Result<()>,
    ) -> anyhow::Result<Option<RuntimeRunOutcome>> {
        if !block_when_worker_missing || !self.worker_is_missing(&spec.worker_id) {
            return Ok(None);
        }
        on_event(CanonicalEventDraft {
            event_type: CanonicalEventType::RunBlocked,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: spec.task_id.clone(),
                worker_id: Some(spec.worker_id.clone()),
                session_id: Some(spec.session_id.clone()),
                run_id: Some(spec.run_id.clone()),
                profile: Some(spec.profile.clone()),
                payload: serde_json::json!({
                    "thread_id": spec.thread_id,
                    "turn_id": turn_id,
                    "blocked_reason": "worker_unavailable",
                    "error": "worker process is not attached to the runtime",
                }),
            },
        })?;
        Ok(Some(RuntimeRunOutcome {
            turn_id: turn_id.to_owned(),
            status: RunStatus::Blocked,
            completed_at: Utc::now(),
            final_output: None,
        }))
    }
}

impl NucleusRuntime for TestRuntime {
    fn runtime_name(&self) -> &'static str {
        "cli-test-runtime"
    }

    fn build_worker_command(&self, spec: &WorkerLaunchSpec) -> RuntimeCommand {
        RuntimeCommand {
            program: "cli-test-runtime".to_owned(),
            args: vec![spec.profile.to_string()],
            current_dir: spec.workdir.clone(),
            env: BTreeMap::new(),
        }
    }

    fn probe_worker(&self, spec: &WorkerLaunchSpec) -> anyhow::Result<WorkerProbeResult> {
        Ok(WorkerProbeResult {
            status: WorkerStatus::Ready,
            snapshot: RuntimeStatusSnapshot {
                source: "cli-test-runtime".to_owned(),
                model: Some("gpt-5.4".to_owned()),
                reasoning_effort: Some("medium".to_owned()),
                account_email: Some(format!("{}@example.com", spec.profile)),
                account_plan: Some("pro".to_owned()),
                five_hour_left_pct: Some(80.0),
                five_hour_reset: Some("Apr 6, 2026 4:00 PM".to_owned()),
                five_hour_remaining_minutes: Some(240),
                weekly_left_pct: Some(90.0),
                weekly_reset: Some("Apr 13, 2026 4:00 PM".to_owned()),
                weekly_remaining_minutes: Some(9000),
            },
            checked_at: Utc::now(),
        })
    }

    fn start_worker(&self, spec: &WorkerLaunchSpec) -> anyhow::Result<WorkerStartResult> {
        {
            let mut state = self.state.lock().expect("runtime state");
            state.start_calls += 1;
            if state.config.fail_start_worker {
                anyhow::bail!("cli-test-runtime start_worker failed");
            }
        }
        let probe = self.probe_worker(spec)?;
        let runtime = WorkerRuntimeView {
            worker_id: spec.worker_id.clone(),
            runtime_name: "cli-test-runtime".to_owned(),
            pid: 4242,
            started_at: Utc::now(),
            checked_at: probe.checked_at,
        };
        self.state
            .lock()
            .expect("runtime state")
            .workers
            .insert(spec.worker_id.to_string(), runtime.clone());
        Ok(WorkerStartResult { runtime, probe })
    }

    fn stop_worker(&self, worker_id: &WorkerId) -> anyhow::Result<()> {
        self.state.lock().expect("runtime state").workers.remove(worker_id.as_str());
        Ok(())
    }

    fn inspect_worker(&self, worker_id: &WorkerId) -> anyhow::Result<Option<WorkerRuntimeView>> {
        Ok(self.state.lock().expect("runtime state").workers.get(worker_id.as_str()).cloned())
    }

    fn ensure_session(&self, spec: &SessionOpenSpec) -> anyhow::Result<SessionOpenResult> {
        if self.state.lock().expect("runtime state").config.fail_ensure_session {
            anyhow::bail!("cli-test-runtime ensure_session failed");
        }
        Ok(SessionOpenResult {
            thread_id: spec
                .resume_thread_id
                .clone()
                .unwrap_or_else(|| format!("thread-{}", spec.session_id)),
            created: spec.resume_thread_id.is_none(),
            opened_at: Utc::now(),
        })
    }

    fn execute_turn(
        &self,
        spec: &RunTurnSpec,
        on_event: &mut dyn FnMut(CanonicalEventDraft) -> anyhow::Result<()>,
    ) -> anyhow::Result<RuntimeRunOutcome> {
        let (
            run_delay,
            step_delay,
            output_deltas,
            fail_execute,
            fail_execute_prompts,
            block_when_worker_missing,
        ) = {
            let state = self.state.lock().expect("runtime state");
            (
                state.config.run_delay,
                state.config.step_delay,
                state.config.output_deltas.clone(),
                state.config.fail_execute,
                state.config.fail_execute_prompts.clone(),
                state.config.block_when_worker_missing,
            )
        };
        let input_text = spec
            .input
            .iter()
            .find_map(|item| match item {
                si_nucleus_runtime::RunInputItem::Text { text } => Some(text.as_str()),
            })
            .unwrap_or_default();
        if fail_execute || fail_execute_prompts.iter().any(|candidate| candidate == input_text) {
            anyhow::bail!("cli-test-runtime execute_turn failed before run.started");
        }
        let turn_id = format!("turn-{}", spec.run_id);
        on_event(CanonicalEventDraft {
            event_type: CanonicalEventType::RunStarted,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: spec.task_id.clone(),
                worker_id: Some(spec.worker_id.clone()),
                session_id: Some(spec.session_id.clone()),
                run_id: Some(spec.run_id.clone()),
                profile: Some(spec.profile.clone()),
                payload: serde_json::json!({
                    "thread_id": spec.thread_id,
                    "turn_id": turn_id,
                }),
            },
        })?;
        if let Some(outcome) =
            self.block_run_for_missing_worker(spec, &turn_id, block_when_worker_missing, on_event)?
        {
            return Ok(outcome);
        }
        if self.wait_for_interrupt_or_timeout(&turn_id, run_delay) {
            on_event(CanonicalEventDraft {
                event_type: CanonicalEventType::RunCancelled,
                source: CanonicalEventSource::System,
                data: EventDataEnvelope {
                    task_id: spec.task_id.clone(),
                    worker_id: Some(spec.worker_id.clone()),
                    session_id: Some(spec.session_id.clone()),
                    run_id: Some(spec.run_id.clone()),
                    profile: Some(spec.profile.clone()),
                    payload: serde_json::json!({
                        "thread_id": spec.thread_id,
                        "turn_id": turn_id,
                        "error": "interrupted",
                    }),
                },
            })?;
            return Ok(RuntimeRunOutcome {
                turn_id,
                status: RunStatus::Cancelled,
                completed_at: Utc::now(),
                final_output: None,
            });
        }
        if let Some(outcome) =
            self.block_run_for_missing_worker(spec, &turn_id, block_when_worker_missing, on_event)?
        {
            return Ok(outcome);
        }
        let final_output = output_deltas.join("");
        for delta in &output_deltas {
            if self.wait_for_interrupt_or_timeout(&turn_id, step_delay) {
                on_event(CanonicalEventDraft {
                    event_type: CanonicalEventType::RunCancelled,
                    source: CanonicalEventSource::System,
                    data: EventDataEnvelope {
                        task_id: spec.task_id.clone(),
                        worker_id: Some(spec.worker_id.clone()),
                        session_id: Some(spec.session_id.clone()),
                        run_id: Some(spec.run_id.clone()),
                        profile: Some(spec.profile.clone()),
                        payload: serde_json::json!({
                            "thread_id": spec.thread_id,
                            "turn_id": turn_id,
                            "error": "interrupted",
                        }),
                    },
                })?;
                return Ok(RuntimeRunOutcome {
                    turn_id,
                    status: RunStatus::Cancelled,
                    completed_at: Utc::now(),
                    final_output: None,
                });
            }
            if let Some(outcome) = self.block_run_for_missing_worker(
                spec,
                &turn_id,
                block_when_worker_missing,
                on_event,
            )? {
                return Ok(outcome);
            }
            on_event(CanonicalEventDraft {
                event_type: CanonicalEventType::RunOutputDelta,
                source: CanonicalEventSource::System,
                data: EventDataEnvelope {
                    task_id: spec.task_id.clone(),
                    worker_id: Some(spec.worker_id.clone()),
                    session_id: Some(spec.session_id.clone()),
                    run_id: Some(spec.run_id.clone()),
                    profile: Some(spec.profile.clone()),
                    payload: serde_json::json!({
                        "thread_id": spec.thread_id,
                        "turn_id": turn_id,
                        "delta": delta,
                    }),
                },
            })?;
        }
        on_event(CanonicalEventDraft {
            event_type: CanonicalEventType::RunCompleted,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: spec.task_id.clone(),
                worker_id: Some(spec.worker_id.clone()),
                session_id: Some(spec.session_id.clone()),
                run_id: Some(spec.run_id.clone()),
                profile: Some(spec.profile.clone()),
                payload: serde_json::json!({
                    "thread_id": spec.thread_id,
                    "turn_id": turn_id,
                    "final_output": final_output,
                }),
            },
        })?;
        Ok(RuntimeRunOutcome {
            turn_id,
            status: RunStatus::Completed,
            completed_at: Utc::now(),
            final_output: Some(final_output),
        })
    }

    fn interrupt_turn(
        &self,
        _worker_id: &WorkerId,
        _thread_id: &str,
        turn_id: &str,
    ) -> anyhow::Result<()> {
        self.state.lock().expect("runtime state").interrupted_turns.insert(turn_id.to_owned());
        Ok(())
    }

    fn probe_events(
        &self,
        spec: &WorkerLaunchSpec,
        probe: &WorkerProbeResult,
    ) -> anyhow::Result<Vec<CanonicalEventDraft>> {
        Ok(vec![CanonicalEventDraft {
            event_type: CanonicalEventType::WorkerReady,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: None,
                worker_id: Some(spec.worker_id.clone()),
                session_id: None,
                run_id: None,
                profile: Some(spec.profile.clone()),
                payload: serde_json::json!({
                    "source": probe.snapshot.source,
                    "model": probe.snapshot.model,
                }),
            },
        }])
    }

    fn status_payload(&self, probe: &WorkerProbeResult) -> serde_json::Value {
        serde_json::json!({
            "source": probe.snapshot.source,
            "model": probe.snapshot.model,
            "account_email": probe.snapshot.account_email,
        })
    }
}

fn write_cron_rule(
    state_root: &Path,
    name: &str,
    schedule_kind: &str,
    schedule: &str,
    instructions: &str,
    next_due_at: &str,
) {
    let path = state_root.join("state").join("producers").join("cron").join(format!("{name}.json"));
    fs::create_dir_all(path.parent().expect("cron parent")).expect("mkdir cron dir");
    fs::write(
        path,
        serde_json::to_vec_pretty(&serde_json::json!({
            "name": name,
            "enabled": true,
            "schedule_kind": schedule_kind,
            "schedule": schedule,
            "instructions": instructions,
            "last_emitted_at": null,
            "next_due_at": next_due_at,
            "version": 1
        }))
        .expect("serialize cron rule"),
    )
    .expect("write cron rule");
}

fn write_hook_rule(state_root: &Path, name: &str, match_event_type: &str, instructions: &str) {
    let path = state_root.join("state").join("producers").join("hook").join(format!("{name}.json"));
    fs::create_dir_all(path.parent().expect("hook parent")).expect("mkdir hook dir");
    fs::write(
        path,
        serde_json::to_vec_pretty(&serde_json::json!({
            "name": name,
            "enabled": true,
            "match_event_type": match_event_type,
            "instructions": instructions,
            "last_processed_event_seq": 0,
            "version": 1
        }))
        .expect("serialize hook rule"),
    )
    .expect("write hook rule");
}

fn inspect_task_over_websocket(ws_url: &str, task_id: &str) -> Value {
    let (mut socket, _) = connect(ws_url).expect("connect websocket");
    let inspect_request = serde_json::json!({
        "id": "task-inspect",
        "method": "task.inspect",
        "params": { "task_id": task_id }
    });
    socket
        .send(WsMessage::Text(inspect_request.to_string().into()))
        .expect("send websocket inspect");
    let response = socket.read().expect("read websocket inspect");
    match response {
        WsMessage::Text(text) => {
            let payload = serde_json::from_str::<Value>(&text).expect("parse websocket inspect");
            assert_eq!(payload["ok"], true);
            payload["result"].clone()
        }
        other => panic!("unexpected websocket response: {other:?}"),
    }
}

fn wait_for_cli_task(ws_url: &str, timeout: Duration, predicate: impl Fn(&Value) -> bool) -> Value {
    let start = Instant::now();
    while start.elapsed() < timeout {
        let list_output = cargo_bin()
            .args(["nucleus", "task", "list", "--endpoint", ws_url, "--format", "json"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone();
        let tasks: Value = serde_json::from_slice(&list_output).expect("parse task list");
        if let Some(task) =
            tasks.as_array().expect("task list array").iter().find(|task| predicate(task))
        {
            return task.clone();
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("task did not appear before timeout");
}

fn wait_for_cli_task_status(ws_url: &str, task_id: &str, expected: &str) -> Value {
    let start = Instant::now();
    while start.elapsed() < Duration::from_secs(5) {
        let output = cargo_bin()
            .args(["nucleus", "task", "inspect", task_id, "--endpoint", ws_url, "--format", "json"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone();
        let task: Value = serde_json::from_slice(&output).expect("parse task inspect");
        if task["status"] == expected {
            return task;
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("task {task_id} did not reach status {expected}");
}

fn wait_for_cli_task_predicate(
    ws_url: &str,
    task_id: &str,
    timeout: Duration,
    predicate: impl Fn(&Value) -> bool,
) -> Value {
    let start = Instant::now();
    while start.elapsed() < timeout {
        let output = cargo_bin()
            .args(["nucleus", "task", "inspect", task_id, "--endpoint", ws_url, "--format", "json"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone();
        let task: Value = serde_json::from_slice(&output).expect("parse task inspect");
        if predicate(&task) {
            return task;
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("task {task_id} did not satisfy predicate");
}

fn inspect_run_via_cli(ws_url: &str, run_id: &str) -> Value {
    let output = cargo_bin()
        .args(["nucleus", "run", "inspect", run_id, "--endpoint", ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    serde_json::from_slice(&output).expect("parse run inspect")
}

fn create_session_via_cli_with_options(
    ws_url: &str,
    profile: &str,
    worker_id: Option<&str>,
    thread_id: Option<&str>,
    home_dir: &Path,
    codex_home: &Path,
    workdir: &Path,
) -> Value {
    let mut command = cargo_bin();
    command.args([
        "nucleus",
        "session",
        "create",
        profile,
        "--home-dir",
        home_dir.to_str().expect("home dir"),
        "--codex-home",
        codex_home.to_str().expect("codex home"),
        "--workdir",
        workdir.to_str().expect("workdir"),
        "--endpoint",
        ws_url,
        "--format",
        "json",
    ]);
    if let Some(worker_id) = worker_id {
        command.args(["--worker-id", worker_id]);
    }
    if let Some(thread_id) = thread_id {
        command.args(["--thread-id", thread_id]);
    }
    let output = command.assert().success().get_output().stdout.clone();
    serde_json::from_slice(&output).expect("parse session create")
}

fn inspect_session_via_cli(ws_url: &str, session_id: &str) -> Value {
    let output = cargo_bin()
        .args(["nucleus", "session", "show", session_id, "--endpoint", ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    serde_json::from_slice(&output).expect("parse session inspect")
}

fn inspect_worker_via_cli(ws_url: &str, worker_id: &str) -> Value {
    let output = cargo_bin()
        .args(["nucleus", "worker", "inspect", worker_id, "--endpoint", ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    serde_json::from_slice(&output).expect("parse worker inspect")
}

fn wait_for_worker_status(ws_url: &str, worker_id: &str, expected: &str) -> Value {
    let start = Instant::now();
    while start.elapsed() < Duration::from_secs(5) {
        let worker = inspect_worker_via_cli(ws_url, worker_id);
        if worker["worker"]["status"] == expected {
            return worker;
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("worker {worker_id} did not reach status {expected}");
}

fn create_task_via_cli(ws_url: &str, title: &str, instructions: &str, profile: &str) -> Value {
    let output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "create",
            title,
            instructions,
            "--profile",
            profile,
            "--endpoint",
            ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    serde_json::from_slice(&output).expect("parse cli create output")
}

fn subscribe_to_events(ws_url: &str) -> WebSocket<MaybeTlsStream<TcpStream>> {
    let (mut socket, _) = connect(ws_url).expect("connect websocket");
    let subscribe_request = serde_json::json!({
        "id": "events-subscribe",
        "method": "events.subscribe",
        "params": {}
    });
    socket
        .send(WsMessage::Text(subscribe_request.to_string().into()))
        .expect("send subscribe request");
    let response = socket.read().expect("read subscribe response");
    match response {
        WsMessage::Text(text) => {
            let payload = serde_json::from_str::<Value>(&text).expect("parse subscribe response");
            assert_eq!(payload["ok"], true);
            assert_eq!(payload["result"]["subscribed"], true);
        }
        other => panic!("unexpected subscribe response: {other:?}"),
    }
    socket
}

fn read_websocket_json(socket: &mut WebSocket<MaybeTlsStream<TcpStream>>) -> Value {
    match socket.read().expect("read websocket message") {
        WsMessage::Text(text) => serde_json::from_str(&text).expect("parse websocket json"),
        other => panic!("unexpected websocket message: {other:?}"),
    }
}

fn create_task_over_websocket(
    ws_url: &str,
    request_id: &str,
    title: &str,
    instructions: &str,
    profile: &str,
    session_id: Option<&str>,
) -> Value {
    let (mut socket, _) = connect(ws_url).expect("connect websocket");
    let mut params = serde_json::json!({
        "title": title,
        "instructions": instructions,
        "profile": profile,
    });
    if let Some(session_id) = session_id {
        params["session_id"] = Value::String(session_id.to_owned());
    }
    let request = serde_json::json!({
        "id": request_id,
        "method": "task.create",
        "params": params,
    });
    socket.send(WsMessage::Text(request.to_string().into())).expect("send websocket task create");
    match socket.read().expect("read websocket task create") {
        WsMessage::Text(text) => {
            let payload =
                serde_json::from_str::<Value>(&text).expect("parse websocket task create");
            assert_eq!(payload["ok"], true);
            payload["result"].clone()
        }
        other => panic!("unexpected websocket task create response: {other:?}"),
    }
}

fn create_session_via_cli(
    ws_url: &str,
    home_dir: &Path,
    codex_home: &Path,
    workdir: &Path,
) -> Value {
    create_session_via_cli_with_options(
        ws_url, "america", None, None, home_dir, codex_home, workdir,
    )
}

fn write_fort_session_state(codex_home: &Path, profile: &str) {
    let session_path = codex_home.join("fort").join("session.json");
    save_persisted_session_state(
        &session_path,
        &PersistedSessionState {
            profile_id: profile.to_owned(),
            agent_id: "si-codex-america".to_owned(),
            session_id: "fort-session".to_owned(),
            host: "https://fort.example.invalid".to_owned(),
            runtime_host: "https://fort-runtime.example.invalid".to_owned(),
            access_expires_at: (Utc::now() + chrono::Duration::hours(1)).to_rfc3339(),
            refresh_expires_at: (Utc::now() + chrono::Duration::hours(12)).to_rfc3339(),
            ..PersistedSessionState::default()
        },
    )
    .expect("write fort session state");
}

fn write_invalid_fort_session_state(codex_home: &Path) {
    let session_path = codex_home.join("fort").join("session.json");
    fs::create_dir_all(session_path.parent().expect("fort parent")).expect("mkdir fort dir");
    fs::write(session_path, b"{ invalid fort state").expect("write invalid fort session state");
}

fn copy_dir_recursive(source: &Path, destination: &Path) {
    fs::create_dir_all(destination).expect("mkdir destination");
    for entry in fs::read_dir(source).expect("read source dir") {
        let entry = entry.expect("dir entry");
        let source_path = entry.path();
        let destination_path = destination.join(entry.file_name());
        let file_type = entry.file_type().expect("file type");
        if file_type.is_dir() {
            copy_dir_recursive(&source_path, &destination_path);
        } else {
            fs::copy(&source_path, &destination_path).expect("copy file");
        }
    }
}

fn load_event_log_values(state_root: &Path) -> Vec<Value> {
    let path = state_root.join("state").join("events").join("events.jsonl");
    fs::read_to_string(path)
        .expect("read events jsonl")
        .lines()
        .filter(|line| !line.trim().is_empty())
        .map(|line| serde_json::from_str::<Value>(line).expect("parse event line"))
        .collect()
}

fn clear_live_session_thread_id(state_root: &Path, session_id: &str) {
    let session_path =
        state_root.join("state").join("sessions").join(session_id).join("session.json");
    let mut persisted_session: Value =
        serde_json::from_slice(&fs::read(&session_path).expect("read session json"))
            .expect("parse session json");
    persisted_session["app_server_thread_id"] = Value::Null;
    fs::write(
        &session_path,
        serde_json::to_vec_pretty(&persisted_session).expect("serialize session json"),
    )
    .expect("write session json");
}

fn write_live_task_updated_at(state_root: &Path, task_id: &str, updated_at: chrono::DateTime<Utc>) {
    let task_path = state_root.join("state").join("tasks").join(task_id).join("task.json");
    let mut persisted_task: Value =
        serde_json::from_slice(&fs::read(&task_path).expect("read task json"))
            .expect("parse task json");
    persisted_task["updated_at"] = Value::String(updated_at.to_rfc3339());
    fs::write(&task_path, serde_json::to_vec_pretty(&persisted_task).expect("serialize task json"))
        .expect("write task json");
}

fn write_live_session_lifecycle_state(state_root: &Path, session_id: &str, lifecycle_state: &str) {
    let session_path =
        state_root.join("state").join("sessions").join(session_id).join("session.json");
    let mut persisted_session: Value =
        serde_json::from_slice(&fs::read(&session_path).expect("read session json"))
            .expect("parse session json");
    persisted_session["lifecycle_state"] = Value::String(lifecycle_state.to_owned());
    fs::write(
        &session_path,
        serde_json::to_vec_pretty(&persisted_session).expect("serialize session json"),
    )
    .expect("write session json");
}

#[test]
fn surf_and_viva_wrappers_render_help() {
    for command in ["surf", "viva"] {
        let output =
            cargo_bin().args([command, "--help"]).assert().success().get_output().stdout.clone();
        let rendered = String::from_utf8(output).expect("utf8 help");
        assert!(rendered.contains("Usage: si"));
    }
}

#[test]
fn surf_wrapper_marks_child_process_as_wrapped() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("surf-args.txt");
    let env_file = bin_dir.path().join("surf-env.txt");
    let surf_path = bin_dir.path().join("surf");
    write_executable_shell_script(
        &surf_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'SI_SURF_WRAPPED=%s\\n' \"${{SI_SURF_WRAPPED:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );

    cargo_bin()
        .args([
            "surf",
            "--home",
            home.path().to_str().expect("home path"),
            "--bin",
            surf_path.to_str().expect("surf path"),
            "status",
            "--json",
        ])
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read surf args");
    assert_eq!(args, "status\n--json\n");
    let env = fs::read_to_string(&env_file).expect("read surf env");
    assert_eq!(env, "SI_SURF_WRAPPED=1\n");
}

#[test]
fn google_youtube_help_paths_render_without_flag_collisions() {
    cargo_bin()
        .args(["orbit", "google", "youtube", "caption", "upload", "--help"])
        .assert()
        .success();
    cargo_bin()
        .args(["orbit", "google", "youtube", "support", "categories", "--help"])
        .assert()
        .success();
}

#[test]
fn google_youtube_support_categories_uses_support_region_flag() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /youtube/v3/videoCategories?"));
        assert!(request.contains("part=snippet"));
        assert!(request.contains("regionCode=US"));
        let body = r#"{"items":[{"id":"cat1","snippet":{"title":"Film"}}]}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "support",
            "categories",
            "--support-region",
            "US",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let payload: Value = serde_json::from_slice(&output).expect("parse json");
    assert_eq!(payload["data"]["items"][0]["id"], "cat1");
}

#[test]
fn viva_wrapper_config_round_trip() {
    let home = tempdir().expect("home tempdir");
    cargo_bin()
        .args([
            "viva",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--repo",
            "/tmp/viva",
            "--build",
            "true",
        ])
        .assert()
        .success();
    let output = cargo_bin()
        .args([
            "viva",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "show",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse viva config");
    assert_eq!(payload.get("repo").and_then(Value::as_str), Some("/tmp/viva"));
    assert_eq!(payload.get("build").and_then(Value::as_bool), Some(true));
}

#[test]
fn build_self_release_assets_writes_archives_and_checksums() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::write(repo.path().join("README.md"), "readme\n").expect("write readme");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("write license");

    let toolchain_dir = tempdir().expect("toolchain tempdir");
    let cargo_path = toolchain_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho si\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", toolchain_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "build",
            "self",
            "release-assets",
            "--repo",
            repo.path().to_str().expect("repo path"),
            "--out-dir",
            out_dir.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .assert()
        .success();

    for name in [
        "si_1.2.3_linux_amd64.tar.gz",
        "si_1.2.3_linux_arm64.tar.gz",
        "si_1.2.3_linux_armv7.tar.gz",
        "si_1.2.3_darwin_amd64.tar.gz",
        "si_1.2.3_darwin_arm64.tar.gz",
    ] {
        assert!(out_dir.join(name).exists(), "missing archive {name}");
    }
    let checksums = fs::read_to_string(out_dir.join("checksums.txt")).expect("read checksums");
    assert!(checksums.contains("si_1.2.3_linux_amd64.tar.gz"));
    assert_eq!(checksums.lines().count(), 5);
    let file = File::open(out_dir.join("si_1.2.3_linux_amd64.tar.gz")).expect("open archive");
    let decoder = flate2::read::GzDecoder::new(file);
    let mut archive = Archive::new(decoder);
    let mut names = archive
        .entries()
        .expect("archive entries")
        .map(|entry| entry.expect("entry").path().expect("entry path").display().to_string())
        .collect::<Vec<_>>();
    names.sort();
    assert!(names.iter().any(|name| name.ends_with("/si")));
}

#[test]
fn build_self_build_no_upgrade_writes_binary() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    fs::create_dir_all(repo.path().join("rust/crates/si-cli")).expect("mkdir cli crate");
    let cargo_path = repo.path().join("fake-cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho built\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    let bin_dir = tempdir().expect("bin tempdir");
    let bin_cargo = bin_dir.path().join("cargo");
    fs::copy(&cargo_path, &bin_cargo).expect("copy cargo");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&bin_cargo).expect("stat cargo").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&bin_cargo, perms).expect("chmod cargo");
    }
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let out = repo.path().join("out/si");
    cargo_bin()
        .args([
            "build",
            "self",
            "build",
            "--repo",
            repo.path().to_str().expect("repo"),
            "--no-upgrade",
            "--output",
            out.to_str().expect("out"),
            "--quiet",
        ])
        .env("PATH", path_env)
        .assert()
        .success();
    assert!(out.exists());
}

#[test]
fn build_self_default_writes_path_binary() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    fs::create_dir_all(repo.path().join("rust/crates/si-cli")).expect("mkdir cli crate");
    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho upgraded\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    let si_path = bin_dir.path().join("si");
    write_executable_shell_script(&si_path, "#!/bin/sh\necho old\n");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    cargo_bin()
        .args(["build", "self", "--repo", repo.path().to_str().expect("repo"), "--quiet"])
        .env("PATH", &path_env)
        .assert()
        .success();
    let rendered = fs::read_to_string(&si_path).expect("read upgraded si");
    assert!(rendered.contains("upgraded"));
}

#[test]
fn build_self_flag_first_no_upgrade_writes_binary() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    fs::create_dir_all(repo.path().join("rust/crates/si-cli")).expect("mkdir cli crate");
    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho flagfirst\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let out = repo.path().join("custom/si");
    cargo_bin()
        .args([
            "build",
            "self",
            "--repo",
            repo.path().to_str().expect("repo"),
            "--no-upgrade",
            "--output",
            out.to_str().expect("out"),
            "--quiet",
        ])
        .env("PATH", path_env)
        .assert()
        .success();
    let rendered = fs::read_to_string(&out).expect("read built binary");
    assert!(rendered.contains("flagfirst"));
}

#[test]
fn build_self_run_forwards_args_to_cargo() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    fs::create_dir_all(repo.path().join("rust/crates/si-cli")).expect("mkdir cli crate");
    let args_path = repo.path().join("args.txt");
    let cargo_path = repo.path().join("fake-cargo");
    write_executable_shell_script(
        &cargo_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nexit 0\n",
            shell_escape_for_test(&args_path)
        ),
    );
    let bin_dir = tempdir().expect("bin tempdir");
    let bin_cargo = bin_dir.path().join("cargo");
    fs::copy(&cargo_path, &bin_cargo).expect("copy cargo");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&bin_cargo).expect("stat cargo").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&bin_cargo, perms).expect("chmod cargo");
    }
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    cargo_bin()
        .args([
            "build",
            "self",
            "run",
            "--repo",
            repo.path().to_str().expect("repo"),
            "--",
            "version",
            "--json",
        ])
        .env("PATH", path_env)
        .assert()
        .success();
    let args = fs::read_to_string(args_path).expect("read args");
    assert!(args.contains("run"));
    assert!(args.contains("--manifest-path"));
    assert!(args.contains("rust/crates/si-cli/Cargo.toml"));
    assert!(args.contains("--bin"));
    assert!(args.contains("si-rs"));
    assert!(args.contains("--"));
    assert!(args.contains("version"));
    assert!(args.contains("--json"));
}

#[test]
fn build_npm_build_package_creates_tarball() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::create_dir_all(repo.path().join("npm/si")).expect("mkdir npm/si");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("write license");
    fs::write(
        repo.path().join("npm/si/package.json"),
        "{\n  \"name\": \"@aureuma/si\",\n  \"version\": \"0.0.0\"\n}\n",
    )
    .expect("write package");
    fs::write(repo.path().join("npm/si/index.js"), "console.log('si');\n").expect("write js");

    let bin_dir = tempdir().expect("bin tempdir");
    let package_check = bin_dir.path().join("package-version.txt");
    let node_path = bin_dir.path().join("node");
    let npm_path = bin_dir.path().join("npm");
    fs::write(&node_path, "#!/bin/sh\necho v20.0.0\n").expect("write node");
    fs::write(
        &npm_path,
        format!(
            "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo 10.0.0\n  exit 0\nfi\nif [ \"$1\" = \"pack\" ]; then\n  grep -q '\"version\": \"1.2.3\"' package.json\n  cp package.json {}\n  touch aureuma-si-1.2.3.tgz\n  exit 0\nfi\nexit 1\n",
            shell_escape_for_test(&package_check)
        ),
    )
    .expect("write npm");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&node_path, &npm_path] {
            let mut perms = fs::metadata(path).expect("stat tool").permissions();
            perms.set_mode(0o755);
            fs::set_permissions(path, perms).expect("chmod tool");
        }
    }

    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "build",
            "npm",
            "build-package",
            "--repo-root",
            repo.path().to_str().expect("repo path"),
            "--out-dir",
            out_dir.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .assert()
        .success();

    assert!(out_dir.join("aureuma-si-1.2.3.tgz").exists());
    let staged_package = fs::read_to_string(package_check).expect("read staged package");
    assert!(staged_package.contains("\"version\": \"1.2.3\""));
}

#[test]
fn build_npm_publish_package_dry_run_uses_generated_tarball() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::create_dir_all(repo.path().join("npm/si")).expect("mkdir npm/si");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("write license");
    fs::write(
        repo.path().join("npm/si/package.json"),
        "{\n  \"name\": \"@aureuma/si\",\n  \"version\": \"0.0.0\"\n}\n",
    )
    .expect("write package");
    fs::write(repo.path().join("npm/si/index.js"), "console.log('si');\n").expect("write js");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("publish-args.txt");
    let node_path = bin_dir.path().join("node");
    let npm_path = bin_dir.path().join("npm");
    fs::write(&node_path, "#!/bin/sh\necho v20.0.0\n").expect("write node");
    fs::write(
        &npm_path,
        format!(
            "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo 10.0.0\n  exit 0\nfi\nif [ \"$1\" = \"view\" ]; then\n  exit 1\nfi\nif [ \"$1\" = \"pack\" ]; then\n  touch aureuma-si-1.2.3.tgz\n  exit 0\nfi\nif [ \"$1\" = \"publish\" ]; then\n  printf '%s\\n' \"$@\" > {}\n  exit 0\nfi\nexit 1\n",
            shell_escape_for_test(&args_file)
        ),
    )
    .expect("write npm");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&node_path, &npm_path] {
            let mut perms = fs::metadata(path).expect("stat tool").permissions();
            perms.set_mode(0o755);
            fs::set_permissions(path, perms).expect("chmod tool");
        }
    }

    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "build",
            "npm",
            "publish-package",
            "--repo-root",
            repo.path().to_str().expect("repo path"),
            "--out-dir",
            out_dir.to_str().expect("out path"),
            "--dry-run",
        ])
        .env("PATH", path_env)
        .env("NPM_TOKEN", "token-123")
        .assert()
        .success();

    let publish_args = fs::read_to_string(args_file).expect("read publish args");
    assert!(publish_args.contains("publish"));
    assert!(publish_args.contains("--access"));
    assert!(publish_args.contains("--dry-run"));
    assert!(publish_args.contains("aureuma-si-1.2.3.tgz"));
}

#[test]
fn nucleus_service_install_writes_systemd_unit_and_reloads_user_manager() {
    let temp = tempdir().expect("tempdir");
    let state_dir = temp.path().join("state");
    let service_dir = temp.path().join("systemd-user");
    let args_file = temp.path().join("systemctl-args.txt");
    let systemctl_path = temp.path().join("systemctl");
    write_executable_shell_script(
        &systemctl_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file),),
    );

    let output = cargo_bin()
        .args([
            "nucleus",
            "service",
            "install",
            "--state-dir",
            state_dir.to_str().expect("state dir"),
            "--service-dir",
            service_dir.to_str().expect("service dir"),
            "--format",
            "json",
        ])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse json");
    let definition_path = service_dir.join("si-nucleus.service");
    assert_eq!(payload["definition_path"], definition_path.display().to_string());
    assert_eq!(payload["manager_command"][0], Value::String(systemctl_path.display().to_string()));
    let unit = fs::read_to_string(&definition_path).expect("read unit");
    assert!(unit.contains("\"nucleus\""));
    assert!(unit.contains("\"service\""));
    assert!(unit.contains("\"run\""));
    assert!(unit.contains(state_dir.to_str().expect("state dir")));

    let args = fs::read_to_string(&args_file).expect("read args");
    assert_eq!(args, "--user\ndaemon-reload\n");
}

#[test]
fn nucleus_service_install_writes_launchd_agent_definition() {
    let temp = tempdir().expect("tempdir");
    let state_dir = temp.path().join("state");
    let service_dir = temp.path().join("launch-agents");

    let output = cargo_bin()
        .args([
            "nucleus",
            "service",
            "install",
            "--state-dir",
            state_dir.to_str().expect("state dir"),
            "--service-dir",
            service_dir.to_str().expect("service dir"),
            "--format",
            "json",
        ])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse json");
    let definition_path = service_dir.join("com.aureuma.si.nucleus.plist");
    assert_eq!(payload["definition_path"], definition_path.display().to_string());
    assert_eq!(payload["service_name"], "com.aureuma.si.nucleus");
    assert_eq!(
        payload["logs_hint"],
        "log stream --style compact --predicate 'process == \"si-nucleus\" || process == \"si-rs\"'"
    );
    assert!(payload["manager_command"].is_null());

    let plist = fs::read_to_string(&definition_path).expect("read plist");
    assert!(plist.contains("<string>nucleus</string>"));
    assert!(plist.contains("<string>service</string>"));
    assert!(plist.contains("<string>run</string>"));
    assert!(plist.contains(state_dir.to_str().expect("state dir")));
}

#[test]
fn nucleus_service_start_and_status_use_systemctl_user_unit() {
    let temp = tempdir().expect("tempdir");
    let calls_file = temp.path().join("systemctl-calls.txt");
    let systemctl_path = temp.path().join("systemctl");
    write_executable_shell_script(
        &systemctl_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> {}\nif [ \"$2\" = \"status\" ]; then\n  printf 'Active: active (running)\\n'\nfi\n",
            shell_escape_for_test(&calls_file),
        ),
    );

    cargo_bin()
        .args(["nucleus", "service", "start", "--format", "json"])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .assert()
        .success();

    let output = cargo_bin()
        .args(["nucleus", "service", "status", "--format", "json"])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse json");
    assert_eq!(payload["service_name"], "si-nucleus.service");
    assert_eq!(payload["logs_hint"], "journalctl --user-unit si-nucleus.service -f");
    assert_eq!(payload["manager_stdout"], "Active: active (running)");

    let calls = fs::read_to_string(&calls_file).expect("read calls");
    assert!(calls.contains("--user\nstart\nsi-nucleus.service\n"));
    assert!(calls.contains("--user\nstatus\n--no-pager\nsi-nucleus.service\n"));
}

#[test]
fn nucleus_service_stop_restart_and_uninstall_use_systemctl_user_unit() {
    let temp = tempdir().expect("tempdir");
    let service_dir = temp.path().join("systemd-user");
    fs::create_dir_all(&service_dir).expect("mkdir service dir");
    let definition_path = service_dir.join("si-nucleus.service");
    fs::write(&definition_path, "[Unit]\nDescription=SI Nucleus\n").expect("write unit");

    let calls_file = temp.path().join("systemctl-calls.txt");
    let systemctl_path = temp.path().join("systemctl");
    write_executable_shell_script(
        &systemctl_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" >> {}\n", shell_escape_for_test(&calls_file)),
    );

    cargo_bin()
        .args(["nucleus", "service", "stop", "--format", "json"])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .assert()
        .success();

    cargo_bin()
        .args(["nucleus", "service", "restart", "--format", "json"])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .assert()
        .success();

    let uninstall_output = cargo_bin()
        .args([
            "nucleus",
            "service",
            "uninstall",
            "--service-dir",
            service_dir.to_str().expect("service dir"),
            "--format",
            "json",
        ])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&uninstall_output).expect("parse uninstall json");
    assert_eq!(payload["action"], "uninstall");
    assert_eq!(payload["definition_path"], definition_path.display().to_string());
    assert!(!definition_path.exists());

    let calls = fs::read_to_string(&calls_file).expect("read calls");
    assert!(calls.contains("--user\nstop\nsi-nucleus.service\n"));
    assert!(calls.contains("--user\nrestart\nsi-nucleus.service\n"));
    assert!(calls.contains("--user\ndaemon-reload\n"));
}

#[test]
fn nucleus_service_launchd_actions_and_status_use_launchctl() {
    let temp = tempdir().expect("tempdir");
    let home = temp.path().join("home");
    fs::create_dir_all(&home).expect("mkdir home");
    let calls_file = temp.path().join("launchctl-calls.txt");
    let launchctl_path = temp.path().join("launchctl");
    write_executable_shell_script(
        &launchctl_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> {}\nif [ \"$1\" = \"print\" ]; then\n  printf 'state = running\\n'\nfi\n",
            shell_escape_for_test(&calls_file),
        ),
    );

    cargo_bin()
        .args(["nucleus", "service", "install", "--format", "json"])
        .env("HOME", &home)
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .env("SI_NUCLEUS_LAUNCHCTL_EXEC", launchctl_path.to_str().expect("launchctl path"))
        .assert()
        .success();

    cargo_bin()
        .args(["nucleus", "service", "start", "--format", "json"])
        .env("HOME", &home)
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .env("SI_NUCLEUS_LAUNCHCTL_EXEC", launchctl_path.to_str().expect("launchctl path"))
        .assert()
        .success();

    let status_output = cargo_bin()
        .args(["nucleus", "service", "status", "--format", "json"])
        .env("HOME", &home)
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .env("SI_NUCLEUS_LAUNCHCTL_EXEC", launchctl_path.to_str().expect("launchctl path"))
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status_payload: Value = serde_json::from_slice(&status_output).expect("parse status json");
    assert_eq!(status_payload["manager_stdout"], "state = running");

    cargo_bin()
        .args(["nucleus", "service", "stop", "--format", "json"])
        .env("HOME", &home)
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .env("SI_NUCLEUS_LAUNCHCTL_EXEC", launchctl_path.to_str().expect("launchctl path"))
        .assert()
        .success();

    cargo_bin()
        .args(["nucleus", "service", "restart", "--format", "json"])
        .env("HOME", &home)
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .env("SI_NUCLEUS_LAUNCHCTL_EXEC", launchctl_path.to_str().expect("launchctl path"))
        .assert()
        .success();

    let domain = format!("gui/{}", unsafe { libc::geteuid() });
    let definition_path = home.join("Library/LaunchAgents/com.aureuma.si.nucleus.plist");
    let calls = fs::read_to_string(&calls_file).expect("read launchctl calls");
    assert!(calls.contains(&format!("bootstrap\n{domain}\n{}\n", definition_path.display())));
    assert!(calls.contains(&format!("print\n{domain}/com.aureuma.si.nucleus\n")));
    assert!(calls.contains(&format!("bootout\n{domain}/com.aureuma.si.nucleus\n")));
    assert!(calls.contains(&format!("kickstart\n-k\n{domain}/com.aureuma.si.nucleus\n")));
}

#[test]
fn nucleus_service_run_execs_nucleus_binary_with_requested_env() {
    let temp = tempdir().expect("tempdir");
    let args_file = temp.path().join("nucleus-args.txt");
    let env_file = temp.path().join("nucleus-env.txt");
    let nucleus_path = temp.path().join("si-nucleus");
    write_executable_shell_script(
        &nucleus_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'SI_NUCLEUS_STATE_DIR=%s\\nSI_NUCLEUS_BIND_ADDR=%s\\n' \"${{SI_NUCLEUS_STATE_DIR:-}}\" \"${{SI_NUCLEUS_BIND_ADDR:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );
    let state_dir = temp.path().join("state");

    cargo_bin()
        .args([
            "nucleus",
            "service",
            "run",
            "--state-dir",
            state_dir.to_str().expect("state dir"),
            "--bind-addr",
            "127.0.0.1:4888",
            "--nucleus-bin",
            nucleus_path.to_str().expect("nucleus path"),
        ])
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read args");
    assert_eq!(args, "\n");
    let env = fs::read_to_string(&env_file).expect("read env");
    assert!(env.contains(&format!("SI_NUCLEUS_STATE_DIR={}\n", state_dir.display())));
    assert!(env.contains("SI_NUCLEUS_BIND_ADDR=127.0.0.1:4888\n"));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_status_reads_ws_endpoint_from_gateway_metadata() {
    let home = tempdir().expect("home tempdir");
    let metadata_dir = home.path().join(".si/nucleus/gateway");
    fs::create_dir_all(&metadata_dir).expect("mkdir gateway metadata");

    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    fs::write(metadata_dir.join("metadata.json"), format!(r#"{{"ws_url":"ws://{addr}/ws"}}"#))
        .expect("write metadata");

    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "version": "metadata-test",
                "bind_addr": addr.to_string(),
                "ws_url": format!("ws://{addr}/ws"),
                "state_dir": "/tmp/nucleus",
                "task_count": 0,
                "worker_count": 0,
                "session_count": 0,
                "run_count": 0,
                "next_event_seq": 1
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args(["nucleus", "status", "--format", "json"])
        .env("HOME", home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["version"], "metadata-test");
    assert_eq!(payload["ws_url"], format!("ws://{addr}/ws"));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_status_reports_live_ws_endpoint_and_counts_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let base_url =
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()));
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse initial status");
    assert_eq!(payload["ws_url"], ws_url);
    assert_eq!(payload["task_count"], 0);
    assert_eq!(payload["worker_count"], 0);
    assert_eq!(payload["session_count"], 0);
    assert_eq!(payload["run_count"], 0);

    let metadata: Value = serde_json::from_slice(
        &fs::read(state_root.join("gateway").join("metadata.json")).expect("read metadata"),
    )
    .expect("parse metadata");
    assert_eq!(metadata["ws_url"], ws_url);
    assert_eq!(metadata["bind_addr"], base_url.trim_start_matches("http://"));

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let created =
        create_task_via_cli(&ws_url, "Status live task", "Reply with nucleus-smoke", "america");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let _done = wait_for_cli_task_status(&ws_url, &task_id, "done");

    let output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse final status");
    assert_eq!(payload["ws_url"], ws_url);
    assert_eq!(payload["state_dir"], state_root.display().to_string());
    assert_eq!(payload["task_count"], 1);
    assert_eq!(payload["worker_count"], 1);
    assert_eq!(payload["session_count"], 1);
    assert_eq!(payload["run_count"], 1);
    assert!(payload["next_event_seq"].as_u64().expect("event seq") > 1);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_status_sends_bearer_token_on_websocket_handshake() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |request: &WsRequest, response: WsResponse| {
            assert_eq!(
                request.headers().get("authorization").and_then(|value| value.to_str().ok()),
                Some("Bearer secret-token")
            );
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "version": "test",
                "bind_addr": "0.0.0.0:4747",
                "ws_url": format!("ws://{addr}/ws"),
                "state_dir": "/tmp/nucleus",
                "task_count": 0,
                "worker_count": 0,
                "session_count": 0,
                "run_count": 0,
                "next_event_seq": 1
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &format!("ws://{addr}/ws"), "--format", "json"])
        .env("SI_NUCLEUS_AUTH_TOKEN", "secret-token")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["version"], "test");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_profile_list_requests_gateway_profile_list_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "profile.list");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": [
                { "profile": "america", "codex_home": "/tmp/codex-america" }
            ]
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "profile",
            "list",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload[0]["profile"], "america");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_profile_list_reflects_live_profiles_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let europe_codex_home = home_dir.join(".si/codex/profiles/europe");
    create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &america_codex_home,
        temp.path(),
    );
    create_session_via_cli_with_options(
        &ws_url,
        "europe",
        None,
        None,
        &home_dir,
        &europe_codex_home,
        temp.path(),
    );

    let output = cargo_bin()
        .args(["nucleus", "profile", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let profiles: Value = serde_json::from_slice(&output).expect("parse profile list");
    let profiles = profiles.as_array().expect("profile list array");
    assert!(profiles.iter().any(|profile| {
        profile["profile"] == "america"
            && profile["account_identity"] == "america@example.com"
            && profile["codex_home"] == america_codex_home.display().to_string()
    }));
    assert!(profiles.iter().any(|profile| {
        profile["profile"] == "europe"
            && profile["account_identity"] == "europe@example.com"
            && profile["codex_home"] == europe_codex_home.display().to_string()
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_create_requests_gateway_task_create_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "task.create");
        assert_eq!(payload["params"]["title"], "Inspect issue");
        assert_eq!(payload["params"]["instructions"], "Summarize the current blocked reason.");
        assert_eq!(payload["params"]["profile"], "america");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "task_id": "si-task-123",
                "title": "Inspect issue",
                "instructions": "Summarize the current blocked reason.",
                "profile": "america",
                "status": "queued"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "create",
            "Inspect issue",
            "Summarize the current blocked reason.",
            "--profile",
            "america",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["task_id"], "si-task-123");
    assert_eq!(payload["status"], "queued");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_create_rejects_non_slug_profile_name_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let failed = cargo_bin()
        .args([
            "nucleus",
            "task",
            "create",
            "Bad profile task",
            "Reject uppercase profile names",
            "--profile",
            "America",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8(failed.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("profile name must match"));

    let status_output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status_output).expect("parse status");
    assert_eq!(status["task_count"], 0);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_list_and_inspect_reflect_live_tasks_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let europe_codex_home = home_dir.join(".si/codex/profiles/europe");
    create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &america_codex_home,
        temp.path(),
    );
    create_session_via_cli_with_options(
        &ws_url,
        "europe",
        None,
        None,
        &home_dir,
        &europe_codex_home,
        temp.path(),
    );

    let america_task =
        create_task_via_cli(&ws_url, "America task inspect", "Reply with nucleus-smoke", "america");
    let europe_task =
        create_task_via_cli(&ws_url, "Europe task inspect", "Reply with nucleus-smoke", "europe");

    let america_task_id = america_task["task_id"].as_str().expect("america task id");
    let europe_task_id = europe_task["task_id"].as_str().expect("europe task id");
    let america_done = wait_for_cli_task_status(&ws_url, america_task_id, "done");
    let _europe_done = wait_for_cli_task_status(&ws_url, europe_task_id, "done");

    let list_output = cargo_bin()
        .args(["nucleus", "task", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&list_output).expect("parse task list");
    let tasks = listed.as_array().expect("task list array");
    assert!(
        tasks.iter().any(|task| task["task_id"] == america_task_id && task["profile"] == "america")
    );
    assert!(
        tasks.iter().any(|task| task["task_id"] == europe_task_id && task["profile"] == "europe")
    );

    let inspect_output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "inspect",
            america_task_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let inspected: Value = serde_json::from_slice(&inspect_output).expect("parse task inspect");
    assert_eq!(inspected["task_id"], america_task_id);
    assert_eq!(inspected["profile"], "america");
    assert_eq!(inspected["status"], "done");
    assert_eq!(inspected["checkpoint_summary"], america_done["checkpoint_summary"]);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_list_requests_gateway_task_list_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "task.list");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": [
                {
                    "task_id": "si-task-123",
                    "title": "Inspect issue",
                    "status": "queued"
                }
            ]
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "list",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload[0]["task_id"], "si-task-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_inspect_requests_gateway_task_inspect_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "task.inspect");
        assert_eq!(payload["params"]["task_id"], "si-task-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "task_id": "si-task-123",
                "title": "Inspect issue",
                "status": "queued"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "inspect",
            "si-task-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["task_id"], "si-task-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_cancel_requests_gateway_task_cancel_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "task.cancel");
        assert_eq!(payload["params"]["task_id"], "si-task-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "task": { "task_id": "si-task-123", "status": "cancelled" },
                "cancellation_requested": false
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "cancel",
            "si-task-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["task"]["status"], "cancelled");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_prune_requests_gateway_task_prune_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "task.prune");
        assert_eq!(payload["params"]["older_than_days"], 30);
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "older_than_days": 30,
                "cutoff_at": "2026-03-07T00:00:00Z",
                "pruned_task_ids": ["si-task-123"],
                "skipped": []
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "prune",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["pruned_task_ids"][0], "si-task-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_list_requests_gateway_worker_list_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "worker.list");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": [
                {
                    "worker_id": "si-worker-123",
                    "profile": "america",
                    "status": "ready"
                }
            ]
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "list",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload[0]["worker_id"], "si-worker-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_probe_requests_gateway_worker_probe_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "worker.probe");
        assert_eq!(payload["params"]["profile"], "america");
        assert_eq!(payload["params"]["worker_id"], "si-worker-123");
        assert_eq!(payload["params"]["home_dir"], "/tmp/si-home");
        assert_eq!(payload["params"]["codex_home"], "/tmp/si-codex");
        assert_eq!(payload["params"]["workdir"], "/tmp/si-work");
        assert_eq!(payload["params"]["env"]["FOO"], "bar");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "worker_id": "si-worker-123",
                "profile": "america",
                "status": "ready"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "probe",
            "america",
            "--worker-id",
            "si-worker-123",
            "--home-dir",
            "/tmp/si-home",
            "--codex-home",
            "/tmp/si-codex",
            "--workdir",
            "/tmp/si-work",
            "--env",
            "FOO=bar",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["worker_id"], "si-worker-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_inspect_requests_gateway_worker_inspect_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "worker.inspect");
        assert_eq!(payload["params"]["worker_id"], "si-worker-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "worker_id": "si-worker-123",
                "profile": "america",
                "status": "ready"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "inspect",
            "si-worker-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["worker_id"], "si-worker-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_requests_gateway_worker_restart_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "worker.restart");
        assert_eq!(payload["params"]["worker_id"], "si-worker-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "worker_id": "si-worker-123",
                "status": "starting"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            "si-worker-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["worker_id"], "si-worker-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_requests_gateway_worker_repair_auth_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "worker.repair_auth");
        assert_eq!(payload["params"]["worker_id"], "si-worker-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "worker_id": "si-worker-123",
                "status": "ready"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            "si-worker-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["worker_id"], "si-worker-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_list_requests_gateway_session_list_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "session.list");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": [
                {
                    "session_id": "si-session-123",
                    "worker_id": "si-worker-123",
                    "status": "ready"
                }
            ]
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "session",
            "list",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload[0]["session_id"], "si-session-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_create_requests_gateway_session_create_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "session.create");
        assert_eq!(payload["params"]["profile"], "america");
        assert_eq!(payload["params"]["worker_id"], "si-worker-123");
        assert_eq!(payload["params"]["thread_id"], "thread-123");
        assert_eq!(payload["params"]["home_dir"], "/tmp/si-home");
        assert_eq!(payload["params"]["codex_home"], "/tmp/si-codex");
        assert_eq!(payload["params"]["workdir"], "/tmp/si-work");
        assert_eq!(payload["params"]["env"]["FOO"], "bar");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "session_id": "si-session-123",
                "worker_id": "si-worker-123",
                "status": "ready"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "session",
            "create",
            "america",
            "--worker-id",
            "si-worker-123",
            "--thread-id",
            "thread-123",
            "--home-dir",
            "/tmp/si-home",
            "--codex-home",
            "/tmp/si-codex",
            "--workdir",
            "/tmp/si-work",
            "--env",
            "FOO=bar",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["session_id"], "si-session-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_create_rejects_non_slug_profile_name_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/America");
    let failed = cargo_bin()
        .args([
            "nucleus",
            "session",
            "create",
            "America",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8(failed.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("profile name must match"));

    let status_output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status_output).expect("parse status");
    assert_eq!(status["worker_count"], 0);
    assert_eq!(status["session_count"], 0);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_show_requests_gateway_session_show_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "session.show");
        assert_eq!(payload["params"]["session_id"], "si-session-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "session_id": "si-session-123",
                "worker_id": "si-worker-123",
                "status": "ready"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "session",
            "show",
            "si-session-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["session_id"], "si-session-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_submit_turn_requests_gateway_run_submit_turn_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "run.submit_turn");
        assert_eq!(payload["params"]["session_id"], "si-session-123");
        assert_eq!(payload["params"]["prompt"], "Hello from SI");
        assert_eq!(payload["params"]["task_id"], "si-task-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "run_id": "si-run-123",
                "session_id": "si-session-123",
                "status": "running"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "run",
            "submit-turn",
            "si-session-123",
            "Hello from SI",
            "--task-id",
            "si-task-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["run_id"], "si-run-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_inspect_requests_gateway_run_inspect_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "run.inspect");
        assert_eq!(payload["params"]["run_id"], "si-run-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "run_id": "si-run-123",
                "session_id": "si-session-123",
                "status": "running"
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "run",
            "inspect",
            "si-run-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["run_id"], "si-run-123");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_cancel_requests_gateway_run_cancel_method() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "run.cancel");
        assert_eq!(payload["params"]["run_id"], "si-run-123");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": {
                "run": {
                    "run_id": "si-run-123",
                    "status": "cancelled"
                },
                "cancellation_requested": false
            }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write message");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "run",
            "cancel",
            "si-run-123",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse output");
    assert_eq!(payload["run"]["status"], "cancelled");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_events_subscribe_prints_streamed_events() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("listener addr");
    thread::spawn(move || {
        let (stream, _) = listener.accept().expect("accept");
        let mut socket = accept_hdr(stream, |_: &WsRequest, response: WsResponse| {
            accept_test_ws_response(response)
        })
        .expect("accept websocket");
        let request = socket.read().expect("read message");
        let payload = match request {
            WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse request"),
            other => panic!("unexpected websocket message: {other:?}"),
        };
        assert_eq!(payload["method"], "events.subscribe");
        let response = serde_json::json!({
            "id": payload["id"].clone(),
            "ok": true,
            "result": { "subscribed": true }
        });
        socket.send(WsMessage::Text(response.to_string().into())).expect("write ack");
        let event = serde_json::json!({
            "event_id": "si-event-123",
            "seq": 1,
            "ts": "2026-04-06T00:00:00Z",
            "type": "worker.ready",
            "source": "system",
            "data": {
                "worker_id": "si-worker-123",
                "profile": "america",
                "payload": { "message": "ready" }
            }
        });
        socket.send(WsMessage::Text(event.to_string().into())).expect("write event");
    });

    let output = cargo_bin()
        .args([
            "nucleus",
            "events",
            "subscribe",
            "--count",
            "1",
            "--endpoint",
            &format!("ws://{addr}/ws"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let rendered = String::from_utf8(output).expect("utf8 output");
    assert!(rendered.contains("\"type\": \"worker.ready\""));
    assert!(rendered.contains("\"event_id\": \"si-event-123\""));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_events_subscribe_streams_live_run_events_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(0),
        Duration::from_millis(200),
        &["alpha", "beta"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    let create_task_ws_url = ws_url.clone();
    let producer = thread::spawn(move || {
        thread::sleep(Duration::from_millis(200));
        create_task_via_cli(
            &create_task_ws_url,
            "Live events task",
            "Reply with alphabet chunks",
            "america",
        )
    });

    let output = ProcessCommand::new(assert_cmd::cargo::cargo_bin("si-rs"))
        .args([
            "nucleus",
            "events",
            "subscribe",
            "--count",
            "7",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .expect("run events subscribe");
    let created = producer.join().expect("producer thread");
    let task_id = created["task_id"].as_str().expect("task id");
    assert!(output.status.success(), "{}", String::from_utf8_lossy(&output.stderr));

    let rendered = String::from_utf8(output.stdout).expect("utf8 output");
    assert!(rendered.contains("\"type\": \"task.created\""));
    assert!(rendered.contains("\"type\": \"run.started\""));
    assert!(rendered.contains("\"type\": \"run.output_delta\""));
    assert!(rendered.contains("\"type\": \"run.completed\""));
    assert!(rendered.contains(&format!("\"task_id\": \"{task_id}\"")));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_task_matches_websocket_and_cli_state() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service(&temp.path().join("nucleus"));
    let client = BlockingClient::new();

    let create_response = client
        .post(format!("{base_url}/tasks"))
        .json(&serde_json::json!({
            "title": "REST parity task",
            "instructions": "Verify REST, websocket, and CLI agree.",
            "profile": "america"
        }))
        .send()
        .expect("create rest task");
    assert!(create_response.status().is_success());
    let created: Value = create_response.json().expect("parse create payload");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let rest_inspect: Value = client
        .get(format!("{base_url}/tasks/{task_id}"))
        .send()
        .expect("inspect rest task")
        .json()
        .expect("parse rest inspect payload");

    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));
    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let inspect_request = serde_json::json!({
        "id": "task-inspect",
        "method": "task.inspect",
        "params": { "task_id": task_id }
    });
    socket
        .send(WsMessage::Text(inspect_request.to_string().into()))
        .expect("send websocket inspect");
    let ws_response = socket.read().expect("read websocket inspect");
    let ws_payload = match ws_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse websocket payload")
        }
        other => panic!("unexpected websocket response: {other:?}"),
    };
    assert_eq!(ws_payload["ok"], true);
    let ws_inspect = ws_payload["result"].clone();

    let cli_output = cargo_bin()
        .args(["nucleus", "task", "inspect", &task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_inspect: Value = serde_json::from_slice(&cli_output).expect("parse cli output");

    for field in ["task_id", "title", "instructions", "profile", "status"] {
        assert_eq!(rest_inspect[field], ws_inspect[field], "field mismatch via websocket: {field}");
        assert_eq!(rest_inspect[field], cli_inspect[field], "field mismatch via cli: {field}");
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_mutations_return_documented_status_codes_and_shapes() {
    let temp = tempdir().expect("tempdir");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(900),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let base_url =
        spawn_live_nucleus_service_with_runtime(&temp.path().join("nucleus"), Arc::new(runtime));
    let client = BlockingClient::new();
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let created_response = client
        .post(format!("{base_url}/tasks"))
        .json(&json!({
            "title": "REST status-code task",
            "instructions": "Reply with nucleus-smoke",
            "profile": "america"
        }))
        .send()
        .expect("rest create task");
    assert_eq!(created_response.status(), reqwest::StatusCode::CREATED);
    let created: Value = created_response.json().expect("parse created task");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    assert_eq!(created["title"], "REST status-code task");
    assert_eq!(created["instructions"], "Reply with nucleus-smoke");
    assert_eq!(created["profile"], "america");
    assert_eq!(created["status"], "queued");

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    let running = wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(5), |task| {
        task["status"] == "running"
    });
    let run_id = running["latest_run_id"].as_str().expect("run id").to_owned();

    let cancel_response =
        client.post(format!("{base_url}/tasks/{task_id}/cancel")).send().expect("rest cancel task");
    assert_eq!(cancel_response.status(), reqwest::StatusCode::OK);
    let cancelled: Value = cancel_response.json().expect("parse cancel result");
    assert_eq!(cancelled["task"]["task_id"], task_id);
    assert_eq!(cancelled["run"]["run_id"], run_id);
    assert!(cancelled["cancellation_requested"].is_boolean());

    let cancelled_task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    let cancelled_run = inspect_run_via_cli(&ws_url, &run_id);
    assert_eq!(cancelled_task["status"], "cancelled");
    assert_eq!(cancelled_run["status"], "cancelled");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_create_rejects_non_slug_profile_name() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service_with_runtime(
        &temp.path().join("nucleus"),
        Arc::new(TestRuntime::default()),
    );
    let client = BlockingClient::new();

    let response = client
        .post(format!("{base_url}/tasks"))
        .json(&json!({
            "title": "Bad profile task",
            "instructions": "Reject uppercase profile names",
            "profile": "America"
        }))
        .send()
        .expect("rest create invalid profile");
    assert_eq!(response.status(), reqwest::StatusCode::BAD_REQUEST);
    let body: Value = response.json().expect("parse invalid profile body");
    assert_eq!(body["error"]["code"], "invalid_params");
    assert!(
        body["error"]["message"]
            .as_str()
            .map(|value| value.contains("profile name must match"))
            .unwrap_or(false)
    );
    assert!(body["error"]["details"].is_null());

    let tasks: Value = client
        .get(format!("{base_url}/tasks"))
        .send()
        .expect("list tasks after invalid create")
        .json()
        .expect("parse task list");
    assert_eq!(tasks.as_array().expect("task list array").len(), 0);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_status_matches_websocket_and_cli_state() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let base_url =
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()));
    let client = BlockingClient::new();
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let created = create_task_via_cli(
        &ws_url,
        "REST status parity task",
        "Reply with nucleus-smoke",
        "america",
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let _done = wait_for_cli_task_status(&ws_url, &task_id, "done");

    let rest_status: Value = client
        .get(format!("{base_url}/status"))
        .send()
        .expect("rest status")
        .json()
        .expect("parse rest status");

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let status_request = serde_json::json!({
        "id": "nucleus-status",
        "method": "nucleus.status",
        "params": {}
    });
    socket.send(WsMessage::Text(status_request.to_string().into())).expect("send websocket status");
    let status_response = socket.read().expect("read websocket status");
    let status_payload = match status_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse status payload")
        }
        other => panic!("unexpected websocket status response: {other:?}"),
    };
    assert_eq!(status_payload["ok"], true);
    let ws_status = status_payload["result"].clone();

    let cli_output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_status: Value = serde_json::from_slice(&cli_output).expect("parse cli status");

    let metadata: Value = serde_json::from_slice(
        &fs::read(state_root.join("gateway").join("metadata.json")).expect("read metadata"),
    )
    .expect("parse metadata");
    assert_eq!(rest_status["ws_url"], metadata["ws_url"]);
    assert_eq!(rest_status["bind_addr"], metadata["bind_addr"]);

    for field in [
        "version",
        "bind_addr",
        "ws_url",
        "state_dir",
        "task_count",
        "worker_count",
        "session_count",
        "run_count",
        "next_event_seq",
    ] {
        assert_eq!(
            rest_status[field], ws_status[field],
            "status field mismatch via websocket: {field}"
        );
        assert_eq!(rest_status[field], cli_status[field], "status field mismatch via cli: {field}");
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_task_and_worker_lists_match_websocket_and_cli_state() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let base_url =
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()));
    let client = BlockingClient::new();
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let europe_codex_home = home_dir.join(".si/codex/profiles/europe");
    let america = create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &america_codex_home,
        temp.path(),
    );
    let europe = create_session_via_cli_with_options(
        &ws_url,
        "europe",
        None,
        None,
        &home_dir,
        &europe_codex_home,
        temp.path(),
    );
    let america_worker_id = america["worker"]["worker_id"].as_str().expect("america worker id");
    let europe_worker_id = europe["worker"]["worker_id"].as_str().expect("europe worker id");

    let america_task = create_task_via_cli(
        &ws_url,
        "REST list parity america",
        "Reply with nucleus-smoke",
        "america",
    );
    let europe_task = create_task_via_cli(
        &ws_url,
        "REST list parity europe",
        "Reply with nucleus-smoke",
        "europe",
    );
    let america_task_id = america_task["task_id"].as_str().expect("america task id").to_owned();
    let europe_task_id = europe_task["task_id"].as_str().expect("europe task id").to_owned();
    let america_done = wait_for_cli_task_status(&ws_url, &america_task_id, "done");
    let europe_done = wait_for_cli_task_status(&ws_url, &europe_task_id, "done");

    let rest_tasks: Value = client
        .get(format!("{base_url}/tasks"))
        .send()
        .expect("list rest tasks")
        .json()
        .expect("parse rest tasks");
    let rest_workers: Value = client
        .get(format!("{base_url}/workers"))
        .send()
        .expect("list rest workers")
        .json()
        .expect("parse rest workers");

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let task_list_request = serde_json::json!({
        "id": "task-list",
        "method": "task.list",
        "params": {}
    });
    socket
        .send(WsMessage::Text(task_list_request.to_string().into()))
        .expect("send websocket task list");
    let task_list_response = socket.read().expect("read websocket task list");
    let task_list_payload = match task_list_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse task list payload")
        }
        other => panic!("unexpected websocket task list response: {other:?}"),
    };
    assert_eq!(task_list_payload["ok"], true);
    let ws_tasks = task_list_payload["result"].clone();

    let worker_list_request = serde_json::json!({
        "id": "worker-list",
        "method": "worker.list",
        "params": {}
    });
    socket
        .send(WsMessage::Text(worker_list_request.to_string().into()))
        .expect("send websocket worker list");
    let worker_list_response = socket.read().expect("read websocket worker list");
    let worker_list_payload = match worker_list_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse worker list payload")
        }
        other => panic!("unexpected websocket worker list response: {other:?}"),
    };
    assert_eq!(worker_list_payload["ok"], true);
    let ws_workers = worker_list_payload["result"].clone();

    let cli_tasks_output = cargo_bin()
        .args(["nucleus", "task", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_tasks: Value = serde_json::from_slice(&cli_tasks_output).expect("parse cli task list");
    let cli_workers_output = cargo_bin()
        .args(["nucleus", "worker", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_workers: Value =
        serde_json::from_slice(&cli_workers_output).expect("parse cli worker list");

    for task in [&america_done, &europe_done] {
        let task_id = task["task_id"].clone();
        let profile = task["profile"].clone();
        let status = task["status"].clone();
        for (surface, tasks) in
            [("rest", &rest_tasks), ("websocket", &ws_tasks), ("cli", &cli_tasks)]
        {
            assert!(
                tasks
                    .as_array()
                    .expect("task list array")
                    .iter()
                    .any(|item| item["task_id"] == task_id
                        && item["profile"] == profile
                        && item["status"] == status),
                "{surface} task list missing expected task {task_id}"
            );
        }
    }

    for (worker_id, profile) in [(america_worker_id, "america"), (europe_worker_id, "europe")] {
        for (surface, workers) in
            [("rest", &rest_workers), ("websocket", &ws_workers), ("cli", &cli_workers)]
        {
            assert!(
                workers
                    .as_array()
                    .expect("worker list array")
                    .iter()
                    .any(|item| item["worker_id"] == worker_id
                        && item["profile"] == profile
                        && item["status"] == "ready"),
                "{surface} worker list missing expected worker {worker_id}"
            );
        }
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_worker_session_and_run_match_websocket_and_cli_state() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service_with_runtime(
        &temp.path().join("nucleus"),
        Arc::new(TestRuntime::default()),
    );
    let client = BlockingClient::new();
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created = create_task_via_cli(
        &ws_url,
        "REST inspect parity task",
        "Reply with nucleus-smoke",
        "america",
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let run_id = task["latest_run_id"].as_str().expect("run id").to_owned();

    let rest_workers: Value = client
        .get(format!("{base_url}/workers"))
        .send()
        .expect("list rest workers")
        .json()
        .expect("parse rest workers");
    let rest_worker: Value = client
        .get(format!("{base_url}/workers/{worker_id}"))
        .send()
        .expect("inspect rest worker")
        .json()
        .expect("parse rest worker");
    let rest_session: Value = client
        .get(format!("{base_url}/sessions/{session_id}"))
        .send()
        .expect("inspect rest session")
        .json()
        .expect("parse rest session");
    let rest_run: Value = client
        .get(format!("{base_url}/runs/{run_id}"))
        .send()
        .expect("inspect rest run")
        .json()
        .expect("parse rest run");

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let worker_request = serde_json::json!({
        "id": "worker-inspect",
        "method": "worker.inspect",
        "params": { "worker_id": worker_id }
    });
    socket
        .send(WsMessage::Text(worker_request.to_string().into()))
        .expect("send websocket worker inspect");
    let worker_response = socket.read().expect("read websocket worker inspect");
    let worker_payload = match worker_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse worker payload")
        }
        other => panic!("unexpected websocket worker response: {other:?}"),
    };
    assert_eq!(worker_payload["ok"], true);
    let ws_worker = worker_payload["result"].clone();

    let session_request = serde_json::json!({
        "id": "session-show",
        "method": "session.show",
        "params": { "session_id": session_id }
    });
    socket
        .send(WsMessage::Text(session_request.to_string().into()))
        .expect("send websocket session show");
    let session_response = socket.read().expect("read websocket session show");
    let session_payload = match session_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse session payload")
        }
        other => panic!("unexpected websocket session response: {other:?}"),
    };
    assert_eq!(session_payload["ok"], true);
    let ws_session = session_payload["result"].clone();

    let run_request = serde_json::json!({
        "id": "run-inspect",
        "method": "run.inspect",
        "params": { "run_id": run_id }
    });
    socket
        .send(WsMessage::Text(run_request.to_string().into()))
        .expect("send websocket run inspect");
    let run_response = socket.read().expect("read websocket run inspect");
    let run_payload = match run_response {
        WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse run payload"),
        other => panic!("unexpected websocket run response: {other:?}"),
    };
    assert_eq!(run_payload["ok"], true);
    let ws_run = run_payload["result"].clone();

    let cli_worker = inspect_worker_via_cli(&ws_url, &worker_id);
    let cli_session = inspect_session_via_cli(&ws_url, &session_id);
    let cli_run = inspect_run_via_cli(&ws_url, &run_id);

    assert!(
        rest_workers
            .as_array()
            .expect("rest worker list array")
            .iter()
            .any(|worker| worker["worker_id"] == worker_id && worker["profile"] == "america")
    );
    for field in ["worker_id", "profile", "status"] {
        assert_eq!(
            rest_worker["worker"][field], ws_worker["worker"][field],
            "worker field mismatch via websocket: {field}"
        );
        assert_eq!(
            rest_worker["worker"][field], cli_worker["worker"][field],
            "worker field mismatch via cli: {field}"
        );
    }

    for field in ["session_id", "worker_id", "profile", "app_server_thread_id"] {
        assert_eq!(
            rest_session[field], ws_session[field],
            "session field mismatch via websocket: {field}"
        );
        assert_eq!(
            rest_session[field], cli_session[field],
            "session field mismatch via cli: {field}"
        );
    }

    for field in ["run_id", "task_id", "session_id", "status"] {
        assert_eq!(rest_run[field], ws_run[field], "run field mismatch via websocket: {field}");
        assert_eq!(rest_run[field], cli_run[field], "run field mismatch via cli: {field}");
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_task_cancel_matches_websocket_and_cli_state() {
    let temp = tempdir().expect("tempdir");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(900),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let base_url =
        spawn_live_nucleus_service_with_runtime(&temp.path().join("nucleus"), Arc::new(runtime));
    let client = BlockingClient::new();
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    let created = create_task_via_cli(
        &ws_url,
        "REST cancel parity task",
        "Reply with nucleus-smoke",
        "america",
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("run id").to_owned();

    let cancelled: Value = client
        .post(format!("{base_url}/tasks/{task_id}/cancel"))
        .send()
        .expect("rest cancel")
        .json()
        .expect("parse rest cancel");
    assert_eq!(cancelled["task"]["task_id"], task_id);
    assert_eq!(cancelled["run"]["run_id"], run_id);

    let cancelled_task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    let rest_task_after: Value = client
        .get(format!("{base_url}/tasks/{task_id}"))
        .send()
        .expect("rest inspect task after cancel")
        .json()
        .expect("parse rest task after cancel");
    let rest_run_after: Value = client
        .get(format!("{base_url}/runs/{run_id}"))
        .send()
        .expect("rest inspect run after cancel")
        .json()
        .expect("parse rest run after cancel");

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let task_request = serde_json::json!({
        "id": "task-inspect-after-rest-cancel",
        "method": "task.inspect",
        "params": { "task_id": task_id }
    });
    socket
        .send(WsMessage::Text(task_request.to_string().into()))
        .expect("send websocket task inspect");
    let task_response = socket.read().expect("read websocket task inspect");
    let task_payload = match task_response {
        WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse task payload"),
        other => panic!("unexpected websocket task response: {other:?}"),
    };
    assert_eq!(task_payload["ok"], true);
    let ws_task = task_payload["result"].clone();

    let run_request = serde_json::json!({
        "id": "run-inspect-after-rest-cancel",
        "method": "run.inspect",
        "params": { "run_id": run_id }
    });
    socket
        .send(WsMessage::Text(run_request.to_string().into()))
        .expect("send websocket run inspect");
    let run_response = socket.read().expect("read websocket run inspect");
    let run_payload = match run_response {
        WsMessage::Text(text) => serde_json::from_str::<Value>(&text).expect("parse run payload"),
        other => panic!("unexpected websocket run response: {other:?}"),
    };
    assert_eq!(run_payload["ok"], true);
    let ws_run = run_payload["result"].clone();

    let cli_task_output = cargo_bin()
        .args(["nucleus", "task", "inspect", &task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_task: Value = serde_json::from_slice(&cli_task_output).expect("parse cli task inspect");
    let cli_run = inspect_run_via_cli(&ws_url, &run_id);

    for field in ["task_id", "status"] {
        assert_eq!(
            rest_task_after[field], ws_task[field],
            "task field mismatch via websocket: {field}"
        );
        assert_eq!(rest_task_after[field], cli_task[field], "task field mismatch via cli: {field}");
    }
    for field in ["run_id", "status"] {
        assert_eq!(
            rest_run_after[field], ws_run[field],
            "run field mismatch via websocket: {field}"
        );
        assert_eq!(rest_run_after[field], cli_run[field], "run field mismatch via cli: {field}");
    }
    assert_eq!(cancelled_task["status"], "cancelled");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_missing_targets_return_canonical_not_found_envelopes() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service(&temp.path().join("nucleus"));
    let client = BlockingClient::new();

    for (method, path) in [
        ("GET", "/tasks/si-task-missing"),
        ("GET", "/workers/si-worker-missing"),
        ("GET", "/sessions/si-session-missing"),
        ("GET", "/runs/si-run-missing"),
        ("POST", "/tasks/si-task-missing/cancel"),
    ] {
        let response = match method {
            "GET" => client.get(format!("{base_url}{path}")).send().expect("send rest get"),
            "POST" => client.post(format!("{base_url}{path}")).send().expect("send rest post"),
            other => panic!("unexpected method: {other}"),
        };
        assert_eq!(response.status(), reqwest::StatusCode::NOT_FOUND, "{method} {path}");
        let body: Value = response.json().expect("parse not found body");
        assert_eq!(body["error"]["code"], "not_found", "{method} {path}");
        assert!(
            body["error"]["message"]
                .as_str()
                .map(|value| value.contains("not found"))
                .unwrap_or(false),
            "{method} {path} missing not-found message"
        );
        assert!(body["error"]["details"].is_null(), "{method} {path} expected null details");
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_rest_task_cancel_returns_unavailable_when_runtime_is_missing() {
    let temp = tempdir().expect("tempdir");
    let source_state_root = temp.path().join("source-nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_secs(5),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let source_ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&source_state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&source_ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    let created = create_task_over_websocket(
        &source_ws_url,
        "rest-cancel-runtime-unavailable-live",
        "Cancel active run without runtime",
        "Keep running until cancellation is attempted",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&source_ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("run id").to_owned();

    let snapshot_state_root = temp.path().join("snapshot-nucleus");
    copy_dir_recursive(&source_state_root, &snapshot_state_root);

    let base_url = spawn_live_nucleus_service(&snapshot_state_root);
    let client = BlockingClient::new();
    let response = client
        .post(format!("{base_url}/tasks/{task_id}/cancel"))
        .send()
        .expect("rest cancel without runtime");
    assert_eq!(response.status(), reqwest::StatusCode::SERVICE_UNAVAILABLE);
    let body: Value = response.json().expect("parse unavailable body");
    assert_eq!(body["error"]["code"], "unavailable");
    assert!(
        body["error"]["message"]
            .as_str()
            .map(|value| value.contains("runtime unavailable"))
            .unwrap_or(false)
    );

    let task: Value = client
        .get(format!("{base_url}/tasks/{task_id}"))
        .send()
        .expect("inspect task after unavailable cancel")
        .json()
        .expect("parse task after unavailable cancel");
    let run: Value = client
        .get(format!("{base_url}/runs/{run_id}"))
        .send()
        .expect("inspect run after unavailable cancel")
        .json()
        .expect("parse run after unavailable cancel");
    assert_eq!(task["status"], "running");
    assert_eq!(run["status"], "running");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_websocket_task_matches_cli_state_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service(&temp.path().join("nucleus"));
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let create_request = serde_json::json!({
        "id": "task-create",
        "method": "task.create",
        "params": {
            "title": "Gateway parity task",
            "instructions": "Verify websocket-created tasks are visible through the CLI.",
            "profile": "america"
        }
    });
    socket.send(WsMessage::Text(create_request.to_string().into())).expect("send websocket create");
    let create_response = socket.read().expect("read websocket create");
    let create_payload = match create_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse websocket create")
        }
        other => panic!("unexpected websocket response: {other:?}"),
    };
    assert_eq!(create_payload["ok"], true);
    let created = create_payload["result"].clone();
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let cli_output = cargo_bin()
        .args(["nucleus", "task", "inspect", &task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_inspect: Value = serde_json::from_slice(&cli_output).expect("parse cli output");

    let list_output = cargo_bin()
        .args(["nucleus", "task", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_list: Value = serde_json::from_slice(&list_output).expect("parse cli list output");
    assert!(
        cli_list.as_array().expect("task list array").iter().any(|task| task["task_id"] == task_id),
        "cli task list did not include websocket-created task"
    );

    for field in ["task_id", "title", "instructions", "profile", "status"] {
        assert_eq!(
            created[field], cli_inspect[field],
            "field mismatch via cli for websocket-created task: {field}"
        );
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_cli_task_matches_websocket_state_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service(&temp.path().join("nucleus"));
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let cli_output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "create",
            "CLI parity task",
            "Verify CLI-created tasks are visible through the websocket gateway.",
            "--profile",
            "america",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cli_create: Value = serde_json::from_slice(&cli_output).expect("parse cli create output");
    let task_id = cli_create["task_id"].as_str().expect("task id").to_owned();

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let inspect_request = serde_json::json!({
        "id": "task-inspect",
        "method": "task.inspect",
        "params": { "task_id": task_id }
    });
    socket
        .send(WsMessage::Text(inspect_request.to_string().into()))
        .expect("send websocket inspect");
    let inspect_response = socket.read().expect("read websocket inspect");
    let inspect_payload = match inspect_response {
        WsMessage::Text(text) => {
            serde_json::from_str::<Value>(&text).expect("parse websocket inspect")
        }
        other => panic!("unexpected websocket response: {other:?}"),
    };
    assert_eq!(inspect_payload["ok"], true);
    let ws_inspect = inspect_payload["result"].clone();

    for field in ["task_id", "title", "instructions", "profile", "status"] {
        assert_eq!(
            cli_create[field], ws_inspect[field],
            "field mismatch via websocket for cli-created task: {field}"
        );
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_cron_producer_task_matches_cli_and_websocket_state() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    let due_at = (Utc::now() - chrono::Duration::seconds(5)).to_rfc3339();
    write_cron_rule(
        &state_root,
        "nightly",
        "once_at",
        &due_at,
        "Reply with nucleus-smoke",
        &due_at,
    );

    let listed = wait_for_cli_task(&ws_url, Duration::from_secs(5), |task| {
        task["source"] == "cron" && task["producer_rule_name"] == "nightly"
    });
    let task_id = listed["task_id"].as_str().expect("task id").to_owned();
    let inspected = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let websocket = inspect_task_over_websocket(&ws_url, &task_id);

    for field in ["task_id", "source", "producer_rule_name", "status", "checkpoint_summary"] {
        assert_eq!(inspected[field], websocket[field], "cron field mismatch: {field}");
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_hook_producer_task_matches_cli_and_websocket_state() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    write_hook_rule(&state_root, "task-created", "task.created", "Reply with nucleus-smoke");

    let (mut socket, _) = connect(ws_url.as_str()).expect("connect websocket");
    let create_request = serde_json::json!({
        "id": "task-create",
        "method": "task.create",
        "params": {
            "title": "Primary task",
            "instructions": "Create hook input",
            "profile": "america"
        }
    });
    socket.send(WsMessage::Text(create_request.to_string().into())).expect("send websocket create");
    let create_response = socket.read().expect("read websocket create");
    match create_response {
        WsMessage::Text(text) => {
            let payload = serde_json::from_str::<Value>(&text).expect("parse create payload");
            assert_eq!(payload["ok"], true);
        }
        other => panic!("unexpected websocket response: {other:?}"),
    }

    let listed = wait_for_cli_task(&ws_url, Duration::from_secs(5), |task| {
        task["source"] == "hook" && task["producer_rule_name"] == "task-created"
    });
    let task_id = listed["task_id"].as_str().expect("task id").to_owned();
    let inspected = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let websocket = inspect_task_over_websocket(&ws_url, &task_id);

    for field in ["task_id", "source", "producer_rule_name", "status", "checkpoint_summary"] {
        assert_eq!(inspected[field], websocket[field], "hook field mismatch: {field}");
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_fort_ready_task_executes_and_projects_event_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    write_fort_session_state(&codex_home, "america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    let cli_output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "create",
            "Fort-backed task",
            "Use si fort bootstrap and then reply with nucleus-smoke",
            "--profile",
            "america",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let created: Value = serde_json::from_slice(&cli_output).expect("parse cli create output");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let inspected = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let websocket = inspect_task_over_websocket(&ws_url, &task_id);
    assert_eq!(inspected["status"], "done");
    assert_eq!(inspected["checkpoint_summary"], "nucleus-smoke");
    assert_eq!(inspected["status"], websocket["status"]);

    let events = load_event_log_values(&state_root);
    assert!(
        events
            .iter()
            .any(|event| { event["type"] == "fort.ready" && event["data"]["task_id"] == task_id })
    );
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_fort_auth_required_blocks_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id");

    let created = create_task_over_websocket(
        &ws_url,
        "task-fort-auth-required",
        "Fort auth required task",
        "Use si fort status before continuing",
        "america",
        Some(session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "auth_required");
    assert!(task["latest_run_id"].is_null());

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "fort.auth_required" && event["data"]["task_id"] == task_id
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_fort_unavailable_blocks_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    write_invalid_fort_session_state(&codex_home);
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id");

    let created = create_task_over_websocket(
        &ws_url,
        "task-fort-unavailable",
        "Fort unavailable task",
        "Use si fort refresh before continuing",
        "america",
        Some(session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "fort_unavailable");
    assert!(task["latest_run_id"].is_null());

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "fort.unavailable" && event["data"]["task_id"] == task_id
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_submit_turn_blocks_fort_unavailable_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_config(TestRuntimeConfig {
        run_delay: Duration::from_millis(900),
        step_delay: Duration::from_millis(0),
        output_deltas: vec!["nucleus-smoke".to_owned()],
        fail_execute: false,
        fail_execute_prompts: Vec::new(),
        block_when_worker_missing: false,
        fail_start_worker: false,
        fail_ensure_session: false,
    });
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    write_invalid_fort_session_state(&codex_home);
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let active = create_task_over_websocket(
        &ws_url,
        "task-fort-busy-session",
        "Keep session busy",
        "Reply with nucleus-smoke before testing fort direct-run failure",
        "america",
        Some(&session_id),
    );
    let active_task_id = active["task_id"].as_str().expect("active task id").to_owned();
    let _running = wait_for_cli_task_status(&ws_url, &active_task_id, "running");

    let created = create_task_over_websocket(
        &ws_url,
        "task-fort-unavailable-direct-run",
        "Fort unavailable direct run task",
        "Use si fort refresh before continuing",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let queued = wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
        task["status"] == "queued" && task["latest_run_id"].is_null()
    });
    assert!(queued["latest_run_id"].is_null());

    let submit = cargo_bin()
        .args([
            "nucleus",
            "run",
            "submit-turn",
            &session_id,
            "Use si fort refresh now",
            "--task-id",
            &task_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8(submit.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("Fort is unavailable"));

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "fort_unavailable");
    assert!(task["latest_run_id"].is_null());

    let completed = wait_for_cli_task_status(&ws_url, &active_task_id, "done");
    assert_eq!(completed["checkpoint_summary"], "nucleus-smoke");

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "fort.unavailable" && event["data"]["task_id"] == task_id
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_backlog_stays_serial_and_reuses_the_same_session_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(700),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let expected_session_id =
        session["session"]["session_id"].as_str().expect("session id").to_owned();
    let first_created = create_task_over_websocket(
        &ws_url,
        "task-first",
        "First queued task",
        "Reply with nucleus-smoke",
        "america",
        Some(&expected_session_id),
    );
    let second_created = create_task_over_websocket(
        &ws_url,
        "task-second",
        "Second queued task",
        "Reply with nucleus-smoke",
        "america",
        Some(&expected_session_id),
    );
    let first_task_id = first_created["task_id"].as_str().expect("first task id").to_owned();
    let second_task_id = second_created["task_id"].as_str().expect("second task id").to_owned();

    let _running = wait_for_cli_task_status(&ws_url, &first_task_id, "running");
    let second_queued =
        wait_for_cli_task_predicate(&ws_url, &second_task_id, Duration::from_secs(5), |task| {
            task["status"] == "queued"
        });
    assert_eq!(second_queued["status"], "queued");

    let first_done = wait_for_cli_task_status(&ws_url, &first_task_id, "done");
    let second_done = wait_for_cli_task_status(&ws_url, &second_task_id, "done");

    let first_run_id = first_done["latest_run_id"].as_str().expect("first latest run id");
    let second_run_id = second_done["latest_run_id"].as_str().expect("second latest run id");
    let first_run = inspect_run_via_cli(&ws_url, first_run_id);
    let second_run = inspect_run_via_cli(&ws_url, second_run_id);
    assert_eq!(first_run["session_id"], expected_session_id);
    assert_eq!(second_run["session_id"], expected_session_id);

    let events = load_event_log_values(&state_root);
    let first_started_seq = events
        .iter()
        .find(|event| event["type"] == "run.started" && event["data"]["task_id"] == first_task_id)
        .and_then(|event| event["seq"].as_u64())
        .expect("first run.started seq");
    let first_completed_seq = events
        .iter()
        .find(|event| event["type"] == "run.completed" && event["data"]["task_id"] == first_task_id)
        .and_then(|event| event["seq"].as_u64())
        .expect("first run.completed seq");
    let second_started_seq = events
        .iter()
        .find(|event| event["type"] == "run.started" && event["data"]["task_id"] == second_task_id)
        .and_then(|event| event["seq"].as_u64())
        .expect("second run.started seq");
    assert!(first_started_seq < first_completed_seq);
    assert!(first_completed_seq < second_started_seq);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_websocket_reconnect_observes_active_run_completion_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(200),
        Duration::from_millis(250),
        &["alpha", "beta", "gamma"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());

    let mut first_socket = subscribe_to_events(&ws_url);
    let created =
        create_task_via_cli(&ws_url, "Reconnect run task", "Reply with alphabet chunks", "america");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let mut saw_active_run_event = false;
    for _ in 0..8 {
        let event = read_websocket_json(&mut first_socket);
        if event["type"] == "run.started" && event["data"]["task_id"] == task_id {
            saw_active_run_event = true;
            break;
        }
        if event["type"] == "run.output_delta" && event["data"]["task_id"] == task_id {
            saw_active_run_event = true;
            break;
        }
    }
    assert!(saw_active_run_event, "did not observe the run becoming active before reconnect");
    first_socket.close(None).expect("close first websocket");

    let mut second_socket = subscribe_to_events(&ws_url);
    let mut saw_completion = false;
    for _ in 0..12 {
        let event = read_websocket_json(&mut second_socket);
        if event["type"] == "run.completed" && event["data"]["task_id"] == task_id {
            saw_completion = true;
            break;
        }
    }
    assert!(saw_completion, "did not observe run.completed after websocket reconnect");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(task["checkpoint_summary"], "alphabetagamma");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_run_streams_output_and_finishes_on_expected_profile() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(150),
        Duration::from_millis(120),
        &["alpha", "beta", "gamma"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let mut socket = subscribe_to_events(&ws_url);
    let created = create_task_over_websocket(
        &ws_url,
        "task-stream-live",
        "Stream live run task",
        "Reply with alphabet chunks",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let mut saw_started = false;
    let mut saw_output = false;
    let mut saw_completed = false;
    for _ in 0..16 {
        let event = read_websocket_json(&mut socket);
        if event["data"]["task_id"] != task_id {
            continue;
        }
        match event["type"].as_str() {
            Some("run.started") => saw_started = true,
            Some("run.output_delta") => saw_output = true,
            Some("run.completed") => {
                saw_completed = true;
                break;
            }
            _ => {}
        }
    }
    assert!(saw_started, "did not observe run.started");
    assert!(saw_output, "did not observe run.output_delta");
    assert!(saw_completed, "did not observe run.completed");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let run_id = task["latest_run_id"].as_str().expect("latest run id");
    let run = inspect_run_via_cli(&ws_url, run_id);
    let session = inspect_session_via_cli(&ws_url, &session_id);
    let worker = inspect_worker_via_cli(&ws_url, &worker_id);
    assert_eq!(task["profile"], "america");
    assert_eq!(task["status"], "done");
    assert_eq!(task["checkpoint_summary"], "alphabetagamma");
    assert_eq!(run["status"], "completed");
    assert_eq!(run["session_id"], session_id);
    assert_eq!(session["summary_state"], "alphabetagamma");
    assert_eq!(worker["worker"]["profile"], "america");
    assert_eq!(worker["worker"]["status"], "ready");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_probe_persists_worker_state_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "probe",
            "america",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let probed: Value = serde_json::from_slice(&output).expect("parse worker probe");
    let worker_id = probed["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    assert_eq!(probed["worker"]["profile"], "america");
    assert_eq!(probed["worker"]["status"], "ready");
    assert_eq!(probed["probe"]["status"], "ready");

    let inspected = wait_for_worker_status(&ws_url, &worker_id, "ready");
    assert_eq!(inspected["worker"]["profile"], "america");

    let worker_summary = fs::read_to_string(
        state_root.join("state").join("workers").join(&worker_id).join("summary.md"),
    )
    .expect("read worker summary");
    assert!(worker_summary.contains("# Worker"));
    assert!(worker_summary.contains("Profile: `america`"));
    assert!(worker_summary.contains("Status: `ready`"));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_list_and_inspect_reflect_live_workers_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let europe_codex_home = home_dir.join(".si/codex/profiles/europe");

    let america = create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &america_codex_home,
        temp.path(),
    );
    let europe = create_session_via_cli_with_options(
        &ws_url,
        "europe",
        None,
        None,
        &home_dir,
        &europe_codex_home,
        temp.path(),
    );

    let america_worker_id = america["worker"]["worker_id"].as_str().expect("america worker id");
    let europe_worker_id = europe["worker"]["worker_id"].as_str().expect("europe worker id");

    let listed_output = cargo_bin()
        .args(["nucleus", "worker", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let workers: Value = serde_json::from_slice(&listed_output).expect("parse worker list");
    let workers = workers.as_array().expect("worker list array");
    assert_eq!(workers.len(), 2);
    assert!(workers.iter().any(|worker| {
        worker["worker_id"] == america_worker_id
            && worker["profile"] == "america"
            && worker["status"] == "ready"
    }));
    assert!(workers.iter().any(|worker| {
        worker["worker_id"] == europe_worker_id
            && worker["profile"] == "europe"
            && worker["status"] == "ready"
    }));

    let inspected = inspect_worker_via_cli(&ws_url, america_worker_id);
    assert_eq!(inspected["worker"]["worker_id"], america_worker_id);
    assert_eq!(inspected["worker"]["profile"], "america");
    assert_eq!(inspected["worker"]["status"], "ready");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_list_and_show_reflect_live_sessions_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let europe_codex_home = home_dir.join(".si/codex/profiles/europe");

    let america = create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &america_codex_home,
        temp.path(),
    );
    let europe = create_session_via_cli_with_options(
        &ws_url,
        "europe",
        None,
        None,
        &home_dir,
        &europe_codex_home,
        temp.path(),
    );

    let america_session_id = america["session"]["session_id"].as_str().expect("america session id");
    let europe_session_id = europe["session"]["session_id"].as_str().expect("europe session id");

    let list_output = cargo_bin()
        .args(["nucleus", "session", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&list_output).expect("parse session list");
    let sessions = listed.as_array().expect("session list array");
    assert!(sessions.iter().any(|session| {
        session["session_id"] == america_session_id && session["profile"] == "america"
    }));
    assert!(sessions.iter().any(|session| {
        session["session_id"] == europe_session_id && session["profile"] == "europe"
    }));

    let shown = inspect_session_via_cli(&ws_url, america_session_id);
    assert_eq!(shown["session_id"], america_session_id);
    assert_eq!(shown["profile"], "america");
    assert_eq!(shown["worker_id"], america["worker"]["worker_id"]);
    assert_eq!(shown["app_server_thread_id"], america["session"]["app_server_thread_id"]);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_inspect_reflects_live_completed_run_on_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-run-inspect-live",
        "Inspect live run state",
        "Reply with nucleus-smoke",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let run_id = task["latest_run_id"].as_str().expect("latest run id");

    let run = inspect_run_via_cli(&ws_url, run_id);
    assert_eq!(run["run_id"], run_id);
    assert_eq!(run["task_id"], task_id);
    assert_eq!(run["session_id"], session_id);
    assert_eq!(run["status"], "completed");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_create_reuses_worker_and_codex_home_per_profile_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let first_codex_home = home_dir.join(".si/codex/profiles/america");
    let second_codex_home = temp.path().join("other/.si/codex/profiles/america-shadow");

    let first = create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &first_codex_home,
        temp.path().join("work-a").as_path(),
    );
    let second = create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &temp.path().join("other"),
        &second_codex_home,
        temp.path().join("work-b").as_path(),
    );

    assert_eq!(runtime.start_call_count(), 1);
    assert_eq!(first["worker"]["worker_id"], second["worker"]["worker_id"]);
    assert_eq!(
        second["worker"]["codex_home"],
        Value::String(first_codex_home.display().to_string())
    );

    let list_output = cargo_bin()
        .args(["nucleus", "session", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&list_output).expect("parse session list");
    assert_eq!(listed.as_array().expect("session list").len(), 2);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_create_prefers_stable_lexical_worker_id_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    for worker_suffix in ["b", "a"] {
        let codex_home = home_dir.join(format!(".si/codex/profiles/america-{worker_suffix}"));
        cargo_bin()
            .args([
                "nucleus",
                "worker",
                "probe",
                "america",
                "--worker-id",
                &format!("si-worker-{worker_suffix}"),
                "--home-dir",
                home_dir.to_str().expect("home dir"),
                "--codex-home",
                codex_home.to_str().expect("codex home"),
                "--workdir",
                temp.path().to_str().expect("workdir"),
                "--endpoint",
                &ws_url,
                "--format",
                "json",
            ])
            .assert()
            .success();
    }

    let session = create_session_via_cli_with_options(
        &ws_url,
        "america",
        None,
        None,
        &home_dir,
        &home_dir.join(".si/codex/profiles/america"),
        temp.path(),
    );
    assert_eq!(session["worker"]["worker_id"], "si-worker-a");
    let worker_list_output = cargo_bin()
        .args(["nucleus", "worker", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let workers: Value = serde_json::from_slice(&worker_list_output).expect("parse worker list");
    let workers = workers.as_array().expect("worker list array");
    assert_eq!(workers.len(), 2);
    assert!(workers.iter().any(|worker| worker["worker_id"] == "si-worker-a"));
    assert!(workers.iter().any(|worker| worker["worker_id"] == "si-worker-b"));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_resume_reuses_worker_thread_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let first = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = first["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    let thread_id =
        first["session"]["app_server_thread_id"].as_str().expect("thread id").to_owned();

    let resumed = create_session_via_cli_with_options(
        &ws_url,
        "america",
        Some(&worker_id),
        Some(&thread_id),
        &home_dir,
        &codex_home,
        temp.path(),
    );
    let resumed_session_id =
        resumed["session"]["session_id"].as_str().expect("session id").to_owned();
    assert_eq!(resumed["worker"]["worker_id"], worker_id);
    assert_eq!(resumed["session"]["worker_id"], worker_id);
    assert_eq!(resumed["session"]["app_server_thread_id"], thread_id);

    let created = create_task_over_websocket(
        &ws_url,
        "task-session-resume",
        "Resume session task",
        "Reply with nucleus-smoke",
        "america",
        Some(&resumed_session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let run_id = task["latest_run_id"].as_str().expect("latest run id");
    let run = inspect_run_via_cli(&ws_url, run_id);
    let session = inspect_session_via_cli(&ws_url, &resumed_session_id);
    assert_eq!(task["checkpoint_summary"], "nucleus-smoke");
    assert_eq!(run["session_id"], resumed_session_id);
    assert_eq!(session["app_server_thread_id"], thread_id);
    assert_eq!(session["summary_state"], "nucleus-smoke");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_session_create_does_not_reuse_session_with_conflicting_active_run_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_config(TestRuntimeConfig {
        run_delay: Duration::from_millis(900),
        step_delay: Duration::from_millis(0),
        output_deltas: vec!["nucleus-smoke".to_owned()],
        fail_execute: false,
        fail_execute_prompts: Vec::new(),
        block_when_worker_missing: false,
        fail_start_worker: false,
        fail_ensure_session: false,
    });
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let first = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let first_session_id =
        first["session"]["session_id"].as_str().expect("first session id").to_owned();
    let worker_id = first["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-conflicting-active-session-live",
        "Conflicting active run",
        "Keep the first session busy",
        "america",
        Some(&first_session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    let second = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let second_session_id =
        second["session"]["session_id"].as_str().expect("second session id").to_owned();
    let second_worker_id = second["worker"]["worker_id"].as_str().expect("second worker id");

    assert_ne!(second_session_id, first_session_id);
    assert_eq!(second_worker_id, worker_id);

    let run = inspect_run_via_cli(&ws_url, &run_id);
    assert_eq!(run["session_id"], first_session_id);

    let completed = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(completed["checkpoint_summary"], "nucleus-smoke");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_blocks_when_referenced_session_is_missing_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let created = create_task_over_websocket(
        &ws_url,
        "task-missing-session-live",
        "Missing session task",
        "Attempt to route through a missing session",
        "america",
        Some("si-session-missing"),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "session_broken");
    assert!(task["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_blocks_behind_non_reusable_session_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    write_live_session_lifecycle_state(&state_root, &session_id, "broken");

    let created = create_task_over_websocket(
        &ws_url,
        "task-broken-session-live",
        "Broken session task",
        "Attempt to route through a broken session",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "session_broken");
    assert!(task["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_blocks_when_session_profile_mismatches_task_profile_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-session-mismatch-live",
        "Mismatched task profile",
        "Attempt cross-profile session reuse",
        "europe",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "session_broken");
    assert!(task["latest_run_id"].is_null());

    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(session["profile"], "america");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_marks_session_broken_when_referenced_session_lacks_thread_id_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    clear_live_session_thread_id(&state_root, &session_id);

    let created = create_task_over_websocket(
        &ws_url,
        "task-missing-thread-live",
        "Missing thread task",
        "Attempt to route through a session without an app server thread id",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(task["blocked_reason"], "session_broken");
    assert!(task["latest_run_id"].is_null());

    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(session["lifecycle_state"], "broken");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_submit_turn_rejects_session_profile_mismatch_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &america_codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-live-run-profile-mismatch",
        "Direct run mismatch",
        "Attempt cross-profile run submission",
        "europe",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let submit = cargo_bin()
        .args([
            "nucleus",
            "run",
            "submit-turn",
            &session_id,
            "Submit a mismatched direct run",
            "--task-id",
            &task_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8(submit.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("task profile does not match session profile"));

    let task = wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
        task["latest_run_id"].is_null()
            && (task["status"] == "queued"
                || (task["status"] == "blocked" && task["blocked_reason"] == "session_broken"))
    });
    assert!(task["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_submit_turn_marks_session_broken_when_thread_id_is_missing_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    clear_live_session_thread_id(&state_root, &session_id);

    let created = create_task_over_websocket(
        &ws_url,
        "task-live-run-missing-thread",
        "Direct run missing thread",
        "Attempt direct run with no app server thread id",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let submit = cargo_bin()
        .args([
            "nucleus",
            "run",
            "submit-turn",
            &session_id,
            "attempt direct run without thread id",
            "--task-id",
            &task_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8(submit.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("session missing app-server thread id"));

    let task = wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
        task["latest_run_id"].is_null()
            && (task["status"] == "blocked" && task["blocked_reason"] == "session_broken")
    });
    assert!(task["latest_run_id"].is_null());

    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(session["lifecycle_state"], "broken");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_submit_turn_failure_before_run_started_marks_run_and_task_failed_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_config(TestRuntimeConfig {
        run_delay: Duration::from_millis(900),
        step_delay: Duration::from_millis(0),
        output_deltas: vec!["nucleus-smoke".to_owned()],
        fail_execute: false,
        fail_execute_prompts: vec!["fail before run.started".to_owned()],
        block_when_worker_missing: false,
        fail_start_worker: false,
        fail_ensure_session: false,
    });
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let active = create_task_over_websocket(
        &ws_url,
        "task-live-run-fail-busy-session",
        "Keep the session busy",
        "Keep the session busy before a direct run failure",
        "america",
        Some(&session_id),
    );
    let active_task_id = active["task_id"].as_str().expect("active task id").to_owned();
    let _running = wait_for_cli_task_status(&ws_url, &active_task_id, "running");

    let created = create_task_over_websocket(
        &ws_url,
        "task-live-run-fail-before-start",
        "Direct run pre-start failure",
        "Attempt a direct run that fails before run.started",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let queued = wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
        task["status"] == "queued" && task["latest_run_id"].is_null()
    });
    assert!(queued["latest_run_id"].is_null());

    let submit_output = cargo_bin()
        .args([
            "nucleus",
            "run",
            "submit-turn",
            &session_id,
            "fail before run.started",
            "--task-id",
            &task_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let submitted: Value = serde_json::from_slice(&submit_output).expect("parse run submit");
    let run_id = submitted["run_id"].as_str().expect("run id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "failed");
    let run = inspect_run_via_cli(&ws_url, &run_id);
    assert_eq!(task["latest_run_id"], run_id);
    assert_eq!(run["status"], "failed");
    let completed = wait_for_cli_task_status(&ws_url, &active_task_id, "done");
    assert_eq!(completed["checkpoint_summary"], "nucleus-smoke");

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "run.failed"
            && event["data"]["run_id"] == run_id
            && event["data"]["task_id"] == task_id
    }));
    assert!(!events.iter().any(|event| {
        event["type"] == "run.started"
            && event["data"]["run_id"] == run_id
            && event["data"]["task_id"] == task_id
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_cancel_transitions_blocked_task_to_cancelled_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let session_path =
        state_root.join("state").join("sessions").join(&session_id).join("session.json");
    let mut persisted_session: Value =
        serde_json::from_slice(&fs::read(&session_path).expect("read session json"))
            .expect("parse session json");
    persisted_session["app_server_thread_id"] = Value::Null;
    fs::write(
        &session_path,
        serde_json::to_vec_pretty(&persisted_session).expect("serialize session json"),
    )
    .expect("write session json");

    let created = create_task_over_websocket(
        &ws_url,
        "task-live-blocked-cancel",
        "Cancel blocked task",
        "Create a blocked task before cancellation",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "session_broken");
    assert!(blocked["latest_run_id"].is_null());

    let cancelled_output = cargo_bin()
        .args(["nucleus", "task", "cancel", &task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cancelled: Value = serde_json::from_slice(&cancelled_output).expect("parse task cancel");
    assert!(cancelled["task"]["status"] == "cancelled" || cancelled["task"]["status"] == "blocked");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    assert!(task["blocked_reason"].is_null());
    assert!(task["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_prune_removes_only_old_terminal_tasks_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(700),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let old_done =
        create_task_via_cli(&ws_url, "Old done task", "Reply with nucleus-smoke", "america");
    let old_done_id = old_done["task_id"].as_str().expect("old done id").to_owned();
    let _done = wait_for_cli_task_status(&ws_url, &old_done_id, "done");
    write_live_task_updated_at(&state_root, &old_done_id, Utc::now() - chrono::Duration::days(45));

    let recent_done =
        create_task_via_cli(&ws_url, "Recent done task", "Reply with nucleus-smoke", "america");
    let recent_done_id = recent_done["task_id"].as_str().expect("recent done id").to_owned();
    let _recent = wait_for_cli_task_status(&ws_url, &recent_done_id, "done");

    let active = create_task_over_websocket(
        &ws_url,
        "task-prune-active",
        "Active prune guard",
        "Reply with nucleus-smoke",
        "america",
        Some(&session_id),
    );
    let active_task_id = active["task_id"].as_str().expect("active task id").to_owned();
    let _running = wait_for_cli_task_status(&ws_url, &active_task_id, "running");

    let queued = create_task_over_websocket(
        &ws_url,
        "task-prune-queued",
        "Queued prune guard",
        "Reply with nucleus-smoke later",
        "america",
        Some(&session_id),
    );
    let queued_task_id = queued["task_id"].as_str().expect("queued task id").to_owned();
    let queued =
        wait_for_cli_task_predicate(&ws_url, &queued_task_id, Duration::from_secs(2), |task| {
            task["status"] == "queued" && task["latest_run_id"].is_null()
        });
    assert!(queued["latest_run_id"].is_null());
    write_live_task_updated_at(
        &state_root,
        &queued_task_id,
        Utc::now() - chrono::Duration::days(45),
    );

    let pruned_output = cargo_bin()
        .args([
            "nucleus",
            "task",
            "prune",
            "--older-than-days",
            "30",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pruned: Value = serde_json::from_slice(&pruned_output).expect("parse task prune");
    assert_eq!(pruned["pruned_task_ids"], json!([old_done_id]));

    assert!(!state_root.join("state").join("tasks").join(&old_done_id).join("task.json").exists());
    assert!(
        state_root.join("state").join("tasks").join(&recent_done_id).join("task.json").exists()
    );
    assert!(
        state_root.join("state").join("tasks").join(&queued_task_id).join("task.json").exists()
    );

    let completed = wait_for_cli_task_status(&ws_url, &active_task_id, "done");
    assert_eq!(completed["checkpoint_summary"], "nucleus-smoke");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_app_server_init_failure_blocks_task_and_breaks_session_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    runtime.set_fail_ensure_session(true);
    let created = create_task_over_websocket(
        &ws_url,
        "task-session-init-fail",
        "Session init failure task",
        "Reply with nucleus-smoke",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(task["blocked_reason"], "session_broken");
    assert!(task["latest_run_id"].is_null());
    assert_eq!(session["lifecycle_state"], "broken");

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "task.blocked"
            && event["data"]["task_id"] == task_id
            && event["data"]["payload"]["blocked_reason"] == "session_broken"
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_startup_isolates_malformed_task_state_and_keeps_live_gateway_usable() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let broken_task_dir = state_root.join("state").join("tasks").join("broken-task");
    fs::create_dir_all(&broken_task_dir).expect("create broken task dir");
    fs::write(broken_task_dir.join("task.json"), b"{\"task_id\":").expect("write broken task file");

    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let status_output = cargo_bin()
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status_output).expect("parse status output");
    assert_eq!(status["task_count"], 0);
    assert!(status["next_event_seq"].as_u64().expect("next event seq") >= 2);

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "system.warning"
            && event["data"]["payload"]["message"]
                == "isolated malformed persisted object during startup recovery"
            && event["data"]["payload"]["details"]["kind"] == "task"
            && event["data"]["payload"]["details"]["path"]
                .as_str()
                .expect("warning path")
                .ends_with("task.json")
    }));

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let created =
        create_task_via_cli(&ws_url, "Recovery task", "Reply with nucleus-smoke", "america");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(task["checkpoint_summary"], "nucleus-smoke");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_cancel_projects_cancelled_state_consistently_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(900),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id");

    let created = create_task_over_websocket(
        &ws_url,
        "task-cancel-live",
        "Cancel live run",
        "Generate enough output to be cancellable",
        "america",
        Some(session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    cargo_bin()
        .args(["nucleus", "run", "cancel", &run_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    let run = inspect_run_via_cli(&ws_url, &run_id);
    assert_eq!(task["status"], "cancelled");
    assert_eq!(run["status"], "cancelled");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_run_cancel_marks_session_broken_when_thread_id_is_missing_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(900),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-run-cancel-missing-thread-live",
        "Cancel run after thread loss",
        "Generate enough output to be cancellable",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    clear_live_session_thread_id(&state_root, &session_id);

    let cancelled_output = cargo_bin()
        .args(["nucleus", "run", "cancel", &run_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cancelled: Value = serde_json::from_slice(&cancelled_output).expect("parse run cancel");
    assert_eq!(cancelled["status"], "cancelled");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    let run = inspect_run_via_cli(&ws_url, &run_id);
    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(task["status"], "cancelled");
    assert_eq!(run["status"], "cancelled");
    assert_eq!(session["lifecycle_state"], "broken");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_restarts_idle_worker_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    let initial_start_calls = runtime.start_call_count();

    let restarted_output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let restarted: Value = serde_json::from_slice(&restarted_output).expect("parse restart");
    assert_eq!(restarted["worker"]["worker_id"], worker_id);
    assert_eq!(restarted["worker"]["status"], "ready");
    assert_eq!(restarted["runtime"]["worker_id"], worker_id);
    assert_eq!(runtime.start_call_count(), initial_start_calls + 1);

    let inspected = wait_for_worker_status(&ws_url, &worker_id, "ready");
    assert_eq!(inspected["worker"]["status"], "ready");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_reprobes_and_starts_missing_runtime_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let probe_output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "probe",
            "america",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let probed: Value = serde_json::from_slice(&probe_output).expect("parse worker probe");
    let worker_id = probed["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    assert_eq!(runtime.start_call_count(), 0);

    runtime
        .stop_worker(&WorkerId::new(worker_id.clone()).expect("worker id"))
        .expect("stop missing runtime worker");

    let repaired_output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let repaired: Value = serde_json::from_slice(&repaired_output).expect("parse repair auth");
    assert_eq!(repaired["worker"]["worker_id"], worker_id);
    assert_eq!(repaired["worker"]["status"], "ready");
    assert_eq!(repaired["runtime"]["worker_id"], worker_id);
    assert_eq!(runtime.start_call_count(), 1);

    let inspected = wait_for_worker_status(&ws_url, &worker_id, "ready");
    assert_eq!(inspected["worker"]["status"], "ready");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_cancel_marks_session_broken_when_thread_id_is_missing_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(900),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-task-cancel-missing-thread-live",
        "Cancel task after thread loss",
        "Generate enough output to be cancellable",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    clear_live_session_thread_id(&state_root, &session_id);

    let cancelled_output = cargo_bin()
        .args(["nucleus", "task", "cancel", &task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let cancelled: Value = serde_json::from_slice(&cancelled_output).expect("parse task cancel");
    assert!(cancelled["task"]["status"] == "cancelled" || cancelled["task"]["status"] == "blocked");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    let start = Instant::now();
    let run = loop {
        let run = inspect_run_via_cli(&ws_url, &run_id);
        if run["status"] == "cancelled" {
            break run;
        }
        assert!(start.elapsed() < Duration::from_secs(5), "run {run_id} did not reach cancelled");
        thread::sleep(Duration::from_millis(50));
    };
    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(task["status"], "cancelled");
    assert_eq!(run["status"], "cancelled");
    assert_eq!(session["lifecycle_state"], "broken");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_loss_blocks_task_run_and_worker_projections_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_config(TestRuntimeConfig {
        run_delay: Duration::from_millis(900),
        step_delay: Duration::from_millis(0),
        output_deltas: vec!["nucleus-smoke".to_owned()],
        fail_execute: false,
        fail_execute_prompts: Vec::new(),
        block_when_worker_missing: true,
        fail_start_worker: false,
        fail_ensure_session: false,
    });
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-worker-loss",
        "Worker loss task",
        "Keep running until the worker disappears",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    runtime.set_fail_start_worker(true);
    runtime
        .stop_worker(&WorkerId::new(worker_id.clone()).expect("worker id"))
        .expect("stop test worker");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    let run = inspect_run_via_cli(&ws_url, &run_id);
    let worker_inspect = wait_for_worker_status(&ws_url, &worker_id, "failed");
    assert_eq!(task["blocked_reason"], "worker_unavailable");
    assert_eq!(run["status"], "blocked");
    assert_eq!(worker_inspect["worker"]["status"], "failed");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_requeues_blocked_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    runtime.set_fail_start_worker(true);
    runtime
        .stop_worker(&WorkerId::new(worker_id.clone()).expect("worker id"))
        .expect("stop test worker");

    let created = create_task_via_cli(
        &ws_url,
        "Restart live recovery task",
        "Reply with nucleus-smoke after worker restart",
        "america",
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "worker_unavailable");

    runtime.set_fail_start_worker(false);
    let restart_output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let restarted: Value = serde_json::from_slice(&restart_output).expect("parse worker restart");
    assert_eq!(restarted["worker"]["worker_id"], worker_id);
    assert_eq!(restarted["worker"]["status"], "ready");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(task["checkpoint_summary"], "nucleus-smoke");

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "task.updated"
            && event["data"]["task_id"] == task_id
            && event["data"]["payload"]["status"] == "queued"
            && event["data"]["payload"]["message"] == "task re-queued after worker restart"
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_clears_exhausted_auto_restart_boundary_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    runtime.set_fail_start_worker(true);
    runtime
        .stop_worker(&WorkerId::new(worker_id.clone()).expect("worker id"))
        .expect("stop test worker");

    let start = Instant::now();
    while start.elapsed() < Duration::from_secs(8) {
        let events = load_event_log_values(&state_root);
        if events.iter().any(|event| {
            event["type"] == "system.warning"
                && event["data"]["payload"]["message"] == "worker restart attempts exhausted"
                && event["data"]["payload"]["details"]["worker_id"] == worker_id
        }) {
            break;
        }
        thread::sleep(Duration::from_millis(50));
    }
    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "system.warning"
            && event["data"]["payload"]["message"] == "worker restart attempts exhausted"
            && event["data"]["payload"]["details"]["worker_id"] == worker_id
    }));
    let exhausted_start_calls = runtime.start_call_count();

    let created = create_task_via_cli(
        &ws_url,
        "Blocked until explicit worker restart",
        "Do not bypass exhausted auto-restart implicitly",
        "america",
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "worker_unavailable");
    assert_eq!(runtime.start_call_count(), exhausted_start_calls);

    runtime.set_fail_start_worker(false);
    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(task["checkpoint_summary"], "nucleus-smoke");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_rejects_active_run_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_config(TestRuntimeConfig {
        run_delay: Duration::from_millis(900),
        step_delay: Duration::from_millis(0),
        output_deltas: vec!["nucleus-smoke".to_owned()],
        fail_execute: false,
        fail_execute_prompts: Vec::new(),
        block_when_worker_missing: false,
        fail_start_worker: false,
        fail_ensure_session: false,
    });
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-worker-restart-active-live",
        "Do not restart active worker",
        "Keep running while restart is attempted",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    let restart = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let restart_stderr =
        String::from_utf8(restart.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(restart_stderr.contains("worker has active runs"));

    let still_running =
        wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
            task["status"] == "running" || task["status"] == "done"
        });
    assert_ne!(still_running["status"], "blocked");
    assert_ne!(still_running["status"], "cancelled");

    let run = inspect_run_via_cli(&ws_url, &run_id);
    assert_ne!(run["status"], "blocked");
    assert_ne!(run["status"], "cancelled");

    let completed = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(completed["checkpoint_summary"], "nucleus-smoke");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_clears_exhausted_auto_restart_boundary_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    runtime.set_fail_start_worker(true);
    runtime
        .stop_worker(&WorkerId::new(worker_id.clone()).expect("worker id"))
        .expect("stop test worker");

    let start = Instant::now();
    while start.elapsed() < Duration::from_secs(8) {
        let events = load_event_log_values(&state_root);
        if events.iter().any(|event| {
            event["type"] == "system.warning"
                && event["data"]["payload"]["message"] == "worker restart attempts exhausted"
                && event["data"]["payload"]["details"]["worker_id"] == worker_id
        }) {
            break;
        }
        thread::sleep(Duration::from_millis(50));
    }
    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "system.warning"
            && event["data"]["payload"]["message"] == "worker restart attempts exhausted"
            && event["data"]["payload"]["details"]["worker_id"] == worker_id
    }));

    let failed_session = cargo_bin()
        .args([
            "nucleus",
            "session",
            "create",
            "america",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let failed_session_stderr =
        String::from_utf8(failed_session.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(failed_session_stderr.contains("worker restart attempts exhausted"));

    runtime.set_fail_start_worker(false);
    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let resumed_session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    assert_eq!(resumed_session["worker"]["worker_id"], worker_id);
    assert_eq!(resumed_session["worker"]["status"], "ready");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_does_not_requeue_broken_session_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_config(TestRuntimeConfig {
        run_delay: Duration::from_millis(900),
        step_delay: Duration::from_millis(0),
        output_deltas: vec!["nucleus-smoke".to_owned()],
        fail_execute: false,
        fail_execute_prompts: Vec::new(),
        block_when_worker_missing: true,
        fail_start_worker: false,
        fail_ensure_session: false,
    });
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-worker-restart-broken-session-live",
        "Do not requeue broken-session live worker task",
        "Keep running until the worker disappears",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("latest run id").to_owned();

    runtime.set_fail_start_worker(false);
    runtime
        .stop_worker(&WorkerId::new(worker_id.clone()).expect("worker id"))
        .expect("stop test worker");

    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    let run = inspect_run_via_cli(&ws_url, &run_id);
    let session = inspect_session_via_cli(&ws_url, &session_id);
    assert_eq!(blocked["blocked_reason"], "worker_unavailable");
    assert_eq!(run["status"], "blocked");
    assert_eq!(session["lifecycle_state"], "broken");

    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let still_blocked =
        wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
            task["status"] == "blocked"
        });
    assert_eq!(still_blocked["blocked_reason"], "worker_unavailable");
    assert_eq!(still_blocked["latest_run_id"].as_str().expect("latest run id"), run_id);
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_requeues_blocked_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created = create_task_over_websocket(
        &ws_url,
        "task-auth-requeue-live",
        "Repair auth live recovery task",
        "Use si fort status before continuing",
        "america",
        None,
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "auth_required");

    write_fort_session_state(&codex_home, "america");
    let repair_output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let repaired: Value = serde_json::from_slice(&repair_output).expect("parse worker repair");
    assert_eq!(repaired["worker"]["worker_id"], worker_id);
    assert_eq!(repaired["worker"]["status"], "ready");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(task["checkpoint_summary"], "nucleus-smoke");

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "task.updated"
            && event["data"]["task_id"] == task_id
            && event["data"]["payload"]["status"] == "queued"
            && event["data"]["payload"]["message"] == "task re-queued after worker auth repair"
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_requeues_fort_unavailable_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    write_invalid_fort_session_state(&codex_home);
    let created = create_task_over_websocket(
        &ws_url,
        "task-fort-unavailable-requeue-live",
        "Repair unavailable Fort live recovery task",
        "Use si fort refresh before continuing",
        "america",
        None,
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "fort_unavailable");

    write_fort_session_state(&codex_home, "america");
    let repair_output = cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let repaired: Value = serde_json::from_slice(&repair_output).expect("parse worker repair");
    assert_eq!(repaired["worker"]["worker_id"], worker_id);
    assert_eq!(repaired["worker"]["status"], "ready");

    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    assert_eq!(task["checkpoint_summary"], "nucleus-smoke");

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "task.updated"
            && event["data"]["task_id"] == task_id
            && event["data"]["payload"]["status"] == "queued"
            && event["data"]["payload"]["message"] == "task re-queued after worker auth repair"
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_refreshes_profile_state_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let profile_path = state_root.join("state").join("profiles").join("america.json");
    fs::write(
        &profile_path,
        serde_json::to_vec_pretty(&json!({
            "profile": "america",
            "account_identity": "stale@example.com",
            "codex_home": temp.path().join("stale/.si/codex/profiles/america").display().to_string(),
            "auth_mode": "stale-auth",
            "preferred_model": "stale-model",
            "runtime_defaults": { "model": "stale-model" }
        }))
        .expect("serialize stale profile"),
    )
    .expect("persist stale profile");

    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let profiles_output = cargo_bin()
        .args(["nucleus", "profile", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let profiles: Value = serde_json::from_slice(&profiles_output).expect("parse profile list");
    let profile = profiles
        .as_array()
        .expect("profile list array")
        .iter()
        .find(|profile| profile["profile"] == "america")
        .expect("america profile");
    assert_eq!(profile["account_identity"], "america@example.com");
    assert_eq!(profile["preferred_model"], "gpt-5.4");
    assert_eq!(profile["codex_home"], codex_home.display().to_string());
    assert!(profile["auth_mode"].is_null());
    assert_eq!(profile["runtime_defaults"], json!({}));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_restart_does_not_requeue_other_profile_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let america_session =
        create_session_via_cli(&ws_url, &home_dir, &america_codex_home, temp.path());
    let america_worker_id =
        america_session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    runtime.set_fail_start_worker(true);
    let britain_created = create_task_via_cli(
        &ws_url,
        "Do not requeue other profile live task",
        "Stay blocked until a britain worker is repaired or restarted",
        "britain",
    );
    let britain_task_id = britain_created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &britain_task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "worker_unavailable");

    runtime.set_fail_start_worker(false);
    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "restart",
            &america_worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let still_blocked =
        wait_for_cli_task_predicate(&ws_url, &britain_task_id, Duration::from_secs(2), |task| {
            task["status"] == "blocked"
        });
    assert_eq!(still_blocked["blocked_reason"], "worker_unavailable");
    assert!(still_blocked["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_does_not_requeue_session_broken_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(runtime.clone()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let worker_id = session["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();

    runtime.set_fail_ensure_session(true);
    let created = create_task_over_websocket(
        &ws_url,
        "task-broken-auth-live",
        "Do not requeue broken session live task",
        "Use si fort status before continuing",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "session_broken");

    runtime.set_fail_ensure_session(false);
    write_fort_session_state(&codex_home, "america");
    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let still_blocked =
        wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
            task["status"] == "blocked"
        });
    assert_eq!(still_blocked["blocked_reason"], "session_broken");
    assert!(still_blocked["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_worker_repair_auth_does_not_requeue_other_profile_task_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let america_codex_home = home_dir.join(".si/codex/profiles/america");
    let britain_codex_home = home_dir.join(".si/codex/profiles/britain");

    let america_session =
        create_session_via_cli(&ws_url, &home_dir, &america_codex_home, temp.path());
    let america_worker_id =
        america_session["worker"]["worker_id"].as_str().expect("worker id").to_owned();
    create_session_via_cli_with_options(
        &ws_url,
        "britain",
        None,
        None,
        &home_dir,
        &britain_codex_home,
        temp.path(),
    );

    let created = create_task_over_websocket(
        &ws_url,
        "task-other-profile-auth-live",
        "Do not requeue other profile auth task",
        "Use si fort status before continuing",
        "britain",
        None,
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let blocked = wait_for_cli_task_status(&ws_url, &task_id, "blocked");
    assert_eq!(blocked["blocked_reason"], "auth_required");

    write_fort_session_state(&america_codex_home, "america");
    cargo_bin()
        .args([
            "nucleus",
            "worker",
            "repair-auth",
            &america_worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let still_blocked =
        wait_for_cli_task_predicate(&ws_url, &task_id, Duration::from_secs(2), |task| {
            task["status"] == "blocked"
        });
    assert_eq!(still_blocked["blocked_reason"], "auth_required");
    assert!(still_blocked["latest_run_id"].is_null());
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_gateway_auth_requires_token_for_mutations_but_not_reads() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let base_url = spawn_live_nucleus_service_with_options(
        &state_root,
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        None,
    );
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success();

    let mutation = cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args([
            "nucleus",
            "task",
            "create",
            "Auth gated task",
            "This should require a bearer token",
            "--profile",
            "america",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8(mutation.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("unauthorized: missing bearer token"));

    let created = cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args([
            "nucleus",
            "task",
            "create",
            "Auth gated task",
            "This should succeed with a bearer token",
            "--profile",
            "america",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&created).expect("parse task create");
    let task_id = payload["task_id"].as_str().expect("task id");
    assert_eq!(payload["title"], "Auth gated task");

    let listed = cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "task", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&listed).expect("parse task list");
    assert!(
        listed
            .as_array()
            .expect("task list array")
            .iter()
            .any(|task| task["task_id"] == payload["task_id"])
    );

    cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "task", "inspect", task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success();
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_gateway_read_surfaces_remain_available_without_token() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::default();
    let base_url = spawn_live_nucleus_service_with_options(
        &state_root,
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(runtime)),
    );
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let created_session = cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args([
            "nucleus",
            "session",
            "create",
            "america",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let created_session: Value =
        serde_json::from_slice(&created_session).expect("parse created session");
    let session_id =
        created_session["session"]["session_id"].as_str().expect("session id").to_owned();
    let worker_id = created_session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created_task = cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args([
            "nucleus",
            "task",
            "create",
            "Auth readable task",
            "Reply with nucleus-smoke",
            "--profile",
            "america",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let created_task: Value = serde_json::from_slice(&created_task).expect("parse created task");
    let task_id = created_task["task_id"].as_str().expect("task id").to_owned();
    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let run_id = task["latest_run_id"].as_str().expect("latest run id").to_owned();

    let profiles = cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "profile", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let profiles: Value = serde_json::from_slice(&profiles).expect("parse profile list");
    assert!(profiles.as_array().expect("profile list array").iter().any(|profile| {
        profile["profile"] == "america" && profile["account_identity"] == "america@example.com"
    }));

    let workers = cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "worker", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let workers: Value = serde_json::from_slice(&workers).expect("parse worker list");
    assert!(
        workers
            .as_array()
            .expect("worker list array")
            .iter()
            .any(|worker| worker["worker_id"] == worker_id)
    );

    cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args([
            "nucleus",
            "worker",
            "inspect",
            &worker_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let sessions = cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "session", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let sessions: Value = serde_json::from_slice(&sessions).expect("parse session list");
    assert!(
        sessions
            .as_array()
            .expect("session list array")
            .iter()
            .any(|session| session["session_id"] == session_id)
    );

    cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args([
            "nucleus",
            "session",
            "show",
            &session_id,
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "run", "inspect", &run_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success();

    cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args([
            "nucleus",
            "events",
            "subscribe",
            "--count",
            "0",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_rest_read_surfaces_remain_available_without_token_and_writes_require_token() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let base_url = spawn_live_nucleus_service_with_options(
        &state_root,
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(TestRuntime::default())),
    );
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));
    let client = BlockingClient::new();

    let openapi_response =
        client.get(format!("{base_url}/openapi.json")).send().expect("openapi response");
    assert!(openapi_response.status().is_success());

    let status_response = client.get(format!("{base_url}/status")).send().expect("status response");
    assert!(status_response.status().is_success());

    let unauthorized_create = client
        .post(format!("{base_url}/tasks"))
        .json(&json!({
            "title": "REST auth gated task",
            "instructions": "This should require a bearer token.",
            "profile": "america"
        }))
        .send()
        .expect("unauthorized rest create");
    assert_eq!(unauthorized_create.status(), reqwest::StatusCode::UNAUTHORIZED);
    let unauthorized_body: Value = unauthorized_create.json().expect("parse unauthorized body");
    assert_eq!(unauthorized_body["error"]["code"], "unauthorized");

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let created_session = cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args([
            "nucleus",
            "session",
            "create",
            "america",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let created_session: Value =
        serde_json::from_slice(&created_session).expect("parse created session");
    let session_id =
        created_session["session"]["session_id"].as_str().expect("session id").to_owned();
    let worker_id = created_session["worker"]["worker_id"].as_str().expect("worker id").to_owned();

    let created_task = client
        .post(format!("{base_url}/tasks"))
        .bearer_auth("test-token")
        .json(&json!({
            "title": "REST readable task",
            "instructions": "Reply with nucleus-smoke",
            "profile": "america"
        }))
        .send()
        .expect("authorized rest create");
    assert!(created_task.status().is_success());
    let created_task: Value = created_task.json().expect("parse created task");
    let task_id = created_task["task_id"].as_str().expect("task id").to_owned();
    let task = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let run_id = task["latest_run_id"].as_str().expect("run id").to_owned();

    let tasks: Value = client
        .get(format!("{base_url}/tasks"))
        .send()
        .expect("list tasks")
        .json()
        .expect("parse tasks");
    assert!(
        tasks.as_array().expect("task list array").iter().any(|item| item["task_id"] == task_id)
    );

    let task_inspect: Value = client
        .get(format!("{base_url}/tasks/{task_id}"))
        .send()
        .expect("inspect task")
        .json()
        .expect("parse task inspect");
    assert_eq!(task_inspect["task_id"], task_id);
    assert_eq!(task_inspect["status"], "done");

    let workers: Value = client
        .get(format!("{base_url}/workers"))
        .send()
        .expect("list workers")
        .json()
        .expect("parse workers");
    assert!(
        workers
            .as_array()
            .expect("worker list array")
            .iter()
            .any(|item| item["worker_id"] == worker_id)
    );

    let worker: Value = client
        .get(format!("{base_url}/workers/{worker_id}"))
        .send()
        .expect("inspect worker")
        .json()
        .expect("parse worker inspect");
    assert_eq!(worker["worker"]["worker_id"], worker_id);

    let session: Value = client
        .get(format!("{base_url}/sessions/{session_id}"))
        .send()
        .expect("inspect session")
        .json()
        .expect("parse session inspect");
    assert_eq!(session["session_id"], session_id);

    let run: Value = client
        .get(format!("{base_url}/runs/{run_id}"))
        .send()
        .expect("inspect run")
        .json()
        .expect("parse run inspect");
    assert_eq!(run["run_id"], run_id);
    assert_eq!(run["status"], "completed");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_openapi_document_advertises_bounded_contract() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service_with_options(
        &temp.path().join("nucleus"),
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(TestRuntime::default())),
    );
    let client = BlockingClient::new();

    let response = client.get(format!("{base_url}/openapi.json")).send().expect("openapi response");
    assert!(response.status().is_success());
    let body: Value = response.json().expect("parse openapi");

    assert_eq!(body["openapi"], json!("3.1.0"));
    assert_eq!(body["components"]["securitySchemes"]["bearerAuth"]["scheme"], json!("bearer"));
    assert_eq!(
        body["components"]["securitySchemes"]["bearerAuth"]["bearerFormat"],
        json!("opaque token")
    );
    assert_eq!(body["paths"]["/tasks"]["post"]["security"][0]["bearerAuth"], json!([]));
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["security"][0]["bearerAuth"],
        json!([])
    );
    assert!(body["paths"]["/tasks"]["get"]["security"].is_null());
    assert!(body["paths"]["/tasks/{task_id}"]["get"]["security"].is_null());
    assert!(body["paths"]["/workers"]["get"]["security"].is_null());
    assert!(body["paths"]["/workers/{worker_id}"]["get"]["security"].is_null());
    assert!(body["paths"]["/sessions/{session_id}"]["get"]["security"].is_null());
    assert!(body["paths"]["/runs/{run_id}"]["get"]["security"].is_null());
    assert!(body["paths"]["/status"]["get"]["security"].is_null());
    assert!(body["paths"]["/openapi.json"]["get"]["security"].is_null());
    assert_eq!(
        body["paths"]["/status"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/NucleusStatusView")
    );
    assert_eq!(
        body["paths"]["/tasks"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["items"]["$ref"],
        json!("#/components/schemas/TaskRecord")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["201"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/TaskRecord")
    );
    assert_eq!(body["paths"]["/tasks"]["post"]["requestBody"]["required"], json!(true));
    assert_eq!(
        body["paths"]["/tasks"]["post"]["requestBody"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/TaskCreateParams")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["401"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["400"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["responses"]["404"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["404"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["503"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["401"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/workers"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["items"]["$ref"],
        json!("#/components/schemas/WorkerRecord")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["200"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/WorkerInspectView")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["200"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/SessionRecord")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RunRecord")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["type"],
        json!("object")
    );
    assert_eq!(
        body["paths"]["/status"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    for (path, method) in [
        ("/status", "get"),
        ("/tasks", "get"),
        ("/tasks", "post"),
        ("/tasks/{task_id}", "get"),
        ("/tasks/{task_id}/cancel", "post"),
        ("/workers", "get"),
        ("/workers/{worker_id}", "get"),
        ("/sessions/{session_id}", "get"),
        ("/runs/{run_id}", "get"),
        ("/openapi.json", "get"),
    ] {
        let operation = &body["paths"][path][method];
        assert!(
            operation["summary"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
            "missing summary for {method} {path}"
        );
        assert!(
            operation["description"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
            "missing description for {method} {path}"
        );
        assert!(
            operation["x-si-purpose"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
            "missing x-si-purpose for {method} {path}"
        );
    }
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_rest_task_cancel_requires_token_and_succeeds_with_bearer() {
    let temp = tempdir().expect("tempdir");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_millis(900),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let base_url = spawn_live_nucleus_service_with_options(
        &temp.path().join("nucleus"),
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(runtime)),
    );
    let ws_url = format!("{}/ws", base_url.replacen("http", "ws", 1));
    let client = BlockingClient::new();

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args([
            "nucleus",
            "session",
            "create",
            "america",
            "--home-dir",
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            temp.path().to_str().expect("workdir"),
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let created = client
        .post(format!("{base_url}/tasks"))
        .bearer_auth("test-token")
        .json(&json!({
            "title": "REST auth cancel task",
            "instructions": "Reply with nucleus-smoke",
            "profile": "america"
        }))
        .send()
        .expect("authorized rest create");
    assert!(created.status().is_success());
    let created: Value = created.json().expect("parse created task");
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let running = wait_for_cli_task_status(&ws_url, &task_id, "running");
    let run_id = running["latest_run_id"].as_str().expect("run id").to_owned();

    let unauthorized_cancel = client
        .post(format!("{base_url}/tasks/{task_id}/cancel"))
        .send()
        .expect("unauthorized rest cancel");
    assert_eq!(unauthorized_cancel.status(), reqwest::StatusCode::UNAUTHORIZED);
    let unauthorized_body: Value = unauthorized_cancel.json().expect("parse unauthorized cancel");
    assert_eq!(unauthorized_body["error"]["code"], "unauthorized");

    let authorized_cancel = client
        .post(format!("{base_url}/tasks/{task_id}/cancel"))
        .bearer_auth("test-token")
        .send()
        .expect("authorized rest cancel");
    assert!(authorized_cancel.status().is_success());
    let authorized_cancel: Value = authorized_cancel.json().expect("parse authorized cancel");
    assert_eq!(authorized_cancel["task"]["task_id"], task_id);
    assert_eq!(authorized_cancel["run"]["run_id"], run_id);

    let cancelled_task = wait_for_cli_task_status(&ws_url, &task_id, "cancelled");
    assert_eq!(cancelled_task["status"], "cancelled");

    let cancelled_run = inspect_run_via_cli(&ws_url, &run_id);
    assert_eq!(cancelled_run["status"], "cancelled");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_rest_missing_targets_preserve_auth_split() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service_with_options(
        &temp.path().join("nucleus"),
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(TestRuntime::default())),
    );
    let client = BlockingClient::new();

    for path in [
        "/tasks/si-task-missing",
        "/workers/si-worker-missing",
        "/sessions/si-session-missing",
        "/runs/si-run-missing",
    ] {
        let response = client.get(format!("{base_url}{path}")).send().expect("missing read");
        assert_eq!(response.status(), reqwest::StatusCode::NOT_FOUND, "{path}");
        let body: Value = response.json().expect("parse missing read body");
        assert_eq!(body["error"]["code"], "not_found", "{path}");
    }

    let unauthorized_cancel = client
        .post(format!("{base_url}/tasks/si-task-missing/cancel"))
        .send()
        .expect("unauthorized missing cancel");
    assert_eq!(unauthorized_cancel.status(), reqwest::StatusCode::UNAUTHORIZED);
    let unauthorized_body: Value = unauthorized_cancel.json().expect("parse unauthorized body");
    assert_eq!(unauthorized_body["error"]["code"], "unauthorized");

    let authorized_cancel = client
        .post(format!("{base_url}/tasks/si-task-missing/cancel"))
        .bearer_auth("test-token")
        .send()
        .expect("authorized missing cancel");
    assert_eq!(authorized_cancel.status(), reqwest::StatusCode::NOT_FOUND);
    let authorized_body: Value = authorized_cancel.json().expect("parse authorized missing body");
    assert_eq!(authorized_body["error"]["code"], "not_found");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_rest_invalid_create_preserves_auth_and_validation_order() {
    let temp = tempdir().expect("tempdir");
    let base_url = spawn_live_nucleus_service_with_options(
        &temp.path().join("nucleus"),
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(TestRuntime::default())),
    );
    let client = BlockingClient::new();

    let unauthorized_create = client
        .post(format!("{base_url}/tasks"))
        .json(&json!({
            "title": "Bad profile task",
            "instructions": "Reject uppercase profile names",
            "profile": "America"
        }))
        .send()
        .expect("unauthorized invalid create");
    assert_eq!(unauthorized_create.status(), reqwest::StatusCode::UNAUTHORIZED);
    let unauthorized_body: Value = unauthorized_create.json().expect("parse unauthorized body");
    assert_eq!(unauthorized_body["error"]["code"], "unauthorized");

    let authorized_create = client
        .post(format!("{base_url}/tasks"))
        .bearer_auth("test-token")
        .json(&json!({
            "title": "Bad profile task",
            "instructions": "Reject uppercase profile names",
            "profile": "America"
        }))
        .send()
        .expect("authorized invalid create");
    assert_eq!(authorized_create.status(), reqwest::StatusCode::BAD_REQUEST);
    let authorized_body: Value = authorized_create.json().expect("parse authorized invalid body");
    assert_eq!(authorized_body["error"]["code"], "invalid_params");
    assert!(
        authorized_body["error"]["message"]
            .as_str()
            .map(|value| value.contains("profile name must match"))
            .unwrap_or(false)
    );
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_rest_error_envelopes_keep_canonical_shape() {
    let temp = tempdir().expect("tempdir");

    let auth_base_url = spawn_live_nucleus_service_with_options(
        &temp.path().join("auth-nucleus"),
        "0.0.0.0",
        "127.0.0.1",
        Some("test-token"),
        Some(Arc::new(TestRuntime::default())),
    );
    let client = BlockingClient::new();

    let unauthorized_create = client
        .post(format!("{auth_base_url}/tasks"))
        .json(&json!({
            "title": "REST unauthorized envelope task",
            "instructions": "This should require a bearer token.",
            "profile": "america"
        }))
        .send()
        .expect("unauthorized create");
    assert_eq!(unauthorized_create.status(), reqwest::StatusCode::UNAUTHORIZED);
    let unauthorized_body: Value = unauthorized_create.json().expect("parse unauthorized body");
    assert_eq!(unauthorized_body["error"]["code"], "unauthorized");
    assert!(
        unauthorized_body["error"]["message"]
            .as_str()
            .map(|value| !value.is_empty())
            .unwrap_or(false)
    );
    assert!(unauthorized_body["error"]["details"].is_null());

    let missing_task = client
        .get(format!("{auth_base_url}/tasks/si-task-missing"))
        .send()
        .expect("missing task read");
    assert_eq!(missing_task.status(), reqwest::StatusCode::NOT_FOUND);
    let missing_body: Value = missing_task.json().expect("parse missing body");
    assert_eq!(missing_body["error"]["code"], "not_found");
    assert!(
        missing_body["error"]["message"]
            .as_str()
            .map(|value| value.contains("not found"))
            .unwrap_or(false)
    );
    assert!(missing_body["error"]["details"].is_null());

    let source_state_root = temp.path().join("source-nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_secs(5),
        Duration::from_millis(0),
        &["nucleus-smoke"],
    );
    let source_ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&source_state_root, Arc::new(runtime))
            .replacen("http", "ws", 1)
    );
    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let session = create_session_via_cli(&source_ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id").to_owned();
    let created = create_task_over_websocket(
        &source_ws_url,
        "rest-envelope-unavailable-live",
        "Keep active run for unavailable envelope",
        "Keep running until cancellation is attempted",
        "america",
        Some(&session_id),
    );
    let task_id = created["task_id"].as_str().expect("task id").to_owned();
    let _running = wait_for_cli_task_status(&source_ws_url, &task_id, "running");

    let snapshot_state_root = temp.path().join("snapshot-nucleus");
    copy_dir_recursive(&source_state_root, &snapshot_state_root);
    let snapshot_base_url = spawn_live_nucleus_service(&snapshot_state_root);

    let unavailable_cancel = client
        .post(format!("{snapshot_base_url}/tasks/{task_id}/cancel"))
        .send()
        .expect("unavailable cancel");
    assert_eq!(unavailable_cancel.status(), reqwest::StatusCode::SERVICE_UNAVAILABLE);
    let unavailable_body: Value = unavailable_cancel.json().expect("parse unavailable body");
    assert_eq!(unavailable_body["error"]["code"], "unavailable");
    assert!(
        unavailable_body["error"]["message"]
            .as_str()
            .map(|value| value.contains("runtime unavailable"))
            .unwrap_or(false)
    );
    assert!(unavailable_body["error"]["details"].is_null());
}

fn shell_escape_for_test(path: &Path) -> String {
    format!("'{}'", path.display().to_string().replace('\'', "'\"'\"'"))
}

fn write_executable_shell_script(path: &Path, body: &str) {
    fs::write(path, body).expect("write shell script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(path).expect("stat shell script").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(path, perms).expect("chmod shell script");
    }
}

#[test]
fn fort_wrapper_forwards_native_command_with_si_settings_defaults() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let env_file = bin_dir.path().join("fort-env.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN=%s\\nFORT_REFRESH_TOKEN=%s\\nFORT_TOKEN_PATH=%s\\nFORT_REFRESH_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN:-}}\" \"${{FORT_REFRESH_TOKEN:-}}\" \"${{FORT_TOKEN_PATH:-}}\" \"${{FORT_REFRESH_TOKEN_PATH:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env("FORT_TOKEN", "legacy-token")
        .env("FORT_REFRESH_TOKEN", "legacy-refresh-token")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(
        args,
        format!(
            "--host\nhttps://fort.example.test\n--token-file\n{}\ndoctor\n",
            home.path().join(".si/fort/bootstrap/admin.token").display()
        )
    );
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN="));
    assert!(env.contains("FORT_REFRESH_TOKEN="));
    assert!(env.contains("FORT_TOKEN_PATH="));
    assert!(env.contains("FORT_REFRESH_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_preserves_explicit_native_flags() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let env_file = bin_dir.path().join("fort-env.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'FORT_HOST=%s\\nFORT_TOKEN_PATH=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_TOKEN_PATH:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "--",
            "--host",
            "https://override.example.test",
            "--token-file",
            "/tmp/runtime.token",
            "doctor",
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(
        args,
        "--host\nhttps://override.example.test\n--token-file\n/tmp/runtime.token\ndoctor\n"
    );
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_TOKEN_PATH="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
}

#[test]
fn fort_wrapper_refreshes_bootstrap_session_from_file_paths() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&refresh_path, "stale-refresh-token\n").expect("write stale refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-refresh-token' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-access-token\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"agent\" ] && [ \"$6\" = \"list\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-access-token'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "agent", "list"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains("--json\nauth\nsession\nrefresh\n--refresh-token-file\n"));
    assert!(args.contains(&format!("--token-file\n{}\nagent\nlist\n", token_path.display())));
    assert_eq!(
        fs::read_to_string(&token_path).expect("read refreshed admin token"),
        "rotated-access-token\n"
    );
    assert_eq!(
        fs::read_to_string(&refresh_path).expect("read rotated refresh token"),
        "rotated-refresh-token\n"
    );
}

#[test]
fn fort_wrapper_refreshes_runtime_session_from_file_paths_before_bootstrap() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(bootstrap_dir.join("admin.token"))
            .expect("stat admin token")
            .permissions();
        perms.set_mode(0o600);
        fs::set_permissions(bootstrap_dir.join("admin.token"), perms).expect("chmod admin token");
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let runtime_dir = tempdir().expect("runtime tempdir");
    let runtime_token_path = runtime_dir.path().join("access.token");
    let runtime_refresh_path = runtime_dir.path().join("refresh.token");
    fs::write(&runtime_token_path, "stale-runtime-token\n").expect("write runtime token");
    fs::write(&runtime_refresh_path, "stale-runtime-refresh\n").expect("write runtime refresh");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&runtime_token_path, &runtime_refresh_path] {
            let mut perms = fs::metadata(path).expect("stat runtime token").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod runtime token");
        }
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{refresh}\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-runtime-refresh' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-runtime-access\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"agent\" ] && [ \"$6\" = \"list\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-runtime-access'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = runtime_token_path.display(),
            refresh = runtime_refresh_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "agent", "list"])
        .env("PATH", path_env)
        .env("FORT_TOKEN_PATH", runtime_token_path.to_str().expect("runtime token path"))
        .env(
            "FORT_REFRESH_TOKEN_PATH",
            runtime_refresh_path.to_str().expect("runtime refresh path"),
        )
        .env_remove("FORT_HOST")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains(&format!(
        "--json\nauth\nsession\nrefresh\n--refresh-token-file\n{}\n",
        runtime_refresh_path.display()
    )));
    assert!(
        args.contains(&format!("--token-file\n{}\nagent\nlist\n", runtime_token_path.display()))
    );
    assert_eq!(
        fs::read_to_string(&runtime_token_path).expect("read refreshed runtime token"),
        "rotated-runtime-access\n"
    );
    assert_eq!(
        fs::read_to_string(&runtime_refresh_path).expect("read rotated runtime refresh"),
        "rotated-runtime-refresh\n"
    );
}

#[test]
fn fort_wrapper_prefers_active_profile_runtime_session_over_bootstrap() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex")).expect("mkdir codex dir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let bootstrap_token_path = bootstrap_dir.join("admin.token");
    let bootstrap_refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&bootstrap_token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&bootstrap_refresh_path, "stale-admin-refresh\n").expect("write stale admin refresh");
    let profile_fort_dir = home.path().join(".si/codex/profiles/profile-delta/fort");
    fs::create_dir_all(&profile_fort_dir).expect("mkdir profile fort dir");
    let profile_token_path = profile_fort_dir.join("access.token");
    let profile_refresh_path = profile_fort_dir.join("refresh.token");
    fs::write(&profile_token_path, "stale-profile-token\n").expect("write stale profile token");
    fs::write(&profile_refresh_path, "stale-profile-refresh\n")
        .expect("write stale profile refresh");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [
            &bootstrap_token_path,
            &bootstrap_refresh_path,
            &profile_token_path,
            &profile_refresh_path,
        ] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n[codex]\nprofile = \"profile-delta\"\n[codex.profiles]\nactive = \"profile-delta\"\n[codex.profiles.entries.profile-delta]\nname = \"Profile Delta\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{bootstrap_refresh}\" ]; then\n  printf 'unexpected bootstrap refresh\\n' >&2\n  exit 1\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{profile_refresh}\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-profile-refresh' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-profile-access\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{profile_token}\" ] && [ \"$5\" = \"list\" ] && [ \"$6\" = \"--repo\" ] && [ \"$7\" = \"safe\" ] && [ \"$8\" = \"--env\" ] && [ \"$9\" = \"dev\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-profile-access'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            bootstrap_refresh = bootstrap_refresh_path.display(),
            profile_refresh = profile_refresh_path.display(),
            profile_token = profile_token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "list",
            "--repo",
            "safe",
            "--env",
            "dev",
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains(&format!(
        "--json\nauth\nsession\nrefresh\n--refresh-token-file\n{}\n",
        profile_refresh_path.display()
    )));
    assert!(args.contains(&format!(
        "--token-file\n{}\nlist\n--repo\nsafe\n--env\ndev\n",
        profile_token_path.display()
    )));
    assert_eq!(
        fs::read_to_string(&profile_token_path).expect("read refreshed profile token"),
        "rotated-profile-access\n"
    );
    assert_eq!(
        fs::read_to_string(&profile_refresh_path).expect("read rotated profile refresh"),
        "rotated-profile-refresh\n"
    );
}

#[test]
fn fort_wrapper_falls_back_to_bootstrap_when_profile_runtime_refresh_fails() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex")).expect("mkdir codex dir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let bootstrap_token_path = bootstrap_dir.join("admin.token");
    let bootstrap_refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&bootstrap_token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&bootstrap_refresh_path, "stale-admin-refresh\n").expect("write stale admin refresh");
    let profile_fort_dir = home.path().join(".si/codex/profiles/profile-delta/fort");
    fs::create_dir_all(&profile_fort_dir).expect("mkdir profile fort dir");
    let profile_token_path = profile_fort_dir.join("access.token");
    let profile_refresh_path = profile_fort_dir.join("refresh.token");
    fs::write(&profile_token_path, "stale-profile-token\n").expect("write stale profile token");
    fs::write(&profile_refresh_path, "stale-profile-refresh\n")
        .expect("write stale profile refresh");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [
            &bootstrap_token_path,
            &bootstrap_refresh_path,
            &profile_token_path,
            &profile_refresh_path,
        ] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n[codex]\nprofile = \"profile-delta\"\n[codex.profiles]\nactive = \"profile-delta\"\n[codex.profiles.entries.profile-delta]\nname = \"Profile Delta\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{profile_refresh}\" ]; then\n  printf '%s\\n' 'fort request failed (status=401): unauthorized' >&2\n  exit 1\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{bootstrap_refresh}\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-bootstrap-refresh' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-bootstrap-access\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{bootstrap_token}\" ] && [ \"$5\" = \"list\" ] && [ \"$6\" = \"--repo\" ] && [ \"$7\" = \"safe\" ] && [ \"$8\" = \"--env\" ] && [ \"$9\" = \"dev\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-bootstrap-access'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            profile_refresh = profile_refresh_path.display(),
            bootstrap_refresh = bootstrap_refresh_path.display(),
            bootstrap_token = bootstrap_token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "list",
            "--repo",
            "safe",
            "--env",
            "dev",
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains(&format!(
        "--json\nauth\nsession\nrefresh\n--refresh-token-file\n{}\n",
        profile_refresh_path.display()
    )));
    assert!(args.contains(&format!(
        "--json\nauth\nsession\nrefresh\n--refresh-token-file\n{}\n",
        bootstrap_refresh_path.display()
    )));
    assert!(args.contains(&format!(
        "--token-file\n{}\nlist\n--repo\nsafe\n--env\ndev\n",
        bootstrap_token_path.display()
    )));
    assert_eq!(
        fs::read_to_string(&bootstrap_token_path).expect("read refreshed bootstrap token"),
        "rotated-bootstrap-access\n"
    );
    assert_eq!(
        fs::read_to_string(&bootstrap_refresh_path).expect("read rotated bootstrap refresh"),
        "rotated-bootstrap-refresh\n"
    );
}

#[test]
fn fort_wrapper_reuses_fresh_bootstrap_token_without_refreshing() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    let payload = format!(
        "{{\"exp\":{},\"iss\":\"fortd\",\"aud\":[\"fort-api\"]}}",
        chrono::Utc::now().timestamp() + 3600
    );
    let token = format!("header.{}.signature\n", URL_SAFE_NO_PAD.encode(payload.as_bytes()));
    fs::write(&token_path, token).expect("write fresh admin token");
    fs::write(&refresh_path, "unused-refresh-token\n").expect("write refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  printf 'unexpected refresh\\n' >&2\n  exit 1\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"agent\" ] && [ \"$6\" = \"list\" ]; then\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "agent", "list"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(!args.contains("--json\nauth\nsession\nrefresh\n"));
    assert!(args.contains(&format!("--token-file\n{}\nagent\nlist\n", token_path.display())));
}

#[test]
fn fort_wrapper_does_not_refresh_stale_bootstrap_session_for_doctor() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&refresh_path, "stale-refresh-token\n").expect("write stale refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"doctor\" ]; then\n  exit 0\nfi\nif [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  printf 'unexpected refresh\\n' >&2\n  exit 1\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(!args.contains("--json\nauth\nsession\nrefresh\n"));
    assert!(args.contains(&format!("--token-file\n{}\ndoctor\n", token_path.display())));
}

#[test]
fn fort_wrapper_builds_configured_repo_when_fort_missing_from_path() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    let repo = tempdir().expect("fort repo");
    let args_file = repo.path().join("fort-args.txt");
    let env_file = repo.path().join("fort-env.txt");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[fort]\nrepo = \"{}\"\nhost = \"https://fort.example.test\"\n",
            repo.path().display()
        ),
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        &format!(
            "#!/bin/sh\nset -eu\nif [ \"$1\" != \"build\" ]; then\n  printf 'unexpected cargo command: %s\\n' \"$1\" >&2\n  exit 1\nfi\nmkdir -p \"$PWD/target/debug\"\ncat > \"$PWD/target/debug/fort\" <<'EOF'\n#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\nEOF\nchmod +x \"$PWD/target/debug/fort\"\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );
    let path_env = format!("{}:/usr/bin:/bin", bin_dir.path().display());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(
        args,
        format!(
            "--host\nhttps://fort.example.test\n--token-file\n{}\ndoctor\n",
            home.path().join(".si/fort/bootstrap/admin.token").display()
        )
    );
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_builds_sibling_checkout_from_runtime_workspace_when_settings_repo_missing() {
    let workspace = tempdir().expect("workspace tempdir");
    let si_dir = workspace.path().join("si");
    let fort_dir = workspace.path().join("fort");
    fs::create_dir_all(&si_dir).expect("mkdir sibling si dir");
    fs::create_dir_all(&fort_dir).expect("mkdir sibling fort dir");
    fs::write(fort_dir.join("Cargo.toml"), "[package]\nname = \"fort\"\nversion = \"0.0.0\"\n")
        .expect("write sibling fort cargo manifest");

    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");

    let args_file = fort_dir.join("fort-args.txt");
    let env_file = fort_dir.join("fort-env.txt");
    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        &format!(
            "#!/bin/sh\nset -eu\nif [ \"$1\" != \"build\" ]; then\n  printf 'unexpected cargo command: %s\\n' \"$1\" >&2\n  exit 1\nfi\nmkdir -p \"$PWD/target/debug\"\ncat > \"$PWD/target/debug/fort\" <<'EOF'\n#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\nEOF\nchmod +x \"$PWD/target/debug/fort\"\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );
    let path_env = format!("{}:/usr/bin:/bin", bin_dir.path().display());

    cargo_bin()
        .current_dir(workspace.path())
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(
        args,
        format!(
            "--token-file\n{}\ndoctor\n",
            home.path().join(".si/fort/bootstrap/admin.token").display()
        )
    );
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_prefers_existing_sibling_binary_before_cargo_build_fallback() {
    let workspace = tempdir().expect("workspace tempdir");
    let fort_dir = workspace.path().join("fort");
    fs::create_dir_all(fort_dir.join("target/debug")).expect("mkdir fort target");
    fs::write(fort_dir.join("Cargo.toml"), "[package]\nname = \"fort\"\nversion = \"0.0.0\"\n")
        .expect("write sibling fort cargo manifest");

    let args_file = fort_dir.join("fort-args.txt");
    let env_file = fort_dir.join("fort-env.txt");
    let fort_binary = fort_dir.join("target/debug/fort");
    write_executable_shell_script(
        &fort_binary,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );

    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");

    cargo_bin()
        .current_dir(workspace.path())
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", "/usr/bin:/bin")
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(
        args,
        format!(
            "--token-file\n{}\ndoctor\n",
            home.path().join(".si/fort/bootstrap/admin.token").display()
        )
    );
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_config_set_and_show_round_trip_si_settings() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--repo",
            "/tmp/fort-repo",
            "--bin",
            "/tmp/fort-bin",
            "--build",
            "true",
            "--host",
            "https://fort.example.test",
            "--runtime-host",
            "http://fort.internal:8088",
        ])
        .assert()
        .success();

    let settings_source =
        fs::read_to_string(home.path().join(".si/settings.toml")).expect("read settings");
    let parsed: toml::Value = toml::from_str(&settings_source).expect("parse settings");
    assert_eq!(parsed["fort"]["repo"].as_str().expect("repo"), "/tmp/fort-repo");
    assert_eq!(parsed["fort"]["bin"].as_str().expect("bin"), "/tmp/fort-bin");
    assert!(parsed["fort"]["build"].as_bool().expect("build"));

    let output = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "show",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["repo"], "/tmp/fort-repo");
    assert_eq!(parsed["bin"], "/tmp/fort-bin");
    assert_eq!(parsed["build"], true);
    assert_eq!(parsed["host"], "https://fort.example.test");
    assert_eq!(parsed["runtime_host"], "http://fort.internal:8088");
}

#[test]
fn build_npm_publish_from_vault_uses_si_vault_wrapper() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("vault-args.txt");
    let si_path = bin_dir.path().join("si");
    fs::write(
        &si_path,
        format!(
            "#!/bin/sh\nif [ \"$1\" = \"vault\" ] && [ \"$2\" = \"check\" ]; then\n  exit 0\nfi\nif [ \"$1\" = \"vault\" ] && [ \"$2\" = \"list\" ]; then\n  echo 'NPM_GAT_AUREUMA_VANGUARDA masked'\n  exit 0\nfi\nif [ \"$1\" = \"vault\" ] && [ \"$2\" = \"run\" ]; then\n  printf '%s\\n' \"$@\" > {}\n  exit 0\nfi\nexit 1\n",
            shell_escape_for_test(&args_file)
        ),
    )
    .expect("write si");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&si_path).expect("stat si").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&si_path, perms).expect("chmod si");
    }

    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "build",
            "npm",
            "vault",
            "--repo-root",
            repo.path().to_str().expect("repo path"),
            "--dry-run",
        ])
        .env("PATH", path_env)
        .assert()
        .success();

    let args = fs::read_to_string(args_file).expect("read vault args");
    assert!(args.contains("vault"));
    assert!(args.contains("run"));
    assert!(args.contains("build"));
    assert!(args.contains("publish"));
    assert!(args.contains("--dry-run"));
}

#[test]
fn build_homebrew_render_core_formula_writes_formula() {
    let dir = tempdir().expect("repo tempdir");
    let out = dir.path().join("Formula/si.rb");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let url = format!("http://{}", addr);

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /Aureuma/si/archive/refs/tags/v1.2.3.tar.gz"));
        let body = b"archive";
        let response = format!("HTTP/1.1 200 OK\r\nContent-Length: {}\r\n\r\n", body.len());
        stream.write_all(response.as_bytes()).expect("write head");
        stream.write_all(body).expect("write body");
    });

    cargo_bin()
        .args([
            "build",
            "homebrew",
            "render-core-formula",
            "--version",
            "v1.2.3",
            "--output",
            out.to_str().expect("out"),
        ])
        .env("SI_RUST_HOMEBREW_SOURCE_BASE_URL", url)
        .assert()
        .success();

    let rendered = fs::read_to_string(out).expect("read formula");
    assert!(rendered.contains("homepage \"https://github.com/Aureuma/si\""));
    assert!(rendered.contains("url \"http://"));
    assert!(rendered.contains("sha256 \""));
    assert!(rendered.contains("depends_on \"rust\" => :build"));
    assert!(rendered.contains("cargo\", \"install\", \"--locked\""));
    assert!(rendered.contains("std_cargo_args(path: \"rust/crates/si-cli\")"));
    assert!(rendered.contains("mv bin/\"si-rs\", bin/\"si\""));
}

#[test]
fn build_homebrew_render_tap_formula_writes_formula() {
    let dir = tempdir().expect("tempdir");
    let checksums = dir.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");
    let output = dir.path().join("Formula/si.rb");

    cargo_bin()
        .args([
            "build",
            "homebrew",
            "render-tap-formula",
            "--version",
            "v1.2.3",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--output",
            output.to_str().expect("output"),
        ])
        .assert()
        .success();

    let rendered = fs::read_to_string(output).expect("read formula");
    assert!(rendered.contains("version \"1.2.3\""));
    assert!(rendered.contains("si_1.2.3_linux_amd64.tar.gz"));
    assert!(rendered.contains("sha4"));
    assert!(rendered.contains("bin.install binary => \"si\""));
}

#[test]
fn build_self_verify_release_assets_checks_archives() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::write(repo.path().join("README.md"), "readme\n").expect("write readme");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("write license");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho si\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .current_dir(repo.path())
        .args([
            "build",
            "self",
            "release-assets",
            "--repo",
            repo.path().to_str().expect("repo path"),
            "--out-dir",
            out_dir.to_str().expect("out path"),
        ])
        .env("PATH", &path_env)
        .assert()
        .success();

    cargo_bin()
        .current_dir(repo.path())
        .args([
            "build",
            "self",
            "verify-release-assets",
            "--version",
            "v1.2.3",
            "--out-dir",
            out_dir.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .assert()
        .success();
}

#[test]
fn build_homebrew_update_tap_repo_writes_formula_without_commit() {
    let dir = tempdir().expect("tempdir");
    let tap_dir = dir.path().join("homebrew-si");
    fs::create_dir_all(&tap_dir).expect("mkdir tap dir");
    let checksums = dir.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    cargo_bin()
        .args([
            "build",
            "homebrew",
            "update-tap-repo",
            "--version",
            "v1.2.3",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--tap-dir",
            tap_dir.to_str().expect("tap dir"),
        ])
        .assert()
        .success();

    assert!(tap_dir.join("Formula/si.rb").exists());
}

#[test]
fn build_installer_run_dry_run_reports_rust_usage() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v0.1.0");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(&cargo_path, "#!/bin/sh\necho cargo 1.88.0\n");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = cargo_bin()
        .args([
            "build",
            "installer",
            "run",
            "--dry-run",
            "--source-dir",
            repo.path().to_str().expect("repo"),
            "--force",
        ])
        .env("PATH", path_env)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let output = String::from_utf8_lossy(&output);
    assert!(output.contains("rust: using system cargo"));
}

#[test]
fn build_installer_run_installs_fake_binary() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v0.1.0");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo cargo 1.88.0\n  exit 0\nfi\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho installed\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let install_dir = repo.path().join("bin");

    cargo_bin()
        .args([
            "build",
            "installer",
            "run",
            "--source-dir",
            repo.path().to_str().expect("repo"),
            "--install-dir",
            install_dir.to_str().expect("install dir"),
            "--force",
            "--quiet",
        ])
        .env("PATH", path_env)
        .assert()
        .success();

    let installed = install_dir.join("si");
    assert!(installed.exists());
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(
            fs::metadata(&installed).expect("stat installed").permissions().mode() & 0o111,
            0o111
        );
    }
}

#[test]
fn build_installer_smoke_host_runs_rust_or_override_commands() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let installer = repo.path().join("installer-fixture");
    let settings = repo.path().join("settings-fixture");
    fs::write(
        &installer,
        "#!/bin/sh\nprev=\nbackend=\nsource_dir=\ninstall_dir=\ninstall_path=\nuninstall=0\ndry_run=0\nfor i in \"$@\"; do\n  if [ \"$prev\" = \"--backend\" ]; then backend=\"$i\"; fi\n  if [ \"$prev\" = \"--source-dir\" ]; then source_dir=\"$i\"; fi\n  if [ \"$prev\" = \"--install-dir\" ]; then install_dir=\"$i\"; fi\n  if [ \"$prev\" = \"--install-path\" ]; then install_path=\"$i\"; fi\n  [ \"$i\" = \"--uninstall\" ] && uninstall=1\n  [ \"$i\" = \"--dry-run\" ] && dry_run=1\n  [ \"$i\" = \"--help\" ] && exit 0\n  prev=\"$i\"\ndone\nif [ -n \"$backend\" ] && [ \"$backend\" != \"local\" ]; then exit 1; fi\nif [ -n \"$install_dir\" ] && [ -n \"$install_path\" ]; then exit 1; fi\nif [ -n \"$source_dir\" ] && [ ! -d \"$source_dir\" ]; then exit 1; fi\ncase \"$source_dir\" in *missing-source*) exit 1;; esac\nif [ -n \"$install_dir\" ]; then target=\"$install_dir/si\"; else target=\"$install_path\"; fi\nif [ \"$uninstall\" = 1 ]; then rm -f \"$target\"; exit 0; fi\nif [ \"$dry_run\" = 1 ]; then exit 0; fi\nmkdir -p \"$(dirname \"$target\")\"\nprintf '#!/bin/sh\\nexit 0\\n' > \"$target\"\nchmod 755 \"$target\"\n",
    )
    .expect("write installer");
    fs::write(&settings, "#!/bin/sh\nexit 0\n").expect("write settings");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&installer, &settings] {
            let mut perms = fs::metadata(path).expect("stat path").permissions();
            perms.set_mode(0o755);
            fs::set_permissions(path, perms).expect("chmod path");
        }
    }
    let bin_dir = tempdir().expect("bin tempdir");
    let git_path = bin_dir.path().join("git");
    fs::write(&git_path, "#!/bin/sh\nexit 0\n").expect("write git");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&git_path).expect("stat git").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&git_path, perms).expect("chmod git");
    }
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    cargo_bin()
        .current_dir(repo.path())
        .args(["build", "installer", "smoke-host"])
        .env("PATH", path_env)
        .env("SI_INSTALLER_RUNNER", &installer)
        .env("SI_INSTALLER_SETTINGS_TEST", &settings)
        .assert()
        .success();
}

#[test]
fn build_installer_smoke_npm_runs_rust_or_override_commands() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let build_assets = repo.path().join("build-assets-fixture");
    let build_npm = repo.path().join("build-npm-fixture");
    fs::write(&build_assets, "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\nexit 0\n").expect("write assets script");
    fs::write(&build_npm, "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\ntouch \"$out/aureuma-si-1.2.3.tgz\"\nexit 0\n").expect("write npm script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&build_assets, &build_npm] {
            let mut perms = fs::metadata(path).expect("stat script").permissions();
            perms.set_mode(0o755);
            fs::set_permissions(path, perms).expect("chmod script");
        }
    }
    let bin_dir = tempdir().expect("bin tempdir");
    let npm_path = bin_dir.path().join("npm");
    fs::write(&npm_path, "#!/bin/sh\nprefix=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--prefix\" ]; then prefix=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$prefix/bin\"\nprintf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"\nchmod 755 \"$prefix/bin/si\"\nexit 0\n").expect("write npm");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&npm_path).expect("stat npm").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&npm_path, perms).expect("chmod npm");
    }
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    cargo_bin()
        .current_dir(repo.path())
        .args(["build", "installer", "smoke-npm"])
        .env("PATH", path_env)
        .env("SI_BUILD_ASSETS_EXEC", &build_assets)
        .env("SI_BUILD_NPM_PACKAGE_EXEC", &build_npm)
        .assert()
        .success();
}

#[test]
fn build_installer_smoke_homebrew_runs_rust_or_override_commands() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let build_assets = repo.path().join("build-assets-fixture");
    fs::write(
        &build_assets,
        "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\ncat > \"$out/checksums.txt\" <<'EOF'\nsha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\nEOF\nexit 0\n",
    )
    .expect("write assets script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&build_assets).expect("stat assets script").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&build_assets, perms).expect("chmod assets script");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"install\" ]; then formula=\"$3\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q 'file://' \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"si-smoke\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ]; then rm -rf \"$prefix\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix)
        ),
    )
    .expect("write brew");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&brew).expect("stat brew").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&brew, perms).expect("chmod brew");
    }

    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    cargo_bin()
        .current_dir(repo.path())
        .args(["build", "installer", "smoke-homebrew"])
        .env("PATH", path_env)
        .env("SI_BUILD_ASSETS_EXEC", &build_assets)
        .assert()
        .success();
}

#[test]
fn aws_doctor_public_runs_rust_probe() {
    let base_url = spawn_single_response_server("200 OK", "<ok/>");
    let stdout = cargo_bin()
        .args([
            "orbit",
            "aws",
            "doctor",
            "--public",
            "true",
            "--base-url",
            &base_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&stdout).expect("parse json");
    assert_eq!(payload["provider"], "aws_iam");
    assert_eq!(payload["base_url"], base_url);
    assert_eq!(payload["checks"][0]["name"], "public.probe");
    assert_eq!(payload["checks"][0]["ok"], true);
}

#[test]
fn apple_appstore_doctor_public_runs_rust_probe() {
    let url = format!(
        "{}/sample-code/app-store-connect/app-store-connect-openapi-specification.zip",
        spawn_single_response_server("200 OK", "zip")
    );
    let stdout = cargo_bin()
        .args(["orbit", "apple", "store", "doctor", "--public", "true", "--format", "json"])
        .env("SI_RUST_APPLE_APPSTORE_PUBLIC_PROBE_URL", &url)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&stdout).expect("parse json");
    assert_eq!(payload["ok"], true);
    assert_eq!(payload["probe"], url);
    assert_eq!(payload["status_code"], 200);
}

#[test]
fn oci_doctor_public_runs_rust_probe() {
    let base_url = spawn_single_response_server("200 OK", "{\"items\":[]}");
    let stdout = cargo_bin()
        .args([
            "orbit",
            "oci",
            "doctor",
            "--public",
            "true",
            "--base-url",
            &base_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&stdout).expect("parse json");
    assert_eq!(payload["provider"], "oci_core");
    assert_eq!(payload["base_url"], base_url);
    assert_eq!(payload["checks"][0]["ok"], true);
}

#[test]
fn build_self_validate_release_version_accepts_matching_tag() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    cargo_bin()
        .current_dir(repo.path())
        .args(["build", "self", "validate-release-version", "--tag", "v1.2.3"])
        .assert()
        .success();
}

#[test]
fn build_self_validate_release_version_rejects_mismatch() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    cargo_bin()
        .current_dir(repo.path())
        .args(["build", "self", "validate-release-version", "--tag", "v1.2.4"])
        .assert()
        .failure();
}

#[test]
fn build_self_release_asset_creates_single_archive() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::write(repo.path().join("README.md"), "readme\n").expect("readme");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("license");
    let toolchain_dir = tempdir().expect("toolchain tempdir");
    let cargo_path = toolchain_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho si\\n' > \"$CARGO_TARGET_DIR/release/si-rs\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si-rs\"\n",
    );
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&cargo_path).expect("stat tool").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&cargo_path, perms).expect("chmod tool");
    }
    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", toolchain_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    cargo_bin()
        .args([
            "build",
            "self",
            "release-asset",
            "--repo-root",
            repo.path().to_str().expect("repo"),
            "--version",
            "v1.2.3",
            "--goos",
            "linux",
            "--goarch",
            "amd64",
            "--out-dir",
            out_dir.to_str().expect("out"),
        ])
        .env("PATH", path_env)
        .assert()
        .success();
    let archive = out_dir.join("si_1.2.3_linux_amd64.tar.gz");
    assert!(archive.exists());
    let file = File::open(&archive).expect("open archive");
    let decoder = flate2::read::GzDecoder::new(file);
    let mut archive = Archive::new(decoder);
    let mut names = archive
        .entries()
        .expect("archive entries")
        .map(|entry| entry.expect("entry").path().expect("entry path").display().to_string())
        .collect::<Vec<_>>();
    names.sort();
    assert!(names.iter().any(|name| name.ends_with("/si")));
}

#[test]
fn build_installer_settings_helper_prints_expected_doc() {
    let dir = tempdir().expect("tempdir");
    let settings = dir.path().join("settings.toml");
    let output = cargo_bin()
        .args([
            "build",
            "installer",
            "settings-helper",
            "--settings",
            settings.to_str().expect("settings"),
            "--default-browser",
            "safari",
            "--print",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(String::from_utf8_lossy(&output), "[codex.login]\ndefault_browser = \"safari\"\n");
}

#[test]
fn build_installer_settings_helper_rewrites_existing_login_block() {
    let dir = tempdir().expect("tempdir");
    let settings = dir.path().join("settings.toml");
    fs::write(&settings, "[codex.login]\ndefault_browser = \"chrome\"\nother = true\n")
        .expect("write settings");
    cargo_bin()
        .args([
            "build",
            "installer",
            "settings-helper",
            "--settings",
            settings.to_str().expect("settings"),
            "--default-browser",
            "safari",
        ])
        .assert()
        .success();
    let rendered = fs::read_to_string(settings).expect("read settings");
    assert!(rendered.contains("default_browser = \"safari\""));
    assert!(rendered.contains("other = true"));
}

fn test_app_private_key_pem() -> &'static str {
    "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCaJZkLuu/uJGz1\n4cxlZ3d7H5b88tcXH0qPZmkCUPWHA4aumx36BErkorXukYD0IRhRaJe8shsgRC4c\nw5TkjrXcG9Kigh3HvifRnA1kCbmwceANdww6J8ggtFDFO026VIEx2R8tjtYLs+pU\n+Xb6llxixE+QWSXQVHqHy67KvWDeRu6es8OZb8klxFejwdTBC0UDxNLwdr+hDV3b\nEDduxm+pnnmTi7ciwDbrO8D/GXkYi7YLXwqcfHLhVqZeVXrs5JPc+7pOHJCf1fZO\n9BBUOVO9qDUqfQk7CWBF3MKyNtx/wv+Mzg5ztl4VMPRgdnbnU8B2en+rPYZg7KTF\nN2n0ORH/AgMBAAECggEAAfNDfkZVXnN1Mh/duKi4S8VTYTbnVBe6we60mb68JIL9\nvhF2AyGbxaHDYIB/G6zxhFIo8qO5kSJxB5R35UkNnE/OeJeMgz2bflzq6cmaYP+d\nKz5xgqjZ24QR2N+jtPL4bCYy7UjMhNBiwMQj5mQRnimdV2uxUp3xq5cpn89ekuFY\n1C48pXicl8OLgdzhNAROk2edrYo+DJl+5VaSPSN5L+dz67pBqAZ4gcUj4ZdmofmB\ninHw83zTvQfSFaykC98TJEpQppaC8gK+mxQF6bWotfxq/Gd2MBhNwJAF1WnJ2cq/\np2vuDCqliKbt40M33qUVIavhY6C50dUQ3VeERxmvyQKBgQDSlBBZJ2auZHgJeR/U\nIYUPOypo8mBBVMh6axbRR5yrpTfGDHqc4Zx4nC3kxRjqnA+sfdZBESOgvj7FdWUj\nf3fEM+RPQLW0zu2F+wmJ2w28kncOFVxHrrrxJToKtBSfR3YIjCnZmy6pxn8WOimM\nabOm5hmSRLgMcRSvptw6crOOtwKBgQC7ZXCuTgnod+Cf25PvKNxSLJOy9lephPYO\nqU7LWywilQEgj7VWrmVKP+6HC3L615++cLlKxoozlvT0dxjfhzgdZxXKLOUf4x3d\n72FXx/sKFFtOCgeDeR2Ln+hSLbGsCLkyOo5zFFCidmE4z0DitiPmSRtJdHt1VthO\n8KW10yTO+QKBgCBZhrlriCa6YIZ0CSO5kotod3dv5MGkmLfVw8eazMLBuvO97wgy\n0Krms1Y1wUIpf27sVgHg9Cw5jcMf6c2uQ2Ps5OIX+tIwB+VRT4HSGSYjCg8r0OVi\nPm3VXjlOuOxPOh7OCY/Yey6xw8xSWxerFWJKbxs9W1jt9lOVurdv7425AoGBAKIU\nQ5hOoN0yydIZjWK92YktSvXvgLR67oKRxze1fH/Qlm/+O55kKfFFSF3+9gyk8GI7\nhtd4ztF+EBFc7ONwRYWQwlTh7a5dtlhdEbllmugF4U6m+Aare3Vm8f4ZzWD5Doy1\n/rzj5jYN41rKTtmHJZeoxXQLzjgXy/DCzOBtZZmpAoGABacst96WKng6XE5MkZpo\nacIEMOPpPYnyc4VgqHPft4D45ARP4wFZryxZ58Ya6194Z9PUzL5N7yKgsQZlnGR8\nL6W4ulLYfyhkWfi592cIKS7eDjWijbcIUzgvuIzCWvme08KQSPkgYNFXomlg4EZv\n9HrWPhpFaH+jHJsVKmD/Qyo=\n-----END PRIVATE KEY-----"
}

#[test]
fn google_youtube_context_current_json_reads_settings() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    fs::write(
        &settings_path,
        r#"
[google]
default_account = "core"

[google.youtube]
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
default_language_code = "en"
default_region_code = "US"
vault_prefix = "google_core"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "context", "current", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "yt-core");
    assert_eq!(parsed["auth_mode"], "api-key");
    assert_eq!(parsed["language_code"], "en");
    assert_eq!(parsed["region_code"], "US");
}

#[test]
fn google_youtube_auth_status_json_verifies_api_key() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /youtube/v3/search?"));
        assert!(request.contains("key=key-123"));
        let body = "{\"items\":[{\"id\":\"v1\"}]}";
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["verify"], true);
    assert_eq!(parsed["auth_mode"], "api-key");
}

#[test]
fn google_youtube_search_list_all_aggregates_pages() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        for expected_page in ["", "pageToken=t2"] {
            let (mut stream, _) = listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            if expected_page.is_empty() {
                assert!(request.contains("GET /youtube/v3/search?"));
                assert!(!request.contains("pageToken=t2"));
                let body = "{\"items\":[{\"id\":\"v1\"}],\"nextPageToken\":\"t2\"}";
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                );
                stream.write_all(response.as_bytes()).expect("write response");
            } else {
                assert!(request.contains("pageToken=t2"));
                let body = "{\"items\":[{\"id\":\"v2\"}]}";
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                );
                stream.write_all(response.as_bytes()).expect("write response");
            }
        }
    });

    let output = cargo_bin()
        .args([
            "orbit", "google", "youtube", "search", "list", "--query", "music", "--all", "--home",
        ])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["count"], 2);
    assert_eq!(parsed["items"].as_array().expect("items array").len(), 2);
}

#[test]
fn google_youtube_support_languages_json_reads_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /youtube/v3/i18nLanguages?"));
        let body = "{\"items\":[{\"id\":\"en\",\"snippet\":{}}]}";
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "support", "languages", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
}

#[test]
fn google_youtube_doctor_json_runs_checks() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
default_language_code = "en"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        for _ in 0..2 {
            let (mut stream, _) = listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            let body = if request.contains("/youtube/v3/search?") {
                "{\"items\":[{\"id\":\"v1\"}]}"
            } else {
                "{\"items\":[{\"id\":\"en\"}]}"
            };
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "doctor", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["checks"].as_array().expect("checks").len(), 3);
}

#[test]
fn google_youtube_channel_list_json_reads_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("/youtube/v3/channels?id=c1"));
        let body = r#"{"items":[{"id":"c1"}]}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "channel", "list", "--id", "c1", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["items"][0]["id"], "c1");
}

#[test]
fn google_youtube_video_list_json_reads_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
default_region_code = "US"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("/youtube/v3/videos?"));
        assert!(request.contains("chart=mostPopular"));
        let body = r#"{"items":[{"id":"v1"}]}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "video", "list", "--chart", "mostPopular", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["items"][0]["id"], "v1");
}

#[test]
fn google_youtube_channel_update_json_writes_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("PUT /youtube/v3/channels?"));
        assert!(request.contains("part=snippet%2CbrandingSettings%2Cstatus"));
        assert!(request.contains("{\"id\":\"c1\"}"));
        let body = r#"{"id":"c1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "channel",
            "update",
            "--body",
            "{\"id\":\"c1\"}",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "c1");
}

#[test]
fn google_youtube_video_rate_json_posts_rating() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/videos/rate?"));
        assert!(request.contains("id=v1"));
        assert!(request.contains("rating=like"));
        let body = r#"{"status":"ok"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit", "google", "youtube", "video", "rate", "--id", "v1", "--rating", "like",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
}

#[test]
fn google_youtube_playlist_create_json_builds_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/playlists?"));
        assert!(request.contains("\"title\":\"Launch\""));
        assert!(request.contains("\"description\":\"Release prep\""));
        assert!(request.contains("\"privacyStatus\":\"unlisted\""));
        let body = r#"{"id":"pl1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "playlist",
            "create",
            "--title",
            "Launch",
            "--description",
            "Release prep",
            "--privacy",
            "unlisted",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "pl1");
}

#[test]
fn google_youtube_playlist_item_add_json_builds_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/playlistItems?"));
        assert!(request.contains("\"playlistId\":\"pl1\""));
        assert!(request.contains("\"videoId\":\"vid9\""));
        assert!(request.contains("\"position\":2"));
        let body = r#"{"id":"pli1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "playlist-item",
            "add",
            "--playlist-id",
            "pl1",
            "--video-id",
            "vid9",
            "--position",
            "2",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "pli1");
}

#[test]
fn google_youtube_subscription_create_json_builds_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/subscriptions?"));
        assert!(request.contains("\"channelId\":\"chan9\""));
        let body = r#"{"id":"sub1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "subscription",
            "create",
            "--channel-id",
            "chan9",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "sub1");
}

#[test]
fn google_youtube_comment_thread_create_json_builds_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/commentThreads?"));
        assert!(request.contains("\"videoId\":\"vid1\""));
        assert!(request.contains("\"textOriginal\":\"ship it\""));
        let body = r#"{"id":"ct1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "comment",
            "thread",
            "create",
            "--video-id",
            "vid1",
            "--text",
            "ship it",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ct1");
}

#[test]
fn google_youtube_report_usage_json_reads_log_file() {
    let home = tempdir().expect("tempdir");
    let log_dir = home.path().join(".si").join("logs");
    fs::create_dir_all(&log_dir).expect("mkdir log dir");
    let log_path = log_dir.join("google-youtube.log");
    fs::write(
        &log_path,
        [
            r#"{"ts":"2026-03-17T00:00:00Z","event":"request","method":"GET","path":"/youtube/v3/search","ctx_account_alias":"core","ctx_environment":"prod"}"#,
            r#"{"ts":"2026-03-17T00:00:01Z","event":"response","method":"GET","path":"/youtube/v3/search","ctx_account_alias":"core","ctx_environment":"prod","status":200,"duration_ms":45,"request_id":"req1"}"#,
        ]
        .join("\n"),
    )
    .expect("write log");

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "report",
            "usage",
            "--account",
            "core",
            "--env",
            "prod",
            "--home",
        ])
        .arg(home.path())
        .args(["--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requests"], 1);
    assert_eq!(parsed["responses"], 1);
    assert_eq!(parsed["errors"], 0);
    assert_eq!(parsed["unique_request_ids"], 1);
}

#[test]
fn google_youtube_live_broadcast_bind_json_posts_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/liveBroadcasts/bind?"));
        assert!(request.contains("id=b1"));
        assert!(request.contains("streamId=s1"));
        let body = r#"{"id":"b1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "live",
            "broadcast",
            "bind",
            "--id",
            "b1",
            "--stream-id",
            "s1",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "b1");
}

#[test]
fn google_youtube_live_chat_create_json_builds_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/liveChat/messages?"));
        assert!(request.contains("\"liveChatId\":\"lc1\""));
        assert!(request.contains("\"messageText\":\"hello stream\""));
        let body = r#"{"id":"m1"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "live",
            "chat",
            "create",
            "--live-chat-id",
            "lc1",
            "--text",
            "hello stream",
            "--home",
        ])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "m1");
}

#[test]
fn google_youtube_video_upload_json_resumable_uploads_file() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    let upload_path = home.path().join("video.mp4");
    fs::write(&upload_path, b"video-bytes").expect("write video");
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
upload_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut init_stream, _) = listener.accept().expect("accept init");
        let mut init_buffer = [0_u8; 8192];
        let init_read = init_stream.read(&mut init_buffer).expect("read init");
        let init_request = String::from_utf8_lossy(&init_buffer[..init_read]);
        assert!(init_request.contains("POST /youtube/v3/videos?"));
        assert!(init_request.contains("uploadType=resumable"));
        let init_response = format!(
            "HTTP/1.1 200 OK\r\nConnection: close\r\nLocation: http://{}/upload-session\r\nContent-Length: 0\r\n\r\n",
            addr
        );
        init_stream.write_all(init_response.as_bytes()).expect("write init response");

        let (mut upload_stream, _) = listener.accept().expect("accept upload");
        let mut upload_buffer = [0_u8; 8192];
        let upload_read = upload_stream.read(&mut upload_buffer).expect("read upload");
        let upload_request = String::from_utf8_lossy(&upload_buffer[..upload_read]);
        assert!(upload_request.contains("PUT /upload-session "));
        assert!(upload_request.contains("video-bytes"));
        let body = r#"{"id":"v-upload"}"#;
        let upload_response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        upload_stream.write_all(upload_response.as_bytes()).expect("write upload response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "video", "upload", "--file"])
        .arg(&upload_path)
        .args(["--title", "Launch", "--home"])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "v-upload");
}

#[test]
fn google_youtube_caption_download_json_writes_file() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    let output_path = home.path().join("captions.vtt");
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /youtube/v3/captions/cap1?"));
        assert!(request.contains("tfmt=vtt"));
        let body = "WEBVTT\n\n00:00.000 --> 00:01.000\nhello\n";
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: text/vtt\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "caption", "download", "--id", "cap1", "--output"])
        .arg(&output_path)
        .args(["--format", "vtt", "--home"])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert!(parsed["bytes_written"].as_i64().unwrap_or_default() > 0);
    assert_eq!(
        fs::read_to_string(&output_path).expect("caption output"),
        "WEBVTT\n\n00:00.000 --> 00:01.000\nhello\n"
    );
}

#[test]
fn google_youtube_thumbnail_set_json_uploads_media() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    let file_path = home.path().join("thumb.jpg");
    fs::write(&file_path, b"jpeg-bytes").expect("write thumb");
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
upload_base_url = "{base_url}"
default_auth_mode = "oauth"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /youtube/v3/thumbnails/set?"));
        assert!(request.contains("uploadType=media"));
        assert!(request.contains("videoId=vthumb"));
        assert!(request.contains("jpeg-bytes"));
        let body = r#"{"kind":"youtube#thumbnailSetResponse"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "youtube", "thumbnail", "set", "--video-id", "vthumb", "--file"])
        .arg(&file_path)
        .args(["--home"])
        .arg(home.path())
        .args(["--access-token", "token-123", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["kind"], "youtube#thumbnailSetResponse");
}

#[test]
fn google_youtube_playlist_list_json_reads_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("/youtube/v3/playlists?"));
        assert!(request.contains("channelId=chan-1"));
        let body = r#"{"items":[{"id":"p1"}]}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "playlist",
            "list",
            "--channel-id",
            "chan-1",
            "--home",
        ])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["items"][0]["id"], "p1");
}

#[test]
fn google_youtube_playlist_item_list_json_reads_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let base_url = format!("http://{}", addr);
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "core"

[google.youtube]
api_base_url = "{base_url}"
default_auth_mode = "api-key"

[google.youtube.accounts.core]
project_id = "yt-core"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("/youtube/v3/playlistItems?"));
        assert!(request.contains("playlistId=pl-1"));
        let body = r#"{"items":[{"id":"pli-1"}]}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "youtube",
            "playlist-item",
            "list",
            "--playlist-id",
            "pl-1",
            "--home",
        ])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_CORE_YOUTUBE_API_KEY", "key-123")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["items"][0]["id"], "pli-1");
}

#[test]
fn google_play_context_current_json_reads_settings() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"https://oauth2.googleapis.com/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n")
    );
    fs::write(
        &settings_path,
        r#"
[google]
default_account = "test"

[google.play.accounts.test]
project_id = "acme-project"
developer_account_id = "dev-123"
default_package_name = "com.acme.app"
default_language_code = "en-US"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "google", "play", "context", "current", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "test");
    assert_eq!(parsed["developer_account_id"], "dev-123");
    assert_eq!(parsed["default_package_name"], "com.acme.app");
}

#[test]
fn google_play_auth_status_json_verifies_package() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let token_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let token_addr = token_listener.local_addr().expect("local addr");
    let api_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let api_addr = api_listener.local_addr().expect("local addr");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"http://{}/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n"),
        token_addr
    );
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "test"

[google.play]
api_base_url = "http://{api_addr}"

[google.play.accounts.test]
project_id = "acme-project"
default_package_name = "com.acme.app"
"#
        ),
    )
    .expect("write settings");

    thread::spawn(move || {
        for _ in 0..2 {
            let (mut stream, _) = token_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let _ = stream.read(&mut buffer).expect("read request");
            let body =
                r#"{"access_token":"ya29.play-token","expires_in":3600,"token_type":"Bearer"}"#;
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });
    thread::spawn(move || {
        for body in [r#"{"id":"edit-1"}"#, ""] {
            let (mut stream, _) = api_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            if body.is_empty() {
                assert!(request.contains(
                    "DELETE /androidpublisher/v3/applications/com.acme.app/edits/edit-1"
                ));
                let response = "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n".to_string();
                stream.write_all(response.as_bytes()).expect("write response");
            } else {
                assert!(
                    request.contains("POST /androidpublisher/v3/applications/com.acme.app/edits")
                );
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                );
                stream.write_all(response.as_bytes()).expect("write response");
            }
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "play", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--verify-package", "com.acme.app"])
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify"]["ok"], true);
}

#[test]
fn google_play_listing_get_json_reads_listing() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let token_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let token_addr = token_listener.local_addr().expect("local addr");
    let api_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let api_addr = api_listener.local_addr().expect("local addr");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"http://{}/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n"),
        token_addr
    );
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "test"

[google.play]
api_base_url = "http://{api_addr}"

[google.play.accounts.test]
default_package_name = "com.acme.app"
default_language_code = "en-US"
"#
        ),
    )
    .expect("write settings");
    thread::spawn(move || {
        for _ in 0..3 {
            let (mut stream, _) = token_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let _ = stream.read(&mut buffer).expect("read request");
            let body =
                r#"{"access_token":"ya29.play-token","expires_in":3600,"token_type":"Bearer"}"#;
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });
    thread::spawn(move || {
        let bodies = [r#"{"id":"edit-9"}"#, r#"{"language":"en-US","title":"Acme"}"#, ""];
        for body in bodies {
            let (mut stream, _) = api_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            if body == r#"{"id":"edit-9"}"# {
                assert!(
                    request.contains("POST /androidpublisher/v3/applications/com.acme.app/edits")
                );
            } else if body.is_empty() {
                assert!(request.contains(
                    "DELETE /androidpublisher/v3/applications/com.acme.app/edits/edit-9"
                ));
            } else {
                assert!(request.contains(
                    "GET /androidpublisher/v3/applications/com.acme.app/edits/edit-9/listings/en-US"
                ));
            }
            let response = if body.is_empty() {
                "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n".to_string()
            } else {
                format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                )
            };
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "play", "listing", "get", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["title"], "Acme");
}

#[test]
fn google_play_app_create_json_hits_custom_app_api() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let token_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let token_addr = token_listener.local_addr().expect("local addr");
    let api_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let api_addr = api_listener.local_addr().expect("local addr");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"http://{}/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n"),
        token_addr
    );
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "test"

[google.play]
custom_app_base_url = "http://{api_addr}"

[google.play.accounts.test]
developer_account_id = "dev-123"
"#
        ),
    )
    .expect("write settings");
    thread::spawn(move || {
        let (mut stream, _) = token_listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let _ = stream.read(&mut buffer).expect("read request");
        let body = r#"{"access_token":"ya29.play-token","expires_in":3600,"token_type":"Bearer"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });
    thread::spawn(move || {
        let (mut stream, _) = api_listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("POST /playcustomapp/v1/accounts/dev-123/customApps"));
        let body = r#"{"customApp":"apps/123"}"#;
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes()).expect("write response");
    });

    let output = cargo_bin()
        .args(["orbit", "google", "play", "app", "create", "--title", "Acme", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["customApp"], "apps/123");
}

#[test]
fn google_play_asset_upload_json_uploads_image() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let shot_path = home.path().join("shot.png");
    fs::write(&shot_path, b"pngdata").expect("write shot");
    let token_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let token_addr = token_listener.local_addr().expect("local addr");
    let api_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let api_addr = api_listener.local_addr().expect("local addr");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"http://{}/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n"),
        token_addr
    );
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "test"

[google.play]
api_base_url = "http://{api_addr}"
upload_base_url = "http://{api_addr}"

[google.play.accounts.test]
default_package_name = "com.acme.app"
default_language_code = "en-US"
"#
        ),
    )
    .expect("write settings");
    thread::spawn(move || {
        for _ in 0..3 {
            let (mut stream, _) = token_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let _ = stream.read(&mut buffer).expect("read request");
            let body =
                r#"{"access_token":"ya29.play-token","expires_in":3600,"token_type":"Bearer"}"#;
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });
    thread::spawn(move || {
        for (idx, body) in
            [r#"{"id":"edit-1"}"#, r#"{"id":"asset-1"}"#, r#"{"id":"edit-1"}"#].iter().enumerate()
        {
            let (mut stream, _) = api_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            match idx {
                0 => assert!(
                    request.contains("POST /androidpublisher/v3/applications/com.acme.app/edits")
                ),
                1 => {
                    assert!(request.contains("POST /upload/androidpublisher/v3/applications/com.acme.app/edits/edit-1/listings/en-US/phoneScreenshots?uploadType=media"));
                }
                _ => assert!(request.contains(
                    "POST /androidpublisher/v3/applications/com.acme.app/edits/edit-1:commit"
                )),
            }
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "play", "asset", "upload", "--type", "phone", "--file"])
        .arg(&shot_path)
        .args(["--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "OK");
}

#[test]
fn google_play_release_status_json_reads_track() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let token_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let token_addr = token_listener.local_addr().expect("local addr");
    let api_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let api_addr = api_listener.local_addr().expect("local addr");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"http://{}/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n"),
        token_addr
    );
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "test"

[google.play]
api_base_url = "http://{api_addr}"

[google.play.accounts.test]
default_package_name = "com.acme.app"
"#
        ),
    )
    .expect("write settings");
    thread::spawn(move || {
        for _ in 0..3 {
            let (mut stream, _) = token_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let _ = stream.read(&mut buffer).expect("read request");
            let body =
                r#"{"access_token":"ya29.play-token","expires_in":3600,"token_type":"Bearer"}"#;
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });
    thread::spawn(move || {
        let bodies = [
            r#"{"id":"edit-9"}"#,
            r#"{"track":"internal","releases":[{"status":"completed","versionCodes":["123"]}]}"#,
            "",
        ];
        for (idx, body) in bodies.iter().enumerate() {
            let (mut stream, _) = api_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            match idx {
                0 => assert!(request.contains("POST /androidpublisher/v3/applications/com.acme.app/edits")),
                1 => assert!(request.contains("GET /androidpublisher/v3/applications/com.acme.app/edits/edit-9/tracks/internal")),
                _ => assert!(request.contains("DELETE /androidpublisher/v3/applications/com.acme.app/edits/edit-9")),
            }
            let response = if body.is_empty() {
                "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n".to_string()
            } else {
                format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                )
            };
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "play", "release", "status", "--track", "internal", "--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["track"], "internal");
    assert_eq!(parsed["data"]["releases"][0]["versionCodes"][0], "123");
}

#[test]
fn google_play_apply_json_applies_metadata_bundle() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    let metadata_dir = home.path().join("play-store");
    fs::create_dir_all(metadata_dir.join("listings")).expect("mkdir listings");
    fs::write(metadata_dir.join("details.json"), r#"{"contactEmail":"dev@acme.test"}"#)
        .expect("write details");
    fs::write(
        metadata_dir.join("listings").join("en-US.json"),
        r#"{"language":"en-US","title":"Acme App"}"#,
    )
    .expect("write listing");
    let token_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let token_addr = token_listener.local_addr().expect("local addr");
    let api_listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let api_addr = api_listener.local_addr().expect("local addr");
    let service_json = format!(
        r#"{{"type":"service_account","project_id":"acme-project","private_key":"{}","client_email":"si-test@acme-project.iam.gserviceaccount.com","token_uri":"http://{}/token"}}"#,
        test_app_private_key_pem().replace('\n', "\\n"),
        token_addr
    );
    fs::write(
        &settings_path,
        format!(
            r#"
[google]
default_account = "test"

[google.play]
api_base_url = "http://{api_addr}"

[google.play.accounts.test]
default_package_name = "com.acme.app"
"#
        ),
    )
    .expect("write settings");
    thread::spawn(move || {
        for _ in 0..4 {
            let (mut stream, _) = token_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let _ = stream.read(&mut buffer).expect("read request");
            let body =
                r#"{"access_token":"ya29.play-token","expires_in":3600,"token_type":"Bearer"}"#;
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });
    thread::spawn(move || {
        let bodies =
            [r#"{"id":"edit-3"}"#, r#"{"ok":true}"#, r#"{"ok":true}"#, r#"{"id":"edit-3"}"#];
        for (idx, body) in bodies.iter().enumerate() {
            let (mut stream, _) = api_listener.accept().expect("accept");
            let mut buffer = [0_u8; 4096];
            let read = stream.read(&mut buffer).expect("read request");
            let request = String::from_utf8_lossy(&buffer[..read]);
            match idx {
                0 => assert!(request.contains("POST /androidpublisher/v3/applications/com.acme.app/edits")),
                1 => assert!(request.contains("PATCH /androidpublisher/v3/applications/com.acme.app/edits/edit-3/details")),
                2 => assert!(request.contains("PATCH /androidpublisher/v3/applications/com.acme.app/edits/edit-3/listings/en-US")),
                _ => assert!(request.contains("POST /androidpublisher/v3/applications/com.acme.app/edits/edit-3:commit")),
            }
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );
            stream.write_all(response.as_bytes()).expect("write response");
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "play", "apply", "--metadata-dir"])
        .arg(&metadata_dir)
        .args(["--home"])
        .arg(home.path())
        .args(["--json"])
        .env("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", service_json)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["summary"]["details_updated"], true);
    assert_eq!(parsed["summary"]["listings_updated"], 1);
    assert_eq!(parsed["summary"]["track_updated"], false);
}

#[test]
fn version_matches_go_repo_version() {
    let output = cargo_bin().arg("version").assert().success().get_output().stdout.clone();
    assert_eq!(String::from_utf8(output).expect("utf8 version"), expected_version_line());
}

#[test]
fn version_flags_render_root_version() {
    let output = cargo_bin().arg("--version").assert().success().get_output().stdout.clone();
    assert_eq!(String::from_utf8(output).expect("utf8 --version"), expected_version_line());

    let output = cargo_bin().arg("-v").assert().success().get_output().stdout.clone();
    assert_eq!(String::from_utf8(output).expect("utf8 -v"), expected_version_line());
}

#[test]
fn help_output_uses_single_word_operational_subcommands() {
    let codex_help = String::from_utf8(
        cargo_bin().args(["codex", "--help"]).assert().success().get_output().stdout.clone(),
    )
    .expect("utf8 codex help");
    assert!(codex_help.contains("spawn"));
    assert!(codex_help.contains("profile"));
    assert!(codex_help.contains("tmux"));
    assert!(codex_help.contains("warmup"));
    assert!(!codex_help.contains("spawnplan"));
    assert!(!codex_help.contains("statusread"));
    assert!(!codex_help.contains("tmuxcommand"));
    assert!(!codex_help.contains("launch"));
    assert!(!codex_help.contains("plan"));

    let codex_spawn_help = String::from_utf8(
        cargo_bin()
            .args(["codex", "spawn", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 codex spawn help");
    assert!(codex_spawn_help.contains("--workspace"));
    assert!(codex_spawn_help.contains("--profile"));
    assert!(!codex_spawn_help.contains("plan"));
    assert!(!codex_spawn_help.contains("spec"));
    assert!(!codex_spawn_help.contains("args"));
    assert!(!codex_spawn_help.contains("Commands:"));

    let codex_tmux_help = String::from_utf8(
        cargo_bin()
            .args(["codex", "tmux", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 codex tmux help");
    assert!(codex_tmux_help.contains("[PROFILE]"));
    assert!(!codex_tmux_help.contains("launch"));
    assert!(!codex_tmux_help.contains("plan"));

    let build_self_help = String::from_utf8(
        cargo_bin()
            .args(["build", "self", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 build self help");
    assert!(build_self_help.contains("check"));
    assert!(build_self_help.contains("assets"));
    assert!(build_self_help.contains("validate"));
    assert!(!build_self_help.contains("releaseassets"));
    assert!(!build_self_help.contains("validatereleaseversion"));

    let build_help = String::from_utf8(
        cargo_bin().args(["build", "--help"]).assert().success().get_output().stdout.clone(),
    )
    .expect("utf8 build help");
    assert!(build_help.contains("installer"));
    assert!(!build_help.contains("image"));
}

#[test]
fn help_output_renders_root_command_summaries() {
    let root_help =
        String::from_utf8(cargo_bin().arg("--help").assert().success().get_output().stdout.clone())
            .expect("utf8 root help");

    assert!(root_help.contains("SI CLI for providers, runtimes, and build flows."));
    assert!(root_help.contains("codex"));
    assert!(root_help.contains("Manage Codex profiles and worker sessions."));
    assert!(root_help.contains("nucleus"));
    assert!(root_help.contains("Manage the SI Nucleus control plane."));
    assert!(root_help.contains("build"));
    assert!(root_help.contains("Build binaries and release assets."));
    assert!(root_help.contains("orbit"));
    assert!(root_help.contains("Manage first-party provider integrations."));
    assert!(!root_help.contains("providers   Inspect provider capabilities."));
    assert!(!root_help.contains("cloudflare"));
    assert!(!root_help.contains("github"));
}

#[test]
fn help_output_renders_nested_command_summaries() {
    let codex_warmup_help = String::from_utf8(
        cargo_bin()
            .args(["codex", "warmup", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 codex warmup help");

    assert!(codex_warmup_help.contains("Inspect Codex warmup state."));
    assert!(codex_warmup_help.contains("decision  Decide whether warmup should run."));
    assert!(codex_warmup_help.contains("run       Warm configured Codex profiles."));
    assert!(codex_warmup_help.contains("status    Show warmup status."));
    assert!(codex_warmup_help.contains("state     Warmup state file commands."));
    assert!(codex_warmup_help.contains("marker    Warmup marker file commands."));

    let codex_warmup_run_help = String::from_utf8(
        cargo_bin()
            .args(["codex", "warmup", "run", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 codex warmup run help");
    assert!(codex_warmup_run_help.contains("Warm configured Codex profiles."));

    let orbit_help = String::from_utf8(
        cargo_bin().args(["orbit", "--help"]).assert().success().get_output().stdout.clone(),
    )
    .expect("utf8 orbit help");
    assert!(orbit_help.contains("Manage first-party provider integrations."));
    assert!(orbit_help.contains("list"));
    assert!(orbit_help.contains("aws"));
    assert!(orbit_help.contains("openai"));
    assert!(orbit_help.contains("github"));

    let aws_s3_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "aws", "s3", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 aws s3 help");

    assert!(aws_s3_help.contains("AWS S3 commands."));
    assert!(aws_s3_help.contains("bucket  AWS S3 Bucket commands."));
    assert!(aws_s3_help.contains("object  AWS S3 Object commands."));
}

#[test]
fn help_output_supports_forced_color_palette() {
    let root_help = String::from_utf8(
        cargo_bin()
            .env("SI_CLI_COLOR", "always")
            .arg("--help")
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 root help");

    assert!(root_help.contains("\u{1b}["));
    assert!(root_help.contains("SI CLI for providers, runtimes, and build flows."));
}

#[test]
fn wrapper_help_and_paths_text_support_forced_color_palette() {
    let surf_help = String::from_utf8(
        cargo_bin()
            .env("SI_CLI_COLOR", "always")
            .args(["surf", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 surf help");
    assert!(surf_help.contains("\u{1b}["));
    assert!(surf_help.contains("Usage:"));

    let home = tempdir().expect("tempdir");
    let settings_output = String::from_utf8(
        cargo_bin()
            .env("SI_CLI_COLOR", "always")
            .args(["settings", "--home"])
            .arg(home.path())
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 settings output");
    assert!(settings_output.contains("\u{1b}["));
    assert!(settings_output.contains("paths.root"));
}

#[test]
fn help_output_uses_single_word_project_and_session_subcommands() {
    let github_project_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "github", "project", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 github project help");
    assert!(github_project_help.contains("add"));
    assert!(github_project_help.contains("unarchive"));
    assert!(!github_project_help.contains("itemadd"));
    assert!(!github_project_help.contains("item-unarchive"));

    let fort_help =
        String::from_utf8(cargo_bin().arg("fort").assert().success().get_output().stdout.clone())
            .expect("utf8 fort help");
    assert!(fort_help.contains("session"));
    assert!(fort_help.contains("runtime"));
    assert!(!fort_help.contains("sessionstate"));
    assert!(!fort_help.contains("runtimeagentstate"));
}

#[test]
fn help_output_uses_single_word_provider_subcommands() {
    let cloudflare_token_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "cloudflare", "token", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 cloudflare token help");
    assert!(cloudflare_token_help.contains("permissions"));
    assert!(!cloudflare_token_help.contains("permissiongroups"));

    let aws_sts_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "aws", "sts", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 aws sts help");
    assert!(aws_sts_help.contains("whoami"));
    assert!(aws_sts_help.contains("assume"));
    assert!(!aws_sts_help.contains("getcalleridentity"));
    assert!(!aws_sts_help.contains("assumerole"));

    let aws_bedrock_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "aws", "bedrock", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 aws bedrock help");
    assert!(aws_bedrock_help.contains("models"));
    assert!(aws_bedrock_help.contains("knowledge"));
    assert!(aws_bedrock_help.contains("assist"));
    assert!(!aws_bedrock_help.contains("foundationmodel"));
    assert!(!aws_bedrock_help.contains("knowledgebase"));
    assert!(!aws_bedrock_help.contains("agentruntime"));

    let gcp_iam_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "gcp", "iam", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 gcp iam help");
    assert!(gcp_iam_help.contains("account"));
    assert!(gcp_iam_help.contains("keys"));
    assert!(!gcp_iam_help.contains("serviceaccount"));
    assert!(!gcp_iam_help.contains("serviceaccountkey"));

    let google_places_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "google", "places", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 google places help");
    assert!(google_places_help.contains("text"));
    assert!(google_places_help.contains("nearby"));
    assert!(!google_places_help.contains("searchtext"));
    assert!(!google_places_help.contains("searchnearby"));

    let openai_project_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "openai", "project", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 openai project help");
    assert!(openai_project_help.contains("keys"));
    assert!(openai_project_help.contains("accounts"));
    assert!(openai_project_help.contains("limits"));
    assert!(!openai_project_help.contains("apikey"));
    assert!(!openai_project_help.contains("serviceaccount"));
    assert!(!openai_project_help.contains("ratelimit"));

    let oci_network_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "oci", "network", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 oci network help");
    assert!(oci_network_help.contains("gateway"));
    assert!(oci_network_help.contains("routes"));
    assert!(oci_network_help.contains("security"));
    assert!(!oci_network_help.contains("internetgateway"));
    assert!(!oci_network_help.contains("routetable"));
    assert!(!oci_network_help.contains("securitylist"));

    let github_git_help = String::from_utf8(
        cargo_bin()
            .args(["orbit", "github", "git", "--help"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone(),
    )
    .expect("utf8 github git help");
    assert!(github_git_help.contains("remote"));
    assert!(github_git_help.contains("clone"));
    assert!(!github_git_help.contains("remoteauth"));
    assert!(!github_git_help.contains("cloneauth"));
}

#[test]
fn legacy_hyphenated_aliases_still_work_for_help_paths() {
    cargo_bin().args(["build", "self", "release-assets", "--help"]).assert().success();
    cargo_bin().args(["orbit", "github", "project", "item-add", "--help"]).assert().success();
    cargo_bin().args(["fort", "session-state", "refresh-outcome", "--help"]).assert().success();
    cargo_bin().args(["fort", "runtime-agent-state", "show", "--help"]).assert().success();
    cargo_bin()
        .args(["orbit", "cloudflare", "token", "permission-groups", "--help"])
        .assert()
        .success();
    cargo_bin().args(["orbit", "aws", "sts", "get-caller-identity", "--help"]).assert().success();
    cargo_bin().args(["orbit", "aws", "bedrock", "foundation-model", "--help"]).assert().success();
    cargo_bin().args(["orbit", "gcp", "iam", "service-account", "--help"]).assert().success();
    cargo_bin().args(["orbit", "google", "places", "search-text", "--help"]).assert().success();
    cargo_bin()
        .args(["orbit", "google", "youtube", "video", "get-rating", "--help"])
        .assert()
        .success();
    cargo_bin().args(["orbit", "openai", "project", "api-key", "--help"]).assert().success();
    cargo_bin().args(["orbit", "oci", "network", "internet-gateway", "--help"]).assert().success();
    cargo_bin().args(["orbit", "stripe", "sync", "live-to-sandbox", "--help"]).assert().success();
    cargo_bin().args(["orbit", "github", "git", "remote-auth", "--help"]).assert().success();
    cargo_bin().args(["codex", "warmup", "marker", "write-autostart", "--help"]).assert().success();
}

#[test]
fn help_output_describes_github_release_create_tag_behavior() {
    let release_help = cargo_bin()
        .args(["orbit", "github", "release", "--help"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let release_help = String::from_utf8_lossy(&release_help);
    assert!(release_help.contains("Manage GitHub releases and release assets."));
    assert!(release_help.contains("create"));

    let create_help = cargo_bin()
        .args(["orbit", "github", "release", "create", "--help"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let create_help = String::from_utf8_lossy(&create_help);
    assert!(create_help.contains("Create a GitHub release."));
    assert!(create_help.contains("Create a GitHub release."));
    assert!(create_help.contains(
        "Target branch or commit SHA. Required when the tag does not already exist on the remote."
    ));
    assert!(create_help.contains("Release tag name, for example v0.55.12."));
}

#[test]
fn singleton_root_commands_run_without_placeholder_subcommands() {
    let commands_help = String::from_utf8(
        cargo_bin().args(["commands", "--help"]).assert().success().get_output().stdout.clone(),
    )
    .expect("utf8 commands help");
    assert!(commands_help.contains("List visible SI root commands."));
    assert!(commands_help.contains("commands [OPTIONS] [COMMAND]"));
    assert!(commands_help.contains("list  List visible SI root commands."));
    assert!(!commands_help.contains("Commandses"));

    let settings_help = String::from_utf8(
        cargo_bin().args(["settings", "--help"]).assert().success().get_output().stdout.clone(),
    )
    .expect("utf8 settings help");
    assert!(settings_help.contains("Show resolved SI settings."));
    assert!(settings_help.contains("settings [OPTIONS] [COMMAND]"));
    assert!(settings_help.contains("show  Show resolved SI settings."));

    let paths_stderr = String::from_utf8(
        cargo_bin().args(["paths", "--help"]).assert().failure().get_output().stderr.clone(),
    )
    .expect("utf8 paths stderr");
    assert!(paths_stderr.contains("`si paths` was removed"));
    assert!(paths_stderr.contains("`si settings`"));
}

#[test]
fn codex_status_command_is_not_available() {
    let output =
        cargo_bin().args(["codex", "status"]).assert().failure().get_output().stderr.clone();
    let stderr = String::from_utf8(output).expect("stderr utf8");
    assert!(stderr.contains("unrecognized subcommand"));
    assert!(stderr.contains("status"));
}

#[test]
fn codex_logs_command_is_not_available() {
    let output = cargo_bin().args(["codex", "logs"]).assert().failure().get_output().stderr.clone();
    let stderr = String::from_utf8(output).expect("stderr utf8");
    assert!(stderr.contains("unrecognized subcommand"));
    assert!(stderr.contains("logs"));
}

#[test]
fn codex_report_command_is_not_available() {
    let output =
        cargo_bin().args(["codex", "report"]).assert().failure().get_output().stderr.clone();
    let stderr = String::from_utf8(output).expect("stderr utf8");
    assert!(stderr.contains("unrecognized subcommand"));
    assert!(stderr.contains("report"));
}

#[test]
fn help_rejects_manifest_only_root_commands() {
    let stderr = String::from_utf8(
        cargo_bin().args(["help", "analyze"]).assert().failure().get_output().stderr.clone(),
    )
    .expect("help stderr utf8");
    assert!(stderr.contains("unknown root command: analyze"));
}

#[test]
fn help_json_lists_known_root_commands() {
    let output = cargo_bin()
        .args(["help", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let commands = parsed["commands"].as_array().expect("commands array should be present");

    assert!(commands.iter().any(|entry| entry["name"] == "build"));
    assert!(commands.iter().any(|entry| entry["name"] == "commands"));
    assert!(commands.iter().any(|entry| entry["name"] == "settings"));
    assert!(commands.iter().any(|entry| entry["name"] == "orbit"));
    assert!(commands.iter().any(|entry| entry["name"] == "codex"));
    assert!(commands.iter().any(|entry| entry["name"] == "nucleus"));
    assert!(commands.iter().any(|entry| entry["name"] == "vault"));
    assert!(commands.iter().any(|entry| entry["name"] == "fort"));
    assert!(!commands.iter().any(|entry| entry["name"] == "github"));
    assert!(!commands.iter().any(|entry| entry["name"] == "analyze"));
    assert!(!commands.iter().any(|entry| entry["name"] == "publish"));
    assert!(!commands.iter().any(|entry| entry["name"] == "social"));
    assert!(!commands.iter().any(|entry| entry["name"] == "releasemind"));
    assert!(!commands.iter().any(|entry| entry["name"] == "spawn"));
    assert!(!commands.iter().any(|entry| entry["name"] == "__fort-runtime-agent"));
}

#[test]
fn help_json_root_command_order_matches_public_cli_order() {
    let output = cargo_bin()
        .args(["help", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let commands = parsed["commands"].as_array().expect("commands array should be present");
    let names = commands
        .iter()
        .map(|entry| entry["name"].as_str().expect("command name"))
        .collect::<Vec<_>>();

    assert_eq!(
        names,
        vec![
            "version", "help", "build", "commands", "settings", "orbit", "image", "codex",
            "nucleus", "surf", "viva", "fort", "vault"
        ]
    );
}

#[test]
fn help_json_for_specific_command_includes_aliases() {
    let output = cargo_bin()
        .args(["help", "creds", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let commands = parsed["commands"].as_array().expect("commands array should be present");

    assert_eq!(commands.len(), 1);
    assert_eq!(commands[0]["name"], "vault");
    assert_eq!(commands[0]["aliases"][0], "creds");
}

#[test]
fn nucleus_help_surface_stays_bounded_to_plan_scope() {
    let output =
        cargo_bin().args(["nucleus", "--help"]).assert().success().get_output().stdout.clone();

    let help = String::from_utf8(output).expect("help output should be utf-8");
    for expected in [
        "Usage: si-rs nucleus <COMMAND>",
        "status",
        "profile",
        "service",
        "task",
        "worker",
        "session",
        "run",
        "events",
    ] {
        assert!(help.contains(expected), "nucleus help output must expose {expected}");
    }
    for forbidden in ["thread", "turn", "tmux", "transcript", "auth-json", "authjson"] {
        assert!(!help.contains(forbidden), "nucleus public surface must not expose {forbidden}");
    }
}

#[test]
fn commands_root_defaults_to_list_output() {
    let output = cargo_bin()
        .args(["commands", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let commands = parsed["commands"].as_array().expect("commands array should be present");
    assert!(commands.iter().any(|entry| entry["name"] == "commands"));
    assert!(commands.iter().any(|entry| entry["name"] == "settings"));
    assert!(commands.iter().any(|entry| entry["name"] == "nucleus"));
}

#[test]
fn settings_root_defaults_to_show() {
    let home = tempdir().expect("tempdir");

    let settings_output = cargo_bin()
        .args(["settings", "--format", "json", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let settings: Value = serde_json::from_slice(&settings_output).expect("json settings output");
    assert_eq!(settings["schema_version"], 1);
    assert_eq!(settings["paths"]["root"], path_string(home.path().join(".si")));
    assert_eq!(
        settings["paths"]["codex_profiles_dir"],
        path_string(home.path().join(".si/codex/profiles"))
    );
}

#[test]
fn settings_show_defaults_to_home_scoped_core_settings() {
    let home = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["settings", "show", "--format", "json", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["schema_version"], 1);
    assert_eq!(parsed["paths"]["root"], path_string(home.path().join(".si")));
    assert_eq!(
        parsed["paths"]["codex_profiles_dir"],
        path_string(home.path().join(".si/codex/profiles"))
    );
}

#[test]
fn settings_show_reads_core_runtime_fields() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    fs::write(
        &settings_path,
        r#"
[paths]
workspace_root = "~/Development"

[codex]
workspace = "~/Development/si"
workdir = "/workspace"
profile = "profile-delta"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["settings", "show", "--format", "json", "--home"])
        .arg(home.path())
        .args(["--settings-file"])
        .arg(&settings_path)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["paths"]["workspace_root"], "~/Development");
    assert_eq!(parsed["codex"]["workspace"], "~/Development/si");
    assert_eq!(parsed["codex"]["workdir"], "/workspace");
    assert_eq!(parsed["codex"]["profile"], "profile-delta");
}

#[test]
fn warmup_status_json_reads_and_upgrades_legacy_state() {
    let home = tempdir().expect("tempdir");
    let warmup_dir = home.path().join(".si/warmup");
    fs::create_dir_all(&warmup_dir).expect("mkdir warmup dir");
    fs::write(
        warmup_dir.join("state.json"),
        r#"{
  "version": 2,
  "profiles": {
    "profile-zeta": {
      "profile_id": " profile-zeta ",
      "last_result": "warmed",
      "last_weekly_used_pct": 12.5,
      "last_weekly_reset": "2030-03-20T00:00:00Z"
    }
  }
}
"#,
    )
    .expect("write state");

    let output = cargo_bin()
        .args(["codex", "warmup", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["version"], 3);
    assert_eq!(parsed["profiles"]["profile-zeta"]["profile_id"], "profile-zeta");
    assert_eq!(parsed["profiles"]["profile-zeta"]["last_warmed_reset"], "2030-03-20T00:00:00Z");
    assert_eq!(parsed["profiles"]["profile-zeta"]["last_weekly_used_ok"], true);
}

#[test]
fn warmup_status_text_reports_empty_state() {
    let home = tempdir().expect("tempdir");

    let output = cargo_bin()
        .args(["codex", "warmup", "status", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    assert_eq!(String::from_utf8_lossy(&output), "warmup state is empty\n");
}

#[test]
fn warmup_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("state.json");

    cargo_bin()
        .args(["codex", "warmup", "state", "write", "--path"])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"version":0,"updated_at":" 2030-03-19T12:00:00Z ","profiles":{" profile-zeta ":{"profile_id":" profile-zeta ","last_result":" ready "}}}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["version"], 3);
    assert_eq!(parsed["updated_at"], "2030-03-19T12:00:00Z");
    assert_eq!(parsed["profiles"]["profile-zeta"]["profile_id"], "profile-zeta");
    assert_eq!(parsed["profiles"]["profile-zeta"]["last_result"], "ready");
}

#[test]
fn warmup_marker_show_reports_disabled_and_autostart() {
    let home = tempdir().expect("tempdir");
    let warmup_dir = home.path().join(".si/warmup");
    fs::create_dir_all(&warmup_dir).expect("mkdir warmup dir");
    fs::write(warmup_dir.join("autostart.v1"), "2030-03-19T12:00:00Z\n").expect("write autostart");
    fs::write(warmup_dir.join("disabled.v1"), "disabled\n").expect("write disabled");

    let output = cargo_bin()
        .args(["codex", "warmup", "marker", "show", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["disabled"], true);
    assert_eq!(parsed["autostart_present"], true);
}

#[test]
fn warmup_marker_write_and_set_disabled_update_files() {
    let state_dir = tempdir().expect("tempdir");
    let autostart_path = state_dir.path().join("autostart.v1");
    let disabled_path = state_dir.path().join("disabled.v1");

    cargo_bin()
        .args(["codex", "warmup", "marker", "write-autostart", "--path"])
        .arg(&autostart_path)
        .assert()
        .success();
    cargo_bin()
        .args(["codex", "warmup", "marker", "set-disabled", "--path"])
        .arg(&disabled_path)
        .args(["--disabled=true"])
        .assert()
        .success();
    assert!(autostart_path.exists());
    assert!(disabled_path.exists());

    cargo_bin()
        .args(["codex", "warmup", "marker", "set-disabled", "--path"])
        .arg(&disabled_path)
        .args(["--disabled=false"])
        .assert()
        .success();
    assert!(!disabled_path.exists());
}

#[test]
fn warmup_autostart_decision_uses_due_schedule_and_disabled_marker() {
    let home = tempdir().expect("tempdir");
    let warmup_dir = home.path().join(".si/warmup");
    fs::create_dir_all(&warmup_dir).expect("mkdir warmup dir");
    fs::write(
        warmup_dir.join("state.json"),
        r#"{"version":3,"profiles":{"profile-zeta":{"profile_id":"profile-zeta","last_result":"ready","next_due":"2099-03-19T12:00:00Z"}}}"#,
    )
    .expect("write state");

    let output = cargo_bin()
        .args(["codex", "warmup", "autostart-decision", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requested"], false);
    assert_eq!(parsed["reason"], "legacy_scheduled");

    fs::write(
        warmup_dir.join("state.json"),
        r#"{"version":3,"profiles":{"profile-zeta":{"profile_id":"profile-zeta","last_result":"ready","next_due":"2000-03-19T12:00:00Z"}}}"#,
    )
    .expect("write due state");
    let output = cargo_bin()
        .args(["codex", "warmup", "autostart-decision", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requested"], true);
    assert_eq!(parsed["reason"], "legacy_due");

    fs::write(warmup_dir.join("disabled.v1"), "disabled\n").expect("write disabled");
    let output = cargo_bin()
        .args(["codex", "warmup", "autostart-decision", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requested"], false);
    assert_eq!(parsed["reason"], "disabled");
}

#[test]
fn providers_characteristics_json_matches_expected_shape() {
    let output = cargo_bin()
        .args(["orbit", "list", "--provider", "github", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["policy"]["defaults"], "built_in_go");
    let providers = parsed["providers"].as_array().expect("providers array");
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0]["provider"], "github");
    assert_eq!(providers[0]["base_url"], "https://api.github.com");
    assert_eq!(providers[0]["api_version"], "2022-11-28");
    assert_eq!(providers[0]["public_probe"]["path"], "/zen");
}

#[test]
fn providers_characteristics_supports_alias_ids() {
    let output = cargo_bin()
        .args(["orbit", "list", "--provider", "twitter", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let providers = parsed["providers"].as_array().expect("providers array");
    assert_eq!(providers[0]["provider"], "social_x");
}

#[test]
fn github_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"
default_auth_mode = "oauth"

[github.accounts.core]
name = "Core"
owner = "Aureuma"
api_base_url = "https://ghe.example/api/v3"
auth_mode = "oauth"

[github.accounts.ops]
name = "Ops"
owner = "OpsOrg"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "github", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts.len(), 2);
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["auth_mode"], "oauth");
    assert_eq!(contexts[1]["alias"], "ops");
    assert_eq!(contexts[1]["auth_mode"], "oauth");
}

#[test]
fn github_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"
default_auth_mode = "app"

[github.accounts.core]
owner = "Aureuma"
api_base_url = "https://ghe.example/api/v3"
auth_mode = "oauth"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "github", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["auth_mode"], "oauth");
    assert_eq!(parsed["base_url"], "https://ghe.example/api/v3");
    assert_eq!(parsed["source"], "settings.default_account,settings.auth_mode");
}

#[test]
fn github_auth_status_json_resolves_oauth_token() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"

[github.accounts.core]
owner = "Aureuma"
auth_mode = "oauth"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args(["orbit", "github", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["auth_mode"], "oauth");
    assert_eq!(parsed["base_url"], "https://api.github.com");
    assert_eq!(parsed["source"], "settings.default_account,settings.auth_mode,env:GITHUB_TOKEN");
    assert_eq!(parsed["token_preview"], "gho_exam...");
}

#[test]
fn github_auth_status_json_resolves_app_credentials() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"
default_auth_mode = "app"

[github.accounts.core]
owner = "Aureuma"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("GITHUB_CORE_APP_ID", "42")
        .env("GITHUB_CORE_APP_PRIVATE_KEY_PEM", "-----BEGIN PRIVATE KEY-----abc")
        .env("GITHUB_CORE_INSTALLATION_ID", "99")
        .args(["orbit", "github", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["auth_mode"], "app");
    assert_eq!(parsed["base_url"], "https://api.github.com");
    assert_eq!(
        parsed["source"],
        "settings.default_account,settings.default_auth_mode,env:GITHUB_CORE_APP_ID,env:GITHUB_CORE_APP_PRIVATE_KEY_PEM,env:GITHUB_CORE_INSTALLATION_ID"
    );
    assert_eq!(parsed["token_preview"], "-");
}

#[test]
fn github_release_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("GET /repos/Aureuma/si/releases?page=1&per_page=100 HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_release_list")],
            r#"[{"id":101,"tag_name":"v1.2.3"}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "release",
            "list",
            "Aureuma/si",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_release_list");
    assert_eq!(parsed["list"][0]["tag_name"], "v1.2.3");
    server.join();
}

#[test]
fn github_release_get_json_fetches_tag_with_app_auth() {
    let call_count = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let calls = std::sync::Arc::clone(&call_count);
    let server = start_http_server(2, move |request| {
        let call = calls.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(
                    request.starts_with("POST /app/installations/321/access_tokens HTTP/1.1\r\n")
                );
                assert!(request.contains("authorization: Bearer "));
                http_json_response(
                    "201 Created",
                    &[("x-github-request-id", "req_gh_install")],
                    r#"{"token":"ghs_install_token","expires_at":"2030-01-01T00:00:00Z"}"#,
                )
            }
            1 => {
                assert!(
                    request.starts_with("GET /repos/Aureuma/si/releases/tags/v1.2.3 HTTP/1.1\r\n")
                );
                assert!(request.contains("authorization: Bearer ghs_install_token\r\n"));
                http_json_response(
                    "200 OK",
                    &[("x-github-request-id", "req_gh_release_get")],
                    r#"{"id":101,"tag_name":"v1.2.3"}"#,
                )
            }
            _ => panic!("unexpected request"),
        }
    });

    let app_key = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCaJZkLuu/uJGz1\n4cxlZ3d7H5b88tcXH0qPZmkCUPWHA4aumx36BErkorXukYD0IRhRaJe8shsgRC4c\nw5TkjrXcG9Kigh3HvifRnA1kCbmwceANdww6J8ggtFDFO026VIEx2R8tjtYLs+pU\n+Xb6llxixE+QWSXQVHqHy67KvWDeRu6es8OZb8klxFejwdTBC0UDxNLwdr+hDV3b\nEDduxm+pnnmTi7ciwDbrO8D/GXkYi7YLXwqcfHLhVqZeVXrs5JPc+7pOHJCf1fZO\n9BBUOVO9qDUqfQk7CWBF3MKyNtx/wv+Mzg5ztl4VMPRgdnbnU8B2en+rPYZg7KTF\nN2n0ORH/AgMBAAECggEAAfNDfkZVXnN1Mh/duKi4S8VTYTbnVBe6we60mb68JIL9\nvhF2AyGbxaHDYIB/G6zxhFIo8qO5kSJxB5R35UkNnE/OeJeMgz2bflzq6cmaYP+d\nKz5xgqjZ24QR2N+jtPL4bCYy7UjMhNBiwMQj5mQRnimdV2uxUp3xq5cpn89ekuFY\n1C48pXicl8OLgdzhNAROk2edrYo+DJl+5VaSPSN5L+dz67pBqAZ4gcUj4ZdmofmB\ninHw83zTvQfSFaykC98TJEpQppaC8gK+mxQF6bWotfxq/Gd2MBhNwJAF1WnJ2cq/\np2vuDCqliKbt40M33qUVIavhY6C50dUQ3VeERxmvyQKBgQDSlBBZJ2auZHgJeR/U\nIYUPOypo8mBBVMh6axbRR5yrpTfGDHqc4Zx4nC3kxRjqnA+sfdZBESOgvj7FdWUj\nf3fEM+RPQLW0zu2F+wmJ2w28kncOFVxHrrrxJToKtBSfR3YIjCnZmy6pxn8WOimM\nabOm5hmSRLgMcRSvptw6crOOtwKBgQC7ZXCuTgnod+Cf25PvKNxSLJOy9lephPYO\nqU7LWywilQEgj7VWrmVKP+6HC3L615++cLlKxoozlvT0dxjfhzgdZxXKLOUf4x3d\n72FXx/sKFFtOCgeDeR2Ln+hSLbGsCLkyOo5zFFCidmE4z0DitiPmSRtJdHt1VthO\n8KW10yTO+QKBgCBZhrlriCa6YIZ0CSO5kotod3dv5MGkmLfVw8eazMLBuvO97wgy\n0Krms1Y1wUIpf27sVgHg9Cw5jcMf6c2uQ2Ps5OIX+tIwB+VRT4HSGSYjCg8r0OVi\nPm3VXjlOuOxPOh7OCY/Yey6xw8xSWxerFWJKbxs9W1jt9lOVurdv7425AoGBAKIU\nQ5hOoN0yydIZjWK92YktSvXvgLR67oKRxze1fH/Qlm/+O55kKfFFSF3+9gyk8GI7\nhtd4ztF+EBFc7ONwRYWQwlTh7a5dtlhdEbllmugF4U6m+Aare3Vm8f4ZzWD5Doy1\n/rzj5jYN41rKTtmHJZeoxXQLzjgXy/DCzOBtZZmpAoGABacst96WKng6XE5MkZpo\nacIEMOPpPYnyc4VgqHPft4D45ARP4wFZryxZ58Ya6194Z9PUzL5N7yKgsQZlnGR8\nL6W4ulLYfyhkWfi592cIKS7eDjWijbcIUzgvuIzCWvme08KQSPkgYNFXomlg4EZv\n9HrWPhpFaH+jHJsVKmD/Qyo=\n-----END PRIVATE KEY-----";

    let output = cargo_bin()
        .env("GITHUB_APP_PRIVATE_KEY_PEM", app_key)
        .args([
            "orbit",
            "github",
            "release",
            "get",
            "Aureuma/si",
            "v1.2.3",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "app",
            "--app-id",
            "123",
            "--installation-id",
            "321",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_release_get");
    assert_eq!(parsed["data"]["tag_name"], "v1.2.3");
    server.join();
}

#[test]
fn github_release_create_json_mutates_via_api_with_oauth() {
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let server = start_http_server(2, move |request| {
        let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(
                    request.starts_with("GET /repos/Aureuma/si/git/ref/tags/v1.2.4 HTTP/1.1\r\n")
                );
                http_json_response(
                    "200 OK",
                    &[("x-github-request-id", "req_gh_tag_ref_get")],
                    r#"{"ref":"refs/tags/v1.2.4","object":{"sha":"abc123"}}"#,
                )
            }
            1 => {
                assert!(request.starts_with("POST /repos/Aureuma/si/releases HTTP/1.1\r\n"));
                assert!(request.contains("\"tag_name\":\"v1.2.4\""));
                assert!(request.contains("\"name\":\"Release 1.2.4\""));
                assert!(request.contains("\"draft\":true"));
                http_json_response(
                    "201 Created",
                    &[("x-github-request-id", "req_gh_release_create")],
                    r#"{"id":102,"tag_name":"v1.2.4","name":"Release 1.2.4"}"#,
                )
            }
            _ => panic!("unexpected request"),
        }
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "release",
            "create",
            "Aureuma/si",
            "--tag",
            "v1.2.4",
            "--title",
            "Release 1.2.4",
            "--draft",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_release_create");
    assert_eq!(parsed["data"]["tag_name"], "v1.2.4");
    server.join();
}

#[test]
fn github_release_create_json_creates_missing_tag_ref_before_release() {
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let server = start_http_server(3, move |request| {
        let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => http_json_response(
                "404 Not Found",
                &[("x-github-request-id", "req_gh_tag_ref_missing")],
                r#"{"message":"Not Found"}"#,
            ),
            1 => {
                assert!(request.starts_with("POST /repos/Aureuma/si/git/refs HTTP/1.1\r\n"));
                assert!(request.contains("\"ref\":\"refs/tags/v1.2.5\""));
                assert!(request.contains("\"sha\":\"abc123def456\""));
                http_json_response(
                    "201 Created",
                    &[("x-github-request-id", "req_gh_tag_ref_create")],
                    r#"{"ref":"refs/tags/v1.2.5"}"#,
                )
            }
            2 => {
                assert!(request.starts_with("POST /repos/Aureuma/si/releases HTTP/1.1\r\n"));
                assert!(request.contains("\"tag_name\":\"v1.2.5\""));
                assert!(request.contains("\"target_commitish\":\"abc123def456\""));
                http_json_response(
                    "201 Created",
                    &[("x-github-request-id", "req_gh_release_create")],
                    r#"{"id":103,"tag_name":"v1.2.5","name":"Release 1.2.5"}"#,
                )
            }
            _ => panic!("unexpected request"),
        }
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "release",
            "create",
            "Aureuma/si",
            "--tag",
            "v1.2.5",
            "--title",
            "Release 1.2.5",
            "--target",
            "abc123def456",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_release_create");
    assert_eq!(parsed["data"]["tag_name"], "v1.2.5");
    server.join();
}

#[test]
fn github_release_create_requires_target_when_tag_is_missing() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/git/ref/tags/v1.2.6 HTTP/1.1\r\n"));
        http_json_response(
            "404 Not Found",
            &[("x-github-request-id", "req_gh_tag_ref_missing")],
            r#"{"message":"Not Found"}"#,
        )
    });

    let assert = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "release",
            "create",
            "Aureuma/si",
            "--tag",
            "v1.2.6",
            "--title",
            "Release 1.2.6",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(
        stderr.contains(
            "github tag v1.2.6 does not exist; pass --target <sha> or create the tag first"
        ),
        "stderr was: {stderr}"
    );

    server.join();
}

#[test]
fn github_release_upload_json_mutates_via_api_with_oauth() {
    let file = tempfile::NamedTempFile::new().expect("temp file");
    std::fs::write(file.path(), b"asset-bytes").expect("write asset");
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let upload_base = std::sync::Arc::new(std::sync::Mutex::new(String::new()));
    let upload_base_for_server = upload_base.clone();
    let server = start_http_server(2, move |request| {
        let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(
                    request.starts_with("GET /repos/Aureuma/si/releases/tags/v1.2.4 HTTP/1.1\r\n")
                );
                let base_url = upload_base_for_server.lock().expect("lock upload base").clone();
                http_json_response(
                    "200 OK",
                    &[("x-github-request-id", "req_gh_release_meta")],
                    &format!(
                        r#"{{"id":102,"tag_name":"v1.2.4","upload_url":"{}/uploads/repos/Aureuma/si/releases/102/assets{{?name,label}}"}}"#,
                        base_url
                    ),
                )
            }
            1 => {
                assert!(
                    request.starts_with("POST /uploads/repos/Aureuma/si/releases/102/assets?name=")
                );
                assert!(request.contains("content-type: application/octet-stream\r\n"));
                http_json_response(
                    "201 Created",
                    &[("x-github-request-id", "req_gh_release_upload")],
                    r#"{"id":301,"name":"asset.tgz"}"#,
                )
            }
            _ => panic!("unexpected request"),
        }
    });
    *upload_base.lock().expect("lock upload base") = server.base_url.clone();
    let asset_path = file.path().to_string_lossy().to_string();
    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "release",
            "upload",
            "Aureuma/si",
            "v1.2.4",
            "--asset",
            &asset_path,
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_release_upload");
    assert_eq!(parsed["data"]["id"], 301);
    server.join();
}

#[test]
fn github_release_delete_json_mutates_via_api_with_oauth() {
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let server = start_http_server(2, move |request| {
        let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(
                    request.starts_with("GET /repos/Aureuma/si/releases/tags/v1.2.4 HTTP/1.1\r\n")
                );
                http_json_response(
                    "200 OK",
                    &[("x-github-request-id", "req_gh_release_meta_delete")],
                    r#"{"id":102,"tag_name":"v1.2.4"}"#,
                )
            }
            1 => {
                assert!(request.starts_with("DELETE /repos/Aureuma/si/releases/102 HTTP/1.1\r\n"));
                http_json_response(
                    "204 No Content",
                    &[("x-github-request-id", "req_gh_release_delete")],
                    "",
                )
            }
            _ => panic!("unexpected request"),
        }
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "release",
            "delete",
            "Aureuma/si",
            "v1.2.4",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 204);
    assert_eq!(parsed["request_id"], "req_gh_release_delete");
    server.join();
}

#[test]
fn github_secret_repo_set_json_encrypts_and_mutates_with_oauth() {
    let key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let server =
        start_http_server(2, move |request| {
            let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            match call {
                0 => {
                    assert!(request.starts_with(
                        "GET /repos/Aureuma/si/actions/secrets/public-key HTTP/1.1\r\n"
                    ));
                    http_json_response(
                        "200 OK",
                        &[("x-github-request-id", "req_gh_secret_key")],
                        &format!(r#"{{"key_id":"kid-1","key":"{}"}}"#, key),
                    )
                }
                1 => {
                    assert!(request.starts_with(
                        "PUT /repos/Aureuma/si/actions/secrets/MY_SECRET HTTP/1.1\r\n"
                    ));
                    assert!(request.contains("\"key_id\":\"kid-1\""));
                    assert!(request.contains("\"encrypted_value\":\""));
                    assert!(!request.contains("super-secret"));
                    http_json_response("201 Created", &[], "")
                }
                _ => panic!("unexpected request"),
            }
        });
    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "secret",
            "repo",
            "set",
            "Aureuma/si",
            "MY_SECRET",
            "--value",
            "super-secret",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["data"]["scope"], "repo");
    server.join();
}

#[test]
fn github_secret_repo_delete_json_mutates_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("DELETE /repos/Aureuma/si/actions/secrets/MY_SECRET HTTP/1.1\r\n")
        );
        http_json_response(
            "204 No Content",
            &[("x-github-request-id", "req_gh_secret_repo_delete")],
            "",
        )
    });
    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "secret",
            "repo",
            "delete",
            "Aureuma/si",
            "MY_SECRET",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 204);
    server.join();
}

#[test]
fn github_secret_env_set_json_encrypts_and_mutates_with_oauth() {
    let key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let server = start_http_server(2, move |request| {
        let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(request.starts_with(
                    "GET /repos/Aureuma/si/environments/prod/secrets/public-key HTTP/1.1\r\n"
                ));
                http_json_response(
                    "200 OK",
                    &[],
                    &format!(r#"{{"key_id":"kid-2","key":"{}"}}"#, key),
                )
            }
            1 => {
                assert!(request.starts_with(
                    "PUT /repos/Aureuma/si/environments/prod/secrets/MY_SECRET HTTP/1.1\r\n"
                ));
                assert!(!request.contains("super-secret"));
                http_json_response("201 Created", &[], "")
            }
            _ => panic!("unexpected request"),
        }
    });
    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "secret",
            "env",
            "set",
            "Aureuma/si",
            "prod",
            "MY_SECRET",
            "--value",
            "super-secret",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["scope"], "env");
    assert_eq!(parsed["data"]["environment"], "prod");
    server.join();
}

#[test]
fn github_secret_env_delete_json_mutates_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "DELETE /repos/Aureuma/si/environments/prod/secrets/MY_SECRET HTTP/1.1\r\n"
        ));
        http_json_response("204 No Content", &[], "")
    });
    cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "secret",
            "env",
            "delete",
            "Aureuma/si",
            "prod",
            "MY_SECRET",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success();
    server.join();
}

#[test]
fn github_secret_org_set_json_encrypts_and_mutates_with_oauth() {
    let key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let seen = calls.clone();
    let server = start_http_server(2, move |request| {
        let call = seen.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(
                    request
                        .starts_with("GET /orgs/Aureuma/actions/secrets/public-key HTTP/1.1\r\n")
                );
                http_json_response(
                    "200 OK",
                    &[],
                    &format!(r#"{{"key_id":"kid-3","key":"{}"}}"#, key),
                )
            }
            1 => {
                assert!(
                    request.starts_with("PUT /orgs/Aureuma/actions/secrets/MY_SECRET HTTP/1.1\r\n")
                );
                assert!(request.contains("\"visibility\":\"selected\""));
                assert!(request.contains("\"selected_repository_ids\":[1,2]"));
                assert!(!request.contains("super-secret"));
                http_json_response("201 Created", &[], "")
            }
            _ => panic!("unexpected request"),
        }
    });
    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "secret",
            "org",
            "set",
            "Aureuma",
            "MY_SECRET",
            "--value",
            "super-secret",
            "--visibility",
            "selected",
            "--repos",
            "1,2",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["scope"], "org");
    assert_eq!(parsed["data"]["org"], "Aureuma");
    server.join();
}

#[test]
fn github_secret_org_delete_json_mutates_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("DELETE /orgs/Aureuma/actions/secrets/MY_SECRET HTTP/1.1\r\n"));
        http_json_response("204 No Content", &[], "")
    });
    cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "secret",
            "org",
            "delete",
            "Aureuma",
            "MY_SECRET",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success();
    server.join();
}

#[test]
fn github_repo_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /users/Aureuma/repos?page=1&per_page=100 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_repo_list")],
            r#"[{"id":101,"full_name":"Aureuma/si"}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "repo",
            "list",
            "Aureuma",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["count"], 1);
    assert_eq!(parsed["data"][0]["full_name"], "Aureuma/si");
    server.join();
}

#[test]
fn github_repo_get_json_fetches_repo_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_repo_get")],
            r#"{"id":101,"full_name":"Aureuma/si"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "repo",
            "get",
            "Aureuma/si",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_repo_get");
    assert_eq!(parsed["data"]["full_name"], "Aureuma/si");
    server.join();
}

#[test]
fn github_repo_create_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /orgs/Aureuma/repos HTTP/1.1\r\n"));
        assert!(request.contains("\"name\":\"si-rs\""));
        assert!(request.contains("\"private\":true"));
        http_json_response(
            "201 Created",
            &[("x-github-request-id", "req_gh_repo_create")],
            r#"{"id":202,"full_name":"Aureuma/si-rs","private":true}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "repo",
            "create",
            "si-rs",
            "--owner",
            "Aureuma",
            "--param",
            "private=true",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_repo_create");
    assert_eq!(parsed["data"]["full_name"], "Aureuma/si-rs");
    server.join();
}

#[test]
fn github_repo_update_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PATCH /repos/Aureuma/si HTTP/1.1\r\n"));
        assert!(request.contains("\"homepage\":\"https://example.com\""));
        assert!(request.contains("\"has_issues\":false"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_repo_update")],
            r#"{"id":101,"full_name":"Aureuma/si","homepage":"https://example.com","has_issues":false}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "repo",
            "update",
            "Aureuma/si",
            "--param",
            "homepage=https://example.com",
            "--param",
            "has_issues=false",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_repo_update");
    assert_eq!(parsed["data"]["homepage"], "https://example.com");
    server.join();
}

#[test]
fn github_repo_archive_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PATCH /repos/Aureuma/si HTTP/1.1\r\n"));
        assert!(request.contains("\"archived\":true"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_repo_archive")],
            r#"{"id":101,"full_name":"Aureuma/si","archived":true}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "repo",
            "archive",
            "Aureuma/si",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_repo_archive");
    assert_eq!(parsed["data"]["archived"], true);
    server.join();
}

#[test]
fn github_repo_delete_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("DELETE /repos/Aureuma/si HTTP/1.1\r\n"));
        http_json_response("204 No Content", &[("x-github-request-id", "req_gh_repo_delete")], "")
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "repo",
            "delete",
            "Aureuma/si",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 204);
    assert_eq!(parsed["request_id"], "req_gh_repo_delete");
    server.join();
}

#[test]
fn github_project_list_json_fetches_from_graphql_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_list")],
            r#"{"data":{"organization":{"projectsV2":{"nodes":[{"id":"PVT_123","number":7,"title":"Roadmap","public":true,"closed":false,"url":"https://github.com/orgs/Aureuma/projects/7"}]}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "list",
            "Aureuma",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["organization"], "Aureuma");
    assert_eq!(parsed["count"], 1);
    assert_eq!(parsed["projects"][0]["title"], "Roadmap");
    server.join();
}

#[test]
fn github_project_get_json_fetches_from_graphql_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_get")],
            r#"{"data":{"node":{"id":"PVT_123","number":7,"title":"Roadmap","public":true,"closed":false,"url":"https://github.com/orgs/Aureuma/projects/7","items":{"totalCount":3},"fields":{"totalCount":4}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "get",
            "PVT_123",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project"]["id"], "PVT_123");
    assert_eq!(parsed["project"]["title"], "Roadmap");
    server.join();
}

#[test]
fn github_project_fields_json_fetches_from_graphql_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_fields")],
            r#"{"data":{"node":{"id":"PVT_123","fields":{"nodes":[{"id":"PVTF_1","name":"Status","dataType":"SINGLE_SELECT","options":[{"id":"opt_1","name":"Todo"}]},{"id":"PVTF_2","name":"Sprint","dataType":"ITERATION","configuration":{"iterations":[{"id":"iter_1","title":"Sprint 1","startDate":"2026-03-01","duration":14}]}}]}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "fields",
            "PVT_123",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["count"], 2);
    assert_eq!(parsed["fields"][0]["name"], "Status");
    server.join();
}

#[test]
fn github_project_items_json_fetches_from_graphql_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        assert!(request.contains("\"includeArchived\":true"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_items")],
            r#"{"data":{"node":{"id":"PVT_123","items":{"nodes":[{"id":"PVTI_1","isArchived":false,"type":"ISSUE","content":{"__typename":"Issue","id":"I_1","number":42,"title":"Port project reads","state":"OPEN","url":"https://github.com/Aureuma/si/issues/42","repository":{"name":"si","owner":{"login":"Aureuma"}}},"fieldValues":{"nodes":[{"text":"in progress","field":{"id":"PVTF_1","name":"Status"}}]}}]}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "items",
            "PVT_123",
            "--include-archived",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["count"], 1);
    assert_eq!(parsed["items"][0]["content"]["number"], 42);
    server.join();
}

#[test]
fn github_project_update_json_mutates_via_graphql_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        assert!(request.contains("updateProjectV2"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_update")],
            r#"{"data":{"updateProjectV2":{"projectV2":{"id":"PVT_123","number":7,"title":"Roadmap 2","shortDescription":"Updated plan","readme":"none","public":true,"closed":false,"url":"https://github.com/orgs/Aureuma/projects/7","updatedAt":"2026-03-16T00:00:00Z"}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "update",
            "PVT_123",
            "--title",
            "Roadmap 2",
            "--description",
            "Updated plan",
            "--public",
            "true",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["project"]["title"], "Roadmap 2");
    assert_eq!(parsed["project"]["public"], true);
    server.join();
}

#[test]
fn github_project_item_add_json_resolves_issue_and_mutates() {
    let server = start_http_server(2, |request| {
        if request.starts_with("GET /repos/Aureuma/si/issues/42 HTTP/1.1\r\n") {
            return http_json_response(
                "200 OK",
                &[("x-github-request-id", "req_gh_issue_node")],
                r#"{"id":42,"node_id":"I_kwDOAAABcd","title":"Port project reads"}"#,
            );
        }
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("addProjectV2ItemById"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_item_add")],
            r#"{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_1","type":"ISSUE"}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "item-add",
            "PVT_123",
            "--repo",
            "Aureuma/si",
            "--issue",
            "42",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["content_id"], "I_kwDOAAABcd");
    assert_eq!(parsed["item"]["id"], "PVTI_1");
    server.join();
}

#[test]
fn github_project_item_set_json_resolves_field_name_and_mutates() {
    let server = start_http_server(2, |request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        if request.contains("fields(first:$first)") {
            return http_json_response(
                "200 OK",
                &[("x-github-request-id", "req_gh_project_fields_lookup")],
                r#"{"data":{"node":{"id":"PVT_123","fields":{"nodes":[{"id":"PVTF_1","name":"Status","dataType":"SINGLE_SELECT","options":[{"id":"opt_1","name":"Todo"},{"id":"opt_2","name":"Done"}]}]}}}}"#,
            );
        }
        assert!(request.contains("updateProjectV2ItemFieldValue"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_item_set")],
            r#"{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_1"}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "item-set",
            "PVT_123",
            "PVTI_1",
            "--field",
            "Status",
            "--single-select",
            "Todo",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["field_id"], "PVTF_1");
    assert_eq!(parsed["value"]["singleSelectOptionId"], "opt_1");
    assert_eq!(parsed["project_item"]["id"], "PVTI_1");
    server.join();
}

#[test]
fn github_project_item_clear_json_resolves_field_name_and_mutates() {
    let server = start_http_server(2, |request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        if request.contains("fields(first:$first)") {
            return http_json_response(
                "200 OK",
                &[("x-github-request-id", "req_gh_project_fields_lookup_clear")],
                r#"{"data":{"node":{"id":"PVT_123","fields":{"nodes":[{"id":"PVTF_1","name":"Status","dataType":"TEXT"}]}}}}"#,
            );
        }
        assert!(request.contains("clearProjectV2ItemFieldValue"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_item_clear")],
            r#"{"data":{"clearProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_1"}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "item-clear",
            "PVT_123",
            "PVTI_1",
            "--field",
            "Status",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["field_id"], "PVTF_1");
    assert_eq!(parsed["project_item"]["id"], "PVTI_1");
    server.join();
}

#[test]
fn github_project_item_archive_json_mutates_via_graphql() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("archiveProjectV2Item"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_item_archive")],
            r#"{"data":{"archiveProjectV2Item":{"item":{"id":"PVTI_1","isArchived":true}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "item-archive",
            "PVT_123",
            "PVTI_1",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["item"]["isArchived"], true);
    server.join();
}

#[test]
fn github_project_item_unarchive_json_mutates_via_graphql() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("unarchiveProjectV2Item"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_item_unarchive")],
            r#"{"data":{"unarchiveProjectV2Item":{"item":{"id":"PVTI_1","isArchived":false}}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "item-unarchive",
            "PVT_123",
            "PVTI_1",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["item"]["isArchived"], false);
    server.join();
}

#[test]
fn github_project_item_delete_json_mutates_via_graphql() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("deleteProjectV2Item"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_project_item_delete")],
            r#"{"data":{"deleteProjectV2Item":{"deletedItemId":"PVTI_1"}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "project",
            "item-delete",
            "PVT_123",
            "PVTI_1",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["project_id"], "PVT_123");
    assert_eq!(parsed["deleted_item_id"], "PVTI_1");
    server.join();
}

#[test]
fn github_workflow_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/actions/workflows HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_workflow_list")],
            r#"{"total_count":1,"workflows":[{"id":11,"name":"CI","path":".github/workflows/ci.yml"}]}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "list",
            "Aureuma/si",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_workflow_list");
    assert_eq!(parsed["list"][0]["name"], "CI");
    server.join();
}

#[test]
fn github_workflow_runs_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/actions/runs?branch=main HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_workflow_runs")],
            r#"{"total_count":1,"workflow_runs":[{"id":21,"name":"CI","status":"completed"}]}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "runs",
            "Aureuma/si",
            "--param",
            "branch=main",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_workflow_runs");
    assert_eq!(parsed["list"][0]["id"], 21);
    server.join();
}

#[test]
fn github_workflow_run_get_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/actions/runs/21 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_workflow_run")],
            r#"{"id":21,"name":"CI","status":"completed"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "run",
            "get",
            "Aureuma/si",
            "21",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_workflow_run");
    assert_eq!(parsed["data"]["id"], 21);
    server.join();
}

#[test]
fn github_workflow_dispatch_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "POST /repos/Aureuma/si/actions/workflows/ci.yml/dispatches HTTP/1.1\r\n"
        ));
        assert!(request.contains("\"ref\":\"main\""));
        assert!(request.contains("\"env\":\"prod\""));
        http_json_response(
            "204 No Content",
            &[("x-github-request-id", "req_gh_workflow_dispatch")],
            "",
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "dispatch",
            "Aureuma/si",
            "ci.yml",
            "--ref",
            "main",
            "--input",
            "env=prod",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 204);
    assert_eq!(parsed["request_id"], "req_gh_workflow_dispatch");
    server.join();
}

#[test]
fn github_workflow_run_cancel_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /repos/Aureuma/si/actions/runs/21/cancel HTTP/1.1\r\n"));
        http_json_response("202 Accepted", &[("x-github-request-id", "req_gh_workflow_cancel")], "")
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "run",
            "cancel",
            "Aureuma/si",
            "21",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 202);
    assert_eq!(parsed["request_id"], "req_gh_workflow_cancel");
    server.join();
}

#[test]
fn github_workflow_run_rerun_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /repos/Aureuma/si/actions/runs/21/rerun HTTP/1.1\r\n"));
        http_json_response("201 Created", &[("x-github-request-id", "req_gh_workflow_rerun")], "")
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "run",
            "rerun",
            "Aureuma/si",
            "21",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_workflow_rerun");
    server.join();
}

#[test]
fn github_workflow_watch_json_waits_until_completed_with_oauth() {
    let calls = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let watch_calls = calls.clone();
    let server = start_http_server(2, move |request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/actions/runs/21 HTTP/1.1\r\n"));
        let idx = watch_calls.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        if idx == 0 {
            return http_json_response(
                "200 OK",
                &[("x-github-request-id", "req_gh_workflow_watch_1")],
                r#"{"id":21,"name":"CI","status":"in_progress","conclusion":null,"head_branch":"main","event":"push"}"#,
            );
        }
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_workflow_watch_2")],
            r#"{"id":21,"name":"CI","status":"completed","conclusion":"success","head_branch":"main","event":"push"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "watch",
            "Aureuma/si",
            "21",
            "--interval-seconds",
            "1",
            "--timeout-seconds",
            "5",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_workflow_watch_2");
    assert_eq!(parsed["data"]["status"], "completed");
    server.join();
}

#[test]
fn github_issue_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /repos/Aureuma/si/issues?page=1&per_page=100&state=open HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_issue_list")],
            r#"[{"number":12,"title":"Investigate"}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "issue",
            "list",
            "Aureuma/si",
            "--param",
            "state=open",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_issue_list");
    assert_eq!(parsed["list"][0]["number"], 12);
    server.join();
}

#[test]
fn github_issue_get_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/issues/12 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_issue_get")],
            r#"{"number":12,"title":"Investigate"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "issue",
            "get",
            "Aureuma/si",
            "12",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_issue_get");
    assert_eq!(parsed["data"]["title"], "Investigate");
    server.join();
}

#[test]
fn github_issue_create_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /repos/Aureuma/si/issues HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        assert!(request.contains("\"title\":\"Rust issue\""));
        http_json_response(
            "201 Created",
            &[("x-github-request-id", "req_gh_issue_create")],
            r#"{"id":1,"number":77,"title":"Rust issue","state":"open"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "issue",
            "create",
            "Aureuma/si",
            "--title",
            "Rust issue",
            "--body",
            "created from rust",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_issue_create");
    assert_eq!(parsed["data"]["number"], 77);
    server.join();
}

#[test]
fn github_issue_comment_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /repos/Aureuma/si/issues/77/comments HTTP/1.1\r\n"));
        assert!(request.contains("\"body\":\"looks good\""));
        http_json_response(
            "201 Created",
            &[("x-github-request-id", "req_gh_issue_comment")],
            r#"{"id":11,"body":"looks good"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "issue",
            "comment",
            "Aureuma/si",
            "77",
            "--body",
            "looks good",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_issue_comment");
    assert_eq!(parsed["data"]["body"], "looks good");
    server.join();
}

#[test]
fn github_issue_close_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PATCH /repos/Aureuma/si/issues/77 HTTP/1.1\r\n"));
        assert!(request.contains("\"state\":\"closed\""));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_issue_close")],
            r#"{"id":1,"number":77,"state":"closed"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "issue",
            "close",
            "Aureuma/si",
            "77",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gh_issue_close");
    assert_eq!(parsed["data"]["state"], "closed");
    server.join();
}

#[test]
fn github_issue_reopen_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PATCH /repos/Aureuma/si/issues/77 HTTP/1.1\r\n"));
        assert!(request.contains("\"state\":\"open\""));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_issue_reopen")],
            r#"{"id":1,"number":77,"state":"open"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "issue",
            "reopen",
            "Aureuma/si",
            "77",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gh_issue_reopen");
    assert_eq!(parsed["data"]["state"], "open");
    server.join();
}

#[test]
fn github_pr_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /repos/Aureuma/si/pulls?page=1&per_page=100&state=open HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_pr_list")],
            r#"[{"number":34,"title":"Refactor Rust bridge"}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "pr",
            "list",
            "Aureuma/si",
            "--param",
            "state=open",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_pr_list");
    assert_eq!(parsed["list"][0]["number"], 34);
    server.join();
}

#[test]
fn github_pr_get_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/pulls/34 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_pr_get")],
            r#"{"number":34,"title":"Refactor Rust bridge"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "pr",
            "get",
            "Aureuma/si",
            "34",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_pr_get");
    assert_eq!(parsed["data"]["title"], "Refactor Rust bridge");
    server.join();
}

#[test]
fn github_pr_create_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /repos/Aureuma/si/pulls HTTP/1.1\r\n"));
        assert!(request.contains("\"head\":\"feature/rust\""));
        assert!(request.contains("\"base\":\"main\""));
        http_json_response(
            "201 Created",
            &[("x-github-request-id", "req_gh_pr_create")],
            r#"{"id":1,"number":35,"title":"Rust PR","state":"open"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "pr",
            "create",
            "Aureuma/si",
            "--head",
            "feature/rust",
            "--base",
            "main",
            "--title",
            "Rust PR",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_pr_create");
    assert_eq!(parsed["data"]["number"], 35);
    server.join();
}

#[test]
fn github_pr_comment_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /repos/Aureuma/si/issues/35/comments HTTP/1.1\r\n"));
        assert!(request.contains("\"body\":\"ship it\""));
        http_json_response(
            "201 Created",
            &[("x-github-request-id", "req_gh_pr_comment")],
            r#"{"id":9,"body":"ship it"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "pr",
            "comment",
            "Aureuma/si",
            "35",
            "--body",
            "ship it",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_pr_comment");
    assert_eq!(parsed["data"]["body"], "ship it");
    server.join();
}

#[test]
fn github_pr_merge_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PUT /repos/Aureuma/si/pulls/35/merge HTTP/1.1\r\n"));
        assert!(request.contains("\"merge_method\":\"squash\""));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_pr_merge")],
            r#"{"sha":"abc123","merged":true,"message":"Pull Request successfully merged"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "pr",
            "merge",
            "Aureuma/si",
            "35",
            "--method",
            "squash",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gh_pr_merge");
    assert_eq!(parsed["data"]["merged"], true);
    server.join();
}

#[test]
fn github_branch_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /repos/Aureuma/si/branches?page=1&per_page=100&protected=true HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_branch_list")],
            r#"[{"name":"main","protected":true}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "branch",
            "list",
            "Aureuma/si",
            "--protected",
            "true",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["repo"], "Aureuma/si");
    assert_eq!(parsed["count"], 1);
    assert_eq!(parsed["data"][0]["name"], "main");
    server.join();
}

#[test]
fn github_branch_get_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/branches/feature%2Frust HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_branch_get")],
            r#"{"name":"feature/rust","protected":false}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "branch",
            "get",
            "Aureuma/si",
            "feature/rust",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_branch_get");
    assert_eq!(parsed["data"]["name"], "feature/rust");
    server.join();
}

#[test]
fn github_branch_create_json_mutates_via_api_with_oauth() {
    let server = start_http_server(3, |request| {
        if request.starts_with("GET /repos/Aureuma/si HTTP/1.1\r\n") {
            return http_json_response(
                "200 OK",
                &[("x-github-request-id", "req_gh_repo_get")],
                r#"{"default_branch":"main"}"#,
            );
        }
        if request.starts_with("GET /repos/Aureuma/si/branches/main HTTP/1.1\r\n") {
            return http_json_response(
                "200 OK",
                &[("x-github-request-id", "req_gh_branch_base")],
                r#"{"name":"main","commit":{"sha":"abc123def456"}}"#,
            );
        }
        assert!(request.starts_with("POST /repos/Aureuma/si/git/refs HTTP/1.1\r\n"));
        assert!(request.contains("\"ref\":\"refs/heads/feature/rust\""));
        assert!(request.contains("\"sha\":\"abc123def456\""));
        http_json_response(
            "201 Created",
            &[("x-github-request-id", "req_gh_branch_create")],
            r#"{"ref":"refs/heads/feature/rust","object":{"sha":"abc123def456"}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "branch",
            "create",
            "Aureuma/si",
            "feature/rust",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_gh_branch_create");
    assert_eq!(parsed["data"]["base_sha_source"], "branch:main");
    server.join();
}

#[test]
fn github_branch_delete_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request
                .starts_with("DELETE /repos/Aureuma/si/git/refs/heads/feature%2Frust HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response("204 No Content", &[("x-github-request-id", "req_gh_branch_delete")], "")
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "branch",
            "delete",
            "Aureuma/si",
            "feature/rust",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 204);
    assert_eq!(parsed["request_id"], "req_gh_branch_delete");
    server.join();
}

#[test]
fn github_branch_protect_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PUT /repos/Aureuma/si/branches/main/protection HTTP/1.1\r\n"));
        assert!(request.contains("\"strict\":true"));
        assert!(request.contains("\"checks\":[\"ci\",\"lint\"]"));
        assert!(request.contains("\"required_approving_review_count\":2"));
        assert!(request.contains("\"users\":[\"alice\"]"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_branch_protect")],
            r#"{"url":"https://api.github.com/repos/Aureuma/si/branches/main/protection","required_linear_history":{"enabled":true}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "branch",
            "protect",
            "Aureuma/si",
            "main",
            "--required-check",
            "ci",
            "--required-check",
            "lint",
            "--required-approvals",
            "2",
            "--restrict-user",
            "alice",
            "--require-linear-history",
            "true",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_branch_protect");
    assert_eq!(parsed["data"]["required_linear_history"]["enabled"], true);
    server.join();
}

#[test]
fn github_branch_unprotect_json_mutates_via_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("DELETE /repos/Aureuma/si/branches/main/protection HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "204 No Content",
            &[("x-github-request-id", "req_gh_branch_unprotect")],
            "",
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "branch",
            "unprotect",
            "Aureuma/si",
            "main",
            "--force",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 204);
    assert_eq!(parsed["request_id"], "req_gh_branch_unprotect");
    server.join();
}

#[test]
fn github_git_credential_get_reads_stdin_and_prints_token() {
    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args(["orbit", "github", "git", "credential", "get", "--auth-mode", "oauth"])
        .write_stdin("protocol=https\nhost=github.com\npath=Aureuma/si.git\n\n")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert_eq!(text, "username=x-access-token\npassword=gho_example_token\n\n");
}

#[test]
fn github_raw_get_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /rate_limit?scope=core HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_raw")],
            r#"{"rate":{"limit":5000,"remaining":4999}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "raw",
            "--method",
            "GET",
            "--path",
            "/rate_limit",
            "--param",
            "scope=core",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_raw");
    assert_eq!(parsed["data"]["rate"]["limit"], 5000);
    server.join();
}

#[test]
fn github_graphql_query_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /graphql HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        assert!(request.contains("\"query\":\"query { viewer { login } }\""));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_graphql")],
            r#"{"data":{"viewer":{"login":"example-user"}}}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "graphql",
            "--query",
            "query { viewer { login } }",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_graphql");
    assert_eq!(parsed["data"]["viewer"]["login"], "example-user");
    server.join();
}

#[test]
fn github_workflow_logs_raw_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/actions/runs/21/logs HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        let body = "step 1\nstep 2\n";
        format!(
            "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nx-github-request-id: req_gh_workflow_logs\r\nContent-Length: {}\r\n\r\n{}",
            body.len(),
            body
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "workflow",
            "logs",
            "Aureuma/si",
            "21",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--raw",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("Status: 200 200 OK"));
    assert!(text.contains("Request ID: req_gh_workflow_logs"));
    assert!(text.contains("step 1\nstep 2\n"));
    server.join();
}

#[test]
fn stripe_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[stripe]
default_account = "core"

[stripe.accounts.core]
id = "acct_core"
name = "Core"
live_key_env = "CORE_LIVE"

[stripe.accounts.ops]
id = "acct_ops"
sandbox_key = "sk_test_ops"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "stripe", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts.len(), 2);
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "yes");
    assert_eq!(contexts[0]["live_key_config"], "yes");
    assert_eq!(contexts[1]["alias"], "ops");
    assert_eq!(contexts[1]["sandbox_key_config"], "yes");
}

#[test]
fn stripe_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[stripe]
default_account = "core"
default_env = "sandbox"

[stripe.accounts.core]
id = "acct_core"
sandbox_key = "sk_test_core"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "stripe", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["account_id"], "acct_core");
    assert_eq!(parsed["environment"], "sandbox");
    assert_eq!(parsed["key_source"], "settings.sandbox_key");
}

#[test]
fn stripe_auth_status_json_resolves_env_key() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[stripe]
default_env = "sandbox"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("SI_STRIPE_API_KEY", "sk_test_shared")
        .args(["orbit", "stripe", "auth", "status", "--account", "acct_123", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "");
    assert_eq!(parsed["account_id"], "acct_123");
    assert_eq!(parsed["environment"], "sandbox");
    assert_eq!(parsed["key_source"], "env:SI_STRIPE_API_KEY");
    assert_eq!(parsed["key_preview"], "sk_test_...");
}

#[test]
fn stripe_raw_json_fetches_from_api() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/products?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        assert!(request.contains("stripe-account: acct_123\r\n"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_raw")],
            r#"{"object":"list","data":[{"id":"prod_123","name":"Core"}],"has_more":false}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "raw"])
        .args([
            "--account",
            "acct_123",
            "--api-key",
            "sk_test_core",
            "--base-url",
            &server.base_url,
            "--path",
            "/v1/products",
            "--param",
            "limit=1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_stripe_raw");
    assert_eq!(parsed["data"]["data"][0]["id"], "prod_123");
    server.join();
}

#[test]
fn stripe_report_revenue_summary_json_aggregates_transactions() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/balance_transactions?"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        assert!(request.contains("type=charge"));
        assert!(request.contains("created%5Bgte%5D=1704067200"));
        assert!(request.contains("created%5Blte%5D=1704153600"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_report")],
            r#"{"object":"list","data":[{"id":"txn_1","amount":1000,"fee":100,"currency":"usd"},{"id":"txn_2","amount":500,"fee":50,"currency":"usd"}],"has_more":false}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "report", "revenue-summary"])
        .args([
            "--api-key",
            "sk_test_core",
            "--base-url",
            &server.base_url,
            "--from",
            "2024-01-01T00:00:00Z",
            "--to",
            "2024-01-02T00:00:00Z",
            "--limit",
            "10",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["preset"], "revenue-summary");
    assert_eq!(parsed["report"]["transactions"], 2);
    assert_eq!(parsed["report"]["gross_amount"], 1500);
    assert_eq!(parsed["report"]["fees"], 150);
    assert_eq!(parsed["report"]["net_amount"], 1350);
    assert_eq!(parsed["report"]["currency"], "USD");
    server.join();
}

#[test]
fn stripe_object_list_json_fetches_list() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/products?limit=2 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_object_list")],
            r#"{"object":"list","data":[{"id":"prod_123","name":"Core"},{"id":"prod_456","name":"Ops"}],"has_more":false}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "object", "list", "product"])
        .args([
            "--api-key",
            "sk_test_core",
            "--base-url",
            &server.base_url,
            "--limit",
            "2",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["object"], "product");
    assert_eq!(parsed["count"], 2);
    assert_eq!(parsed["data"][0]["id"], "prod_123");
    server.join();
}

#[test]
fn stripe_object_get_json_fetches_single_object() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/products/prod_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_object_get")],
            r#"{"id":"prod_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "object", "get", "product", "prod_123"])
        .args(["--api-key", "sk_test_core", "--base-url", &server.base_url, "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_stripe_object_get");
    assert_eq!(parsed["data"]["id"], "prod_123");
    server.join();
}

#[test]
fn stripe_object_create_json_posts_object() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/products HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        assert!(request.contains("idempotency-key: idem_123\r\n"));
        assert!(request.contains("\r\n\r\nname=Core"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_object_create")],
            r#"{"id":"prod_789","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "object", "create", "product"])
        .args([
            "--api-key",
            "sk_test_core",
            "--base-url",
            &server.base_url,
            "--param",
            "name=Core",
            "--idempotency-key",
            "idem_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_stripe_object_create");
    assert_eq!(parsed["data"]["id"], "prod_789");
    server.join();
}

#[test]
fn stripe_object_update_json_posts_object() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/products/prod_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        assert!(request.contains("idempotency-key: idem_456\r\n"));
        assert!(request.contains("\r\n\r\nname=Core+2"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_object_update")],
            r#"{"id":"prod_123","name":"Core 2"}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "object", "update", "product", "prod_123"])
        .args([
            "--api-key",
            "sk_test_core",
            "--base-url",
            &server.base_url,
            "--param",
            "name=Core 2",
            "--idempotency-key",
            "idem_456",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_stripe_object_update");
    assert_eq!(parsed["data"]["name"], "Core 2");
    server.join();
}

#[test]
fn stripe_object_delete_json_deletes_object() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("DELETE /v1/products/prod_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_test_core\r\n"));
        assert!(request.contains("idempotency-key: idem_789\r\n"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_object_delete")],
            r#"{"id":"prod_123","deleted":true}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "object", "delete", "product", "prod_123"])
        .args([
            "--api-key",
            "sk_test_core",
            "--base-url",
            &server.base_url,
            "--force",
            "--idempotency-key",
            "idem_789",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_stripe_object_delete");
    assert_eq!(parsed["data"]["deleted"], true);
    server.join();
}

#[test]
fn stripe_sync_plan_json_detects_missing_sandbox_product() {
    let server = start_http_server(2, move |request| {
        if request.contains("authorization: Bearer sk_live\r\n") {
            return http_json_response(
                "200 OK",
                &[("Request-Id", "req_stripe_sync_live")],
                r#"{"object":"list","data":[{"id":"prod_live","name":"Core","active":true}],"has_more":false}"#,
            );
        }
        assert!(request.contains("authorization: Bearer sk_sandbox\r\n"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_sync_sandbox")],
            r#"{"object":"list","data":[],"has_more":false}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "sync", "live-to-sandbox", "plan"])
        .args([
            "--live-api-key",
            "sk_live",
            "--sandbox-api-key",
            "sk_sandbox",
            "--base-url",
            &server.base_url,
            "--only",
            "products",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["families"][0], "products");
    assert_eq!(parsed["summary"]["create"], 1);
    assert_eq!(parsed["actions"][0]["action"], "create");
    assert_eq!(parsed["actions"][0]["live_id"], "prod_live");
    server.join();
}

#[test]
fn stripe_sync_apply_json_creates_missing_sandbox_product() {
    let server = start_http_server(3, move |request| {
        if request.starts_with("POST /v1/products HTTP/1.1\r\n") {
            assert!(request.contains("authorization: Bearer sk_sandbox\r\n"));
            assert!(request.contains("idempotency-key: idem_sync\r\n"));
            assert!(request.contains("metadata%5Bsi_live_id%5D=prod_live"));
            return http_json_response(
                "200 OK",
                &[("Request-Id", "req_stripe_sync_apply")],
                r#"{"id":"prod_sandbox","name":"Core"}"#,
            );
        }
        if request.contains("authorization: Bearer sk_live\r\n") {
            return http_json_response(
                "200 OK",
                &[("Request-Id", "req_stripe_sync_apply_live")],
                r#"{"object":"list","data":[{"id":"prod_live","name":"Core","active":true}],"has_more":false}"#,
            );
        }
        assert!(request.contains("authorization: Bearer sk_sandbox\r\n"));
        http_json_response(
            "200 OK",
            &[("Request-Id", "req_stripe_sync_apply_sandbox")],
            r#"{"object":"list","data":[],"has_more":false}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "stripe", "sync", "live-to-sandbox", "apply"])
        .args([
            "--live-api-key",
            "sk_live",
            "--sandbox-api-key",
            "sk_sandbox",
            "--base-url",
            &server.base_url,
            "--only",
            "products",
            "--force",
            "--idempotency-key",
            "idem_sync",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["applied"], 1);
    assert_eq!(parsed["failures"], 0);
    assert_eq!(parsed["mappings"]["prod_live"], "prod_sandbox");
    server.join();
}

#[test]
fn workos_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[workos]
default_account = "core"
default_env = "prod"

[workos.accounts.core]
name = "Core"
prod_api_key_env = "CORE_PROD"
client_id_env = "CORE_CLIENT"
organization_id = "org_123"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "workos", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["api_key_env"], "CORE_PROD");
}

#[test]
fn workos_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[workos]
default_account = "core"
default_env = "prod"

[workos.accounts.core]
prod_api_key_env = "CORE_PROD"
organization_id = "org_123"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROD", "sk_workos_prod")
        .args(["orbit", "workos", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["organization_id"], "org_123");
    assert_eq!(parsed["source"], "env:CORE_PROD,settings.organization_id");
}

#[test]
fn workos_auth_status_json_resolves_env_sources() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[workos]
default_account = "core"

[workos.accounts.core]
prod_api_key_env = "CORE_PROD"
client_id_env = "CORE_CLIENT"
organization_id = "org_123"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROD", "sk_workos_prod")
        .env("CORE_CLIENT", "client_123")
        .args(["orbit", "workos", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["source"], "env:CORE_PROD,env:CORE_CLIENT,settings.organization_id");
    assert_eq!(parsed["key_preview"], "sk_worko...");
}

#[test]
fn workos_doctor_json_verifies_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /organizations?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_workos_prod\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_workos_doctor")],
            r#"{"data":[{"id":"org_123"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "workos",
            "doctor",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk_workos_prod",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["provider"], "workos");
    assert_eq!(parsed["checks"][2]["detail"], "200 200 OK");
    server.join();
}

#[test]
fn workos_raw_json_fetches_with_headers_and_query_params() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /organizations?limit=2 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_workos_prod\r\n"));
        assert!(request.contains("x-test: alpha\r\n") || request.contains("X-Test: alpha\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_workos_raw")],
            r#"{"data":[{"id":"org_123"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "workos",
            "raw",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk_workos_prod",
            "--method",
            "GET",
            "--path",
            "/organizations",
            "--param",
            "limit=2",
            "--header",
            "x-test=alpha",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_workos_raw");
    assert_eq!(parsed["data"]["data"][0]["id"], "org_123");
    server.join();
}

#[test]
fn workos_organization_list_json_fetches_from_api() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /organizations?limit=2 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk_workos_prod\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_workos_org_list")],
            r#"{"data":[{"id":"org_123"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "workos",
            "organization",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk_workos_prod",
            "--limit",
            "2",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_workos_org_list");
    assert_eq!(parsed["data"]["data"][0]["id"], "org_123");
    server.join();
}

#[test]
fn workos_invitation_revoke_json_posts_empty_body() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("POST /user_management/invitations/inv_123/revoke HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer sk_workos_prod\r\n"));
        assert!(request.contains("content-type: application/json\r\n"));
        assert!(request.contains("\r\n\r\n{}"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_workos_revoke")],
            r#"{"id":"inv_123","status":"revoked"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "workos",
            "invitation",
            "revoke",
            "inv_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk_workos_prod",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["data"]["status"], "revoked");
    server.join();
}

#[test]
fn cloudflare_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[cloudflare]
default_account = "core"

[cloudflare.accounts.core]
name = "Core"
account_id = "acc_core"
prod_zone_id = "zone_prod"

[cloudflare.accounts.ops]
account_id = "acc_ops"
staging_zone_id = "zone_staging"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[1]["staging_zone"], "zone_staging");
}

#[test]
fn cloudflare_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[cloudflare]
default_account = "core"
default_env = "prod"

[cloudflare.accounts.core]
account_id = "acc_core"
default_zone_name = "example.com"
prod_zone_id = "zone_prod"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["account_id"], "acc_core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["zone_id"], "zone_prod");
    assert_eq!(parsed["zone_name"], "example.com");
    assert_eq!(parsed["source"], "settings.account_id,settings.prod_zone_id");
}

#[test]
fn cloudflare_auth_status_json_verifies_token() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /user/tokens/verify HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[],
            r#"{"success":true,"result":{"id":"verify_123","status":"active"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "auth", "status"])
        .args(["--account", "core"])
        .args(["--env", "prod"])
        .args(["--zone-id", "zone_prod"])
        .args(["--account-id", "acc_core"])
        .args(["--api-token", "cf-test-token"])
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["account_id"], "acc_core");
    assert_eq!(parsed["zone_id"], "zone_prod");
    assert_eq!(parsed["token_preview"], "cf-tes...");
    assert_eq!(parsed["verify"]["result"]["id"], "verify_123");
    server.join();
}

#[test]
fn cloudflare_raw_json_fetches_with_query_params() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /zones?per_page=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_raw")],
            r#"{"success":true,"result":[{"id":"zone_123","name":"example.com"}]}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "raw"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--path",
            "/zones",
            "--param",
            "per_page=1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_cloudflare_raw");
    assert_eq!(parsed["list"][0]["id"], "zone_123");
    server.join();
}

#[test]
fn cloudflare_raw_text_prints_body_for_raw_mode() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /zones HTTP/1.1\r\n"));
        assert!(request.contains("content-type: application/json\r\n"));
        assert!(request.contains("\r\n\r\n{\"name\":\"example.com\"}"));
        http_json_response(
            "200 OK",
            &[],
            r#"{"success":true,"result":{"id":"zone_123","name":"example.com"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "raw"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--method",
            "POST",
            "--path",
            "/zones",
            "--body",
            "{\"name\":\"example.com\"}",
            "--raw",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("\"id\":\"zone_123\""));
    server.join();
}

#[test]
fn cloudflare_analytics_http_json_fetches_zone_dashboard() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /zones/zone_123/analytics/dashboard?since=2026-03-01 HTTP/1.1\r\n"
            ));
            assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
            http_json_response(
                "200 OK",
                &[("cf-ray", "req_cloudflare_analytics")],
                r#"{"success":true,"result":{"totals":{"requests":{"all":123}}}}"#,
            )
        });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "analytics", "http"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "since=2026-03-01",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_cloudflare_analytics");
    assert_eq!(parsed["data"]["totals"]["requests"]["all"], 123);
    server.join();
}

#[test]
fn cloudflare_report_billing_summary_json_fetches_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /accounts/acc_core/billing/subscriptions?since=2026-03-01&until=2026-03-15 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_report")],
            r#"{"success":true,"result":{"subscriptions":[{"id":"sub_123"}]}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "report", "billing-summary"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acc_core",
            "--from",
            "2026-03-01",
            "--to",
            "2026-03-15",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_cloudflare_report");
    assert_eq!(parsed["data"]["subscriptions"][0]["id"], "sub_123");
    server.join();
}

#[test]
fn cloudflare_smoke_json_runs_public_checks_and_skips_account_scoped_ones() {
    let call_count = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let calls = std::sync::Arc::clone(&call_count);
    let server = start_http_server(3, move |request| {
        let call = calls.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(request.starts_with("GET /user/tokens/verify HTTP/1.1\r\n"));
                assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
                http_json_response(
                    "200 OK",
                    &[("cf-ray", "req_cf_smoke_verify")],
                    r#"{"success":true,"result":{"id":"verify_123"}}"#,
                )
            }
            1 => {
                assert!(request.starts_with("GET /accounts HTTP/1.1\r\n"));
                assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
                http_json_response(
                    "200 OK",
                    &[("cf-ray", "req_cf_smoke_accounts")],
                    r#"{"success":true,"result":[{"id":"acc_1"}]}"#,
                )
            }
            2 => {
                assert!(request.starts_with("GET /zones?per_page=1 HTTP/1.1\r\n"));
                assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
                http_json_response(
                    "200 OK",
                    &[("cf-ray", "req_cf_smoke_zones")],
                    r#"{"success":true,"result":[{"id":"zone_123"}]}"#,
                )
            }
            _ => panic!("unexpected request"),
        }
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "smoke"])
        .args(["--api-token", "cf-test-token", "--base-url", &server.base_url, "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["summary"]["pass"], 3);
    assert_eq!(parsed["summary"]["fail"], 0);
    assert_eq!(parsed["summary"]["skip"], 11);
    let checks = parsed["checks"].as_array().expect("checks array");
    assert_eq!(checks.len(), 14);
    assert_eq!(checks[0]["name"], "token_verify");
    assert_eq!(checks[0]["ok"], true);
    server.join();
}

#[test]
fn cloudflare_logs_received_json_fetches_zone_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /zones/zone_123/logs/received?count=10 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_logs")],
            r#"{"success":true,"result":{"url":"https://example.com/logs.gz"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "logs", "received"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "count=10",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_cloudflare_logs");
    assert_eq!(parsed["data"]["url"], "https://example.com/logs.gz");
    server.join();
}

#[test]
fn cloudflare_logs_job_list_json_fetches_zone_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /zones/zone_123/logpush/jobs?page=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_logs_job_list")],
            r#"{"success":true,"result":[{"id":"job_123","name":"core-job"}],"result_info":{"page":1,"total_pages":1}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "logs", "job", "list"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["object"], "log job");
    assert_eq!(parsed["count"], 1);
    assert_eq!(parsed["data"][0]["id"], "job_123");
    server.join();
}

#[test]
fn cloudflare_logs_job_get_json_fetches_zone_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /zones/zone_123/logpush/jobs/job_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_logs_job_get")],
            r#"{"success":true,"result":{"id":"job_123","name":"core-job"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "logs", "job", "get", "job_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_logs_job_get");
    assert_eq!(parsed["data"]["id"], "job_123");
    server.join();
}

#[test]
fn cloudflare_logs_job_create_json_posts_body() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /zones/zone_123/logpush/jobs HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        assert!(request.contains("content-type: application/json\r\n"));
        assert!(request.contains("\"name\":\"core-job\""));
        assert!(request.contains("\"enabled\":true"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_logs_job_create")],
            r#"{"success":true,"result":{"id":"job_123","name":"core-job"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "logs", "job", "create"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "name=core-job",
            "--param",
            "enabled=true",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_logs_job_create");
    assert_eq!(parsed["data"]["name"], "core-job");
    server.join();
}

#[test]
fn cloudflare_logs_job_update_json_patches_body() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PATCH /zones/zone_123/logpush/jobs/job_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        assert!(request.contains("content-type: application/json\r\n"));
        assert!(request.contains("\"enabled\":false"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_logs_job_update")],
            r#"{"success":true,"result":{"id":"job_123","enabled":false}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "logs", "job", "update", "job_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "enabled=false",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_logs_job_update");
    assert_eq!(parsed["data"]["enabled"], false);
    server.join();
}

#[test]
fn cloudflare_logs_job_delete_json_deletes_job() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("DELETE /zones/zone_123/logpush/jobs/job_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_logs_job_delete")],
            r#"{"success":true,"result":{"id":"job_123","deleted":true}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "logs", "job", "delete", "job_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--force",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_logs_job_delete");
    assert_eq!(parsed["data"]["deleted"], true);
    server.join();
}

#[test]
fn cloudflare_zone_list_json_fetches_global_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /zones?page=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_zone_list")],
            r#"{"success":true,"result":[{"id":"zone_123","name":"example.com"}],"result_info":{"page":1,"total_pages":1}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "zone", "list"])
        .args(["--api-token", "cf-test-token", "--base-url", &server.base_url, "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["object"], "zone");
    assert_eq!(parsed["data"][0]["id"], "zone_123");
    server.join();
}

#[test]
fn cloudflare_dns_create_json_posts_zone_body() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /zones/zone_123/dns_records HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        assert!(request.contains("\"type\":\"A\""));
        assert!(request.contains("\"name\":\"app.example.com\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_dns_create")],
            r#"{"success":true,"result":{"id":"dns_123","type":"A"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "dns", "create"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "type=A",
            "--param",
            "name=app.example.com",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_dns_create");
    assert_eq!(parsed["data"]["id"], "dns_123");
    server.join();
}

#[test]
fn cloudflare_email_address_get_json_fetches_account_endpoint() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /accounts/acct_123/email/routing/addresses/addr_123 HTTP/1.1\r\n"
            ));
            assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
            http_json_response(
                "200 OK",
                &[("cf-ray", "req_cloudflare_email_address_get")],
                r#"{"success":true,"result":{"id":"addr_123","email":"ops@example.com"}}"#,
            )
        });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "email", "address", "get", "addr_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_email_address_get");
    assert_eq!(parsed["data"]["email"], "ops@example.com");
    server.join();
}

#[test]
fn cloudflare_token_delete_json_deletes_global_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("DELETE /user/tokens/tok_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_token_delete")],
            r#"{"success":true,"result":{"id":"tok_123","deleted":true}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "token", "delete", "tok_123"])
        .args(["--api-token", "cf-test-token", "--base-url", &server.base_url, "--force", "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_token_delete");
    assert_eq!(parsed["data"]["deleted"], true);
    server.join();
}

#[test]
fn cloudflare_ruleset_update_json_patches_zone_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PATCH /zones/zone_123/rulesets/rule_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        assert!(request.contains("\"name\":\"core-rules\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_ruleset_update")],
            r#"{"success":true,"result":{"id":"rule_123","name":"core-rules"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "ruleset", "update", "rule_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "name=core-rules",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_ruleset_update");
    assert_eq!(parsed["data"]["name"], "core-rules");
    server.join();
}

#[test]
fn cloudflare_workers_script_update_json_puts_account_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("PUT /accounts/acct_123/workers/scripts/script_123 HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        assert!(request.contains("\"name\":\"core-worker\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_worker_script_update")],
            r#"{"success":true,"result":{"id":"script_123","name":"core-worker"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "workers", "script", "update", "script_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--param",
            "name=core-worker",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_worker_script_update");
    assert_eq!(parsed["data"]["id"], "script_123");
    server.join();
}

#[test]
fn cloudflare_pages_project_create_json_posts_account_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /accounts/acct_123/pages/projects HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        assert!(request.contains("\"name\":\"docs\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_pages_project_create")],
            r#"{"success":true,"result":{"id":"proj_123","name":"docs"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "pages", "project", "create"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--param",
            "name=docs",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_pages_project_create");
    assert_eq!(parsed["data"]["name"], "docs");
    server.join();
}

#[test]
fn cloudflare_queue_list_json_fetches_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /accounts/acct_123/queues?page=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_queue_list")],
            r#"{"success":true,"result":[{"id":"queue_123","name":"jobs"}],"result_info":{"page":1,"total_pages":1}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "queue", "list"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["object"], "queue");
    assert_eq!(parsed["data"][0]["name"], "jobs");
    server.join();
}

#[test]
fn cloudflare_waf_update_json_patches_zone_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("PATCH /zones/zone_123/firewall/waf/packages/waf_123 HTTP/1.1\r\n")
        );
        assert!(request.contains("\"mode\":\"on\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_waf_update")],
            r#"{"success":true,"result":{"id":"waf_123","mode":"on"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "waf", "update", "waf_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "mode=on",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_waf_update");
    assert_eq!(parsed["data"]["mode"], "on");
    server.join();
}

#[test]
fn cloudflare_r2_bucket_get_json_fetches_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /accounts/acct_123/r2/buckets/bucket_123 HTTP/1.1\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_r2_bucket_get")],
            r#"{"success":true,"result":{"id":"bucket_123","name":"assets"}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "r2", "bucket", "get", "bucket_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_r2_bucket_get");
    assert_eq!(parsed["data"]["name"], "assets");
    server.join();
}

#[test]
fn cloudflare_d1_db_create_json_posts_account_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /accounts/acct_123/d1/database HTTP/1.1\r\n"));
        assert!(request.contains("\"name\":\"core\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_d1_db_create")],
            r#"{"success":true,"result":{"id":"db_123","name":"core"}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "d1", "db", "create"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--param",
            "name=core",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_d1_db_create");
    assert_eq!(parsed["data"]["id"], "db_123");
    server.join();
}

#[test]
fn cloudflare_kv_namespace_delete_json_deletes_account_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request
                .starts_with("DELETE /accounts/acct_123/storage/kv/namespaces/ns_123 HTTP/1.1\r\n")
        );
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_kv_namespace_delete")],
            r#"{"success":true,"result":{"id":"ns_123","deleted":true}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "kv", "namespace", "delete", "ns_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--force",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_kv_namespace_delete");
    assert_eq!(parsed["data"]["deleted"], true);
    server.join();
}

#[test]
fn cloudflare_access_app_list_json_fetches_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /accounts/acct_123/access/apps?page=1 HTTP/1.1\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_access_app_list")],
            r#"{"success":true,"result":[{"id":"app_123","name":"core"}],"result_info":{"page":1,"total_pages":1}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "access", "app", "list"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["object"], "access app");
    assert_eq!(parsed["data"][0]["id"], "app_123");
    server.join();
}

#[test]
fn cloudflare_access_policy_update_json_patches_account_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("PATCH /accounts/acct_123/access/policies/pol_123 HTTP/1.1\r\n")
        );
        assert!(request.contains("\"name\":\"core-policy\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_access_policy_update")],
            r#"{"success":true,"result":{"id":"pol_123","name":"core-policy"}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "access", "policy", "update", "pol_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--param",
            "name=core-policy",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_access_policy_update");
    assert_eq!(parsed["data"]["name"], "core-policy");
    server.join();
}

#[test]
fn cloudflare_tunnel_get_json_fetches_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /accounts/acct_123/cfd_tunnel/tun_123 HTTP/1.1\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_tunnel_get")],
            r#"{"success":true,"result":{"id":"tun_123","name":"edge"}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "tunnel", "get", "tun_123"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_tunnel_get");
    assert_eq!(parsed["data"]["name"], "edge");
    server.join();
}

#[test]
fn cloudflare_tls_cert_create_json_posts_zone_resource() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /zones/zone_123/custom_certificates HTTP/1.1\r\n"));
        assert!(request.contains("\"hostname\":\"example.com\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_tls_cert_create")],
            r#"{"success":true,"result":{"id":"cert_123","hostname":"example.com"}}"#,
        )
    });
    let output = cargo_bin()
        .args(["orbit", "cloudflare", "tls", "cert", "create"])
        .args([
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--param",
            "hostname=example.com",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_tls_cert_create");
    assert_eq!(parsed["data"]["id"], "cert_123");
    server.join();
}

#[test]
fn cloudflare_token_verify_json_fetches_global_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /user/tokens/verify HTTP/1.1\r\n"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_token_verify")],
            r#"{"success":true,"result":{"status":"active"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "token", "verify"])
        .args(["--api-token", "cf-test-token", "--base-url", &server.base_url, "--json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_token_verify");
    assert_eq!(parsed["data"]["status"], "active");
    server.join();
}

#[test]
fn cloudflare_workers_secret_set_json_puts_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "PUT /accounts/acct_123/workers/scripts/core-worker/secrets HTTP/1.1\r\n"
        ));
        assert!(request.contains("\"name\":\"API_TOKEN\""));
        assert!(request.contains("\"text\":\"secret-value\""));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_worker_secret")],
            r#"{"success":true,"result":{"name":"API_TOKEN"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "workers", "secret", "set"])
        .args([
            "--script",
            "core-worker",
            "--name",
            "API_TOKEN",
            "--text",
            "secret-value",
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_worker_secret");
    assert_eq!(parsed["data"]["name"], "API_TOKEN");
    server.join();
}

#[test]
fn cloudflare_tunnel_token_json_fetches_account_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("GET /accounts/acct_123/cfd_tunnel/tun_123/token HTTP/1.1\r\n")
        );
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_tunnel_token")],
            r#"{"success":true,"result":{"token":"tok_123"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "tunnel", "token"])
        .args([
            "--tunnel",
            "tun_123",
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--account-id",
            "acct_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_tunnel_token");
    assert_eq!(parsed["data"]["token"], "tok_123");
    server.join();
}

#[test]
fn cloudflare_cache_purge_json_posts_zone_endpoint() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /zones/zone_123/purge_cache HTTP/1.1\r\n"));
        assert!(request.contains("\"purge_everything\":true"));
        http_json_response(
            "200 OK",
            &[("cf-ray", "req_cloudflare_cache_purge")],
            r#"{"success":true,"result":{"id":"purge_123"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "cloudflare", "cache", "purge"])
        .args([
            "--everything",
            "--force",
            "--api-token",
            "cf-test-token",
            "--base-url",
            &server.base_url,
            "--zone-id",
            "zone_123",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_cloudflare_cache_purge");
    assert_eq!(parsed["data"]["id"], "purge_123");
    server.join();
}

#[test]
fn apple_appstore_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[apple]
default_account = "core"

[apple.appstore.accounts.core]
name = "Core"
project_id = "proj_core"
default_bundle_id = "com.example.core"
default_platform = "IOS"
default_language = "en-US"

[apple.appstore.accounts.ops]
project_id = "proj_ops"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "apple", "store", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["bundle_id"], "com.example.core");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn apple_appstore_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[apple]
default_account = "core"
default_env = "prod"

[apple.appstore.accounts.core]
project_id = "proj_core"
issuer_id_env = "CORE_ISSUER"
key_id_env = "CORE_KEY"
private_key_env = "CORE_PRIVATE_KEY"
default_bundle_id = "com.example.core"
default_language = "en-US"
default_platform = "IOS"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_ISSUER", "issuer_123")
        .env("CORE_KEY", "key_123")
        .env("CORE_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----")
        .args(["orbit", "apple", "store", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["bundle_id"], "com.example.core");
    assert_eq!(parsed["platform"], "IOS");
    assert_eq!(parsed["token_source"], "env:CORE_PRIVATE_KEY");
    assert_eq!(
        parsed["source"],
        "settings.apple.project_id,settings.apple.default_bundle_id,settings.apple.default_language,settings.apple.default_platform,env:CORE_ISSUER,env:CORE_KEY"
    );
}

#[test]
fn aws_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[aws]
default_account = "core"
default_region = "us-east-1"

[aws.accounts.core]
name = "Core"
access_key_id_env = "CORE_AWS_ACCESS_KEY_ID"

[aws.accounts.ops]
region = "us-west-2"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "aws", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["region"], "us-east-1");
    assert_eq!(contexts[1]["region"], "us-west-2");
}

#[test]
fn aws_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[aws]
default_account = "core"
default_region = "us-east-1"

[aws.accounts.core]
access_key_id_env = "CORE_AWS_ACCESS_KEY_ID"
secret_access_key_env = "CORE_AWS_SECRET_ACCESS_KEY"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_AWS_ACCESS_KEY_ID", "AKIA1234567890ABCD")
        .env("CORE_AWS_SECRET_ACCESS_KEY", "secret")
        .args(["orbit", "aws", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["region"], "us-east-1");
    assert_eq!(parsed["base_url"], "https://iam.amazonaws.com");
    assert_eq!(parsed["source"], "env:CORE_AWS_ACCESS_KEY_ID,env:CORE_AWS_SECRET_ACCESS_KEY");
    assert_eq!(parsed["access_key"], "AKIA**********ABCD");
}

#[test]
fn aws_auth_status_json_verifies_signed_get_user_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(
            request.contains("content-type: application/x-www-form-urlencoded; charset=utf-8\r\n")
        );
        assert!(request.contains("x-amz-date: "));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(
            request.contains(
                "SignedHeaders=host;x-amz-content-sha256;x-amz-date;x-amz-security-token"
            )
        );
        assert!(request.contains("\r\n\r\nAction=GetUser&Version=2010-05-08"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_auth")],
            r#"<GetUserResponse><ResponseMetadata><RequestId>req_aws_auth</RequestId></ResponseMetadata></GetUserResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "auth",
            "status",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify_status"], 200);
    assert_eq!(parsed["verify"]["response"], "GetUserResponse");
    assert_eq!(parsed["verify"]["request_id"], "req_aws_auth");
    server.join();
}

#[test]
fn aws_doctor_json_verifies_signed_get_user_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("\r\n\r\nAction=GetUser&Version=2010-05-08"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_doctor")],
            r#"<GetUserResponse><ResponseMetadata><RequestId>req_aws_doctor</RequestId></ResponseMetadata></GetUserResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "doctor",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["provider"], "aws_iam");
    assert_eq!(parsed["checks"][2]["detail"], "200 200 OK");
    server.join();
}

#[test]
fn aws_sts_get_caller_identity_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("\r\n\r\nAction=GetCallerIdentity&Version=2011-06-15"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_sts_identity")],
            r#"<GetCallerIdentityResponse><ResponseMetadata><RequestId>req_aws_sts_identity</RequestId></ResponseMetadata></GetCallerIdentityResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "sts",
            "get-caller-identity",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_sts_identity");
    assert_eq!(parsed["data"]["response"], "GetCallerIdentityResponse");
    server.join();
}

#[test]
fn aws_sts_assume_role_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("Action=AssumeRole"));
        assert!(request.contains("Version=2011-06-15"));
        assert!(request.contains("RoleArn=arn%3Aaws%3Aiam%3A%3A123456789012%3Arole%2Fdemo"));
        assert!(request.contains("RoleSessionName=session-demo"));
        assert!(request.contains("DurationSeconds=900"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_sts_assume")],
            r#"<AssumeRoleResponse><ResponseMetadata><RequestId>req_aws_sts_assume</RequestId></ResponseMetadata></AssumeRoleResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "sts",
            "assume-role",
            "--role-arn",
            "arn:aws:iam::123456789012:role/demo",
            "--session-name",
            "session-demo",
            "--duration-seconds",
            "900",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_sts_assume");
    assert_eq!(parsed["data"]["response"], "AssumeRoleResponse");
    server.join();
}

#[test]
fn aws_iam_user_create_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("Action=CreateUser"));
        assert!(request.contains("Version=2010-05-08"));
        assert!(request.contains("UserName=deploy-bot"));
        assert!(request.contains("Path=%2Fsystem%2F"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_iam_create")],
            r#"<CreateUserResponse><ResponseMetadata><RequestId>req_aws_iam_create</RequestId></ResponseMetadata></CreateUserResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "iam",
            "user",
            "create",
            "--name",
            "deploy-bot",
            "--path",
            "/system/",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_iam_create");
    assert_eq!(parsed["data"]["response"], "CreateUserResponse");
    server.join();
}

#[test]
fn aws_iam_query_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("Action=ListUsers"));
        assert!(request.contains("Version=2010-05-08"));
        assert!(request.contains("MaxItems=25"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_iam_query")],
            r#"<ListUsersResponse><ResponseMetadata><RequestId>req_aws_iam_query</RequestId></ResponseMetadata></ListUsersResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "iam",
            "query",
            "--action",
            "ListUsers",
            "--param",
            "MaxItems=25",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_iam_query");
    assert_eq!(parsed["data"]["response"], "ListUsersResponse");
    server.join();
}

#[test]
fn aws_s3_bucket_list_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_xml_response(
            "200 OK",
            &[("x-amz-request-id", "req_aws_s3_list")],
            r#"<ListAllMyBucketsResult><Owner><ID>owner</ID></Owner></ListAllMyBucketsResult>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "s3",
            "bucket",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_s3_list");
    assert_eq!(parsed["data"]["response"], "ListAllMyBucketsResult");
    server.join();
}

#[test]
fn aws_s3_bucket_create_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("PUT /demo-bucket HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("<LocationConstraint>us-west-2</LocationConstraint>"));
        http_xml_response(
            "200 OK",
            &[("x-amz-request-id", "req_aws_s3_create")],
            r#"<CreateBucketResult/>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "s3",
            "bucket",
            "create",
            "demo-bucket",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_s3_create");
    assert_eq!(parsed["data"]["response"], "CreateBucketResult");
    server.join();
}

#[test]
fn aws_ec2_instance_list_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("Action=DescribeInstances"));
        assert!(request.contains("Version=2016-11-15"));
        assert!(request.contains("InstanceId.1=i-123"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_ec2_list")],
            r#"<DescribeInstancesResponse><requestId>req_aws_ec2_list</requestId></DescribeInstancesResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "ec2",
            "instance",
            "list",
            "--id",
            "i-123",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_ec2_list");
    assert_eq!(parsed["data"]["response"], "DescribeInstancesResponse");
    server.join();
}

#[test]
fn aws_ec2_instance_start_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("Action=StartInstances"));
        assert!(request.contains("Version=2016-11-15"));
        assert!(request.contains("InstanceId.1=i-123"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_ec2_start")],
            r#"<StartInstancesResponse><requestId>req_aws_ec2_start</requestId></StartInstancesResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "ec2",
            "instance",
            "start",
            "--id",
            "i-123",
            "--force",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_ec2_start");
    assert_eq!(parsed["data"]["response"], "StartInstancesResponse");
    server.join();
}

#[test]
fn aws_lambda_function_list_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /2015-03-31/functions/?MaxItems=50 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_lambda_list")],
            r#"{"Functions":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "lambda",
            "function",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_lambda_list");
    assert_eq!(parsed["data"]["Functions"][0], Value::Null);
    server.join();
}

#[test]
fn aws_lambda_function_get_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /2015-03-31/functions/demo-fn HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_lambda_get")],
            r#"{"FunctionName":"demo-fn"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "lambda",
            "function",
            "get",
            "demo-fn",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_lambda_get");
    assert_eq!(parsed["data"]["FunctionName"], "demo-fn");
    server.join();
}

#[test]
fn aws_ecr_repository_list_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains(
            "x-amz-target: AmazonEC2ContainerRegistry_V20150921.DescribeRepositories\r\n"
        ));
        assert!(request.contains("content-type: application/x-amz-json-1.1\r\n"));
        assert!(
            request.contains(r#"{\"maxResults\":50}"#) || request.contains(r#"{"maxResults":50}"#)
        );
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_ecr_repo_list")],
            r#"{"repositories":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "ecr",
            "repository",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_ecr_repo_list");
    assert_eq!(parsed["data"]["repositories"][0], Value::Null);
    server.join();
}

#[test]
fn aws_ecr_repository_create_json_executes_signed_json_target_request() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with("POST / HTTP/1.1\r\n"));
            assert!(request.contains(
                "x-amz-target: AmazonEC2ContainerRegistry_V20150921.CreateRepository\r\n"
            ));
            assert!(request.contains(r#""repositoryName":"demo-repo""#));
            http_json_response(
                "200 OK",
                &[("x-amzn-requestid", "req_aws_ecr_repo_create")],
                r#"{"repository":{"repositoryName":"demo-repo"}}"#,
            )
        });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "ecr",
            "repository",
            "create",
            "demo-repo",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_ecr_repo_create");
    assert_eq!(parsed["data"]["repository"]["repositoryName"], "demo-repo");
    server.join();
}

#[test]
fn aws_ecr_image_list_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(
            request.contains("x-amz-target: AmazonEC2ContainerRegistry_V20150921.ListImages\r\n")
        );
        assert!(request.contains(r#""repositoryName":"demo-repo""#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_ecr_image_list")],
            r#"{"imageIds":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "ecr",
            "image",
            "list",
            "--repository",
            "demo-repo",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_ecr_image_list");
    assert_eq!(parsed["data"]["imageIds"][0], Value::Null);
    server.join();
}

#[test]
fn aws_s3_object_list_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /demo-bucket?list-type=2&max-keys=100 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_xml_response(
            "200 OK",
            &[("x-amz-request-id", "req_aws_s3_object_list")],
            r#"<ListBucketResult/>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "s3",
            "object",
            "list",
            "--bucket",
            "demo-bucket",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_s3_object_list");
    assert_eq!(parsed["data"]["response"], "ListBucketResult");
    server.join();
}

#[test]
fn aws_s3_object_get_output_writes_file() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /demo-bucket/nested/key.txt HTTP/1.1\r\n"));
        http_xml_response(
            "200 OK",
            &[("x-amz-request-id", "req_aws_s3_object_get")],
            "hello object",
        )
    });
    let dir = tempdir().expect("tempdir");
    let output_path = dir.path().join("object.txt");

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "s3",
            "object",
            "get",
            "--bucket",
            "demo-bucket",
            "--key",
            "nested/key.txt",
            "--output",
        ])
        .arg(&output_path)
        .args([
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["output"], output_path.to_string_lossy().to_string());
    assert_eq!(fs::read_to_string(&output_path).expect("read output file"), "hello object");
    server.join();
}

#[test]
fn aws_secrets_list_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("x-amz-target: secretsmanager.ListSecrets\r\n"));
        assert!(request.contains(r#""MaxResults":100"#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_secrets_list")],
            r#"{"SecretList":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "secrets",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_secrets_list");
    assert_eq!(parsed["data"]["SecretList"][0], Value::Null);
    server.join();
}

#[test]
fn aws_secrets_create_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: secretsmanager.CreateSecret\r\n"));
        assert!(request.contains(r#""Name":"demo-secret""#));
        assert!(request.contains(r#""SecretString":"super-secret""#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_secrets_create")],
            r#"{"ARN":"arn:aws:secretsmanager:demo"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "secrets",
            "create",
            "--name",
            "demo-secret",
            "--value",
            "super-secret",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_secrets_create");
    assert_eq!(parsed["data"]["ARN"], "arn:aws:secretsmanager:demo");
    server.join();
}

#[test]
fn aws_kms_key_list_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: TrentService.ListKeys\r\n"));
        assert!(request.contains(r#""Limit":100"#));
        http_json_response("200 OK", &[("x-amzn-requestid", "req_aws_kms_list")], r#"{"Keys":[]}"#)
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "kms",
            "key",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_kms_list");
    assert_eq!(parsed["data"]["Keys"][0], Value::Null);
    server.join();
}

#[test]
fn aws_kms_encrypt_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: TrentService.Encrypt\r\n"));
        assert!(request.contains(r#""KeyId":"key-123""#));
        assert!(request.contains(r#""Plaintext":"aGVsbG8=""#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_kms_encrypt")],
            r#"{"CiphertextBlob":"cipher"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "kms",
            "encrypt",
            "--key-id",
            "key-123",
            "--plaintext",
            "hello",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_kms_encrypt");
    assert_eq!(parsed["data"]["CiphertextBlob"], "cipher");
    server.join();
}

#[test]
fn aws_dynamodb_table_list_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: DynamoDB_20120810.ListTables\r\n"));
        assert!(request.contains(r#""Limit":100"#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_dynamodb_list")],
            r#"{"TableNames":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "dynamodb",
            "table",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_dynamodb_list");
    assert_eq!(parsed["data"]["TableNames"][0], Value::Null);
    server.join();
}

#[test]
fn aws_dynamodb_item_get_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: DynamoDB_20120810.GetItem\r\n"));
        assert!(request.contains(r#""TableName":"demo-table""#));
        assert!(request.contains(r#""Key":{"id":{"S":"123"}}"#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_dynamodb_get")],
            r#"{"Item":{"id":{"S":"123"}}}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "dynamodb",
            "item",
            "get",
            "--table",
            "demo-table",
            "--key-json",
            r#"{"id":{"S":"123"}}"#,
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_dynamodb_get");
    assert_eq!(parsed["data"]["Item"]["id"]["S"], "123");
    server.join();
}

#[test]
fn aws_ssm_parameter_get_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: AmazonSSM.GetParameter\r\n"));
        assert!(request.contains(r#""Name":"demo-param""#));
        assert!(request.contains(r#""WithDecryption":true"#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_ssm_get")],
            r#"{"Parameter":{"Name":"demo-param"}}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "ssm",
            "parameter",
            "get",
            "demo-param",
            "--decrypt",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_ssm_get");
    assert_eq!(parsed["data"]["Parameter"]["Name"], "demo-param");
    server.join();
}

#[test]
fn aws_logs_group_list_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: Logs_20140328.DescribeLogGroups\r\n"));
        assert!(request.contains(r#""limit":50"#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_logs_group")],
            r#"{"logGroups":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "logs",
            "group",
            "list",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_logs_group");
    assert_eq!(parsed["data"]["logGroups"][0], Value::Null);
    server.join();
}

#[test]
fn aws_logs_events_json_executes_signed_json_target_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("x-amz-target: Logs_20140328.FilterLogEvents\r\n"));
        assert!(request.contains(r#""logGroupName":"demo-group""#));
        assert!(request.contains(r#""logStreamNames":["demo-stream"]"#));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_logs_events")],
            r#"{"events":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "logs",
            "events",
            "--group",
            "demo-group",
            "--stream",
            "demo-stream",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_logs_events");
    assert_eq!(parsed["data"]["events"][0], Value::Null);
    server.join();
}

#[test]
fn aws_cloudwatch_metric_list_json_executes_signed_query_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST / HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("Action=ListMetrics"));
        assert!(request.contains("Version=2010-08-01"));
        assert!(request.contains("Namespace=AWS%2FEC2"));
        assert!(request.contains("MetricName=CPUUtilization"));
        http_xml_response(
            "200 OK",
            &[("x-amzn-RequestId", "req_aws_cloudwatch_metrics")],
            r#"<ListMetricsResponse><ResponseMetadata><RequestId>req_aws_cloudwatch_metrics</RequestId></ResponseMetadata></ListMetricsResponse>"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "cloudwatch",
            "metric",
            "--namespace",
            "AWS/EC2",
            "--name",
            "CPUUtilization",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_cloudwatch_metrics");
    assert_eq!(parsed["data"]["response"], "ListMetricsResponse");
    server.join();
}

#[test]
fn aws_bedrock_foundation_model_list_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /foundation-models?byProvider=anthropic HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_models")],
            r#"{"modelSummaries":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "foundation-model",
            "list",
            "--provider",
            "anthropic",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_models");
    assert_eq!(parsed["data"]["modelSummaries"][0], Value::Null);
    server.join();
}

#[test]
fn aws_bedrock_guardrail_get_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /guardrails/gr-123?guardrailVersion=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_guardrail")],
            r#"{"guardrailId":"gr-123"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "guardrail",
            "get",
            "gr-123",
            "--version",
            "1",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_guardrail");
    assert_eq!(parsed["data"]["guardrailId"], "gr-123");
    server.join();
}

#[test]
fn aws_bedrock_runtime_invoke_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /model/m-1/invoke HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("x-amzn-bedrock-trace: ENABLED\r\n"));
        assert!(request.contains("\r\n\r\n{\"inputText\":\"hello\"}"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_invoke")],
            r#"{"outputText":"ok"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "runtime",
            "invoke",
            "--model-id",
            "m-1",
            "--prompt",
            "hello",
            "--trace",
            "ENABLED",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_invoke");
    assert_eq!(parsed["data"]["outputText"], "ok");
    server.join();
}

#[test]
fn aws_bedrock_runtime_count_tokens_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /model/m-1/count-tokens HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("\r\n\r\n{\"inputText\":\"hello\"}"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_count")],
            r#"{"inputTextTokenCount":5}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "runtime",
            "count-tokens",
            "--model-id",
            "m-1",
            "--prompt",
            "hello",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_count");
    assert_eq!(parsed["data"]["inputTextTokenCount"], 5);
    server.join();
}

#[test]
fn aws_bedrock_job_create_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /model-invocation-job HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("\"jobName\":\"nightly-batch\""));
        assert!(request.contains("\"timeoutDurationInHours\":2"));
        assert!(request.contains("\"tags\":[{\"key\":\"env\",\"value\":\"prod\"},{\"key\":\"team\",\"value\":\"platform\"}]"));
        http_json_response(
            "201 Created",
            &[("x-amzn-requestid", "req_aws_bedrock_job_create")],
            r#"{"jobArn":"arn:aws:bedrock:job/nightly-batch"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "job",
            "create",
            "--name",
            "nightly-batch",
            "--role-arn",
            "arn:aws:iam::123456789012:role/bedrock-batch",
            "--model-id",
            "anthropic.claude-v2",
            "--input-s3-uri",
            "s3://bucket/input.jsonl",
            "--output-s3-uri",
            "s3://bucket/output/",
            "--timeout-hours",
            "2",
            "--tag",
            "team=platform",
            "--tag",
            "env=prod",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 201);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_job_create");
    assert_eq!(parsed["data"]["jobArn"], "arn:aws:bedrock:job/nightly-batch");
    server.join();
}

#[test]
fn aws_bedrock_agent_alias_get_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /agents/agent-1/agentAliases/alias-1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_agent_alias")],
            r#"{"agentAlias":{"agentAliasId":"alias-1"}}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "agent",
            "alias",
            "get",
            "--agent-id",
            "agent-1",
            "--alias-id",
            "alias-1",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_agent_alias");
    assert_eq!(parsed["data"]["agentAlias"]["agentAliasId"], "alias-1");
    server.join();
}

#[test]
fn aws_bedrock_knowledge_base_data_source_list_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("GET /knowledgebases/kb-1/datasources?maxResults=5 HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_datasources")],
            r#"{"dataSourceSummaries":[]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "knowledge-base",
            "data-source",
            "list",
            "--knowledge-base-id",
            "kb-1",
            "--limit",
            "5",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_datasources");
    assert_eq!(parsed["data"]["dataSourceSummaries"][0], Value::Null);
    server.join();
}

#[test]
fn aws_bedrock_agent_runtime_invoke_agent_json_executes_signed_rest_request() {
    let home = tempdir().expect("tempdir");
    let session_state_path = home.path().join("session-state.json");
    fs::write(&session_state_path, r#"{"promptSessionAttributes":{"mode":"debug"}}"#)
        .expect("write session state");

    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "POST /agents/agent-1/agentAliases/alias-1/sessions/sess-1/text HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("\"inputText\":\"hello\""));
        assert!(request.contains("\"enableTrace\":true"));
        assert!(
            request.contains("\"sessionState\":{\"promptSessionAttributes\":{\"mode\":\"debug\"}}")
        );
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_agent_runtime")],
            r#"{"completion":"ok"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "agent-runtime",
            "invoke-agent",
            "--agent-id",
            "agent-1",
            "--agent-alias-id",
            "alias-1",
            "--session-id",
            "sess-1",
            "--input-text",
            "hello",
            "--enable-trace",
            "--session-state-file",
            session_state_path.to_str().expect("session state path"),
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_agent_runtime");
    assert_eq!(parsed["data"]["completion"], "ok");
    server.join();
}

#[test]
fn aws_bedrock_agent_runtime_retrieve_and_generate_json_executes_signed_rest_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /retrieveAndGenerate HTTP/1.1\r\n"));
        assert!(request.contains("authorization: AWS4-HMAC-SHA256 Credential=AKIA1234567890ABCD/"));
        assert!(request.contains("\"knowledgeBaseId\":\"kb-1\""));
        assert!(request.contains("\"text\":\"hello\""));
        assert!(request.contains("\"numberOfResults\":3"));
        http_json_response(
            "200 OK",
            &[("x-amzn-requestid", "req_aws_bedrock_rag")],
            r#"{"output":{"text":"answer"}}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "aws",
            "bedrock",
            "agent-runtime",
            "retrieve-and-generate",
            "--knowledge-base-id",
            "kb-1",
            "--query",
            "hello",
            "--results",
            "3",
            "--base-url",
            &server.base_url,
            "--access-key",
            "AKIA1234567890ABCD",
            "--secret-key",
            "secret",
            "--session-token",
            "session",
            "--region",
            "us-west-2",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_aws_bedrock_rag");
    assert_eq!(parsed["data"]["output"]["text"], "answer");
    server.join();
}

#[test]
fn gcp_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[gcp]
default_account = "core"

[gcp.accounts.core]
project_id = "proj_core"
access_token_env = "CORE_GCP_ACCESS_TOKEN"

[gcp.accounts.ops]
project_id_env = "OPS_GCP_PROJECT"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "gcp", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["project_id"], "proj_core");
    assert_eq!(contexts[0]["token_env"], "CORE_GCP_ACCESS_TOKEN");
    assert_eq!(contexts[1]["alias"], "ops");
    assert_eq!(contexts[1]["project_id_env"], "OPS_GCP_PROJECT");
}

#[test]
fn gcp_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[gcp]
default_account = "core"
default_env = "prod"

[gcp.accounts.core]
project_id_env = "CORE_PROJECT"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .args(["orbit", "gcp", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["base_url"], "https://serviceusage.googleapis.com");
    assert_eq!(parsed["source"], "env:CORE_PROJECT");
}

#[test]
fn gcp_auth_status_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[gcp]
default_account = "core"
default_env = "staging"

[gcp.accounts.core]
project_id_env = "CORE_PROJECT"
access_token_env = "CORE_TOKEN"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "staging");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["base_url"], "https://serviceusage.googleapis.com");
    assert_eq!(parsed["source"], "env:CORE_PROJECT,env:CORE_TOKEN");
    assert_eq!(parsed["token_preview"], "ya2*************xyz");
}

#[test]
fn gcp_doctor_json_verifies_request() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/projects/proj_core/services/serviceusage.googleapis.com HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer ya29.token-core-xyz\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_doctor")],
            r#"{"state":"ENABLED"}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "doctor", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    server.join();
}

#[test]
fn gcp_service_list_json_fetches_services() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/projects/proj_core/services?"));
        assert!(request.contains("pageSize=2"));
        assert!(request.contains("filter=state%3AENABLED"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_list")],
            r#"{"services":[{"config":{"name":"aiplatform.googleapis.com"}}]}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "service", "list", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--limit",
            "2",
            "--filter",
            "state:ENABLED",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gcp_list");
    assert_eq!(parsed["data"]["services"][0]["config"]["name"], "aiplatform.googleapis.com");
    server.join();
}

#[test]
fn gcp_service_enable_json_posts_operation() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "POST /v1/projects/proj_core/services/aiplatform.googleapis.com:enable HTTP/1.1\r\n"
        ));
        assert!(request.contains("\r\n\r\n{}"));
        http_json_response("200 OK", &[], r#"{"name":"operations/op_123"}"#)
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "service", "enable", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--name",
            "aiplatform.googleapis.com",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["name"], "operations/op_123");
    server.join();
}

#[test]
fn gcp_raw_json_fetches_with_headers_and_query_params() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/projects/proj_core/services/serviceusage.googleapis.com?view=full HTTP/1.1\r\n"
        ));
        assert!(request.contains("x-custom: yes\r\n"));
        http_json_response("200 OK", &[("x-request-id", "req_gcp_raw")], r#"{"state":"ENABLED"}"#)
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "raw", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--method",
            "GET",
            "--path",
            "/v1/projects/proj_core/services/serviceusage.googleapis.com",
            "--param",
            "view=full",
            "--header",
            "x-custom=yes",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gcp_raw");
    assert_eq!(parsed["data"]["state"], "ENABLED");
    server.join();
}

#[test]
fn gcp_apikey_list_json_fetches_keys() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v2/projects/proj_core/locations/global/keys?"));
        assert!(request.contains("pageSize=3"));
        assert!(request.contains("showDeleted=true"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_apikey_list")],
            r#"{"keys":[{"name":"projects/proj_core/locations/global/keys/key1"}]}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "keys", "list", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--limit",
            "3",
            "--show-deleted",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gcp_apikey_list");
    assert_eq!(parsed["data"]["keys"][0]["name"], "projects/proj_core/locations/global/keys/key1");
    server.join();
}

#[test]
fn gcp_apikey_get_json_expands_key_id() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(
            request
                .starts_with("GET /v2/projects/proj_core/locations/global/keys/key1 HTTP/1.1\r\n")
        );
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_apikey_get")],
            r#"{"name":"projects/proj_core/locations/global/keys/key1"}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "keys", "get", "key1", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["name"], "projects/proj_core/locations/global/keys/key1");
    server.join();
}

#[test]
fn gcp_apikey_create_json_posts_payload() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("POST /v2/projects/proj_core/locations/global/keys HTTP/1.1\r\n")
        );
        assert!(request.contains("\"displayName\":\"Primary key\""));
        assert!(request.contains("\"apiTargets\""));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_apikey_create")],
            r#"{"name":"operations/create-key"}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "keys", "create", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--display-name",
            "Primary key",
            "--restrictions-json",
            "{\"apiTargets\":[{\"service\":\"translate.googleapis.com\"}]}",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gcp_apikey_create");
    server.join();
}

#[test]
fn gcp_apikey_lookup_json_queries_key_string() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v2/keys:lookupKey?"));
        assert!(request.contains("keyString=AIzaLookup"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_apikey_lookup")],
            r#"{"parent":"projects/proj_core/locations/global/keys/key1"}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "keys", "lookup", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--key-string", "AIzaLookup", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["parent"], "projects/proj_core/locations/global/keys/key1");
    server.join();
}

#[test]
fn gcp_apikey_delete_json_requires_force_and_deletes() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "DELETE /v2/projects/proj_core/locations/global/keys/key1 HTTP/1.1\r\n"
            ));
            http_json_response(
                "200 OK",
                &[("x-request-id", "req_gcp_apikey_delete")],
                r#"{"done":true}"#,
            )
        });

    cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "keys", "delete", "key1", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .failure();

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "keys", "delete", "key1", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--force", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gcp_apikey_delete");
    server.join();
}

#[test]
fn gcp_iam_service_account_list_json_fetches_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/projects/proj_core/serviceAccounts?"));
        assert!(request.contains("pageSize=2"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_gcp_iam_sa_list")],
            r#"{"accounts":[{"email":"svc@proj_core.iam.gserviceaccount.com"}]}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "iam", "service-account", "list", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--limit", "2", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_gcp_iam_sa_list");
    server.join();
}

#[test]
fn gcp_iam_service_account_create_json_posts_payload() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/projects/proj_core/serviceAccounts HTTP/1.1\r\n"));
        assert!(request.contains("\"accountId\":\"svc-core\""));
        assert!(request.contains("\"displayName\":\"Core service\""));
        http_json_response(
            "200 OK",
            &[],
            r#"{"email":"svc-core@proj_core.iam.gserviceaccount.com"}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "iam", "service-account", "create", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--account-id",
            "svc-core",
            "--display-name",
            "Core service",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["email"], "svc-core@proj_core.iam.gserviceaccount.com");
    server.join();
}

#[test]
fn gcp_iam_service_account_key_create_json_posts_defaults() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/projects/proj_core/serviceAccounts/svc@proj_core.iam.gserviceaccount.com/keys HTTP/1.1\r\n"));
        assert!(request.contains("\"privateKeyType\":\"TYPE_GOOGLE_CREDENTIALS_FILE\""));
        assert!(request.contains("\"keyAlgorithm\":\"KEY_ALG_RSA_2048\""));
        http_json_response(
            "200 OK",
            &[],
            r#"{"name":"projects/proj_core/serviceAccounts/svc@proj_core.iam.gserviceaccount.com/keys/key1"}"#,
        )
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "iam", "service-account-key", "create", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--service-account",
            "svc@proj_core.iam.gserviceaccount.com",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert!(parsed["data"]["name"].as_str().unwrap_or_default().ends_with("/keys/key1"));
    server.join();
}

#[test]
fn gcp_iam_policy_get_json_defaults_project_resource() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/projects/proj_core:getIamPolicy HTTP/1.1\r\n"));
        assert!(request.contains("\r\n\r\n{}"));
        http_json_response("200 OK", &[], r#"{"bindings":[]}"#)
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "iam", "policy", "get", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["bindings"], serde_json::json!([]));
    server.join();
}

#[test]
fn gcp_iam_role_list_json_fetches_roles() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/roles?"));
        assert!(request.contains("pageSize=5"));
        http_json_response("200 OK", &[], r#"{"roles":[{"name":"roles/viewer"}]}"#)
    });

    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "iam", "role", "list", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--limit", "5", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["roles"][0]["name"], "roles/viewer");
    server.join();
}

#[test]
fn gcp_gemini_models_list_json_fetches_models() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1beta/models?"));
        assert!(request.contains("key=AIzaGemini"));
        http_json_response("200 OK", &[], r#"{"models":[{"name":"models/gemini-2.0-flash"}]}"#)
    });

    let output = cargo_bin()
        .env("GEMINI_API_KEY", "AIzaGemini")
        .args(["orbit", "gcp", "gemini", "models", "list", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["models"][0]["name"], "models/gemini-2.0-flash");
    server.join();
}

#[test]
fn gcp_gemini_generate_json_posts_prompt_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1beta/models/gemini-2.0-flash:generateContent?"));
        assert!(request.contains("\"text\":\"hello world\""));
        assert!(request.contains("\"temperature\":0.4"));
        http_json_response(
            "200 OK",
            &[],
            r#"{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}"#,
        )
    });

    let output = cargo_bin()
        .env("GEMINI_API_KEY", "AIzaGemini")
        .args(["orbit", "gcp", "gemini", "generate", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--prompt",
            "hello world",
            "--temperature",
            "0.4",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["candidates"][0]["content"]["parts"][0]["text"], "ok");
    server.join();
}

#[test]
fn gcp_gemini_embed_json_posts_text() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1beta/models/text-embedding-004:embedContent?"));
        assert!(request.contains("\"text\":\"embed me\""));
        http_json_response("200 OK", &[], r#"{"embedding":{"values":[0.1,0.2]}}"#)
    });

    let output = cargo_bin()
        .env("GEMINI_API_KEY", "AIzaGemini")
        .args(["orbit", "gcp", "gemini", "embed", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--text", "embed me", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["embedding"]["values"][1], 0.2);
    server.join();
}

#[test]
fn gcp_gemini_raw_json_passes_headers() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n",
    )
    .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1beta/models?key=AIzaGemini HTTP/1.1\r\n"));
        assert!(request.contains("x-extra: yes\r\n"));
        http_json_response("200 OK", &[], r#"{"models":[]}"#)
    });

    let output = cargo_bin()
        .env("GEMINI_API_KEY", "AIzaGemini")
        .args(["orbit", "gcp", "gemini", "raw", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--method",
            "GET",
            "--path",
            "/v1beta/models",
            "--header",
            "x-extra=yes",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["models"], serde_json::json!([]));
    server.join();
}

#[test]
fn gcp_gemini_image_generate_writes_png_and_reports_json() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n",
    )
    .expect("write settings");
    let png_bytes = b"png-data";
    let png_b64 = BASE64_STANDARD.encode(png_bytes);
    let output_path = home.path().join("image.png");
    let server = start_one_shot_http_server(move |request| {
        assert!(request.starts_with("POST /v1beta/models/gemini-2.5-flash-image:generateContent?"));
        let body = format!(
            "{{\"candidates\":[{{\"content\":{{\"parts\":[{{\"text\":\"note\"}},{{\"inlineData\":{{\"mimeType\":\"image/png\",\"data\":\"{}\"}}}}]}}}}]}}",
            png_b64
        );
        http_json_response("200 OK", &[], &body)
    });

    let output = cargo_bin()
        .env("GEMINI_API_KEY", "AIzaGemini")
        .args(["orbit", "gcp", "gemini", "image", "generate", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--prompt",
            "draw",
            "--output",
            output_path.to_str().expect("output path"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["mime_type"], "image/png");
    assert_eq!(fs::read(&output_path).expect("read image"), png_bytes);
    server.join();
}

#[test]
fn gcp_vertex_model_list_json_fetches_models() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(settings_dir.join("settings.toml"), "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n").expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/projects/proj_core/locations/us-central1/models?"));
        assert!(request.contains("pageSize=2"));
        http_json_response(
            "200 OK",
            &[],
            r#"{"models":[{"name":"projects/proj_core/locations/us-central1/models/m1"}]}"#,
        )
    });
    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "vertex", "model", "list", "--home"])
        .arg(home.path())
        .args(["--base-url", &server.base_url, "--limit", "2", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert!(
        parsed["data"]["models"][0]["name"].as_str().unwrap_or_default().ends_with("/models/m1")
    );
    server.join();
}

#[test]
fn gcp_vertex_endpoint_create_json_posts_body() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(settings_dir.join("settings.toml"), "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n").expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "POST /v1/projects/proj_core/locations/us-central1/endpoints HTTP/1.1\r\n"
        ));
        assert!(request.contains("\"displayName\":\"endpoint-a\""));
        http_json_response("200 OK", &[], r#"{"name":"operations/create-endpoint"}"#)
    });
    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "vertex", "endpoint", "create", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--param",
            "displayName=endpoint-a",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["name"], "operations/create-endpoint");
    server.join();
}

#[test]
fn gcp_vertex_endpoint_predict_json_posts_instances() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(settings_dir.join("settings.toml"), "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n").expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "POST /v1/projects/proj_core/locations/us-central1/endpoints/ep1:predict HTTP/1.1\r\n"
        ));
        assert!(request.contains("\"instances\":[{\"prompt\":\"hi\"}]"));
        http_json_response("200 OK", &[], r#"{"predictions":[{"text":"ok"}]}"#)
    });
    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "vertex", "endpoint", "predict", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "ep1",
            "--instances-json",
            "[{\"prompt\":\"hi\"}]",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["predictions"][0]["text"], "ok");
    server.join();
}

#[test]
fn gcp_vertex_raw_json_fetches_with_header() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(settings_dir.join("settings.toml"), "schema_version = 1\n[gcp]\ndefault_account = \"core\"\n[gcp.accounts.core]\nproject_id = \"proj_core\"\naccess_token_env = \"CORE_TOKEN\"\n").expect("write settings");
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /v1/projects/proj_core/locations/us-central1/models HTTP/1.1\r\n"
            ));
            assert!(request.contains("x-extra: yes\r\n"));
            http_json_response("200 OK", &[], r#"{"models":[]}"#)
        });
    let output = cargo_bin()
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["orbit", "gcp", "vertex", "raw", "--home"])
        .arg(home.path())
        .args([
            "--base-url",
            &server.base_url,
            "--path",
            "/v1/projects/proj_core/locations/us-central1/models",
            "--header",
            "x-extra=yes",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["models"], serde_json::json!([]));
    server.join();
}

#[test]
fn google_places_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[google]
default_account = "core"

[google.accounts.core]
project_id = "proj_core"
default_language_code = "en"
default_region_code = "US"

[google.accounts.ops]
project_id = "proj_ops"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "google", "places", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["project"], "proj_core");
    assert_eq!(contexts[0]["language"], "en");
    assert_eq!(contexts[0]["region"], "US");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn google_places_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[google]
default_account = "core"
default_env = "prod"

[google.accounts.core]
project_id_env = "CORE_PROJECT"
places_api_key_env = "CORE_API_KEY"
default_language_code = "en"
default_region_code = "US"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .env("CORE_API_KEY", "AIza.token.core.xyz")
        .args(["orbit", "google", "places", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["language_code"], "en");
    assert_eq!(parsed["region_code"], "US");
    assert_eq!(parsed["base_url"], "https://places.googleapis.com");
    assert_eq!(
        parsed["source"],
        "env:CORE_API_KEY,env:CORE_PROJECT,settings.default_language_code,settings.default_region_code"
    );
}

#[test]
fn google_places_auth_status_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[google]
default_account = "core"
default_env = "staging"

[google.accounts.core]
project_id_env = "CORE_PROJECT"
staging_places_api_key_env = "CORE_STAGING_KEY"
default_language_code = "en"
default_region_code = "GB"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .env("CORE_STAGING_KEY", "AIza.staging-token-xyz")
        .args(["orbit", "google", "places", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["environment"], "staging");
    assert_eq!(parsed["language_code"], "en");
    assert_eq!(parsed["region_code"], "GB");
    assert_eq!(parsed["base_url"], "https://places.googleapis.com");
    assert_eq!(
        parsed["source"],
        "env:CORE_STAGING_KEY,env:CORE_PROJECT,settings.default_language_code,settings.default_region_code"
    );
    assert_eq!(parsed["key_preview"], "AIz****************xyz");
}

#[test]
fn google_places_autocomplete_json_posts_request() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/places:autocomplete HTTP/1.1\r\n"));
        assert!(request.contains("x-goog-api-key: google-test-key\r\n"));
        assert!(request.contains("x-goog-fieldmask: suggestions.placePrediction.placeId"));
        assert!(request.contains("\"input\":\"coffee\""));
        assert!(request.contains("\"languageCode\":\"en\""));
        assert!(request.contains("\"regionCode\":\"US\""));
        http_json_response(
            "200 OK",
            &[("x-goog-request-id", "req_google_autocomplete")],
            r#"{"suggestions":[{"placePrediction":{"placeId":"place_123","text":{"text":"Coffee Bar"}}}]}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "google", "places", "autocomplete"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--language",
            "en",
            "--region",
            "US",
            "--input",
            "coffee",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_google_autocomplete");
    assert_eq!(parsed["data"]["suggestions"][0]["placePrediction"]["placeId"], "place_123");
    server.join();
}

#[test]
fn google_places_search_text_json_fetches_all_pages() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_http_server(2, |request| {
        if request.contains("\"pageToken\":\"token_2\"") {
            assert!(request.starts_with("POST /v1/places:searchText HTTP/1.1\r\n"));
            http_json_response("200 OK", &[], r#"{"places":[{"id":"place_2"}]}"#)
        } else {
            assert!(request.starts_with("POST /v1/places:searchText HTTP/1.1\r\n"));
            assert!(request.contains("\"textQuery\":\"coffee\""));
            assert!(request.contains("x-goog-fieldmask: places.id"));
            http_json_response(
                "200 OK",
                &[],
                r#"{"places":[{"id":"place_1"}],"nextPageToken":"token_2"}"#,
            )
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "places", "search-text"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--query",
            "coffee",
            "--all",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["count"], 2);
    assert_eq!(parsed["items"][0]["id"], "place_1");
    assert_eq!(parsed["items"][1]["id"], "place_2");
    server.join();
}

#[test]
fn google_places_search_nearby_json_posts_location_body() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/places:searchNearby HTTP/1.1\r\n"));
        assert!(request.contains("\"latitude\":37.78"));
        assert!(request.contains("\"longitude\":-122.4"));
        assert!(request.contains("\"radius\":500.0"));
        http_json_response("200 OK", &[], r#"{"places":[{"id":"place_nearby"}]}"#)
    });

    let output = cargo_bin()
        .args(["orbit", "google", "places", "search-nearby"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--center",
            "37.78,-122.4",
            "--radius",
            "500",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["data"]["places"][0]["id"], "place_nearby");
    server.join();
}

#[test]
fn google_places_details_json_gets_place() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/places/place_123?"));
        assert!(request.contains("sessionToken=sess_123"));
        assert!(request.contains("languageCode=en"));
        assert!(request.contains("regionCode=US"));
        http_json_response(
            "200 OK",
            &[("x-goog-request-id", "req_google_details")],
            r#"{"id":"place_123","formattedAddress":"1 Main St"}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "google", "places", "details", "place_123"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--session",
            "sess_123",
            "--language",
            "en",
            "--region",
            "US",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_google_details");
    assert_eq!(parsed["data"]["id"], "place_123");
    server.join();
}

#[test]
fn google_places_photo_download_json_writes_output() {
    let home = tempdir().expect("tempdir");
    let output_dir = tempdir().expect("tempdir");
    let output_path = output_dir.path().join("photo.jpg");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/places/place_123/photos/photo_123/media?maxWidthPx=400 HTTP/1.1\r\n"
        ));
        "HTTP/1.1 200 OK\r\nContent-Type: image/jpeg\r\nContent-Length: 10\r\nx-goog-request-id: req_google_photo\r\n\r\njpeg-bytes".to_owned()
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "google",
            "places",
            "photo",
            "download",
            "places/place_123/photos/photo_123",
        ])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--output",
            output_path.to_str().expect("output str"),
            "--max-width",
            "400",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["bytes_written"], 10);
    assert_eq!(parsed["request_id"], "req_google_photo");
    assert_eq!(fs::read(&output_path).expect("read output"), b"jpeg-bytes");
    server.join();
}

#[test]
fn google_places_doctor_json_verifies_requests() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_http_server(2, |request| {
        if request.starts_with("POST /v1/places:autocomplete HTTP/1.1\r\n") {
            assert!(request.contains("\"input\":\"cafe\""));
            http_json_response(
                "200 OK",
                &[("x-goog-request-id", "req_google_doctor_autocomplete")],
                r#"{"suggestions":[]}"#,
            )
        } else {
            assert!(request.starts_with("POST /v1/places:searchText HTTP/1.1\r\n"));
            assert!(request.contains("\"textQuery\":\"coffee\""));
            http_json_response(
                "200 OK",
                &[("x-goog-request-id", "req_google_doctor_search")],
                r#"{"places":[]}"#,
            )
        }
    });

    let output = cargo_bin()
        .args(["orbit", "google", "places", "doctor"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["checks"][0]["request_id"], "req_google_doctor_autocomplete");
    assert_eq!(parsed["checks"][1]["request_id"], "req_google_doctor_search");
    server.join();
}

#[test]
fn google_places_raw_json_fetches_with_headers_and_query_params() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/places/place_123?regionCode=US HTTP/1.1\r\n"));
        assert!(request.contains("x-goog-api-key: google-test-key\r\n"));
        assert!(request.contains("x-custom: value\r\n"));
        http_json_response(
            "200 OK",
            &[("x-goog-request-id", "req_google_raw")],
            r#"{"id":"place_123","displayName":{"text":"Cafe"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "google", "places", "raw"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--api-key",
            "google-test-key",
            "--base-url",
            &server.base_url,
            "--method",
            "GET",
            "--path",
            "/v1/places/place_123",
            "--param",
            "regionCode=US",
            "--header",
            "x-custom=value",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_google_raw");
    assert_eq!(parsed["data"]["id"], "place_123");
    server.join();
}

#[test]
fn google_places_session_json_round_trip_uses_home_store() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[google]\ndefault_account = \"core\"\n",
    )
    .expect("write settings");

    let created = cargo_bin()
        .args(["orbit", "google", "places", "session", "new"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--note",
            "demo",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let created: Value = serde_json::from_slice(&created).expect("json output");
    let token = created["token"].as_str().expect("token").to_owned();
    assert_eq!(created["account_alias"], "core");

    let listed = cargo_bin()
        .args(["orbit", "google", "places", "session", "list"])
        .args(["--home", home.path().to_str().expect("home str"), "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&listed).expect("json output");
    assert_eq!(listed[0]["token"], token);

    let ended = cargo_bin()
        .args(["orbit", "google", "places", "session", "end", &token])
        .args(["--home", home.path().to_str().expect("home str"), "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let ended: Value = serde_json::from_slice(&ended).expect("json output");
    assert_eq!(ended["token"], token);
    assert!(!ended["ended_at"].as_str().expect("ended_at").is_empty());
}

#[test]
fn google_places_types_validate_json_reports_group() {
    let output = cargo_bin()
        .args(["orbit", "google", "places", "types", "validate", "cafe", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["valid"], true);
    assert_eq!(parsed["group"], "food");
}

#[test]
fn google_places_report_usage_json_reads_log_file() {
    let home = tempdir().expect("tempdir");
    let log_dir = tempdir().expect("tempdir");
    let log_path = log_dir.path().join("google-places.log");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir settings dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    fs::write(
        &log_path,
        concat!(
            "{\"ts\":\"2026-03-16T00:00:00Z\",\"event\":\"request\",\"method\":\"POST\",\"path\":\"/v1/places:autocomplete\",\"ctx_account_alias\":\"core\",\"ctx_environment\":\"prod\"}\n",
            "{\"ts\":\"2026-03-16T00:00:01Z\",\"event\":\"response\",\"method\":\"POST\",\"path\":\"/v1/places:autocomplete\",\"status\":200,\"duration_ms\":42,\"request_id\":\"req_1\",\"ctx_account_alias\":\"core\",\"ctx_environment\":\"prod\"}\n"
        ),
    )
    .expect("write log");

    let output = cargo_bin()
        .env("SI_GOOGLE_PLACES_LOG_FILE", log_path.to_str().expect("log path"))
        .args(["orbit", "google", "places", "report", "usage"])
        .args(["--home", home.path().to_str().expect("home str"), "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requests"], 1);
    assert_eq!(parsed["responses"], 1);
    assert_eq!(parsed["status_buckets"]["2xx"], 1);
}

#[test]
fn google_places_report_sessions_json_reads_store() {
    let home = tempdir().expect("tempdir");
    let store_dir = home.path().join(".si/google/places");
    fs::create_dir_all(&store_dir).expect("mkdir store dir");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");
    fs::write(
        store_dir.join("sessions.json"),
        concat!(
            "{\n  \"sessions\": {\n",
            "    \"sess_1\": {\"token\":\"sess_1\",\"account_alias\":\"core\",\"created_at\":\"2026-03-16T00:00:00Z\",\"updated_at\":\"2026-03-16T00:00:00Z\"},\n",
            "    \"sess_2\": {\"token\":\"sess_2\",\"account_alias\":\"core\",\"created_at\":\"2026-03-16T01:00:00Z\",\"updated_at\":\"2026-03-16T02:00:00Z\",\"ended_at\":\"2026-03-16T02:00:00Z\"}\n",
            "  }\n}\n"
        ),
    )
    .expect("write store");

    let output = cargo_bin()
        .args(["orbit", "google", "places", "report", "sessions"])
        .args([
            "--home",
            home.path().to_str().expect("home str"),
            "--account",
            "core",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["total"], 2);
    assert_eq!(parsed["active"], 1);
    assert_eq!(parsed["ended"], 1);
}

#[test]
fn openai_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[openai]
default_account = "core"

[openai.accounts.core]
api_key_env = "CORE_OPENAI_API_KEY"
admin_api_key_env = "CORE_OPENAI_ADMIN_API_KEY"
organization_id = "org_core"
project_id = "proj_core"

[openai.accounts.ops]
project_id_env = "OPS_OPENAI_PROJECT_ID"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "openai", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["api_key_env"], "CORE_OPENAI_API_KEY");
    assert_eq!(contexts[0]["admin_api_key_env"], "CORE_OPENAI_ADMIN_API_KEY");
    assert_eq!(contexts[0]["org_id"], "org_core");
    assert_eq!(contexts[0]["project_id"], "proj_core");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn openai_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[openai]
default_account = "core"
default_project_id = "proj_default"

[openai.accounts.core]
api_key_env = "CORE_OPENAI_API_KEY"
organization_id_env = "CORE_OPENAI_ORG"
admin_api_key_env = "CORE_OPENAI_ADMIN_KEY"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_OPENAI_API_KEY", "sk-test")
        .env("CORE_OPENAI_ORG", "org_core")
        .env("CORE_OPENAI_ADMIN_KEY", "sk-admin")
        .args(["orbit", "openai", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["base_url"], "https://api.openai.com");
    assert_eq!(parsed["organization_id"], "org_core");
    assert_eq!(parsed["project_id"], "proj_default");
    assert_eq!(parsed["admin_key_set"], true);
    assert_eq!(
        parsed["source"],
        "env:CORE_OPENAI_API_KEY,env:CORE_OPENAI_ADMIN_KEY,env:CORE_OPENAI_ORG,settings.default_project_id"
    );
}

#[test]
fn apple_appstore_auth_status_json_reads_local_inputs() {
    let key_file = tempdir().expect("tempdir");
    let key_path = key_file.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args(["orbit", "apple", "store", "auth", "status"])
        .args(["--account", "mobile"])
        .args(["--env", "staging"])
        .args(["--project-id", "proj_mobile"])
        .args(["--bundle-id", "com.example.mobile"])
        .args(["--locale", "fr-FR"])
        .args(["--platform", "MAC_OS"])
        .args(["--issuer-id", "issuer_123"])
        .args(["--key-id", "key_123"])
        .args(["--private-key-file", key_path.to_str().expect("utf8")])
        .args(["--verify=false", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["account_alias"], "mobile");
    assert_eq!(parsed["environment"], "staging");
    assert_eq!(parsed["project_id"], "proj_mobile");
    assert_eq!(parsed["bundle_id"], "com.example.mobile");
    assert_eq!(parsed["locale"], "fr-FR");
    assert_eq!(parsed["platform"], "MAC_OS");
    assert_eq!(parsed["token_source"], "flag:--private-key-file");
}

const APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM: &str = r#"-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgXv1fLQwQYWpHLmrJ
BDNK155BX3ig/zpgQGtC9XlwhN2hRANCAASXYN6j6kX+3XZV6tbvsSjPrF542r1z
IiirJwd3+qH5BaD2H1FSA45SwJBmSifpUAaqEFjt5zEvDmqpRReOsvvY
-----END PRIVATE KEY-----
"#;

#[test]
fn apple_appstore_app_list_json_fetches_apps() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/apps?filter%5BbundleId%5D=com.example.mobile&limit=5 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer "));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_apple_apps")],
            r#"{"data":[{"id":"app_123","type":"apps","attributes":{"bundleId":"com.example.mobile"}}]}"#,
        )
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "app",
            "list",
            "--base-url",
            &server.base_url,
            "--bundle-id",
            "com.example.mobile",
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--limit",
            "5",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_apple_apps");
    assert_eq!(parsed["data"]["data"][0]["id"], "app_123");
    server.join();
}

#[test]
fn apple_appstore_app_create_json_creates_bundle_and_app() {
    let server = start_http_server_with_body(4, |request| {
        if request.starts_with(
            "GET /v1/bundleIds?filter%5Bidentifier%5D=com.example.mobile&limit=1 HTTP/1.1\r\n",
        ) {
            return http_json_response("200 OK", &[], r#"{"data":[]}"#);
        }
        if request.starts_with("POST /v1/bundleIds HTTP/1.1\r\n") {
            assert!(request.contains("\"identifier\":\"com.example.mobile\""));
            return http_json_response(
                "201 Created",
                &[],
                r#"{"data":{"id":"bundle_123","type":"bundleIds"}}"#,
            );
        }
        assert!(
            request.starts_with("POST /v1/apps HTTP/1.1\r\n")
                || request.starts_with(
                    "GET /v1/apps?filter%5BbundleId%5D=com.example.mobile&limit=1 HTTP/1.1\r\n"
                )
        );
        if request.starts_with(
            "GET /v1/apps?filter%5BbundleId%5D=com.example.mobile&limit=1 HTTP/1.1\r\n",
        ) {
            return http_json_response("200 OK", &[], r#"{"data":[]}"#);
        }
        assert!(request.contains("\"sku\":\"mobile-sku\""));
        http_json_response("201 Created", &[], r#"{"data":{"id":"app_456","type":"apps"}}"#)
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "app",
            "create",
            "--base-url",
            &server.base_url,
            "--bundle-id",
            "com.example.mobile",
            "--app-name",
            "Mobile",
            "--sku",
            "mobile-sku",
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["bundle_created"], true);
    assert_eq!(parsed["bundle_resource_id"], "bundle_123");
    assert_eq!(parsed["app_created"], true);
    assert_eq!(parsed["app_id"], "app_456");
    server.join();
}

#[test]
fn apple_appstore_listing_update_json_updates_localized_metadata() {
    let server = start_http_server(4, |request| {
        if request.starts_with(
            "GET /v1/apps?filter%5BbundleId%5D=com.example.mobile&limit=1 HTTP/1.1\r\n",
        ) {
            return http_json_response("200 OK", &[], r#"{"data":[{"id":"app_123"}]}"#);
        }
        if request.starts_with("GET /v1/apps/app_123/appInfos?limit=1 HTTP/1.1\r\n") {
            return http_json_response("200 OK", &[], r#"{"data":[{"id":"info_123"}]}"#);
        }
        if request.starts_with("GET /v1/appInfos/info_123/appInfoLocalizations?filter%5Blocale%5D=en-US&limit=1 HTTP/1.1\r\n") {
            return http_json_response("200 OK", &[], r#"{"data":[{"id":"loc_123"}]}"#);
        }
        assert!(request.starts_with("PATCH /v1/appInfoLocalizations/loc_123 HTTP/1.1\r\n"));
        assert!(request.contains("\"name\":\"New Name\""));
        http_json_response("200 OK", &[], r#"{"data":{"id":"loc_123"}}"#)
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "listing",
            "update",
            "--base-url",
            &server.base_url,
            "--bundle-id",
            "com.example.mobile",
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--name",
            "New Name",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["app_info_updated"], true);
    assert_eq!(parsed["version_info_updated"], false);
    server.join();
}

#[test]
fn apple_appstore_raw_json_fetches_api_path() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/apps?limit=1 HTTP/1.1\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_apple_raw")],
            r#"{"data":[{"id":"app_123"}]}"#,
        )
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "raw",
            "--base-url",
            &server.base_url,
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--path",
            "/v1/apps",
            "--param",
            "limit=1",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_apple_raw");
    assert_eq!(parsed["data"]["data"][0]["id"], "app_123");
    server.join();
}

#[test]
fn apple_appstore_apply_json_applies_metadata_bundle() {
    let metadata_dir = tempdir().expect("tempdir");
    fs::create_dir_all(metadata_dir.path().join("app-info")).expect("mkdir app-info");
    fs::write(metadata_dir.path().join("app-info").join("en-US.json"), r#"{"name":"Bundle Name"}"#)
        .expect("write app-info");

    let server = start_http_server(4, |request| {
        if request.starts_with(
            "GET /v1/apps?filter%5BbundleId%5D=com.example.mobile&limit=1 HTTP/1.1\r\n",
        ) {
            return http_json_response("200 OK", &[], r#"{"data":[{"id":"app_123"}]}"#);
        }
        if request.starts_with("GET /v1/apps/app_123/appInfos?limit=1 HTTP/1.1\r\n") {
            return http_json_response("200 OK", &[], r#"{"data":[{"id":"info_123"}]}"#);
        }
        if request.starts_with("GET /v1/appInfos/info_123/appInfoLocalizations?filter%5Blocale%5D=en-US&limit=1 HTTP/1.1\r\n") {
            return http_json_response("200 OK", &[], r#"{"data":[]}"#);
        }
        assert!(request.starts_with("POST /v1/appInfoLocalizations HTTP/1.1\r\n"));
        assert!(request.contains("\"locale\":\"en-US\""));
        http_json_response("201 Created", &[], r#"{"data":{"id":"loc_999"}}"#)
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "apply",
            "--base-url",
            &server.base_url,
            "--bundle-id",
            "com.example.mobile",
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--metadata-dir",
            metadata_dir.path().to_str().expect("utf8"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["locales_applied"], 1);
    assert_eq!(parsed["app_info_updated"], 1);
    assert_eq!(parsed["version_info_updated"], 0);
    server.join();
}

#[test]
fn apple_appstore_auth_status_json_verifies_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/apps?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer "));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_apple_auth_verify")],
            r#"{"data":[{"id":"app_123"}]}"#,
        )
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "auth",
            "status",
            "--base-url",
            &server.base_url,
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify"]["ok"], true);
    assert_eq!(parsed["verify"]["status_code"], 200);
    assert_eq!(parsed["verify"]["items"], 1);
    assert!(parsed["token_expires_at"].as_str().unwrap_or_default().contains('T'));
    server.join();
}

#[test]
fn apple_appstore_doctor_json_verifies_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/apps?limit=1 HTTP/1.1\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_apple_doctor")],
            r#"{"data":[{"id":"app_123"}]}"#,
        )
    });

    let key_dir = tempdir().expect("tempdir");
    let key_path = key_dir.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, APPLE_APPSTORE_TEST_EC_PRIVATE_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args([
            "orbit",
            "apple",
            "store",
            "doctor",
            "--base-url",
            &server.base_url,
            "--issuer-id",
            "issuer_123",
            "--key-id",
            "key_123",
            "--private-key-file",
            key_path.to_str().expect("utf8"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify"]["ok"], true);
    assert_eq!(parsed["verify"]["items"], 1);
    server.join();
}

#[test]
fn openai_auth_status_json_verifies_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_auth")],
            r#"{"data":[{"id":"gpt-4.1-mini","object":"model"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "auth",
            "status",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify_status"], 200);
    assert_eq!(parsed["verify"]["data"][0]["id"], "gpt-4.1-mini");
    server.join();
}

#[test]
fn openai_doctor_json_verifies_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_doctor")],
            r#"{"data":[{"id":"gpt-4.1-mini","object":"model"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "doctor",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["provider"], "openai");
    assert_eq!(parsed["checks"][2]["name"], "request");
    assert_eq!(parsed["checks"][2]["ok"], true);
    assert_eq!(parsed["checks"][2]["detail"], "200 200 OK");
    server.join();
}

#[test]
fn openai_model_list_json_fetches_from_api() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        assert!(request.contains("openai-organization: org_core\r\n"));
        assert!(request.contains("openai-project: proj_core\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_models")],
            r#"{"data":[{"id":"gpt-4.1-mini","object":"model"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "model",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--org-id",
            "org_core",
            "--project-id",
            "proj_core",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_models");
    assert_eq!(parsed["data"]["data"][0]["id"], "gpt-4.1-mini");
    server.join();
}

#[test]
fn openai_model_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models/gpt-test HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_model")],
            r#"{"id":"gpt-test","object":"model"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "model",
            "get",
            "gpt-test",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_model"));
    assert!(rendered.contains("\"id\": \"gpt-test\""));
    server.join();
}

#[test]
fn openai_project_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects?limit=1&include_archived=true HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_projects")],
            r#"{"data":[{"id":"proj_123","name":"Core"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--include-archived",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_projects");
    assert_eq!(parsed["data"]["data"][0]["id"], "proj_123");
    server.join();
}

#[test]
fn openai_project_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/projects/proj_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_project")],
            r#"{"id":"proj_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "get",
            "proj_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_project"));
    assert!(rendered.contains("\"id\": \"proj_123\""));
    server.join();
}

#[test]
fn openai_project_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/organization/projects HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        assert!(request.contains(r#""name":"Core""#));
        assert!(request.contains(r#""geography":"eu""#));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_project_create")],
            r#"{"id":"proj_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "create",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--name",
            "Core",
            "--geography",
            "eu",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_project_create");
    assert_eq!(parsed["data"]["id"], "proj_123");
    server.join();
}

#[test]
fn openai_project_archive_requires_force() {
    let stderr = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "archive",
            "proj_123",
            "--base-url",
            "https://api.example.test",
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8_lossy(&stderr).contains("requires --force"));
}

#[test]
fn openai_project_api_key_list_json_fetches_from_api_with_admin_key() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /v1/organization/projects/proj_123/api_keys?limit=1 HTTP/1.1\r\n"
            ));
            assert!(request.contains("authorization: Bearer sk-admin\r\n"));
            http_json_response(
                "200 OK",
                &[("x-request-id", "req_keys")],
                r#"{"data":[{"id":"key_123","name":"Deploy"}]}"#,
            )
        });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "api-key",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_keys");
    assert_eq!(parsed["data"]["data"][0]["id"], "key_123");
    server.join();
}

#[test]
fn openai_project_api_key_get_text_formats_response() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /v1/organization/projects/proj_123/api_keys/key_123 HTTP/1.1\r\n"
            ));
            assert!(request.contains("authorization: Bearer sk-admin\r\n"));
            http_json_response(
                "200 OK",
                &[("x-request-id", "req_key")],
                r#"{"id":"key_123","name":"Deploy"}"#,
            )
        });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "api-key",
            "get",
            "key_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_key"));
    assert!(rendered.contains("\"id\": \"key_123\""));
    server.join();
}

#[test]
fn openai_project_api_key_delete_json_deletes_with_force() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "DELETE /v1/organization/projects/proj_123/api_keys/key_123 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_key_delete")],
            r#"{"id":"key_123","deleted":true}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "api-key",
            "delete",
            "key_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--force",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_key_delete");
    assert_eq!(parsed["data"]["deleted"], true);
    server.join();
}

#[test]
fn openai_project_service_account_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/service_accounts?limit=1 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_service_accounts")],
            r#"{"data":[{"id":"sa_123","name":"Deploy"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "service-account",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_service_accounts");
    assert_eq!(parsed["data"]["data"][0]["id"], "sa_123");
    server.join();
}

#[test]
fn openai_project_service_account_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/service_accounts/sa_123 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_service_account")],
            r#"{"id":"sa_123","name":"Deploy"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "service-account",
            "get",
            "sa_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_service_account"));
    assert!(rendered.contains("\"id\": \"sa_123\""));
    server.join();
}

#[test]
fn openai_project_service_account_create_json_posts_payload() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "POST /v1/organization/projects/proj_123/service_accounts HTTP/1.1\r\n"
            ));
            assert!(request.contains(r#"{"name":"Deploy"}"#));
            http_json_response(
                "200 OK",
                &[("x-request-id", "req_service_account_create")],
                r#"{"id":"sa_123","name":"Deploy"}"#,
            )
        });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "service-account",
            "create",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--name",
            "Deploy",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_service_account_create");
    assert_eq!(parsed["data"]["id"], "sa_123");
    server.join();
}

#[test]
fn openai_project_rate_limit_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/rate_limits?limit=1&after=cursor HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_rate_limits")],
            r#"{"data":[{"id":"rl_123","max_requests_per_1_minute":60}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "rate-limit",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--after",
            "cursor",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_rate_limits");
    assert_eq!(parsed["data"]["data"][0]["id"], "rl_123");
    server.join();
}

#[test]
fn openai_project_rate_limit_update_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "POST /v1/organization/projects/proj_123/rate_limits/rl_123 HTTP/1.1\r\n"
        ));
        assert!(request.contains(r#"{"max_requests_per_1_minute":42}"#));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_rate_limit_update")],
            r#"{"id":"rl_123","max_requests_per_1_minute":42}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "project",
            "rate-limit",
            "update",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--rate-limit-id",
            "rl_123",
            "--max-requests-per-1-minute",
            "42",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_rate_limit_update");
    assert_eq!(parsed["data"]["max_requests_per_1_minute"], 42);
    server.join();
}

#[test]
fn openai_key_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request
                .starts_with("GET /v1/organization/admin_api_keys?limit=1&order=desc HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_admin_keys")],
            r#"{"data":[{"id":"adminkey_123","name":"Core"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "key",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--order",
            "desc",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_admin_keys");
    assert_eq!(parsed["data"]["data"][0]["id"], "adminkey_123");
    server.join();
}

#[test]
fn openai_key_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("GET /v1/organization/admin_api_keys/adminkey_123 HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_admin_key")],
            r#"{"id":"adminkey_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "key",
            "get",
            "adminkey_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_admin_key"));
    assert!(rendered.contains("\"id\": \"adminkey_123\""));
    server.join();
}

#[test]
fn openai_key_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/organization/admin_api_keys HTTP/1.1\r\n"));
        assert!(request.contains(r#"{"name":"Core"}"#));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_admin_key_create")],
            r#"{"id":"adminkey_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "key",
            "create",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--name",
            "Core",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_admin_key_create");
    assert_eq!(parsed["data"]["id"], "adminkey_123");
    server.join();
}

#[test]
fn openai_key_delete_requires_force() {
    let stderr = cargo_bin()
        .args([
            "orbit",
            "openai",
            "key",
            "delete",
            "adminkey_123",
            "--base-url",
            "https://api.example.test",
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8_lossy(&stderr).contains("requires --force"));
}

#[test]
fn openai_usage_completions_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/usage/completions?start_time="));
        assert!(request.contains("&limit=1"));
        assert!(request.contains("&models=gpt-5-codex"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_usage")],
            r#"{"data":[{"object":"bucket","results":[{"input_tokens":42}]}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "usage",
            "completions",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--model",
            "gpt-5-codex",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_usage");
    assert_eq!(parsed["data"]["data"][0]["object"], "bucket");
    server.join();
}

#[test]
fn openai_codex_usage_json_defaults_codex_model() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/usage/completions?start_time="));
        assert!(request.contains("&limit=1"));
        assert!(request.contains("&models=gpt-5-codex"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_codex_usage")],
            r#"{"data":[{"object":"bucket","results":[{"input_tokens":7}]}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "codex",
            "usage",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_codex_usage");
    assert_eq!(parsed["data"]["data"][0]["object"], "bucket");
    server.join();
}

#[test]
fn openai_monitor_usage_json_defaults_to_completions() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/usage/completions?start_time="));
        assert!(request.contains("&limit=1"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_monitor_usage")],
            r#"{"data":[{"object":"bucket","results":[{"input_tokens":11}]}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "monitor",
            "usage",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_monitor_usage");
    assert_eq!(parsed["data"]["data"][0]["object"], "bucket");
    server.join();
}

#[test]
fn openai_monitor_limits_json_fetches_project_rate_limits() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/rate_limits?limit=1 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_monitor_limits")],
            r#"{"data":[{"id":"rl_456","max_requests_per_1_minute":120}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "monitor",
            "limits",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_monitor_limits");
    assert_eq!(parsed["data"]["data"][0]["id"], "rl_456");
    server.join();
}

#[test]
fn openai_raw_json_fetches_with_headers_and_query_params() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models?limit=2 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        assert!(request.contains("openai-organization: org_123\r\n"));
        assert!(request.contains("openai-project: proj_123\r\n"));
        assert!(request.contains("X-Test: alpha\r\n") || request.contains("x-test: alpha\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_openai_raw_get")],
            r#"{"data":[{"id":"gpt-4.1-mini"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "raw",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--org-id",
            "org_123",
            "--project-id",
            "proj_123",
            "--path",
            "/v1/models",
            "--param",
            "limit=2",
            "--header",
            "x-test=alpha",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_openai_raw_get");
    assert_eq!(parsed["data"]["data"][0]["id"], "gpt-4.1-mini");
    server.join();
}

#[test]
fn openai_raw_json_posts_json_body_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /v1/organization/projects HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        assert!(request.contains("content-type: application/json\r\n"));
        assert!(request.contains("\r\n\r\n{\"name\":\"Core\"}"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_openai_raw_post")],
            r#"{"id":"proj_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "orbit",
            "openai",
            "raw",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--admin",
            "--method",
            "POST",
            "--path",
            "/v1/organization/projects",
            "--json-body",
            "{\"name\":\"Core\"}",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_openai_raw_post");
    assert_eq!(parsed["data"]["id"], "proj_123");
    server.join();
}

#[test]
fn oci_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[oci]
default_account = "core"
profile = "DEFAULT"

[oci.accounts.core]
region = "us-phoenix-1"
config_file = "/tmp/core-config"

[oci.accounts.ops]
profile = "OPS"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["orbit", "oci", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["profile"], "DEFAULT");
    assert_eq!(contexts[0]["region"], "us-phoenix-1");
    assert_eq!(contexts[0]["config_file"], "/tmp/core-config");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn oci_context_current_json_reads_profile_config() {
    let config_dir = tempdir().expect("tempdir");
    let config_file = config_dir.path().join("config");
    fs::write(&config_file, "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nregion=us-phoenix-1\n")
        .expect("write config");

    let output = cargo_bin()
        .args(["orbit", "oci", "context", "current"])
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .arg("--format")
        .arg("json")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile"], "DEFAULT");
    assert_eq!(parsed["region"], "us-phoenix-1");
    assert_eq!(parsed["auth_style"], "signature");
    assert_eq!(parsed["tenancy_ocid"], "ocid1.tenancy.oc1..example");
    assert_eq!(parsed["source"], "profile:DEFAULT");
}

#[test]
fn oci_auth_status_json_reads_local_signature_context() {
    let config_dir = tempdir().expect("tempdir");
    let config_file = config_dir.path().join("config");
    let key_dir = config_dir.path().join("keys");
    let key_file = key_dir.join("oci.pem");
    fs::create_dir_all(&key_dir).expect("mkdir key dir");
    fs::write(
        &config_file,
        "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nuser=ocid1.user.oc1..example\nfingerprint=aa:bb:cc\nkey_file=keys/oci.pem\nregion=us-phoenix-1\n",
    )
    .expect("write config");
    fs::write(&key_file, OCI_TEST_RSA_KEY_PEM).expect("write key");

    let output = cargo_bin()
        .args(["orbit", "oci", "auth", "status"])
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--verify=false", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["profile"], "DEFAULT");
    assert_eq!(parsed["region"], "us-phoenix-1");
    assert_eq!(parsed["auth_style"], "signature");
    assert_eq!(parsed["tenancy_ocid"], "ocid1.tenancy.oc1..example");
    assert_eq!(parsed["user_ocid"], "ocid1.user.oc1..example");
    assert_eq!(parsed["fingerprint"], "aa:bb:cc");
    assert_eq!(parsed["source"], "profile:DEFAULT");
}

#[test]
fn oci_auth_status_json_verifies_with_identity_probe() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /20160918/availabilityDomains?compartmentId=ocid1.tenancy.oc1..example HTTP/1.1\r\n"
        ));
        assert!(request.contains("Signature version=\"1\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_verify")],
            r#"[{"name":"AD-1"}]"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "auth", "status"])
        .args(["--home"])
        .arg(home.path())
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify_status"], 200);
    assert_eq!(parsed["verify"][0]["name"], "AD-1");
}

#[test]
fn oci_doctor_json_verifies_runtime_probe() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /20160918/availabilityDomains?compartmentId=ocid1.tenancy.oc1..example HTTP/1.1\r\n"
        ));
        assert!(request.contains("Signature version=\"1\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_doctor")],
            r#"[{"name":"AD-1"}]"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "doctor"])
        .args(["--home"])
        .arg(home.path())
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ok"], true);
    assert_eq!(parsed["provider"], "oci_core");
    assert_eq!(parsed["checks"][3]["name"], "request");
    assert_eq!(parsed["checks"][3]["ok"], true);
}

#[test]
fn oci_oracular_tenancy_json_reads_profile_config() {
    let config_dir = tempdir().expect("tempdir");
    let config_file = config_dir.path().join("config");
    fs::write(&config_file, "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nregion=us-phoenix-1\n")
        .expect("write config");

    let output = cargo_bin()
        .args(["orbit", "oci", "oracular", "tenancy"])
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile"], "DEFAULT");
    assert_eq!(parsed["config_file"], config_file.to_str().expect("utf8"));
    assert_eq!(parsed["tenancy_ocid"], "ocid1.tenancy.oc1..example");
}

const OCI_TEST_RSA_KEY_PEM: &str = r#"-----BEGIN PRIVATE KEY-----
MIICdgIBADANBgkqhkiG9w0BAQEFAASCAmAwggJcAgEAAoGBAMHdWNb6AMmJKYK2
AtBSIA5dld4B22eLwBBeQaqsbqyZj3Wpu4lgs2Hu/PBRIgqN/VT83RRyhLjp1PTL
9fNTlykVRd3aBOj8QwIWsVS+10a/8GuPx5N4vZlzsiplkIOEwcrpCQs30uNPtJqv
br2DSoulEAzFiboOri2wsY+MIbKxAgMBAAECgYAn0+mkgMgYn20/xVTep4CecuuP
KKKCq1tSAYtMHRC/tOycJ7q3hn5T6F1eocx0jqc1Bp4EzWIm+yMdB6oHy2yKUH/f
N5zX1Hi/pulp5zO6c8ANaHjb48fBiBOTck7FQ9c/uppCleBESdE773zk6fN7XKgm
z6Y9EegeBYMrAP5DYQJBAOtaAtKsQYKiPoQM6EiskBfO3kpRS7C4WgrJchgArY74
+tBk5s0Bf6ibSxSyNfSZ4gZyyF7kLNDR3CWAxFp9EX8CQQDS34pEuKVSEYz41uiS
MzM+hQJiszF8M2NPj9IzqT8EmvXIvveK29f6C6nxkzllKB6WyjnB0PcbYqHnCsGv
G/PPAkBw6m+eShzoIxVhX5v2eixr78mA2H47HEe/EyVVVMXwaY5Ue4SsaQKpj1A3
bsUqRMZHl7yAonLKAVXg/GW4kHbbAkBkqCXFJepsIUqMYXFEkEIOvsjjuiuN4K2w
BbPNyyT0ms9l0pow4z3V8oldcew8uAjZ64/kT04U+WDU+1J2tr4LAkEAo2Jr+HY3
n7bZhk8wZV/UBPJY/hjPoMGweaYAz8Vx4OujBqJhYaVd4XHFSH8cOGiXGsj5IVfE
ytNZBG2qI/IOCw==
-----END PRIVATE KEY-----
"#;

fn write_oci_test_config(home: &tempfile::TempDir, base_url: &str) -> std::path::PathBuf {
    let config_dir = home.path().join("oci");
    fs::create_dir_all(&config_dir).expect("mkdir oci config dir");
    let key_file = config_dir.join("oci.pem");
    fs::write(&key_file, OCI_TEST_RSA_KEY_PEM).expect("write oci key");
    let config_file = config_dir.join("config");
    fs::write(
        &config_file,
        "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nuser=ocid1.user.oc1..example\nfingerprint=aa:bb:cc\nkey_file=oci.pem\nregion=local\n",
    )
    .expect("write oci config");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir .si");
    fs::write(
        settings_dir.join("settings.toml"),
        format!("schema_version = 1\n\n[oci]\napi_base_url = \"{base_url}\"\n"),
    )
    .expect("write settings");
    config_file
}

#[test]
fn oci_oracular_cloud_init_json_renders_base64_payload() {
    let output = cargo_bin()
        .args(["orbit", "oci", "oracular", "cloud-init", "--ssh-port", "7129", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["ssh_port"], 7129);
    let decoded = BASE64_STANDARD
        .decode(parsed["user_data_b64"].as_str().expect("user_data_b64"))
        .expect("decode cloud-init");
    let text = String::from_utf8(decoded).expect("utf8 cloud-init");
    assert!(text.contains("Port 7129"));
}

#[test]
fn oci_identity_availability_domains_json_signs_and_lists() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /20160918/availabilityDomains?compartmentId=ocid1.tenancy.oc1..example HTTP/1.1\r\n"));
        assert!(request.contains("Signature version=\"1\""));
        http_json_response("200 OK", &[("opc-request-id", "req_oci_ads")], r#"[{"name":"AD-1"}]"#)
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "identity", "availability-domains", "list"])
        .args(["--home"])
        .arg(home.path())
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--base-url", &server.base_url])
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_oci_ads");
    assert_eq!(parsed["list"][0]["name"], "AD-1");
}

#[test]
fn oci_identity_compartment_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/compartments HTTP/1.1\r\n"));
        assert!(request.contains("\"name\":\"prod\""));
        assert!(request.contains("\"compartmentId\":\"ocid1.compartment.oc1..root\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_compartment")],
            r#"{"id":"ocid1.compartment.oc1..prod","name":"prod"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "identity", "compartment", "create"])
        .args(["--home"])
        .arg(home.path())
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args([
            "--base-url",
            &server.base_url,
            "--parent",
            "ocid1.compartment.oc1..root",
            "--name",
            "prod",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["name"], "prod");
}

#[test]
fn oci_compute_image_latest_ubuntu_json_queries_core_api() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.contains("GET /20160918/images?"));
        assert!(request.contains("operatingSystem=Canonical+Ubuntu"));
        assert!(request.contains("operatingSystemVersion=24.04"));
        assert!(request.contains("shape=VM.Standard.A1.Flex"));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_image")],
            r#"[{"id":"ocid1.image.oc1..ubuntu","displayName":"Ubuntu 24.04"}]"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "compute", "image", "latest-ubuntu"])
        .args(["--home"])
        .arg(home.path())
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["list"][0]["id"], "ocid1.image.oc1..ubuntu");
}

#[test]
fn oci_network_vcn_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/vcns HTTP/1.1\r\n"));
        assert!(request.contains("\"cidrBlocks\":[\"10.0.0.0/16\"]"));
        assert!(request.contains("\"displayName\":\"oracular-vcn\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_vcn")],
            r#"{"id":"ocid1.vcn.oc1..vcn"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "network", "vcn", "create"])
        .args(["--home"])
        .arg(home.path())
        .args([
            "--config-file",
            config_file.to_str().expect("utf8"),
            "--compartment",
            "ocid1.compartment.oc1..prod",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ocid1.vcn.oc1..vcn");
}

#[test]
fn oci_network_internet_gateway_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/internetGateways HTTP/1.1\r\n"));
        assert!(request.contains("\"vcnId\":\"ocid1.vcn.oc1..vcn\""));
        assert!(request.contains("\"isEnabled\":true"));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_igw")],
            r#"{"id":"ocid1.internetgateway.oc1..igw"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "network", "internet-gateway", "create"])
        .args(["--home"])
        .arg(home.path())
        .args([
            "--config-file",
            config_file.to_str().expect("utf8"),
            "--compartment",
            "ocid1.compartment.oc1..prod",
            "--vcn-id",
            "ocid1.vcn.oc1..vcn",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ocid1.internetgateway.oc1..igw");
}

#[test]
fn oci_network_route_table_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/routeTables HTTP/1.1\r\n"));
        assert!(request.contains("\"networkEntityId\":\"ocid1.internetgateway.oc1..igw\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_rt")],
            r#"{"id":"ocid1.routetable.oc1..rt"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "network", "route-table", "create"])
        .args(["--home"])
        .arg(home.path())
        .args([
            "--config-file",
            config_file.to_str().expect("utf8"),
            "--compartment",
            "ocid1.compartment.oc1..prod",
            "--vcn-id",
            "ocid1.vcn.oc1..vcn",
            "--target",
            "ocid1.internetgateway.oc1..igw",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ocid1.routetable.oc1..rt");
}

#[test]
fn oci_network_security_list_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/securityLists HTTP/1.1\r\n"));
        assert!(request.contains("\"displayName\":\"oracular-sec\""));
        assert!(request.contains("\"min\":7129"));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_sec")],
            r#"{"id":"ocid1.securitylist.oc1..sec"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "network", "security-list", "create"])
        .args(["--home"])
        .arg(home.path())
        .args([
            "--config-file",
            config_file.to_str().expect("utf8"),
            "--compartment",
            "ocid1.compartment.oc1..prod",
            "--vcn-id",
            "ocid1.vcn.oc1..vcn",
            "--ssh-port",
            "7129",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ocid1.securitylist.oc1..sec");
}

#[test]
fn oci_network_subnet_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/subnets HTTP/1.1\r\n"));
        assert!(request.contains("\"routeTableId\":\"ocid1.routetable.oc1..rt\""));
        assert!(request.contains("\"securityListIds\":[\"ocid1.securitylist.oc1..sec\"]"));
        assert!(request.contains("\"dhcpOptionsId\":\"ocid1.dhcpoptions.oc1..dhcp\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_subnet")],
            r#"{"id":"ocid1.subnet.oc1..sub"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "network", "subnet", "create"])
        .args(["--home"])
        .arg(home.path())
        .args([
            "--config-file",
            config_file.to_str().expect("utf8"),
            "--compartment",
            "ocid1.compartment.oc1..prod",
            "--vcn-id",
            "ocid1.vcn.oc1..vcn",
            "--route-table-id",
            "ocid1.routetable.oc1..rt",
            "--security-list-id",
            "ocid1.securitylist.oc1..sec",
            "--dhcp-options-id",
            "ocid1.dhcpoptions.oc1..dhcp",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ocid1.subnet.oc1..sub");
}

#[test]
fn oci_compute_instance_create_json_posts_payload() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("POST /20160918/instances HTTP/1.1\r\n"));
        assert!(request.contains("\"availabilityDomain\":\"AD-1\""));
        assert!(request.contains("\"sourceId\":\"ocid1.image.oc1..img\""));
        assert!(request.contains("\"ssh_authorized_keys\":\"ssh-rsa AAA-test\""));
        assert!(request.contains("\"user_data\":\"dGVzdA==\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_instance")],
            r#"{"id":"ocid1.instance.oc1..inst"}"#,
        )
    });
    let home = tempdir().expect("tempdir");
    let config_file = write_oci_test_config(&home, &server.base_url);

    let output = cargo_bin()
        .args(["orbit", "oci", "compute", "instance", "create"])
        .args(["--home"])
        .arg(home.path())
        .args([
            "--config-file",
            config_file.to_str().expect("utf8"),
            "--compartment",
            "ocid1.compartment.oc1..prod",
            "--ad",
            "AD-1",
            "--subnet-id",
            "ocid1.subnet.oc1..sub",
            "--image-id",
            "ocid1.image.oc1..img",
            "--ssh-public-key",
            "ssh-rsa AAA-test",
            "--user-data-b64",
            "dGVzdA==",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["data"]["id"], "ocid1.instance.oc1..inst");
}

#[test]
fn oci_raw_json_supports_auth_none_and_query_headers() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /20160918/vcns?limit=2 HTTP/1.1\r\n"));
        assert!(request.contains("x-test: alpha") || request.contains("X-Test: alpha"));
        assert!(!request.contains("Signature version=\"1\""));
        http_json_response(
            "200 OK",
            &[("opc-request-id", "req_oci_raw")],
            r#"{"items":[{"id":"ocid1.vcn.oc1..example"}]}"#,
        )
    });

    let output = cargo_bin()
        .args(["orbit", "oci", "raw"])
        .args([
            "--auth",
            "none",
            "--base-url",
            &server.base_url,
            "--path",
            "/20160918/vcns",
            "--param",
            "limit=2",
            "--header",
            "x-test=alpha",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    server.join();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["request_id"], "req_oci_raw");
    assert_eq!(parsed["data"]["items"][0]["id"], "ocid1.vcn.oc1..example");
}

#[test]
fn settings_show_honors_path_overrides() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    fs::write(
        &settings_path,
        r#"
[paths]
root = "~/state/si"
settings_file = "~/config/si/settings.toml"
codex_profiles_dir = "~/state/si/profiles"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["settings", "show", "--format", "json", "--home"])
        .arg(home.path())
        .args(["--settings-file"])
        .arg(&settings_path)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["paths"]["root"], path_string(home.path().join("state/si")));
    assert_eq!(
        parsed["paths"]["settings_file"],
        path_string(home.path().join("config/si/settings.toml"))
    );
    assert_eq!(
        parsed["paths"]["codex_profiles_dir"],
        path_string(home.path().join("state/si/profiles"))
    );
}

#[test]
fn fort_session_state_show_reads_and_normalizes_persisted_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " profile-zeta ",
  "agent_id": " agent-profile-zeta ",
  "session_id": " session-123 ",
  "host": " https://fort.example.test ",
  "runtime_host": " http://fort.internal:8088 ",
  "access_expires_at": " 2030-01-01T00:00:00Z ",
  "refresh_expires_at": " 2030-02-01T00:00:00Z "
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "show", "--path"])
        .arg(&state_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["agent_id"], "agent-profile-zeta");
    assert_eq!(parsed["session_id"], "session-123");
    assert_eq!(parsed["host"], "https://fort.example.test");
    assert_eq!(parsed["runtime_host"], "http://fort.internal:8088");
}

#[test]
fn fort_session_state_classify_reports_refreshing_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "classify", "--path"])
        .arg(&state_path)
        .args(["--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["Refreshing"]["profile_id"], "profile-zeta");
    assert_eq!(parsed["Refreshing"]["agent_id"], "agent-profile-zeta");
    assert_eq!(parsed["Refreshing"]["session_id"], "session-123");
    assert_eq!(parsed["Refreshing"]["access_expires_at_unix"], 90);
    assert_eq!(parsed["Refreshing"]["refresh_expires_at_unix"], 400);
}

#[test]
fn fort_runtime_agent_state_show_reads_and_normalizes_persisted_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " profile-zeta ",
  "pid": 4242,
  "command_path": " /tmp/si ",
  "started_at": " 2030-01-01T00:00:00Z ",
  "updated_at": " 2030-01-01T00:00:01Z "
}
"#,
    )
    .expect("write runtime agent state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod runtime agent state");

    let output = cargo_bin()
        .args(["fort", "runtime", "show", "--path"])
        .arg(&state_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["pid"], 4242);
    assert_eq!(parsed["command_path"], "/tmp/si");
}

#[test]
fn fort_session_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");

    cargo_bin()
        .args([
            "fort",
            "session",
            "write",
            "--path",
        ])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"profile_id":" profile-zeta ","agent_id":" agent-profile-zeta ","session_id":" session-123 ","host":" https://fort.example.test "}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["agent_id"], "agent-profile-zeta");
    assert_eq!(parsed["session_id"], "session-123");
    assert_eq!(parsed["host"], "https://fort.example.test");
}

#[test]
fn fort_runtime_agent_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");

    cargo_bin()
        .args(["fort", "runtime", "write", "--path"])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"profile_id":" profile-zeta ","pid":4242,"command_path":" /tmp/si "}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted runtime state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["pid"], 4242);
    assert_eq!(parsed["command_path"], "/tmp/si");
}

#[test]
fn fort_session_state_clear_removes_persisted_file() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(&state_path, "{}\n").expect("write session state");

    cargo_bin().args(["fort", "session", "clear", "--path"]).arg(&state_path).assert().success();

    assert!(!state_path.exists());
}

#[test]
fn fort_session_state_bootstrap_view_normalizes_fallbacks() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " profile-zeta ",
  "agent_id": "",
  "host": " http://127.0.0.1:8088 "
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "bootstrap", "--path"])
        .arg(&state_path)
        .args([
            "--access-token-path",
            "/tmp/access.token",
            "--refresh-token-path",
            "/tmp/refresh.token",
            "--access-token-runtime-path",
            "/home/si/.si/access.token",
            "--refresh-token-runtime-path",
            "/home/si/.si/refresh.token",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["agent_id"], "si-codex-profile-zeta");
    assert_eq!(parsed["runtime_host_url"], "http://127.0.0.1:8088");
    assert_eq!(parsed["access_token_runtime_path"], "/home/si/.si/access.token");
}

#[test]
fn fort_runtime_agent_state_clear_removes_persisted_file() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");
    fs::write(&state_path, "{}\n").expect("write runtime agent state");

    cargo_bin().args(["fort", "runtime", "clear", "--path"]).arg(&state_path).assert().success();

    assert!(!state_path.exists());
}

#[test]
fn fort_session_state_refresh_outcome_returns_updated_state_and_classification() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "refresh", "--path"])
        .arg(&state_path)
        .args([
            "--outcome",
            "success",
            "--now-unix",
            "100",
            "--access-expires-at-unix",
            "500",
            "--refresh-expires-at-unix",
            "800",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["state"]["access_expires_at"], "1970-01-01T00:08:20Z");
    assert_eq!(parsed["state"]["refresh_expires_at"], "1970-01-01T00:13:20Z");
    assert_eq!(parsed["classification"]["state"], "resumable");
}

#[test]
fn fort_session_state_teardown_reports_closed_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "teardown", "--path"])
        .arg(&state_path)
        .args(["--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["state"], "closed");
}

#[test]
fn fort_session_state_refresh_outcome_unauthorized_clears_session_id() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "refresh", "--path"])
        .arg(&state_path)
        .args(["--outcome", "unauthorized", "--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["state"]["session_id"], "");
    assert_eq!(parsed["classification"]["state"], "revoked");
}

#[test]
fn vault_trust_lookup_reports_matching_entry() {
    let store_dir = tempdir().expect("tempdir");
    let store_path = store_dir.path().join("trust.json");
    fs::write(
        &store_path,
        r#"{
  "schema_version": 3,
  "entries": [
    {
      "repo_root": "/repo",
      "file": "/repo/.env",
      "fingerprint": "deadbeef",
      "trusted_at": "2030-01-01T00:00:00Z"
    }
  ]
}
"#,
    )
    .expect("write trust store");

    let output = cargo_bin()
        .args(["vault", "trust", "lookup", "--path"])
        .arg(&store_path)
        .args([
            "--repo-root",
            "/repo",
            "--file",
            "/repo/.env",
            "--fingerprint",
            "deadbeef",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["found"], true);
    assert_eq!(parsed["matches"], true);
    assert_eq!(parsed["stored_fingerprint"], "deadbeef");
    assert_eq!(parsed["trusted_at"], "2030-01-01T00:00:00Z");
}

#[test]
fn vault_trust_lookup_reports_missing_entry() {
    let store_dir = tempdir().expect("tempdir");
    let store_path = store_dir.path().join("missing.json");

    let output = cargo_bin()
        .args(["vault", "trust", "lookup", "--path"])
        .arg(&store_path)
        .args([
            "--repo-root",
            "/repo",
            "--file",
            "/repo/.env",
            "--fingerprint",
            "deadbeef",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["found"], false);
    assert_eq!(parsed["matches"], false);
}

#[test]
fn vault_check_staged_all_reports_plaintext_env_files() {
    let repo = tempdir().expect("tempdir");
    run_git(repo.path(), &["init"]);
    fs::write(repo.path().join(".env.dev"), "FOO=bar\n").expect("write env");
    run_git(repo.path(), &["add", ".env.dev"]);

    let output = cargo_bin()
        .current_dir(repo.path())
        .args(["vault", "check", "--staged", "--all"])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();

    let stderr = String::from_utf8(output).expect("utf8 stderr");
    assert!(stderr.contains("plaintext values detected"));
    assert!(stderr.contains(".env.dev: FOO"));
}

#[test]
fn vault_hooks_install_status_and_uninstall_manage_pre_commit() {
    let repo = tempdir().expect("tempdir");
    run_git(repo.path(), &["init"]);
    let hook_path = repo.path().join(".git").join("hooks").join("pre-commit");

    cargo_bin().current_dir(repo.path()).args(["vault", "hooks", "install"]).assert().success();

    let hook = fs::read_to_string(&hook_path).expect("read hook");
    assert!(hook.contains("si-vault:hook pre-commit v2"));
    assert!(hook.contains("vault check --staged --all"));

    let status = cargo_bin()
        .current_dir(repo.path())
        .args(["vault", "hooks", "status"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status = String::from_utf8(status).expect("utf8 status");
    assert!(status.contains("pre-commit: installed"));

    cargo_bin().current_dir(repo.path()).args(["vault", "hooks", "uninstall"]).assert().success();
    assert!(!hook_path.exists());
}

#[test]
fn vault_local_keypair_set_get_list_run_and_status_roundtrip() {
    let repo = tempdir().expect("tempdir");
    run_git(repo.path(), &["init"]);
    let keyring_path = repo.path().join("state").join("si-vault-keyring.json");
    let env_path = repo.path().join(".env.dev");

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "keypair", "--env-file", ".env.dev", "--format", "json"])
        .assert()
        .success();

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "set", "--env-file", ".env.dev", "SECRET_TOKEN", "super-secret"])
        .assert()
        .success();

    let raw = fs::read_to_string(&env_path).expect("read env");
    assert!(raw.contains("SI_VAULT_PUBLIC_KEY="));
    assert!(raw.contains("SECRET_TOKEN=encrypted:si-vault:"));

    let list_output = cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "list", "--env-file", ".env.dev", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let list: Value = serde_json::from_slice(&list_output).expect("json list");
    assert_eq!(list[0]["key"], "SECRET_TOKEN");
    assert_eq!(list[0]["state"], "encrypted");

    let get_output = cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "get", "--env-file", ".env.dev", "SECRET_TOKEN", "--reveal"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(String::from_utf8(get_output).expect("utf8 output"), "super-secret\n");

    let run_output = cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args([
            "vault",
            "run",
            "--env-file",
            ".env.dev",
            "--",
            "sh",
            "-lc",
            "printf %s \"$SECRET_TOKEN\"",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(String::from_utf8(run_output).expect("utf8 output"), "super-secret");

    let status_output = cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "status", "--env-file", ".env.dev", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status_output).expect("json status");
    assert_eq!(status["keypair_present"], true);
    assert_eq!(status["public_key_header"], true);
    assert_eq!(status["encrypted_keys"], 1);
}

#[test]
fn vault_encrypt_decrypt_and_restore_round_trip() {
    let repo = tempdir().expect("tempdir");
    run_git(repo.path(), &["init"]);
    let keyring_path = repo.path().join("state").join("si-vault-keyring.json");
    let env_path = repo.path().join(".env.dev");
    fs::write(&env_path, "PLAIN_TOKEN=abc123\nEMPTY_VALUE=\n").expect("write env");

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "encrypt", "--env-file", ".env.dev"])
        .assert()
        .success();

    let encrypted = fs::read_to_string(&env_path).expect("read encrypted env");
    assert!(encrypted.contains("SI_VAULT_PUBLIC_KEY="));
    assert!(encrypted.contains("PLAIN_TOKEN=encrypted:si-vault:"));

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "decrypt", "--env-file", ".env.dev", "--inplace"])
        .assert()
        .success();

    let decrypted = fs::read_to_string(&env_path).expect("read decrypted env");
    assert!(decrypted.contains("PLAIN_TOKEN=abc123"));

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "restore", "--env-file", ".env.dev"])
        .assert()
        .success();

    let restored = fs::read_to_string(&env_path).expect("read restored env");
    assert_eq!(restored, encrypted);
}

#[test]
fn vault_set_accepts_legacy_stdin_env_and_section_flags() {
    let repo = tempdir().expect("tempdir");
    run_git(repo.path(), &["init"]);
    let keyring_path = repo.path().join("state").join("si-vault-keyring.json");
    let env_path = repo.path().join(".env.dev");

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "keypair", "--env", "dev"])
        .assert()
        .success();

    cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .write_stdin("super-secret-from-stdin")
        .args([
            "vault",
            "set",
            "--stdin",
            "--env",
            "dev",
            "--format",
            "--section",
            "default",
            "SECRET_TOKEN",
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&env_path).expect("read env");
    assert!(raw.contains("SI_VAULT_PUBLIC_KEY="));
    assert!(raw.contains("SECRET_TOKEN=encrypted:si-vault:"));
    assert!(!raw.contains("super-secret-from-stdin"));

    let revealed = cargo_bin()
        .current_dir(repo.path())
        .env("SI_VAULT_KEYRING_FILE", &keyring_path)
        .args(["vault", "get", "--env-file", ".env.dev", "SECRET_TOKEN", "--reveal"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(String::from_utf8(revealed).expect("utf8 revealed"), "super-secret-from-stdin\n");
}

#[test]
fn codex_profile_swap_requires_logged_in_profile() {
    let home = tempdir().expect("tempdir");
    write_named_codex_profile_settings(
        home.path(),
        "profile-gamma",
        &[("profile-gamma", "🛰 Profile Gamma", "gamma@example.test")],
    );
    let host_codex_home = home.path().join(".codex");
    fs::create_dir_all(&host_codex_home).expect("mkdir host codex home");
    fs::write(host_codex_home.join("config.toml"), "model = \"gpt-5\"\n").expect("write config");

    let output = cargo_bin()
        .args(["codex", "profile", "swap", "profile-gamma", "--home"])
        .arg(home.path())
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("utf8 stderr");
    assert!(stderr.contains("is not Logged-In"));
    assert_eq!(
        fs::read_to_string(host_codex_home.join("config.toml")).expect("read config"),
        "model = \"gpt-5\"\n"
    );
    assert!(!host_codex_home.join("auth.json").exists());
}

fn path_string(path: impl AsRef<Path>) -> Value {
    Value::String(path.as_ref().display().to_string())
}

struct TestHttpServer {
    base_url: String,
    handle: Option<thread::JoinHandle<()>>,
}

impl TestHttpServer {
    fn join(mut self) {
        if let Some(handle) = self.handle.take() {
            handle.join().expect("server thread should join");
        }
    }
}

fn start_one_shot_http_server<F>(handler: F) -> TestHttpServer
where
    F: FnOnce(String) -> String + Send + 'static,
{
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let addr = listener.local_addr().expect("local addr");
    let handle = thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut request = Vec::new();
        let mut buffer = [0_u8; 4096];
        loop {
            let read = stream.read(&mut buffer).expect("read request");
            if read == 0 {
                break;
            }
            request.extend_from_slice(&buffer[..read]);
            if request.windows(4).any(|window| window == b"\r\n\r\n") {
                break;
            }
        }
        let request = String::from_utf8(request).expect("request utf8");
        let response = handler(request);
        stream.write_all(response.as_bytes()).expect("write response");
        stream.flush().expect("flush response");
    });
    TestHttpServer { base_url: format!("http://{addr}"), handle: Some(handle) }
}

fn start_http_server<F>(requests: usize, handler: F) -> TestHttpServer
where
    F: Fn(String) -> String + Send + Sync + 'static,
{
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let addr = listener.local_addr().expect("local addr");
    let handler = std::sync::Arc::new(handler);
    let handle = thread::spawn(move || {
        for _ in 0..requests {
            let (mut stream, _) = listener.accept().expect("accept");
            let mut request = Vec::new();
            let mut buffer = [0_u8; 4096];
            loop {
                let read = stream.read(&mut buffer).expect("read request");
                if read == 0 {
                    break;
                }
                request.extend_from_slice(&buffer[..read]);
                if request.windows(4).any(|window| window == b"\r\n\r\n") {
                    break;
                }
            }
            let request = String::from_utf8(request).expect("request utf8");
            let response = handler(request);
            stream.write_all(response.as_bytes()).expect("write response");
            stream.flush().expect("flush response");
        }
    });
    TestHttpServer { base_url: format!("http://{addr}"), handle: Some(handle) }
}

fn start_http_server_with_body<F>(requests: usize, handler: F) -> TestHttpServer
where
    F: Fn(String) -> String + Send + Sync + 'static,
{
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let addr = listener.local_addr().expect("local addr");
    let handler = std::sync::Arc::new(handler);
    let handle = thread::spawn(move || {
        for _ in 0..requests {
            let (mut stream, _) = listener.accept().expect("accept");
            let mut request = Vec::new();
            let mut buffer = [0_u8; 4096];
            let mut content_length = 0_usize;
            let mut header_end = None;
            loop {
                let read = stream.read(&mut buffer).expect("read request");
                if read == 0 {
                    break;
                }
                request.extend_from_slice(&buffer[..read]);
                if header_end.is_none()
                    && let Some(pos) = request.windows(4).position(|window| window == b"\r\n\r\n")
                {
                    header_end = Some(pos + 4);
                    let headers = String::from_utf8_lossy(&request[..pos + 4]).to_ascii_lowercase();
                    for line in headers.lines() {
                        if let Some(value) = line.strip_prefix("content-length:") {
                            content_length = value.trim().parse::<usize>().unwrap_or(0);
                            break;
                        }
                    }
                }
                if let Some(end) = header_end
                    && request.len() >= end + content_length
                {
                    break;
                }
            }
            let request = String::from_utf8(request).expect("request utf8");
            let response = handler(request);
            stream.write_all(response.as_bytes()).expect("write response");
            stream.flush().expect("flush response");
        }
    });
    TestHttpServer { base_url: format!("http://{addr}"), handle: Some(handle) }
}

fn http_json_response(status: &str, headers: &[(&str, &str)], body: &str) -> String {
    let mut response = format!(
        "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n",
        body.len()
    );
    for (key, value) in headers {
        response.push_str(&format!("{key}: {value}\r\n"));
    }
    response.push_str("\r\n");
    response.push_str(body);
    response
}

fn http_xml_response(status: &str, headers: &[(&str, &str)], body: &str) -> String {
    let mut response = format!(
        "HTTP/1.1 {status}\r\nContent-Type: application/xml\r\nContent-Length: {}\r\nConnection: close\r\n",
        body.len()
    );
    for (key, value) in headers {
        response.push_str(&format!("{key}: {value}\r\n"));
    }
    response.push_str("\r\n");
    response.push_str(body);
    response
}

#[test]
fn github_git_setup_json_dry_run_normalizes_remotes_and_helper() {
    let home = tempdir().expect("tempdir");
    let root = tempdir().expect("tempdir");
    let repo = root.path().join("demo");

    fs::create_dir_all(&repo).expect("create repo");
    run_git(&repo, &["init"]);
    run_git(&repo, &["remote", "add", "origin", "git@github.com:Aureuma/demo.git"]);

    let output = cargo_bin()
        .env("HOME", home.path())
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "orbit",
            "github",
            "git",
            "setup",
            "--root",
            root.path().to_str().expect("root str"),
            "--account",
            "core",
            "--owner",
            "Aureuma",
            "--auth-mode",
            "oauth",
            "--dry-run",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["repos_scanned"], 1);
    assert_eq!(parsed["repos_updated"], 1);
    assert_eq!(parsed["hosts"][0], "github.com");
    assert_eq!(
        parsed["helper_command"],
        "!si vault run -- si orbit github git credential --account core --auth-mode oauth"
    );
    assert_eq!(parsed["changes"][0]["after"], "https://github.com/Aureuma/demo.git");
}

#[test]
fn github_git_remote_auth_json_dry_run_reads_pat_from_env() {
    let home = tempdir().expect("tempdir");
    let root = tempdir().expect("tempdir");
    let repo = root.path().join("demo");

    fs::create_dir_all(&repo).expect("create repo");
    run_git(&repo, &["init"]);
    run_git(&repo, &["config", "user.email", "test@example.com"]);
    run_git(&repo, &["config", "user.name", "test"]);
    run_git(&repo, &["remote", "add", "origin", "https://github.com/Aureuma/demo.git"]);
    fs::write(repo.join("README.md"), "demo\n").expect("write file");
    run_git(&repo, &["add", "README.md"]);
    run_git(&repo, &["commit", "-m", "init"]);

    let output = cargo_bin()
        .env("HOME", home.path())
        .env("GH_PAT", "github_pat_example123")
        .args([
            "orbit",
            "github",
            "git",
            "remote-auth",
            "--root",
            root.path().to_str().expect("root str"),
            "--vault-key",
            "GH_PAT",
            "--dry-run",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["repos_scanned"], 1);
    assert_eq!(parsed["repos_updated"], 1);
    assert_eq!(parsed["repos_errored"], 0);
    assert_eq!(parsed["changes"][0]["tracking"], "would-set");
    let after = parsed["changes"][0]["after"].as_str().expect("after string");
    assert!(!after.contains("github_pat_example123"));
    assert!(after.contains("github.com/Aureuma/demo.git"));
}

#[test]
fn github_git_clone_auth_json_dry_run_reads_pat_from_env() {
    let home = tempdir().expect("tempdir");
    let root = tempdir().expect("tempdir");

    let output = cargo_bin()
        .env("HOME", home.path())
        .env("GH_PAT", "github_pat_example123")
        .args([
            "orbit",
            "github",
            "git",
            "clone-auth",
            "Aureuma/demo",
            "--root",
            root.path().to_str().expect("root str"),
            "--vault-key",
            "GH_PAT",
            "--dry-run",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["name"], "demo");
    assert_eq!(parsed["destination"], root.path().join("demo").to_str().expect("destination str"));
    let clone_url = parsed["clone_url"].as_str().expect("clone url string");
    assert!(!clone_url.contains("github_pat_example123"));
    assert!(clone_url.contains("github.com/Aureuma/demo.git"));
}

fn run_git(repo: &Path, args: &[&str]) -> String {
    let output =
        std::process::Command::new("git").arg("-C").arg(repo).args(args).output().expect("run git");
    if !output.status.success() {
        panic!(
            "git {} failed: {}{}",
            args.join(" "),
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
    }
    String::from_utf8_lossy(&output.stdout).to_string()
}
