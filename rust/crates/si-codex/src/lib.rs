use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use chrono::{Local, TimeZone};
use serde::Deserialize;
use serde_json::{Value, json};
use thiserror::Error;

pub const TMUX_SESSION_PREFIX: &str = "si-codex-pane-";
pub const CODEX_PROFILE_FORT_DIR_NAME: &str = "fort";
pub const CODEX_PROFILE_FORT_SESSION_FILE_NAME: &str = "session.json";
pub const CODEX_PROFILE_FORT_ACCESS_TOKEN_FILE_NAME: &str = "access.token";
pub const CODEX_PROFILE_FORT_REFRESH_TOKEN_FILE_NAME: &str = "refresh.token";
pub const CODEX_PROFILE_FORT_RUNTIME_LOCK_FILE_NAME: &str = "runtime.lock";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CodexProfileFortSessionPaths {
    pub dir: PathBuf,
    pub session_path: PathBuf,
    pub access_token_path: PathBuf,
    pub refresh_token_path: PathBuf,
    pub lock_path: PathBuf,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PromptSegment {
    pub prompt: String,
    pub lines: Vec<String>,
    pub raw: Vec<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReportParseResult {
    pub segments: Vec<PromptSegment>,
    pub report: String,
}

#[derive(Clone, Debug, PartialEq)]
pub struct CodexAppServerStatus {
    pub source: String,
    pub model: Option<String>,
    pub reasoning_effort: Option<String>,
    pub account_email: Option<String>,
    pub account_plan: Option<String>,
    pub five_hour_left_pct: Option<f64>,
    pub five_hour_reset: Option<String>,
    pub five_hour_remaining_minutes: Option<i32>,
    pub weekly_left_pct: Option<f64>,
    pub weekly_reset: Option<String>,
    pub weekly_remaining_minutes: Option<i32>,
}

#[derive(Debug, Error)]
pub enum CodexAppServerStatusError {
    #[error("empty app-server output")]
    EmptyOutput,
    #[error("{0}")]
    RateLimitsRequestFailed(String),
    #[error("rate limits missing")]
    RateLimitsMissing,
    #[error("parse rate limits response: {0}")]
    ParseRateLimits(serde_json::Error),
}

pub fn codex_worker_name(profile_id: &str) -> String {
    profile_id.trim().to_owned()
}

pub fn codex_tmux_session_name(profile_id: &str) -> String {
    let suffix = codex_worker_name(profile_id);
    if suffix.is_empty() {
        TMUX_SESSION_PREFIX.to_owned()
    } else {
        format!("{TMUX_SESSION_PREFIX}{suffix}")
    }
}

pub fn codex_profile_fort_session_paths(codex_home: &Path) -> CodexProfileFortSessionPaths {
    let dir = codex_home.join(CODEX_PROFILE_FORT_DIR_NAME);
    CodexProfileFortSessionPaths {
        session_path: dir.join(CODEX_PROFILE_FORT_SESSION_FILE_NAME),
        access_token_path: dir.join(CODEX_PROFILE_FORT_ACCESS_TOKEN_FILE_NAME),
        refresh_token_path: dir.join(CODEX_PROFILE_FORT_REFRESH_TOKEN_FILE_NAME),
        lock_path: dir.join(CODEX_PROFILE_FORT_RUNTIME_LOCK_FILE_NAME),
        dir,
    }
}

pub fn codex_profile_fort_runtime_env(codex_home: &Path) -> BTreeMap<String, String> {
    let paths = codex_profile_fort_session_paths(codex_home);
    BTreeMap::from([
        ("FORT_TOKEN_PATH".to_owned(), paths.access_token_path.display().to_string()),
        ("FORT_REFRESH_TOKEN_PATH".to_owned(), paths.refresh_token_path.display().to_string()),
    ])
}

pub fn build_codex_app_server_status_input(
    client_name: &str,
    client_version: &str,
    cwd: Option<String>,
) -> Vec<u8> {
    serialize_app_server_requests(&[
        build_app_server_initialize_request(1, client_name, client_version),
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

pub fn parse_codex_app_server_status(
    raw: &str,
) -> Result<CodexAppServerStatus, CodexAppServerStatusError> {
    let raw = raw.trim();
    if raw.is_empty() {
        return Err(CodexAppServerStatusError::EmptyOutput);
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
        let Some(id) = parse_codex_app_server_request_id(&envelope.id) else {
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
        return Err(CodexAppServerStatusError::RateLimitsRequestFailed(rate_err));
    }
    let rate_resp = rate_resp.ok_or(CodexAppServerStatusError::RateLimitsMissing)?;
    let account_resp = account_resp.unwrap_or(Value::Null);
    let config_resp = config_resp.unwrap_or(Value::Null);
    codex_app_server_status_from_values(&rate_resp, &account_resp, &config_resp)
}

fn build_app_server_request(id: i64, method: &str, params: Value) -> Value {
    json!({
        "jsonrpc": "2.0",
        "id": id,
        "method": method,
        "params": params,
    })
}

fn build_app_server_initialize_request(id: i64, client_name: &str, client_version: &str) -> Value {
    build_app_server_request(
        id,
        "initialize",
        json!({
            "clientInfo": {
                "name": client_name,
                "version": client_version,
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

pub fn codex_app_server_status_from_values(
    rate: &Value,
    account: &Value,
    config: &Value,
) -> Result<CodexAppServerStatus, CodexAppServerStatusError> {
    let rate_resp = serde_json::from_value::<AppRateLimitsResponse>(rate.clone())
        .map_err(CodexAppServerStatusError::ParseRateLimits)?;
    let account_resp =
        serde_json::from_value::<AppAccountResponse>(account.clone()).unwrap_or_default();
    let config_resp =
        serde_json::from_value::<AppConfigResponse>(config.clone()).unwrap_or_default();

    let total_limit_min = std::env::var("CODEX_PLAN_LIMIT_MINUTES")
        .ok()
        .and_then(|value| value.trim().parse::<i64>().ok())
        .filter(|value| *value > 0)
        .unwrap_or(300);
    let now = Local::now();
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

    Ok(CodexAppServerStatus {
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

pub fn parse_codex_app_server_request_id(value: &Value) -> Option<i64> {
    match value {
        Value::Number(number) => number.as_i64(),
        Value::String(value) => value.trim().parse::<i64>().ok(),
        _ => None,
    }
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

pub fn parse_prompt_segments_dual(clean: &str, raw: &str) -> Vec<PromptSegment> {
    let mut clean_lines: Vec<String> = clean.split('\n').map(str::to_owned).collect();
    let mut raw_lines: Vec<String> = raw.split('\n').map(str::to_owned).collect();
    if raw_lines.len() < clean_lines.len() {
        raw_lines.resize(clean_lines.len(), String::new());
    }
    if clean_lines.len() < raw_lines.len() {
        clean_lines.resize(raw_lines.len(), String::new());
    }
    let mut segments = Vec::with_capacity(8);
    let mut current: Option<PromptSegment> = None;
    for (clean_line, raw_line) in clean_lines.into_iter().zip(raw_lines.into_iter()) {
        let trimmed = clean_line.trim_start();
        if let Some(prompt) = trimmed.strip_prefix('›') {
            if let Some(segment) = current.take() {
                segments.push(segment);
            }
            current = Some(PromptSegment {
                prompt: prompt.trim().to_owned(),
                lines: Vec::new(),
                raw: Vec::new(),
            });
            continue;
        }
        if let Some(segment) = current.as_mut() {
            segment.lines.push(clean_line);
            segment.raw.push(raw_line);
        }
    }
    if let Some(segment) = current {
        segments.push(segment);
    }
    segments
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

pub fn parse_report_capture(
    clean: &str,
    raw: &str,
    prompt_index: usize,
    ansi: bool,
) -> ReportParseResult {
    let segments = parse_prompt_segments_dual(clean, raw);
    let report = if prompt_index < segments.len() {
        extract_report_lines_from_lines(
            &segments[prompt_index].raw,
            &segments[prompt_index].lines,
            ansi,
        )
    } else {
        String::new()
    };
    ReportParseResult { segments, report }
}

fn extract_report_lines_from_lines(
    raw_lines: &[String],
    clean_lines: &[String],
    ansi: bool,
) -> String {
    let max = raw_lines.len().min(clean_lines.len());
    struct Block {
        raw: Vec<String>,
        clean: Vec<String>,
    }
    let mut blocks: Vec<Block> = Vec::new();
    let mut current = Block { raw: Vec::new(), clean: Vec::new() };
    let mut in_report = false;
    let mut worked_line_raw = String::new();
    let mut worked_line_clean = String::new();
    for i in 0..max {
        let raw = raw_lines[i].trim_end_matches([' ', '\t']).to_owned();
        let clean = clean_lines[i].trim_end_matches([' ', '\t']).to_owned();
        let clean_core = clean.trim_start().to_owned();
        if clean_core.to_ascii_lowercase().contains("worked for") {
            worked_line_raw = raw.clone();
            worked_line_clean = clean.clone();
        }
        if clean_core.starts_with("• ") {
            in_report = true;
            current.raw.push(raw);
            current.clean.push(clean);
            continue;
        }
        if !in_report {
            continue;
        }
        if clean.trim().is_empty() {
            if !current.raw.is_empty() {
                blocks.push(current);
                current = Block { raw: Vec::new(), clean: Vec::new() };
            }
            in_report = false;
            continue;
        }
        if clean.starts_with("  ") {
            current.raw.push(raw);
            current.clean.push(clean);
            continue;
        }
        let core = clean.trim().to_owned();
        if core.starts_with('⚠')
            || core.starts_with("Tip:")
            || core.starts_with('›')
            || core.starts_with("• Starting MCP")
            || core.starts_with("• Starting")
        {
            if !current.raw.is_empty() {
                blocks.push(current);
            }
            current = Block { raw: Vec::new(), clean: Vec::new() };
            break;
        }
        current.raw.push(raw);
        current.clean.push(clean);
    }
    if !current.raw.is_empty() {
        blocks.push(current);
    }
    for block in blocks.into_iter().rev() {
        if block.raw.is_empty() || is_transient_report(&block.clean) {
            continue;
        }
        let mut out = if ansi { block.raw } else { block.clean };
        let worked_line = if ansi { worked_line_raw.clone() } else { worked_line_clean.clone() };
        while out.last().is_some_and(|line| line.trim().is_empty()) {
            out.pop();
        }
        if !worked_line.is_empty() && !out.iter().any(|line| line == &worked_line) {
            out.push(worked_line);
        }
        return out.join("\n");
    }
    String::new()
}

fn is_transient_report(lines: &[String]) -> bool {
    if lines.is_empty() {
        return true;
    }
    let head = lines[0].trim();
    head.starts_with("• Working")
        || head.contains("esc to interrupt")
        || head.starts_with("• Starting MCP")
}

#[cfg(test)]
mod tests {
    use super::{
        build_codex_app_server_status_input, codex_profile_fort_runtime_env,
        codex_profile_fort_session_paths, codex_tmux_session_name, parse_codex_app_server_status,
        parse_prompt_segments_dual, parse_report_capture,
    };
    use serde_json::json;
    use std::path::Path;

    #[test]
    fn codex_tmux_session_name_uses_profile_slug() {
        assert_eq!(codex_tmux_session_name("profile-delta"), "si-codex-pane-profile-delta");
    }

    #[test]
    fn codex_profile_fort_session_paths_are_under_codex_home() {
        let paths =
            codex_profile_fort_session_paths(Path::new("/tmp/home/.si/codex/profiles/cadma"));

        assert_eq!(paths.dir, Path::new("/tmp/home/.si/codex/profiles/cadma/fort"));
        assert_eq!(
            paths.session_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/session.json")
        );
        assert_eq!(
            paths.access_token_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/access.token")
        );
        assert_eq!(
            paths.refresh_token_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/refresh.token")
        );
        assert_eq!(
            paths.lock_path,
            Path::new("/tmp/home/.si/codex/profiles/cadma/fort/runtime.lock")
        );
    }

    #[test]
    fn codex_profile_fort_runtime_env_exports_token_paths() {
        let env = codex_profile_fort_runtime_env(Path::new("/tmp/home/.si/codex/profiles/cadma"));

        assert_eq!(
            env.get("FORT_TOKEN_PATH").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/cadma/fort/access.token")
        );
        assert_eq!(
            env.get("FORT_REFRESH_TOKEN_PATH").map(String::as_str),
            Some("/tmp/home/.si/codex/profiles/cadma/fort/refresh.token")
        );
    }

    #[test]
    fn app_server_status_input_uses_low_refresh_protocol_shape() {
        let payload = String::from_utf8(build_codex_app_server_status_input(
            "si-test",
            "v0.0.0",
            Some("/tmp/work".to_owned()),
        ))
        .expect("utf8");

        assert!(payload.contains("\"name\":\"si-test\""));
        assert!(payload.contains("\"account/rateLimits/read\""));
        assert!(payload.contains("\"account/read\""));
        assert!(payload.contains("\"refreshToken\":false"));
        assert!(payload.contains("\"config/read\""));
        assert!(payload.contains("\"includeLayers\":false"));
        assert!(payload.contains("\"cwd\":\"/tmp/work\""));
    }

    #[test]
    fn parse_app_server_status_uses_rate_account_and_config_shapes() {
        let raw = [
            json!({"id":1,"result":{"userAgent":"si-test/0.0.0"}}),
            json!({"id":2,"result":{"rateLimits":{"primary":{"usedPercent":20,"windowDurationMins":300,"resetsAt":1775448461},"secondary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1775660971}}}}),
            json!({"id":3,"result":{"account":{"type":"chatgpt","email":"agent@example.com","planType":"plus"},"requiresOpenaiAuth":false}}),
            json!({"id":4,"result":{"config":{"model":"gpt-5.4","model_reasoning_effort":"high"}}}),
        ]
        .into_iter()
        .map(|item| serde_json::to_string(&item).expect("line"))
        .collect::<Vec<_>>()
        .join("\n");

        let status = parse_codex_app_server_status(&raw).expect("status");
        assert_eq!(status.model.as_deref(), Some("gpt-5.4"));
        assert_eq!(status.reasoning_effort.as_deref(), Some("high"));
        assert_eq!(status.account_email.as_deref(), Some("agent@example.com"));
        assert_eq!(status.account_plan.as_deref(), Some("plus"));
        assert_eq!(status.five_hour_left_pct, Some(80.0));
        assert_eq!(status.weekly_left_pct, Some(90.0));
    }

    #[test]
    fn parse_prompt_segments_dual_pairs_clean_and_raw() {
        let clean = "› first\nline a\n› second\nline b";
        let raw = "› first\nraw a\n› second\nraw b";
        let parsed = parse_prompt_segments_dual(clean, raw);
        assert_eq!(parsed.len(), 2);
        assert_eq!(parsed[0].prompt, "first");
        assert_eq!(parsed[0].lines, vec!["line a".to_owned()]);
        assert_eq!(parsed[0].raw, vec!["raw a".to_owned()]);
        assert_eq!(parsed[1].prompt, "second");
    }

    #[test]
    fn parse_report_capture_extracts_report_block_for_prompt_index() {
        let clean = "› prompt\n• Did the thing\n  detail\n  worked for 42s\n";
        let raw = clean;
        let parsed = parse_report_capture(clean, raw, 0, false);
        assert_eq!(parsed.segments.len(), 1);
        assert!(parsed.report.contains("Did the thing"));
        assert!(parsed.report.contains("worked for 42s"));
    }
}
