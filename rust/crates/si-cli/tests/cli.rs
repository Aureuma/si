use assert_cmd::Command;
use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::Utc;
use reqwest::blocking::Client as BlockingClient;
use serde_json::{Value, json};
use si_fort::{SessionState, classify_persisted_session_state, load_persisted_session_state};
use si_nucleus::{NucleusConfig, NucleusService};
use si_nucleus_core::{
    CanonicalEventSource, CanonicalEventType, EventDataEnvelope, RunStatus, WorkerId, WorkerStatus,
};
use si_nucleus_runtime::{
    CanonicalEventDraft, NucleusRuntime, RunTurnSpec, RuntimeCommand, RuntimeRunOutcome,
    RuntimeStatusSnapshot, SessionOpenResult, SessionOpenSpec, WorkerLaunchSpec, WorkerProbeResult,
    WorkerRuntimeView, WorkerStartResult,
};
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
    Command::cargo_bin("si").expect("si binary should build")
}

#[allow(clippy::result_large_err)]
fn accept_test_ws_response(
    response: WsResponse,
) -> Result<WsResponse, HttpResponse<Option<String>>> {
    Ok(response)
}

fn write_named_codex_profile_settings(
    home: &Path,
    active_profile: &str,
    profiles: &[(&str, &str, &str)],
) {
    let settings_dir = home.join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let mut source = String::from("schema_version = 1\n[codex]\n");
    source.push_str(&format!("profile = {active_profile:?}\n\n"));
    source.push_str("[codex.profiles]\n");
    source.push_str(&format!("active = {active_profile:?}\n\n"));
    for (profile, name, email) in profiles {
        source.push_str(&format!("[codex.profiles.entries.{profile}]\n"));
        source.push_str(&format!("name = \"{name}\"\n"));
        source.push_str(&format!("email = \"{email}\"\n"));
        source.push_str(&format!(
            "auth_path = {:?}\n\n",
            home.join(".si").join("codex").join("profiles").join(profile).join("auth.json")
        ));
    }
    fs::write(settings_dir.join("settings.toml"), source).expect("write named codex settings");
}

fn fake_jwt(payload: Value) -> String {
    format!(
        "header.{}.signature",
        URL_SAFE_NO_PAD.encode(serde_json::to_vec(&payload).expect("serialize jwt payload"))
    )
}

fn write_codex_auth_file(path: &Path, email: &str) {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).expect("mkdir auth dir");
    }
    let access_token = fake_jwt(json!({
        "https://api.openai.com/profile": {
            "email": email,
        }
    }));
    let id_token = fake_jwt(json!({
        "email": email,
    }));
    fs::write(
        path,
        serde_json::to_vec_pretty(&json!({
            "tokens": {
                "access_token": access_token,
                "id_token": id_token,
            }
        }))
        .expect("serialize auth json"),
    )
    .expect("write auth json");
}

fn write_reusable_codex_fort_session(codex_home: &Path, profile_id: &str) {
    let fort_dir = codex_home.join("fort");
    fs::create_dir_all(&fort_dir).expect("mkdir fort session dir");
    let access_token_path = fort_dir.join("access.token");
    let refresh_token_path = fort_dir.join("refresh.token");
    fs::write(&access_token_path, "access-token\n").expect("write fort access token");
    fs::write(&refresh_token_path, "refresh-token\n").expect("write fort refresh token");
    let access_expires_at = (Utc::now() + chrono::Duration::hours(1)).to_rfc3339();
    let refresh_expires_at = (Utc::now() + chrono::Duration::days(30)).to_rfc3339();
    let session_path = fort_dir.join("session.json");
    fs::write(
        &session_path,
        serde_json::to_vec_pretty(&json!({
            "profile_id": profile_id,
            "agent_id": format!("si-codex-{profile_id}"),
            "session_id": format!("fort-session-{profile_id}"),
            "host": "https://fort.example.test",
            "runtime_host": "https://fort.example.test",
            "access_token_path": access_token_path,
            "refresh_token_path": refresh_token_path,
            "access_expires_at": access_expires_at,
            "refresh_expires_at": refresh_expires_at,
            "updated_at": Utc::now().to_rfc3339(),
        }))
        .expect("serialize fort session"),
    )
    .expect("write fort session state");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&session_path, &access_token_path, &refresh_token_path] {
            fs::set_permissions(path, fs::Permissions::from_mode(0o600))
                .expect("chmod fort session file");
        }
    }
}

fn write_codex_worker_state_for_test(
    home: &Path,
    profile_id: &str,
    worker_slot: &str,
    session_name: &str,
    workspace: &Path,
    workdir: &Path,
) {
    let path = home
        .join(".si")
        .join("codex")
        .join("workers")
        .join(profile_id)
        .join(format!("{worker_slot}.json"));
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).expect("mkdir worker state dir");
    }
    fs::write(
        &path,
        serde_json::to_vec_pretty(&json!({
            "schema_version": 1,
            "profile_id": profile_id,
            "worker_slot": worker_slot,
            "profile_name": "America",
            "session_name": session_name,
            "workspace": workspace.display().to_string(),
            "workdir": workdir.display().to_string(),
            "updated_at": Utc::now().to_rfc3339(),
        }))
        .expect("serialize worker state"),
    )
    .expect("write worker state");
}

fn write_workspace_manifest(repo: &Path, version: &str) {
    fs::create_dir_all(repo.join("rust/crates/si-cli")).expect("mkdir cli crate");
    fs::write(
        repo.join("Cargo.toml"),
        format!(
            "[workspace]\nmembers = [\"rust/crates/si-cli\"]\nresolver = \"2\"\n\n[workspace.package]\nversion = \"{}\"\nedition = \"2024\"\nlicense = \"AGPL-3.0-only\"\nrepository = \"https://example.invalid/si\"\nrust-version = \"1.94\"\n",
            version.trim_start_matches('v')
        ),
    )
    .expect("write Cargo.toml");
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
        let service_auth_token = auth_token.clone();
        let runtime = runtime.clone();
        thread::spawn(move || {
            let tokio_runtime = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .expect("tokio runtime");
            tokio_runtime.block_on(async move {
                let config =
                    NucleusConfig { bind_addr: addr, state_dir, auth_token: service_auth_token };
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
            let mut request = client.get(format!("{base_url}/status"));
            if let Some(token) = auth_token.as_deref() {
                request = request.bearer_auth(token);
            }
            if let Ok(response) = request.send()
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
            .map(|item| match item {
                si_nucleus_runtime::RunInputItem::Text { text } => text.as_str(),
            })
            .next()
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
    wait_for_cli_task_status_with_token(ws_url, task_id, expected, None)
}

fn wait_for_cli_task_status_with_token(
    ws_url: &str,
    task_id: &str,
    expected: &str,
    auth_token: Option<&str>,
) -> Value {
    let start = Instant::now();
    let mut last_task = Value::Null;
    while start.elapsed() < Duration::from_secs(5) {
        let mut command = cargo_bin();
        if let Some(token) = auth_token {
            command.env("SI_NUCLEUS_AUTH_TOKEN", token);
        }
        let output = command
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
        last_task = task;
        thread::sleep(Duration::from_millis(50));
    }
    panic!("task {task_id} did not reach status {expected}; last observed task: {last_task}");
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
    inspect_run_via_cli_with_token(ws_url, run_id, None)
}

fn inspect_run_via_cli_with_token(ws_url: &str, run_id: &str, auth_token: Option<&str>) -> Value {
    let mut command = cargo_bin();
    if let Some(token) = auth_token {
        command.env("SI_NUCLEUS_AUTH_TOKEN", token);
    }
    let output = command
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
    write_reusable_codex_fort_session(codex_home, profile);
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

fn write_live_profile_record(state_root: &Path, profile: &str, codex_home: &Path) {
    let path = state_root.join("state").join("profiles").join(format!("{profile}.json"));
    fs::create_dir_all(path.parent().expect("profile parent")).expect("mkdir profile dir");
    fs::write(
        path,
        serde_json::to_vec_pretty(&json!({
            "profile": profile,
            "account_identity": format!("{profile}@example.com"),
            "codex_home": codex_home.display().to_string(),
            "auth_mode": null,
            "preferred_model": "gpt-5.4",
            "runtime_defaults": {},
        }))
        .expect("serialize profile record"),
    )
    .expect("write profile record");
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

fn write_fort_session_state_via_cli(codex_home: &Path, profile: &str) {
    let fort_dir = codex_home.join("fort");
    let session_path = fort_dir.join("session.json");
    cargo_bin()
        .args(["fort", "session", "write", "--path"])
        .arg(&session_path)
        .args([
            "--state-json",
            &json!({
                "profile_id": profile,
                "agent_id": format!("si-codex-{profile}"),
                "session_id": "fort-session",
                "host": "https://fort.example.invalid",
                "runtime_host": "https://fort-runtime.example.invalid",
                "access_expires_at": (Utc::now() + chrono::Duration::hours(1)).to_rfc3339(),
                "refresh_expires_at": (Utc::now() + chrono::Duration::hours(12)).to_rfc3339(),
            })
            .to_string(),
        ])
        .assert()
        .success();
    fs::write(fort_dir.join("access.token"), "access-token\n").expect("write fort access token");
    fs::write(fort_dir.join("refresh.token"), "refresh-token\n").expect("write fort refresh token");
}

fn clear_fort_session_state_via_cli(codex_home: &Path) {
    let session_path = codex_home.join("fort").join("session.json");
    cargo_bin().args(["fort", "session", "clear", "--path"]).arg(&session_path).assert().success();
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
    let mut last_error = None;
    for _ in 0..5 {
        let raw = fs::read_to_string(&path).expect("read events jsonl");
        let mut values = Vec::new();
        let mut parse_failed = false;
        for line in raw.lines().filter(|line| !line.trim().is_empty()) {
            match serde_json::from_str::<Value>(line) {
                Ok(value) => values.push(value),
                Err(error) => {
                    last_error = Some(format!("{error}: {line}"));
                    parse_failed = true;
                    break;
                }
            }
        }
        if !parse_failed {
            return values;
        }
        thread::sleep(Duration::from_millis(20));
    }
    panic!(
        "parse event line: {}",
        last_error.unwrap_or_else(|| "unknown parse failure".to_owned())
    );
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
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'SI_SURF_WRAPPED=%s\\nSURF_VNC_PASSWORD=%s\\n' \"${{SI_SURF_WRAPPED:-}}\" \"${{SURF_VNC_PASSWORD:-}}\" > {}\n",
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
    assert_eq!(env, "SI_SURF_WRAPPED=1\nSURF_VNC_PASSWORD=\n");
}

#[test]
fn surf_wrapper_trace_prints_resolved_binary_without_args() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    let bin_dir = tempdir().expect("bin tempdir");
    let surf_path = bin_dir.path().join("surf");
    write_executable_shell_script(&surf_path, "#!/bin/sh\nexit 0\n");

    let output = cargo_bin()
        .args([
            "surf",
            "--home",
            home.path().to_str().expect("home path"),
            "--bin",
            surf_path.to_str().expect("surf path"),
            "status",
            "--json",
        ])
        .env("SI_WRAPPER_TRACE", "1")
        .assert()
        .success()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("utf8 stderr");

    assert!(stderr.contains("si wrapper trace: surf binary="));
    assert!(stderr.contains(surf_path.to_str().expect("surf path")));
    assert!(!stderr.contains("--json"));
}

#[test]
fn surf_wrapper_config_round_trip_does_not_call_native_surf() {
    let home = tempdir().expect("home tempdir");
    cargo_bin()
        .args([
            "surf",
            "--home",
            home.path().to_str().expect("home path"),
            "wrapper",
            "config",
            "set",
            "--repo",
            "/tmp/surf",
            "--build",
            "true",
        ])
        .assert()
        .success();

    let output = cargo_bin()
        .args([
            "surf",
            "--home",
            home.path().to_str().expect("home path"),
            "wrapper",
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
    let payload: Value = serde_json::from_slice(&output).expect("parse surf wrapper config");

    assert_eq!(payload.get("repo").and_then(Value::as_str), Some("/tmp/surf"));
    assert_eq!(payload.get("build").and_then(Value::as_bool), Some(true));
    assert!(home.path().join(".si/surf/si.settings.toml").is_file());
}

#[test]
fn surf_native_config_remains_passthrough() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("surf-args.txt");
    let surf_path = bin_dir.path().join("surf");
    write_executable_shell_script(
        &surf_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file),),
    );

    cargo_bin()
        .args([
            "surf",
            "--home",
            home.path().to_str().expect("home path"),
            "--bin",
            surf_path.to_str().expect("surf path"),
            "config",
            "show",
            "--json",
        ])
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read surf args");
    assert_eq!(args, "config\nshow\n--json\n");
}

#[test]
fn surf_wrapper_fetches_vnc_password_from_fort_for_start() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let profile_home = home.path().join(".si/codex/profiles/profile-surf");
    write_reusable_codex_fort_session(&profile_home, "profile-surf");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("surf-args.txt");
    let env_file = bin_dir.path().join("surf-env.txt");
    let fort_args_file = bin_dir.path().join("fort-args.txt");
    let surf_path = bin_dir.path().join("surf");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &surf_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'SI_SURF_WRAPPED=%s\\nSURF_VNC_PASSWORD=%s\\n' \"${{SI_SURF_WRAPPED:-}}\" \"${{SURF_VNC_PASSWORD:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );
    write_executable_shell_script(
        &fort_path,
        &format!(
            r#"#!/bin/sh
set -eu
printf '%s\n' "$@" > {}
if [ "$1" = "--host" ] && [ "$2" = "https://fort.example.test" ] && [ "$3" = "--json" ] && [ "$4" = "auth" ] && [ "$5" = "session" ] && [ "$6" = "refresh" ]; then
  refresh_out="${{10}}"
  printf '%s\n' 'rotated-refresh-token' > "$refresh_out"
  printf '%s\n' '{{"access_token":"rotated-access-token","refresh_token_file":"'"$refresh_out"'"}}'
  exit 0
fi
if [ "$1" = "--host" ] && [ "$2" = "https://fort.example.test" ] && [ "$3" = "--token-file" ] && [ "$5" = "get" ] && [ "$6" = "--repo" ] && [ "$7" = "surf" ] && [ "$8" = "--env" ] && [ "$9" = "dev" ] && [ "${{10}}" = "--key" ] && [ "${{11}}" = "SURF_VNC_PASSWORD" ]; then
  test "$(cat "$4")" = 'rotated-access-token'
  printf '%s\n' 'stable-surf-password'
  exit 0
fi
printf 'unexpected fort invocation\n' >&2
exit 1
"#,
            shell_escape_for_test(&fort_args_file),
        ),
    );
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[fort]\nbin = {fort_path:?}\nhost = \"https://fort.example.test\"\n[surf]\nvnc_password_fort_key = \"SURF_VNC_PASSWORD\"\nvnc_password_fort_repo = \"surf\"\nvnc_password_fort_env = \"dev\"\n",
        ),
    )
    .expect("write settings");

    cargo_bin()
        .args([
            "surf",
            "--home",
            home.path().to_str().expect("home path"),
            "--bin",
            surf_path.to_str().expect("surf path"),
            "start",
            "--json",
        ])
        .env("CODEX_HOME", &profile_home)
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read surf args");
    assert_eq!(args, "start\n--json\n");
    let env = fs::read_to_string(&env_file).expect("read surf env");
    assert_eq!(env, "SI_SURF_WRAPPED=1\nSURF_VNC_PASSWORD=stable-surf-password\n");
    let fort_args = fs::read_to_string(&fort_args_file).expect("read fort args");
    assert!(
        fort_args
            .contains("get\n--repo\nsurf\n--env\ndev\n--key\nSURF_VNC_PASSWORD\n--format\nraw\n")
    );
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
fn distribution_doctor_outputs_json() {
    let output = cargo_bin()
        .args(["doctor", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse doctor json");
    assert!(payload.get("version").and_then(Value::as_str).is_some());
    assert!(payload.get("binary").and_then(Value::as_str).is_some());
    assert!(payload.get("checks").and_then(Value::as_array).is_some());
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
        "#!/bin/sh\ntarget=\"\"\nprev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--target\" ]; then target=\"$arg\"; fi\n  prev=\"$arg\"\ndone\nout=\"$CARGO_TARGET_DIR/release\"\nif [ -n \"$target\" ]; then out=\"$CARGO_TARGET_DIR/$target/release\"; fi\nmkdir -p \"$out\"\nprintf '#!/bin/sh\\necho si\\n' > \"$out/si\"\nchmod 755 \"$out/si\"\n",
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
        "si_1.2.3_darwin_amd64.tar.gz",
        "si_1.2.3_darwin_arm64.tar.gz",
    ] {
        assert!(out_dir.join(name).exists(), "missing archive {name}");
    }
    let checksums = fs::read_to_string(out_dir.join("checksums.txt")).expect("read checksums");
    assert!(checksums.contains("si_1.2.3_linux_amd64.tar.gz"));
    assert_eq!(checksums.lines().count(), 4);
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
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho built\\n' > \"$CARGO_TARGET_DIR/release/si\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si\"\n",
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
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho upgraded\\n' > \"$CARGO_TARGET_DIR/release/si\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si\"\n",
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
        "#!/bin/sh\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho flagfirst\\n' > \"$CARGO_TARGET_DIR/release/si\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si\"\n",
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
    assert!(args.contains("si"));
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
        repo.path().join("npm/si/package.template.json"),
        "{\n  \"name\": \"@aureuma/si\"\n}\n",
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
        repo.path().join("npm/si/package.template.json"),
        "{\n  \"name\": \"@aureuma/si\"\n}\n",
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
        "log stream --style compact --predicate 'process == \"si-nucleus\" || process == \"si\"'"
    );
    assert!(payload["manager_command"].is_null());

    let plist = fs::read_to_string(&definition_path).expect("read plist");
    assert!(plist.contains("<string>nucleus</string>"));
    assert!(plist.contains("<string>service</string>"));
    assert!(plist.contains("<string>run</string>"));
    assert!(plist.contains(state_dir.to_str().expect("state dir")));
    assert!(!plist.contains("SI_NUCLEUS_AUTH_TOKEN"));
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
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'SI_NUCLEUS_STATE_DIR=%s\\nSI_NUCLEUS_BIND_ADDR=%s\\nSI_NUCLEUS_AUTH_TOKEN=%s\\n' \"${{SI_NUCLEUS_STATE_DIR:-}}\" \"${{SI_NUCLEUS_BIND_ADDR:-}}\" \"${{SI_NUCLEUS_AUTH_TOKEN:-}}\" > {}\n",
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
    assert!(env.contains("SI_NUCLEUS_AUTH_TOKEN=\n"));
}

#[test]
fn nucleus_service_run_prefers_si_nucleus_bin_env_override() {
    let temp = tempdir().expect("tempdir");
    let marker_file = temp.path().join("marker.txt");
    let env_file = temp.path().join("env.txt");
    let nucleus_path = temp.path().join("si-nucleus-custom");
    write_executable_shell_script(
        &nucleus_path,
        &format!(
            "#!/bin/sh\nprintf 'custom\\n' > {}\nprintf 'SI_NUCLEUS_STATE_DIR=%s\\nSI_NUCLEUS_BIND_ADDR=%s\\n' \"${{SI_NUCLEUS_STATE_DIR:-}}\" \"${{SI_NUCLEUS_BIND_ADDR:-}}\" > {}\n",
            shell_escape_for_test(&marker_file),
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
            "0.0.0.0:4747",
        ])
        .env("SI_NUCLEUS_BIN", nucleus_path.to_str().expect("nucleus path"))
        .assert()
        .success();

    assert_eq!(fs::read_to_string(&marker_file).expect("read marker"), "custom\n");
    let env = fs::read_to_string(&env_file).expect("read env");
    assert!(env.contains(&format!("SI_NUCLEUS_STATE_DIR={}\n", state_dir.display())));
    assert!(env.contains("SI_NUCLEUS_BIND_ADDR=0.0.0.0:4747\n"));
}

#[test]
fn nucleus_service_install_persists_auth_token_in_definitions() {
    let temp = tempdir().expect("tempdir");
    let state_dir = temp.path().join("state");
    let service_dir = temp.path().join("service-dir");
    let nucleus_bin = temp.path().join("si-nucleus-public");
    fs::write(&nucleus_bin, "").expect("write nucleus bin placeholder");

    let systemd_output = cargo_bin()
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
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", "/bin/true")
        .env("SI_NUCLEUS_AUTH_TOKEN", "secret-token")
        .env("SI_NUCLEUS_BIN", nucleus_bin.to_str().expect("nucleus bin"))
        .env("SI_NUCLEUS_PUBLIC_URL", "https://nucleus.example.test")
        .env("PATH", "/usr/local/bin:/usr/bin:/bin")
        .assert()
        .success();
    let _systemd_payload: Value =
        serde_json::from_slice(&systemd_output.get_output().stdout).expect("parse systemd json");
    let systemd_unit =
        fs::read_to_string(service_dir.join("si-nucleus.service")).expect("read systemd unit");
    assert!(systemd_unit.contains("Environment=\"PATH=/usr/local/bin:/usr/bin:/bin\""));
    assert!(
        systemd_unit
            .contains(&format!("Environment=\"SI_NUCLEUS_STATE_DIR={}\"", state_dir.display()))
    );
    assert!(systemd_unit.contains("Environment=\"SI_NUCLEUS_BIND_ADDR=127.0.0.1:4747\""));
    assert!(
        systemd_unit.contains(&format!("Environment=\"SI_NUCLEUS_BIN={}\"", nucleus_bin.display()))
    );
    assert!(systemd_unit.contains("Environment=\"SI_NUCLEUS_AUTH_TOKEN=secret-token\""));
    assert!(
        systemd_unit.contains("Environment=\"SI_NUCLEUS_PUBLIC_URL=https://nucleus.example.test\"")
    );

    let launchd_dir = temp.path().join("launch-agents");
    let launchd_output = cargo_bin()
        .args([
            "nucleus",
            "service",
            "install",
            "--state-dir",
            state_dir.to_str().expect("state dir"),
            "--service-dir",
            launchd_dir.to_str().expect("launchd dir"),
            "--format",
            "json",
        ])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "launchd-agent")
        .env("SI_NUCLEUS_AUTH_TOKEN", "secret-token")
        .env("SI_NUCLEUS_BIN", nucleus_bin.to_str().expect("nucleus bin"))
        .env("SI_NUCLEUS_PUBLIC_URL", "https://nucleus.example.test")
        .env("PATH", "/usr/local/bin:/usr/bin:/bin")
        .assert()
        .success();
    let _launchd_payload: Value =
        serde_json::from_slice(&launchd_output.get_output().stdout).expect("parse launchd json");
    let plist = fs::read_to_string(launchd_dir.join("com.aureuma.si.nucleus.plist"))
        .expect("read launchd plist");
    assert!(plist.contains("<key>PATH</key>"));
    assert!(plist.contains("<string>/usr/local/bin:/usr/bin:/bin</string>"));
    assert!(plist.contains("<key>SI_NUCLEUS_STATE_DIR</key>"));
    assert!(plist.contains(&format!("<string>{}</string>", state_dir.display())));
    assert!(plist.contains("<key>SI_NUCLEUS_BIND_ADDR</key>"));
    assert!(plist.contains("<string>127.0.0.1:4747</string>"));
    assert!(plist.contains("<key>SI_NUCLEUS_BIN</key>"));
    assert!(plist.contains(&format!("<string>{}</string>", nucleus_bin.display())));
    assert!(plist.contains("<key>SI_NUCLEUS_AUTH_TOKEN</key>"));
    assert!(plist.contains("<string>secret-token</string>"));
    assert!(plist.contains("<key>SI_NUCLEUS_PUBLIC_URL</key>"));
    assert!(plist.contains("<string>https://nucleus.example.test</string>"));
}

#[test]
fn nucleus_service_install_uses_state_and_bind_env_defaults() {
    let temp = tempdir().expect("tempdir");
    let state_dir = temp.path().join("custom-state");
    let service_dir = temp.path().join("service-dir");
    let systemctl_path = temp.path().join("systemctl");
    write_executable_shell_script(&systemctl_path, "#!/bin/sh\nexit 0\n");

    let output = cargo_bin()
        .args([
            "nucleus",
            "service",
            "install",
            "--service-dir",
            service_dir.to_str().expect("service dir"),
            "--format",
            "json",
        ])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .env("SI_NUCLEUS_STATE_DIR", state_dir.to_str().expect("state dir"))
        .env("SI_NUCLEUS_BIND_ADDR", "0.0.0.0:4747")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let payload: Value = serde_json::from_slice(&output).expect("parse json");
    assert_eq!(payload["state_dir"], state_dir.display().to_string());
    assert_eq!(payload["bind_addr"], "0.0.0.0:4747");

    let systemd_unit =
        fs::read_to_string(service_dir.join("si-nucleus.service")).expect("read systemd unit");
    assert!(
        systemd_unit
            .contains(&format!("Environment=\"SI_NUCLEUS_STATE_DIR={}\"", state_dir.display()))
    );
    assert!(systemd_unit.contains("Environment=\"SI_NUCLEUS_BIND_ADDR=0.0.0.0:4747\""));
    assert!(systemd_unit.contains(&format!("\"{}\"", state_dir.display())));
    assert!(systemd_unit.contains("\"0.0.0.0:4747\""));
}

#[test]
fn nucleus_service_install_sanitizes_transient_path_entries() {
    let temp = tempdir().expect("tempdir");
    let service_dir = temp.path().join("service-dir");
    let systemctl_path = temp.path().join("systemctl");
    let unstable_dir = temp.path().join("tmp").join("arg0").join("codex-arg0demo");
    let stable_dir = temp.path().join("stable-bin");
    fs::create_dir_all(&unstable_dir).expect("mkdir unstable dir");
    fs::create_dir_all(&stable_dir).expect("mkdir stable dir");
    write_executable_shell_script(&systemctl_path, "#!/bin/sh\nexit 0\n");
    let path_value =
        format!("{}:{}:{}", unstable_dir.display(), stable_dir.display(), stable_dir.display());

    cargo_bin()
        .args([
            "nucleus",
            "service",
            "install",
            "--service-dir",
            service_dir.to_str().expect("service dir"),
            "--format",
            "json",
        ])
        .env("SI_NUCLEUS_SERVICE_PLATFORM", "systemd-user")
        .env("SI_NUCLEUS_SYSTEMCTL_EXEC", systemctl_path.to_str().expect("systemctl path"))
        .env("PATH", path_value)
        .assert()
        .success();

    let systemd_unit =
        fs::read_to_string(service_dir.join("si-nucleus.service")).expect("read systemd unit");
    assert!(systemd_unit.contains(&format!("Environment=\"PATH={}\"", stable_dir.display())));
    assert!(!systemd_unit.contains(&unstable_dir.display().to_string()));
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
    let temp = tempdir().expect("tempdir");
    let home_dir = temp.path().join("si-home");
    let codex_home = temp.path().join("si-codex");
    let workdir = temp.path().join("si-work");
    fs::create_dir_all(&workdir).expect("mkdir workdir");
    write_reusable_codex_fort_session(&codex_home, "america");
    let expected_home_dir = home_dir.display().to_string();
    let expected_codex_home = codex_home.display().to_string();
    let expected_workdir = workdir.display().to_string();
    let expected_access_token = codex_home.join("fort/access.token").display().to_string();
    let expected_refresh_token = codex_home.join("fort/refresh.token").display().to_string();
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
        assert_eq!(payload["params"]["home_dir"], expected_home_dir);
        assert_eq!(payload["params"]["codex_home"], expected_codex_home);
        assert_eq!(payload["params"]["workdir"], expected_workdir);
        assert_eq!(payload["params"]["env"]["FOO"], "bar");
        assert_eq!(payload["params"]["env"]["FORT_TOKEN_PATH"], expected_access_token);
        assert_eq!(payload["params"]["env"]["FORT_REFRESH_TOKEN_PATH"], expected_refresh_token);
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
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            workdir.to_str().expect("workdir"),
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
    let temp = tempdir().expect("tempdir");
    let home_dir = temp.path().join("si-home");
    let codex_home = temp.path().join("si-codex");
    let workdir = temp.path().join("si-work");
    fs::create_dir_all(&workdir).expect("mkdir workdir");
    write_reusable_codex_fort_session(&codex_home, "america");
    let expected_home_dir = home_dir.display().to_string();
    let expected_codex_home = codex_home.display().to_string();
    let expected_workdir = workdir.display().to_string();
    let expected_access_token = codex_home.join("fort/access.token").display().to_string();
    let expected_refresh_token = codex_home.join("fort/refresh.token").display().to_string();
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
        assert_eq!(payload["params"]["home_dir"], expected_home_dir);
        assert_eq!(payload["params"]["codex_home"], expected_codex_home);
        assert_eq!(payload["params"]["workdir"], expected_workdir);
        assert_eq!(payload["params"]["env"]["FOO"], "bar");
        assert_eq!(payload["params"]["env"]["FORT_TOKEN_PATH"], expected_access_token);
        assert_eq!(payload["params"]["env"]["FORT_REFRESH_TOKEN_PATH"], expected_refresh_token);
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
            home_dir.to_str().expect("home dir"),
            "--codex-home",
            codex_home.to_str().expect("codex home"),
            "--workdir",
            workdir.to_str().expect("workdir"),
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

    let output = ProcessCommand::new(assert_cmd::cargo::cargo_bin("si"))
        .args([
            "nucleus",
            "events",
            "subscribe",
            "--count",
            "6",
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
        Duration::from_secs(3),
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
        Duration::from_secs(3),
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
fn nucleus_hook_rule_cli_round_trips_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let upsert_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "hook",
            "upsert",
            "github-notify",
            "--match-event-type",
            "github.notification",
            "--instructions",
            "Reply with nucleus-github-hook",
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
    let upserted: Value = serde_json::from_slice(&upsert_output).expect("parse hook upsert");
    assert_eq!(upserted["name"], json!("github-notify"));
    assert_eq!(upserted["match_event_type"], json!("github.notification"));
    assert_eq!(upserted["enabled"], json!(true));

    let list_output = cargo_bin()
        .args(["nucleus", "producer", "hook", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&list_output).expect("parse hook list");
    assert_eq!(listed.as_array().map(Vec::len), Some(1));
    assert_eq!(listed[0]["name"], json!("github-notify"));

    let inspect_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "hook",
            "inspect",
            "github-notify",
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
    let inspected: Value = serde_json::from_slice(&inspect_output).expect("parse hook inspect");
    assert_eq!(inspected["instructions"], json!("Reply with nucleus-github-hook"));

    let delete_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "hook",
            "delete",
            "github-notify",
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
    let deleted: Value = serde_json::from_slice(&delete_output).expect("parse hook delete");
    assert_eq!(deleted["rule_name"], json!("github-notify"));
    assert_eq!(deleted["deleted"], json!(true));

    let list_after_delete_output = cargo_bin()
        .args(["nucleus", "producer", "hook", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed_after_delete: Value =
        serde_json::from_slice(&list_after_delete_output).expect("parse hook list after delete");
    assert_eq!(listed_after_delete.as_array().map(Vec::len), Some(0));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_cron_rule_cli_round_trips_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let upsert_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "cron",
            "upsert",
            "svelte-docs-nightly",
            "--schedule-kind",
            "every",
            "--schedule",
            "60s",
            "--instructions",
            "Audit Svelte docs and blog readiness",
            "--reset",
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
    let upserted: Value = serde_json::from_slice(&upsert_output).expect("parse cron upsert");
    assert_eq!(upserted["name"], json!("svelte-docs-nightly"));
    assert_eq!(upserted["schedule_kind"], json!("every"));
    assert_eq!(upserted["schedule"], json!("60s"));
    assert!(upserted["next_due_at"].is_string());

    let list_output = cargo_bin()
        .args(["nucleus", "producer", "cron", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&list_output).expect("parse cron list");
    assert_eq!(listed.as_array().map(Vec::len), Some(1));
    assert_eq!(listed[0]["name"], json!("svelte-docs-nightly"));

    let inspect_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "cron",
            "inspect",
            "svelte-docs-nightly",
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
    let inspected: Value = serde_json::from_slice(&inspect_output).expect("parse cron inspect");
    assert_eq!(inspected["instructions"], json!("Audit Svelte docs and blog readiness"));

    let delete_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "cron",
            "delete",
            "svelte-docs-nightly",
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
    let deleted: Value = serde_json::from_slice(&delete_output).expect("parse cron delete");
    assert_eq!(deleted["rule_name"], json!("svelte-docs-nightly"));
    assert_eq!(deleted["deleted"], json!(true));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_github_notification_event_ingest_routes_hook_task_and_logs_event() {
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

    cargo_bin()
        .args([
            "nucleus",
            "producer",
            "hook",
            "upsert",
            "github-notify",
            "--match-event-type",
            "github.notification",
            "--instructions",
            "Reply with nucleus-github-hook",
            "--endpoint",
            &ws_url,
            "--format",
            "json",
        ])
        .assert()
        .success();

    let ingest_output = cargo_bin()
        .args([
            "nucleus",
            "events",
            "ingest",
            "--endpoint",
            &ws_url,
            "--type",
            "github.notification",
            "--source",
            "github",
            "--payload",
            "{\"repository\":\"Aureuma/si\",\"reason\":\"mention\",\"subject\":{\"type\":\"PullRequest\",\"title\":\"Stabilize nucleus ingress\"}}",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let ingested: Value = serde_json::from_slice(&ingest_output).expect("parse event ingest");
    assert_eq!(ingested["type"], json!("github.notification"));
    assert_eq!(ingested["source"], json!("github"));

    let listed = wait_for_cli_task(&ws_url, Duration::from_secs(5), |task| {
        task["source"] == "hook" && task["producer_rule_name"] == "github-notify"
    });
    let task_id = listed["task_id"].as_str().expect("task id").to_owned();
    let inspected = wait_for_cli_task_status(&ws_url, &task_id, "done");
    let websocket = inspect_task_over_websocket(&ws_url, &task_id);

    for field in ["task_id", "source", "producer_rule_name", "status", "checkpoint_summary"] {
        assert_eq!(inspected[field], websocket[field], "github hook field mismatch: {field}");
    }

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == json!("github.notification")
            && event["source"] == json!("github")
            && event["data"]["payload"]["repository"] == json!("Aureuma/si")
    }));
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_new_hook_rule_does_not_backfill_old_github_notifications_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let first_ingest_output = cargo_bin()
        .args([
            "nucleus",
            "events",
            "ingest",
            "--endpoint",
            &ws_url,
            "--type",
            "github.notification",
            "--source",
            "github",
            "--payload",
            "{\"repository\":\"Aureuma/si\",\"reason\":\"mention\",\"subject\":{\"type\":\"Issue\",\"title\":\"Older github notification\"}}",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let first_ingested: Value =
        serde_json::from_slice(&first_ingest_output).expect("parse first event ingest");
    let first_seq = first_ingested["seq"].as_u64().expect("first seq");

    let upsert_output = cargo_bin()
        .args([
            "nucleus",
            "producer",
            "hook",
            "upsert",
            "github-notify",
            "--match-event-type",
            "github.notification",
            "--instructions",
            "Reply with nucleus-github-hook",
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
    let upserted: Value = serde_json::from_slice(&upsert_output).expect("parse hook upsert");
    assert_eq!(upserted["last_processed_event_seq"], json!(first_seq));

    let list_output = cargo_bin()
        .args(["nucleus", "task", "list", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let tasks: Value = serde_json::from_slice(&list_output).expect("parse task list");
    assert!(
        tasks.as_array().expect("task list array").iter().all(|task| task["source"] != "hook"),
        "new hook rule should not backfill old github events"
    );

    cargo_bin()
        .args([
            "nucleus",
            "events",
            "ingest",
            "--endpoint",
            &ws_url,
            "--type",
            "github.notification",
            "--source",
            "github",
            "--payload",
            "{\"repository\":\"Aureuma/si\",\"reason\":\"assign\",\"subject\":{\"type\":\"Issue\",\"title\":\"Fresh github notification\"}}",
            "--format",
            "json",
        ])
        .assert()
        .success();

    let listed = wait_for_cli_task(&ws_url, Duration::from_secs(5), |task| {
        task["source"] == "hook" && task["producer_rule_name"] == "github-notify"
    });
    assert!(
        listed["instructions"]
            .as_str()
            .expect("hook instructions")
            .contains("Canonical event sequence"),
        "fresh github event should create the hook task"
    );
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
    write_fort_session_state_via_cli(&codex_home, "america");
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
fn nucleus_fort_public_cli_lifecycle_drives_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let ws_url = format!(
        "{}/ws",
        spawn_live_nucleus_service_with_runtime(&state_root, Arc::new(TestRuntime::default()))
            .replacen("http", "ws", 1)
    );

    let home_dir = temp.path().join("home");
    let codex_home = home_dir.join(".si/codex/profiles/america");
    let fort_session_path = codex_home.join("fort").join("session.json");
    write_fort_session_state_via_cli(&codex_home, "america");

    let shown_output = cargo_bin()
        .args(["fort", "session", "show", "--path"])
        .arg(&fort_session_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let shown: Value = serde_json::from_slice(&shown_output).expect("parse fort session show");
    assert_eq!(shown["profile_id"], "america");
    assert_eq!(shown["agent_id"], "si-codex-america");

    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    let session_id = session["session"]["session_id"].as_str().expect("session id");

    let first_created = create_task_over_websocket(
        &ws_url,
        "task-fort-cli-lifecycle-ready",
        "Fort CLI lifecycle ready task",
        "Use si fort bootstrap and then reply with nucleus-smoke",
        "america",
        Some(session_id),
    );
    let first_task_id = first_created["task_id"].as_str().expect("task id").to_owned();
    let first_done = wait_for_cli_task_status(&ws_url, &first_task_id, "done");
    assert_eq!(first_done["checkpoint_summary"], "nucleus-smoke");

    clear_fort_session_state_via_cli(&codex_home);
    let second_created = create_task_over_websocket(
        &ws_url,
        "task-fort-cli-lifecycle-cleared",
        "Fort CLI lifecycle cleared task",
        "Use si fort status before continuing",
        "america",
        Some(session_id),
    );
    let second_task_id = second_created["task_id"].as_str().expect("task id").to_owned();
    let second_blocked = wait_for_cli_task_status(&ws_url, &second_task_id, "blocked");
    assert_eq!(second_blocked["blocked_reason"], "auth_required");
    assert!(second_blocked["latest_run_id"].is_null());

    let events = load_event_log_values(&state_root);
    assert!(events.iter().any(|event| {
        event["type"] == "fort.ready" && event["data"]["task_id"] == first_task_id
    }));
    assert!(events.iter().any(|event| {
        event["type"] == "fort.auth_required" && event["data"]["task_id"] == second_task_id
    }));
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
    clear_fort_session_state_via_cli(&codex_home);
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
    let session = create_session_via_cli(&ws_url, &home_dir, &codex_home, temp.path());
    write_invalid_fort_session_state(&codex_home);
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
        run_delay: Duration::from_secs(3),
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
    write_invalid_fort_session_state(&codex_home);
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
        Duration::from_secs(3),
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
    write_reusable_codex_fort_session(&codex_home, "america");
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
        write_reusable_codex_fort_session(&codex_home, "america");
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
        run_delay: Duration::from_secs(3),
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
    assert!(
        stderr.contains("session missing app-server thread id")
            || stderr.contains("task references a non-reusable session")
    );

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
        run_delay: Duration::from_secs(3),
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
    let active =
        wait_for_cli_task_predicate(&ws_url, &active_task_id, Duration::from_secs(5), |task| {
            task["status"] == "done" || task["status"] == "blocked"
        });
    assert!(
        active["checkpoint_summary"].is_null() || active["checkpoint_summary"] == "nucleus-smoke"
    );

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
        Duration::from_secs(3),
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
        Duration::from_secs(3),
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
        Duration::from_secs(3),
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
    write_reusable_codex_fort_session(&codex_home, "america");
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
    let initial_start_calls = runtime.start_call_count();

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
    assert_eq!(runtime.start_call_count(), initial_start_calls + 1);

    let inspected = wait_for_worker_status(&ws_url, &worker_id, "ready");
    assert_eq!(inspected["worker"]["status"], "ready");
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_task_cancel_marks_session_broken_when_thread_id_is_missing_on_live_service() {
    let temp = tempdir().expect("tempdir");
    let state_root = temp.path().join("nucleus");
    let runtime = TestRuntime::with_streaming_output(
        Duration::from_secs(3),
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
        run_delay: Duration::from_secs(3),
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
        run_delay: Duration::from_secs(3),
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
        run_delay: Duration::from_secs(3),
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
    clear_fort_session_state_via_cli(&codex_home);
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

    write_fort_session_state_via_cli(&codex_home, "america");
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

    write_fort_session_state_via_cli(&codex_home, "america");
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
    let britain_codex_home = home_dir.join(".si/codex/profiles/britain");
    write_live_profile_record(&state_root, "britain", &britain_codex_home);

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
    write_fort_session_state_via_cli(&codex_home, "america");
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
    clear_fort_session_state_via_cli(&britain_codex_home);

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

    write_fort_session_state_via_cli(&america_codex_home, "america");
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
fn nucleus_live_gateway_auth_requires_token_for_all_operations() {
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

    let status = cargo_bin()
        .env_remove("SI_NUCLEUS_AUTH_TOKEN")
        .args(["nucleus", "status", "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .failure();
    let stderr = String::from_utf8(status.get_output().stderr.clone()).expect("utf8 stderr");
    assert!(stderr.contains("unauthorized: missing bearer token"));

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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args(["nucleus", "task", "inspect", task_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success();
}

#[test]
#[allow(clippy::result_large_err)]
fn nucleus_live_gateway_read_surfaces_require_token() {
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
    write_reusable_codex_fort_session(&codex_home, "america");
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
    let task = wait_for_cli_task_status_with_token(&ws_url, &task_id, "done", Some("test-token"));
    let run_id = task["latest_run_id"].as_str().expect("latest run id").to_owned();

    let profiles = cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
        .args(["nucleus", "run", "inspect", &run_id, "--endpoint", &ws_url, "--format", "json"])
        .assert()
        .success();

    cargo_bin()
        .env("SI_NUCLEUS_AUTH_TOKEN", "test-token")
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
fn nucleus_live_rest_operations_require_token_when_auth_is_configured() {
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
    assert_eq!(openapi_response.status(), reqwest::StatusCode::OK);

    let status_response = client.get(format!("{base_url}/status")).send().expect("status response");
    assert_eq!(status_response.status(), reqwest::StatusCode::UNAUTHORIZED);

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
    write_reusable_codex_fort_session(&codex_home, "america");
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
    let task = wait_for_cli_task_status_with_token(&ws_url, &task_id, "done", Some("test-token"));
    let run_id = task["latest_run_id"].as_str().expect("run id").to_owned();

    let tasks: Value = client
        .get(format!("{base_url}/tasks"))
        .bearer_auth("test-token")
        .send()
        .expect("list tasks")
        .json()
        .expect("parse tasks");
    assert!(
        tasks.as_array().expect("task list array").iter().any(|item| item["task_id"] == task_id)
    );

    let task_inspect: Value = client
        .get(format!("{base_url}/tasks/{task_id}"))
        .bearer_auth("test-token")
        .send()
        .expect("inspect task")
        .json()
        .expect("parse task inspect");
    assert_eq!(task_inspect["task_id"], task_id);
    assert_eq!(task_inspect["status"], "done");

    let workers: Value = client
        .get(format!("{base_url}/workers"))
        .bearer_auth("test-token")
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
        .bearer_auth("test-token")
        .send()
        .expect("inspect worker")
        .json()
        .expect("parse worker inspect");
    assert_eq!(worker["worker"]["worker_id"], worker_id);

    let session: Value = client
        .get(format!("{base_url}/sessions/{session_id}"))
        .bearer_auth("test-token")
        .send()
        .expect("inspect session")
        .json()
        .expect("parse session inspect");
    assert_eq!(session["session_id"], session_id);

    let run: Value = client
        .get(format!("{base_url}/runs/{run_id}"))
        .bearer_auth("test-token")
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
    assert_eq!(body["info"]["title"], json!("SI Nucleus REST API"));
    assert_eq!(body["info"]["version"], json!(env!("CARGO_PKG_VERSION")));
    assert_eq!(
        body["info"]["description"],
        json!(
            "Bounded external integration API over the canonical SI Nucleus task, worker, session, and run model."
        )
    );
    assert_eq!(body["servers"][0]["url"], json!(base_url));
    assert_eq!(body["components"]["securitySchemes"]["bearerAuth"]["type"], json!("http"));
    assert_eq!(body["components"]["securitySchemes"]["bearerAuth"]["scheme"], json!("bearer"));
    assert_eq!(
        body["components"]["securitySchemes"]["bearerAuth"]["bearerFormat"],
        json!("opaque token")
    );
    for (path, method, operation_id) in [
        ("/status", "get", "getNucleusStatus"),
        ("/tasks", "get", "listTasks"),
        ("/tasks", "post", "createTask"),
        ("/tasks/{task_id}", "get", "inspectTask"),
        ("/tasks/{task_id}/cancel", "post", "cancelTask"),
        ("/workers", "get", "listWorkers"),
        ("/workers/{worker_id}", "get", "inspectWorker"),
        ("/sessions/{session_id}", "get", "inspectSession"),
        ("/runs/{run_id}", "get", "inspectRun"),
        ("/openapi.json", "get", "getOpenApiDocument"),
    ] {
        assert_eq!(body["paths"][path][method]["operationId"], json!(operation_id));
    }
    assert_eq!(
        body["components"]["schemas"]["TaskCreateParams"]["additionalProperties"],
        json!(false)
    );
    assert_eq!(
        body["components"]["schemas"]["TaskCreateParams"]["properties"]["source"]["default"],
        json!("rest")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["parameters"][0]["schema"]["pattern"],
        json!("^si-task-.+")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["parameters"][0]["schema"]["pattern"],
        json!("^si-worker-.+")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["parameters"][0]["schema"]["pattern"],
        json!("^si-session-.+")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["parameters"][0]["schema"]["pattern"],
        json!("^si-run-.+")
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
    ] {
        assert_eq!(
            body["paths"][path][method]["security"],
            json!([{ "bearerAuth": [] }]),
            "OpenAPI security must require bearer auth for {method} {path}"
        );
    }
    assert_eq!(body["paths"]["/openapi.json"]["get"]["security"], json!([]));
    assert_eq!(body["paths"]["/tasks"]["post"]["security"][0]["bearerAuth"], json!([]));
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["security"][0]["bearerAuth"],
        json!([])
    );
    assert_eq!(
        body["paths"]["/status"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/NucleusStatusView")
    );
    assert_eq!(
        body["paths"]["/status"]["get"]["responses"]["200"]["description"],
        json!("Current nucleus status.")
    );
    assert_eq!(body["paths"]["/status"]["get"]["summary"], json!("Inspect Nucleus status"));
    assert_eq!(
        body["paths"]["/status"]["get"]["description"],
        json!(
            "Read the current Nucleus status projection, including bind address, state directory, and durable object counts."
        )
    );
    assert_eq!(
        body["paths"]["/status"]["get"]["x-si-purpose"],
        json!(
            "Use this for bounded external health and topology inspection without opening the websocket control plane."
        )
    );
    assert_eq!(
        body["paths"]["/tasks"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["items"]["$ref"],
        json!("#/components/schemas/TaskRecord")
    );
    assert_eq!(
        body["paths"]["/tasks"]["get"]["responses"]["200"]["description"],
        json!("All durable tasks.")
    );
    assert_eq!(body["paths"]["/tasks"]["get"]["summary"], json!("List tasks"));
    assert_eq!(
        body["paths"]["/tasks"]["get"]["description"],
        json!(
            "List durable tasks from the same source of truth used by the websocket gateway and CLI."
        )
    );
    assert_eq!(
        body["paths"]["/tasks"]["get"]["x-si-purpose"],
        json!(
            "Use this for bounded task inspection and polling from external tools such as GPT Actions."
        )
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["201"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/TaskRecord")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["201"]["description"],
        json!("Created task.")
    );
    assert_eq!(body["paths"]["/tasks"]["post"]["summary"], json!("Create a task"));
    assert_eq!(
        body["paths"]["/tasks"]["post"]["description"],
        json!(
            "Create a durable task through Nucleus so it can be routed, executed, and observed through the canonical control plane."
        )
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["x-si-purpose"],
        json!(
            "Use this to create bounded external work without bypassing Nucleus task intake rules."
        )
    );
    assert_eq!(body["paths"]["/tasks"]["post"]["requestBody"]["required"], json!(true));
    assert_eq!(
        body["paths"]["/tasks"]["post"]["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        json!("#/components/schemas/TaskCreateParams")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["401"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["401"]["description"],
        json!("Bearer token missing or invalid for an authenticated request.")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["400"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["400"]["description"],
        json!("Invalid request.")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["responses"]["404"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["responses"]["200"]["description"],
        json!("Task record.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["parameters"][0]["description"],
        json!("Canonical SI task id returned by task creation or task listing.")
    );
    assert_eq!(body["paths"]["/tasks/{task_id}"]["get"]["summary"], json!("Inspect one task"));
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["description"],
        json!("Read one durable task projection by task id.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["x-si-purpose"],
        json!("Use this to inspect bounded task state from external tooling.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["responses"]["404"]["description"],
        json!("Task not found.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["404"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["200"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/TaskCancelResultView")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["200"]["description"],
        json!("Cancellation result.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["parameters"][0]["description"],
        json!("Canonical SI task id returned by task creation or task listing.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["summary"],
        json!("Cancel one task")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["description"],
        json!(
            "Request cancellation for a task through Nucleus. Queued tasks cancel immediately; active runs are interrupted through the runtime when needed."
        )
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["x-si-purpose"],
        json!(
            "Use this for bounded external cancellation requests and then re-read the task or run to observe final state."
        )
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["503"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["503"]["description"],
        json!("Runtime unavailable for active-run cancellation.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["401"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["401"]["description"],
        json!("Bearer token missing or invalid for an authenticated request.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["404"]["description"],
        json!("Task not found.")
    );
    assert_eq!(
        body["paths"]["/workers"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["items"]["$ref"],
        json!("#/components/schemas/WorkerRecord")
    );
    assert_eq!(
        body["paths"]["/workers"]["get"]["responses"]["200"]["description"],
        json!("All durable workers.")
    );
    assert_eq!(body["paths"]["/workers"]["get"]["summary"], json!("List workers"));
    assert_eq!(
        body["paths"]["/workers"]["get"]["description"],
        json!("List durable worker records tracked by Nucleus.")
    );
    assert_eq!(
        body["paths"]["/workers"]["get"]["x-si-purpose"],
        json!(
            "Use this for bounded worker inspection without relying on tmux or direct runtime internals."
        )
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["200"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/WorkerInspectView")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["200"]["description"],
        json!("Worker inspect view.")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["summary"],
        json!("Inspect one worker")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["description"],
        json!("Read one worker projection, including persisted runtime view when available.")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["x-si-purpose"],
        json!(
            "Use this to inspect worker assignment and runtime attachment through the Nucleus model."
        )
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["parameters"][0]["description"],
        json!("Canonical SI worker id returned by worker listing.")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["404"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["404"]["description"],
        json!("Worker not found.")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["200"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/SessionRecord")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["200"]["description"],
        json!("Session record.")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["summary"],
        json!("Inspect one session")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["description"],
        json!("Read one durable session projection by session id.")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["x-si-purpose"],
        json!(
            "Use this to inspect worker/session binding and reusable thread identity from external tooling."
        )
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["parameters"][0]["description"],
        json!("Canonical SI session id returned by session or run inspection.")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["404"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["404"]["description"],
        json!("Session not found.")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RunRecord")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["200"]["description"],
        json!("Run record.")
    );
    assert_eq!(body["paths"]["/runs/{run_id}"]["get"]["summary"], json!("Inspect one run"));
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["description"],
        json!("Read one durable run projection by run id.")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["x-si-purpose"],
        json!(
            "Use this to inspect bounded run state from external tools without subscribing to websocket events."
        )
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["parameters"][0]["schema"]["type"],
        json!("string")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["parameters"][0]["description"],
        json!("Canonical SI run id returned by task or session inspection.")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["404"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["404"]["description"],
        json!("Run not found.")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/OpenApiDocumentView")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["responses"]["200"]["description"],
        json!("Public OpenAPI document.")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["summary"],
        json!("Fetch the OpenAPI document")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["description"],
        json!(
            "Read the public OpenAPI-compatible REST description for bounded external integrations."
        )
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["x-si-purpose"],
        json!(
            "Use this unauthenticated endpoint to bootstrap GPT Actions or other external tool clients against the bounded REST surface."
        )
    );
    assert_eq!(
        body["paths"]["/tasks"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/tasks"]["post"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["responses"]["500"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["500"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/workers"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/workers"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["500"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/workers/{worker_id}"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["500"]["content"]["application/json"]
            ["schema"]["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/sessions/{session_id}"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/runs/{run_id}"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/status"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/status"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
            ["$ref"],
        json!("#/components/schemas/RestErrorEnvelope")
    );
    assert_eq!(
        body["paths"]["/openapi.json"]["get"]["responses"]["500"]["description"],
        json!("Request failed.")
    );
    assert_eq!(
        body["components"]["schemas"]["TaskCancelResultView"]["required"],
        json!(["task", "cancellation_requested"])
    );
    assert_eq!(body["components"]["schemas"]["RestErrorEnvelope"]["required"], json!(["error"]));
    assert_eq!(
        body["components"]["schemas"]["RestErrorEnvelope"]["properties"]["error"]["required"],
        json!(["code", "message"])
    );
    assert!(
        body["components"]["schemas"]["RestErrorEnvelope"]["properties"]["error"]["properties"]
            ["details"]
            .is_object()
    );
    assert_eq!(
        body["components"]["schemas"]["WorkerInspectView"]["properties"]["worker"]["allOf"][0]["$ref"],
        json!("#/components/schemas/WorkerRecord")
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
        Duration::from_secs(3),
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
    write_reusable_codex_fort_session(&codex_home, "america");
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
    let running =
        wait_for_cli_task_status_with_token(&ws_url, &task_id, "running", Some("test-token"));
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

    let cancelled_task =
        wait_for_cli_task_status_with_token(&ws_url, &task_id, "cancelled", Some("test-token"));
    assert_eq!(cancelled_task["status"], "cancelled");

    let cancelled_run = inspect_run_via_cli_with_token(&ws_url, &run_id, Some("test-token"));
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
        let response = client
            .get(format!("{base_url}{path}"))
            .bearer_auth("test-token")
            .send()
            .expect("missing read");
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
        .bearer_auth("test-token")
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
        .env_remove("CODEX_HOME")
        .env("FORT_TOKEN", "legacy-token")
        .env("FORT_REFRESH_TOKEN", "legacy-refresh-token")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
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
        .env_remove("CODEX_HOME")
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
        .env_remove("CODEX_HOME")
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
fn fort_wrapper_ignores_caller_runtime_session_paths_for_runtime_commands() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
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
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
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
        .env("FORT_TOKEN_PATH", runtime_token_path.to_str().expect("runtime token path"))
        .env(
            "FORT_REFRESH_TOKEN_PATH",
            runtime_refresh_path.to_str().expect("runtime refresh path"),
        )
        .env_remove("FORT_HOST")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked without managed runtime");
    assert_eq!(
        fs::read_to_string(&runtime_token_path).expect("read runtime token"),
        "stale-runtime-token\n"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required"));
    assert!(stderr.contains("si codex shell"));
}

#[test]
fn fort_wrapper_ignores_caller_runtime_session_paths_for_doctor() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
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
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"doctor\" ]; then\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env("FORT_TOKEN_PATH", runtime_token_path.to_str().expect("runtime token path"))
        .env(
            "FORT_REFRESH_TOKEN_PATH",
            runtime_refresh_path.to_str().expect("runtime refresh path"),
        )
        .env_remove("FORT_HOST")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
    assert_eq!(
        fs::read_to_string(&runtime_token_path).expect("read runtime token"),
        "stale-runtime-token\n"
    );
}

#[test]
fn fort_wrapper_ignores_active_profile_runtime_session_outside_managed_runtime() {
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
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n[codex]\nprofile = \"profile-delta\"\n[codex.profiles]\nactive = \"profile-delta\"\n[codex.profiles.entries.profile-delta]\nname = \"Profile Delta\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
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
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked without CODEX_HOME");
    assert_eq!(
        fs::read_to_string(&profile_token_path).expect("read profile token"),
        "stale-profile-token\n"
    );
    assert_eq!(
        fs::read_to_string(&profile_refresh_path).expect("read profile refresh"),
        "stale-profile-refresh\n"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required"));
    assert!(stderr.contains("si codex shell"));
}

#[test]
fn fort_wrapper_fails_loudly_when_codex_home_runtime_refresh_fails() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex")).expect("mkdir codex dir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let bootstrap_token_path = bootstrap_dir.join("admin.token");
    let bootstrap_refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&bootstrap_token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&bootstrap_refresh_path, "stale-admin-refresh\n").expect("write stale admin refresh");
    let profile_home = home.path().join(".si/codex/profiles/profile-delta");
    let profile_fort_dir = profile_home.join("fort");
    fs::create_dir_all(&profile_fort_dir).expect("mkdir profile fort dir");
    let profile_token_path = profile_fort_dir.join("access.token");
    let profile_refresh_path = profile_fort_dir.join("refresh.token");
    let profile_session_path = profile_fort_dir.join("session.json");
    fs::write(&profile_token_path, "stale-profile-token\n").expect("write stale profile token");
    fs::write(&profile_refresh_path, "stale-profile-refresh\n")
        .expect("write stale profile refresh");
    fs::write(
        &profile_session_path,
        serde_json::to_vec_pretty(&json!({
            "profile_id": "profile-delta",
            "agent_id": "si-codex-profile-delta",
            "session_id": "fort-session-profile-delta",
            "host": "https://fort.example.test",
            "runtime_host": "https://fort.example.test",
            "access_token_path": profile_token_path,
            "refresh_token_path": profile_refresh_path,
            "access_expires_at": (Utc::now() - chrono::Duration::hours(1)).to_rfc3339(),
            "refresh_expires_at": (Utc::now() + chrono::Duration::days(30)).to_rfc3339(),
            "updated_at": Utc::now().to_rfc3339(),
        }))
        .expect("serialize fort session"),
    )
    .expect("write fort session state");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [
            &bootstrap_token_path,
            &bootstrap_refresh_path,
            &profile_token_path,
            &profile_refresh_path,
            &profile_session_path,
        ] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n[codex]\nprofile = \"profile-delta\"\n[codex.profiles]\nactive = \"profile-delta\"\n[codex.profiles.entries.profile-delta]\nname = \"Profile Delta\"\n",
            home.path().join(".si/codex/profiles")
        ),
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

    let assert = cargo_bin()
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
        .env("CODEX_HOME", &profile_home)
        .assert()
        .failure();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(
        args.contains(&profile_refresh_path.display().to_string()),
        "fort args did not include profile refresh path; args={args}"
    );
    assert!(args.contains("auth\nsession\nrefresh"), "fort args did not refresh; args={args}");
    assert!(!args.contains(&bootstrap_refresh_path.display().to_string()));
    assert!(!args.contains(&bootstrap_token_path.display().to_string()));
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("refresh fort session"));
    assert!(stderr.contains("unauthorized"));
    assert_eq!(
        fs::read_to_string(&bootstrap_token_path).expect("read refreshed bootstrap token"),
        "stale-admin-token\n"
    );
    assert_eq!(
        fs::read_to_string(&bootstrap_refresh_path).expect("read rotated bootstrap refresh"),
        "stale-admin-refresh\n"
    );
    let persisted = load_persisted_session_state(&profile_session_path)
        .expect("load revoked profile session state");
    assert!(persisted.session_id.trim().is_empty());
    match classify_persisted_session_state(&persisted, Utc::now().timestamp())
        .expect("classify revoked profile session state")
    {
        SessionState::Revoked { .. } => {}
        other => panic!("expected revoked profile session state, got {other:?}"),
    }
}

#[test]
fn fort_wrapper_does_not_fall_back_to_bootstrap_when_codex_home_session_is_missing() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/fort/bootstrap")).expect("mkdir fort bootstrap");
    fs::write(home.path().join(".si/fort/bootstrap/admin.token"), "bootstrap-token\n")
        .expect("write bootstrap token");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let codex_home = home.path().join(".si/codex/profiles/profile-epsilon");
    fs::create_dir_all(&codex_home).expect("mkdir codex home");
    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nexit 0\n",
            shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
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
        .env("CODEX_HOME", &codex_home)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .failure();

    assert!(
        !args_file.exists(),
        "fort binary should not be invoked after missing CODEX_HOME session"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required for CODEX_HOME"));
    assert!(stderr.contains(codex_home.join("fort").display().to_string().as_str()));
}

#[test]
fn fort_wrapper_rejects_profile_refresh_token_rotation_to_noncanonical_path() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex/profiles/profile-zeta/fort"))
        .expect("mkdir profile fort dir");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");
    let refresh_path = home.path().join(".si/codex/profiles/profile-zeta/fort/refresh.token");
    fs::write(&refresh_path, "profile-refresh-token\n").expect("write profile refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&refresh_path).expect("stat refresh token").permissions();
        perms.set_mode(0o600);
        fs::set_permissions(&refresh_path, perms).expect("chmod refresh token");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let out_path = home.path().join("detached-refresh.token");

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "auth",
            "session",
            "refresh",
            "--refresh-token-file",
            refresh_path.to_str().expect("refresh path"),
            "--refresh-token-out",
            out_path.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked after guard failure");
    assert!(!out_path.exists(), "guard must not write a detached refresh token");
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("refusing to rotate Codex profile Fort refresh token"));
    assert!(stderr.contains("refreshed in place"));
}

#[test]
fn fort_wrapper_rejects_nonprimary_profile_refresh_token_rotation_to_noncanonical_path() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex/profiles/profile-zeta/workers/review/fort"))
        .expect("mkdir worker fort dir");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");
    let refresh_path =
        home.path().join(".si/codex/profiles/profile-zeta/workers/review/fort/refresh.token");
    fs::write(&refresh_path, "profile-refresh-token\n").expect("write profile refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&refresh_path).expect("stat refresh token").permissions();
        perms.set_mode(0o600);
        fs::set_permissions(&refresh_path, perms).expect("chmod refresh token");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let out_path = home.path().join("detached-refresh.token");

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "auth",
            "session",
            "refresh",
            "--refresh-token-file",
            refresh_path.to_str().expect("refresh path"),
            "--refresh-token-out",
            out_path.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked after guard failure");
    assert!(!out_path.exists(), "guard must not write a detached refresh token");
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("refusing to rotate Codex profile Fort refresh token"));
    assert!(stderr.contains("refreshed in place"));
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(!args.contains("--json\nauth\nsession\nrefresh\n"));
    assert!(args.contains(&format!("--token-file\n{}\nagent\nlist\n", token_path.display())));
}

#[test]
fn fort_wrapper_refreshes_bootstrap_token_before_near_expiry() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    let payload = format!(
        "{{\"exp\":{},\"iss\":\"fortd\",\"aud\":[\"fort-api\"]}}",
        chrono::Utc::now().timestamp() + 120
    );
    let token = format!("header.{}.signature\n", URL_SAFE_NO_PAD.encode(payload.as_bytes()));
    fs::write(&token_path, token).expect("write near-expiry admin token");
    fs::write(&refresh_path, "near-expiry-refresh-token\n").expect("write refresh token");
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains("--json\nauth\nsession\nrefresh\n--refresh-token-file\n"));
    assert_eq!(
        fs::read_to_string(&token_path).expect("read refreshed admin token"),
        "rotated-access-token\n"
    );
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
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"doctor\" ]; then\n  exit 0\nfi\nif [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  printf 'unexpected refresh\\n' >&2\n  exit 1\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(!args.contains("--json\nauth\nsession\nrefresh\n"));
    assert!(!args.contains("--token-file\n"));
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
}

#[test]
fn fort_wrapper_doctor_fails_when_codex_home_session_is_missing() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/fort/bootstrap")).expect("mkdir fort bootstrap");
    fs::write(home.path().join(".si/fort/bootstrap/admin.token"), "bootstrap-token\n")
        .expect("write bootstrap token");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let codex_home = home.path().join(".si/codex/profiles/profile-epsilon");
    fs::create_dir_all(&codex_home).expect("mkdir codex home");
    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nexit 0\n",
            shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env("CODEX_HOME", &codex_home)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .failure();

    assert!(
        !args_file.exists(),
        "fort binary should not be invoked after missing CODEX_HOME doctor session"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required for CODEX_HOME"));
    assert!(stderr.contains(codex_home.join("fort").display().to_string().as_str()));
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "doctor\n");
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "doctor\n");
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_builds_configured_repo_from_custom_target_dir() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    let repo = tempdir().expect("fort repo");
    fs::create_dir_all(repo.path().join(".cargo")).expect("mkdir cargo dir");
    fs::write(
        repo.path().join(".cargo/config.toml"),
        "[build]\ntarget-dir = \".artifacts/cargo-target\"\n",
    )
    .expect("write cargo config");
    fs::create_dir_all(repo.path().join("target/debug")).expect("mkdir stale target dir");
    write_executable_shell_script(
        &repo.path().join("target/debug/fort"),
        "#!/bin/sh\nprintf 'stale default target fort binary should not be used\\n' >&2\nexit 1\n",
    );
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
            "#!/bin/sh\nset -eu\nif [ \"$1\" != \"build\" ]; then\n  printf 'unexpected cargo command: %s\\n' \"$1\" >&2\n  exit 1\nfi\nmkdir -p \"$PWD/.artifacts/cargo-target/debug\"\ncat > \"$PWD/.artifacts/cargo-target/debug/fort\" <<'EOF'\n#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\nEOF\nchmod +x \"$PWD/.artifacts/cargo-target/debug/fort\"\n",
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
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
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
            "https://fort-runtime.example.test",
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
    assert_eq!(parsed["runtime_host"], "https://fort-runtime.example.test");
}

#[test]
fn fort_config_set_rejects_persistent_local_hosts() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    let output = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--host",
            "http://127.0.0.1:8088",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8_lossy(&output);
    assert!(stderr.contains("fort.host must use https"));

    let output = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--runtime-host",
            "https://fort.internal",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8_lossy(&output);
    assert!(stderr.contains("fort.runtime_host must resolve through a public Fort HTTPS endpoint"));
}

#[test]
fn fort_wrapper_rejects_persistent_local_host_from_settings() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"http://127.0.0.1:8088\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(&fort_path, "#!/bin/sh\nexit 0\n");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8_lossy(&output);
    assert!(stderr.contains("fort.host must use https"));
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
    let url = format!("http://{addr}");

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
    assert!(rendered.contains("\"--bin\", \"si\""));
}

#[test]
fn build_homebrew_render_core_formula_without_version_uses_workspace_version() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let out = repo.path().join("Formula/si.rb");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let url = format!("http://{addr}");

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
        .current_dir(repo.path())
        .args(["build", "homebrew", "render-core-formula", "--output", out.to_str().expect("out")])
        .env("SI_RUST_HOMEBREW_SOURCE_BASE_URL", url)
        .assert()
        .success();

    let rendered = fs::read_to_string(out).expect("read formula");
    assert!(rendered.contains("url \"http://"));
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
fn build_homebrew_render_tap_formula_without_version_uses_workspace_version() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let checksums = repo.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");
    let output = repo.path().join("Formula/si.rb");

    cargo_bin()
        .current_dir(repo.path())
        .args([
            "build",
            "homebrew",
            "render-tap-formula",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--output",
            output.to_str().expect("output"),
        ])
        .assert()
        .success();

    let rendered = fs::read_to_string(output).expect("read formula");
    assert!(rendered.contains("version \"1.2.3\""));
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
        "#!/bin/sh\ntarget=\"\"\nprev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--target\" ]; then target=\"$arg\"; fi\n  prev=\"$arg\"\ndone\nout=\"$CARGO_TARGET_DIR/release\"\nif [ -n \"$target\" ]; then out=\"$CARGO_TARGET_DIR/$target/release\"; fi\nmkdir -p \"$out\"\nprintf '#!/bin/sh\\necho si\\n' > \"$out/si\"\nchmod 755 \"$out/si\"\n",
    );
    let file_path = bin_dir.path().join("file");
    write_executable_shell_script(
        &file_path,
        "#!/bin/sh\ncase \"$1\" in\n  *linux_amd64*) echo \"$1: ELF 64-bit LSB executable, x86-64\" ;;\n  *linux_arm64*) echo \"$1: ELF 64-bit LSB executable, ARM aarch64\" ;;\n  *darwin_amd64*) echo \"$1: Mach-O 64-bit executable x86_64\" ;;\n  *darwin_arm64*) echo \"$1: Mach-O 64-bit executable arm64\" ;;\n  *) echo \"$1: unknown\" ;;\nesac\n",
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
fn build_homebrew_update_tap_repo_without_version_uses_workspace_version() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let tap_dir = repo.path().join("homebrew-si");
    fs::create_dir_all(&tap_dir).expect("mkdir tap dir");
    let checksums = repo.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    cargo_bin()
        .current_dir(repo.path())
        .args([
            "build",
            "homebrew",
            "update-tap-repo",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--tap-dir",
            tap_dir.to_str().expect("tap dir"),
        ])
        .assert()
        .success();

    let rendered = fs::read_to_string(tap_dir.join("Formula/si.rb")).expect("read formula");
    assert!(rendered.contains("version \"1.2.3\""));
}

#[test]
fn build_installer_run_dry_run_reports_rust_usage() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v0.1.0");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(&cargo_path, "#!/bin/sh\necho cargo 1.94.0\n");
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
        "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo cargo 1.94.0\n  exit 0\nfi\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho installed\\n' > \"$CARGO_TARGET_DIR/release/si\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si\"\n",
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
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q 'file://' \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
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
fn build_installer_smoke_homebrew_uses_provided_assets_dir() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    let provided_assets = repo.path().join("dist");
    fs::create_dir_all(&provided_assets).expect("mkdir dist");
    fs::write(
        provided_assets.join("checksums.txt"),
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nexpected_path={expected_path}\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q \"file://$expected_path\" \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix),
            expected_path = shell_escape_for_test(&provided_assets),
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
        .env("SI_INSTALL_SMOKE_ASSETS_DIR", &provided_assets)
        .assert()
        .success();
}

#[test]
fn build_installer_smoke_homebrew_resolves_relative_assets_dir() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    let provided_assets = repo.path().join("dist");
    fs::create_dir_all(&provided_assets).expect("mkdir dist");
    fs::write(
        provided_assets.join("checksums.txt"),
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nexpected_path={expected_path}\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q \"file://$expected_path\" \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix),
            expected_path = shell_escape_for_test(&provided_assets),
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
        .env("SI_INSTALL_SMOKE_ASSETS_DIR", "dist")
        .assert()
        .success();
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
        "#!/bin/sh\ntarget=\"\"\nprev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--target\" ]; then target=\"$arg\"; fi\n  prev=\"$arg\"\ndone\nout=\"$CARGO_TARGET_DIR/release\"\nif [ -n \"$target\" ]; then out=\"$CARGO_TARGET_DIR/$target/release\"; fi\nmkdir -p \"$out\"\nprintf '#!/bin/sh\\necho si\\n' > \"$out/si\"\nchmod 755 \"$out/si\"\n",
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
            "--target",
            "linux-amd64",
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

#[test]
fn codex_profile_list_preserves_logged_in_state_when_live_probe_fails() {
    let home = tempdir().expect("tempdir");
    write_named_codex_profile_settings(
        home.path(),
        "america",
        &[("america", "America", "america@example.test")],
    );
    write_codex_auth_file(
        &home.path().join(".si/codex/profiles/america/auth.json"),
        "america@example.test",
    );

    let bin_dir = tempdir().expect("bin tempdir");
    let codex_path = bin_dir.path().join("codex");
    write_executable_shell_script(
        &codex_path,
        "#!/bin/sh\nset -eu\nif [ \"${1:-}\" = \"app-server\" ]; then\n  printf 'temporary app-server failure\\n' >&2\n  exit 1\nfi\nprintf 'unexpected codex invocation\\n' >&2\nexit 1\n",
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = cargo_bin()
        .env("PATH", path_env)
        .args([
            "codex",
            "profile",
            "list",
            "--home",
            home.path().to_str().expect("home path"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let profiles: Value = serde_json::from_slice(&output).expect("parse profile list");
    let profile = profiles
        .as_array()
        .expect("profile list array")
        .iter()
        .find(|profile| profile["profile"] == "america")
        .expect("america profile");
    assert_eq!(profile["state"], "Logged-In");
    assert!(profile["five_hour_left_pct"].is_null());
    assert!(profile["weekly_left_pct"].is_null());
}

#[test]
fn codex_repair_auth_all_provisions_slot_specific_agents_with_30d_ttl() {
    let home = tempdir().expect("home tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let codex_profiles_dir = home.path().join(".si/codex/profiles");
    fs::create_dir_all(&codex_profiles_dir).expect("mkdir codex profiles");

    write_codex_worker_state_for_test(
        home.path(),
        "america",
        "primary",
        "si-codex-pane-america",
        &workspace,
        &workspace,
    );
    write_codex_worker_state_for_test(
        home.path(),
        "america",
        "review",
        "si-codex-pane-america-review",
        &workspace,
        &workspace,
    );

    let requests_seen = Arc::new(Mutex::new(Vec::<String>::new()));
    let seen_clone = Arc::clone(&requests_seen);
    let call_index = Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let call_index_clone = Arc::clone(&call_index);
    let server = start_http_server_with_body(8, move |request| {
        seen_clone.lock().expect("requests lock").push(request.clone());
        let call = call_index_clone.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        if request.starts_with("GET /v1/agents/si-codex-america HTTP/1.1\r\n")
            || request.starts_with("GET /v1/agents/si-codex-america--review HTTP/1.1\r\n")
        {
            return http_json_response("404 Not Found", &[], r#"{"error":"not found"}"#);
        }
        if request.starts_with("POST /v1/agents HTTP/1.1\r\n") {
            return http_json_response("201 Created", &[], r#"{"ok":true}"#);
        }
        if request.starts_with("PUT /v1/agents/si-codex-america/policy HTTP/1.1\r\n")
            || request.starts_with("PUT /v1/agents/si-codex-america--review/policy HTTP/1.1\r\n")
        {
            return http_json_response("200 OK", &[], r#"{"ok":true}"#);
        }
        if request.starts_with("POST /v1/auth/session/open HTTP/1.1\r\n") {
            let body = format!(
                r#"{{"access_token":"{}","refresh_token":"rft-{call}","session_id":"session-{call}","access_expires_at":"2030-01-01T00:00:00Z","refresh_expires_at":"2030-01-31T00:00:00Z"}}"#,
                fake_jwt(json!({"exp": Utc::now().timestamp() + 3600}))
            );
            return http_json_response("200 OK", &[], &body);
        }
        panic!("unexpected request: {}", request.lines().next().unwrap_or("<empty>"));
    });

    fs::create_dir_all(home.path().join(".si/fort/bootstrap")).expect("mkdir fort bootstrap");
    fs::write(
        home.path().join(".si/fort/bootstrap/admin.token"),
        format!("{}\n", fake_jwt(json!({"exp": Utc::now().timestamp() + 3600}))),
    )
    .expect("write bootstrap token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(
            home.path().join(".si/fort/bootstrap/admin.token"),
            fs::Permissions::from_mode(0o600),
        )
        .expect("chmod bootstrap token");
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[codex]\nprofile = \"america\"\n[codex.profiles]\nactive = \"america\"\n[codex.profiles.entries.america]\nname = \"America\"\nemail = \"america@example.test\"\nauth_path = {:?}\n[fort]\nhost = {:?}\n",
            codex_profiles_dir,
            codex_profiles_dir.join("america").join("auth.json"),
            server.base_url,
        ),
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("HOME", home.path())
        .env("SI_FORT_ALLOW_INSECURE_HOST", "1")
        .args(["codex", "repair-auth", "--all", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("parse repair output");
    let repaired = parsed["repaired"].as_array().expect("repaired array");
    assert_eq!(repaired.len(), 2);
    assert!(repaired.iter().any(|item| item["agent_id"] == "si-codex-america"));
    assert!(repaired.iter().any(|item| item["agent_id"] == "si-codex-america--review"));

    let requests = requests_seen.lock().expect("requests lock");
    let open_calls = requests
        .iter()
        .filter(|request| request.starts_with("POST /v1/auth/session/open HTTP/1.1\r\n"))
        .collect::<Vec<_>>();
    assert_eq!(open_calls.len(), 2, "expected two Fort session open calls");
    assert!(open_calls.iter().any(|request| request.contains(r#""agent_id":"si-codex-america""#)));
    assert!(
        open_calls
            .iter()
            .any(|request| request.contains(r#""agent_id":"si-codex-america--review""#))
    );
    assert!(open_calls.iter().all(|request| request.contains(r#""refresh_ttl":"30d""#)));
    server.join();
}

#[test]
fn codex_profile_resolution_forms_are_consistent_across_lifecycle_commands() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[codex]\nprofile = \"america\"\n[codex.profiles]\nactive = \"america\"\n[codex.profiles.entries.america]\nname = \"America\"\nemail = \"america@example.test\"\nauth_path = \"/tmp/nonexistent\"\n",
    )
    .expect("write settings");

    for args in [
        vec!["codex", "remove", "america", "--slot", "primary"],
        vec!["codex", "remove", "--profile", "america", "--slot", "primary"],
        vec!["codex", "stop", "america", "--slot", "primary"],
        vec!["codex", "stop", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tail", "america", "--slot", "primary"],
        vec!["codex", "tail", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tmux", "america", "--slot", "primary", "--format", "json"],
        vec!["codex", "tmux", "--profile", "america", "--slot", "primary", "--format", "json"],
    ] {
        let output = cargo_bin()
            .env("HOME", home.path())
            .args(args)
            .assert()
            .failure()
            .get_output()
            .stderr
            .clone();
        let stderr = String::from_utf8(output).expect("stderr utf8");
        assert!(stderr.contains("no codex worker session found for profile"));
        assert!(!stderr.contains("unexpected argument"));
    }
}

#[test]
fn codex_lifecycle_commands_reject_dual_profile_forms_and_shell_legacy_positional_profile() {
    for args in [
        vec!["codex", "remove", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "stop", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tail", "america", "--profile", "america", "--slot", "primary"],
        vec![
            "codex",
            "tmux",
            "america",
            "--profile",
            "america",
            "--slot",
            "primary",
            "--format",
            "json",
        ],
    ] {
        let output = cargo_bin().args(args).assert().failure().get_output().stderr.clone();
        let stderr = String::from_utf8(output).expect("stderr utf8");
        assert!(stderr.contains("cannot be used with"));
        assert!(stderr.contains("[PROFILE]"));
    }

    let output = cargo_bin()
        .args(["codex", "shell", "america", "--", "echo", "ok"])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("stderr utf8");
    assert!(stderr.contains("legacy positional profile form"));
    assert!(stderr.contains("use `si codex shell --profile <profile> --slot <slot> -- <command>`"));
}

#[test]
fn codex_stop_keeps_worker_state_and_fort_auth_files() {
    let home = tempdir().expect("tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    write_named_codex_profile_settings(
        home.path(),
        "america",
        &[("america", "America", "america@example.test")],
    );

    let codex_home = home.path().join(".si/codex/profiles/america");
    write_reusable_codex_fort_session(&codex_home, "america");
    write_codex_worker_state_for_test(
        home.path(),
        "america",
        "primary",
        "si-codex-pane-america",
        &workspace,
        &workspace,
    );

    let state_path =
        home.path().join(".si").join("codex").join("workers").join("america").join("primary.json");
    let access_token_path = codex_home.join("fort/access.token");
    let refresh_token_path = codex_home.join("fort/refresh.token");
    let session_path = codex_home.join("fort/session.json");

    let output = cargo_bin()
        .env("HOME", home.path())
        .args(["codex", "stop", "--profile", "america", "--slot", "primary"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    assert!(String::from_utf8(output).expect("utf8").contains("stopped si-codex-pane-america"));

    assert!(state_path.exists());
    assert!(access_token_path.exists());
    assert!(refresh_token_path.exists());
    assert!(session_path.exists());
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
