use reqwest::Method;
use reqwest::blocking::Client;
use serde::Serialize;
use serde_json::Value;
use si_rs_config::settings::{GCPAccountEntry, GCPSettings};
use std::collections::BTreeMap;
use std::time::Duration;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GCPContextListEntry {
    pub alias: String,
    pub name: String,
    pub default: String,
    pub project_id: String,
    pub project_id_env: String,
    pub token_env: String,
    pub api_key_env: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GCPAuthOverrides {
    pub account: String,
    pub environment: String,
    pub project_id: String,
    pub base_url: String,
    pub access_token: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GCPCurrentContext {
    pub account_alias: String,
    pub environment: String,
    pub project_id: String,
    pub base_url: String,
    pub source: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GCPAuthStatus {
    pub account_alias: String,
    pub environment: String,
    pub project_id: String,
    pub base_url: String,
    pub source: String,
    pub token_preview: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GCPRuntime {
    pub account_alias: String,
    pub environment: String,
    pub project_id: String,
    pub base_url: String,
    pub access_token: String,
    pub source: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct GCPAPIRequest {
    pub method: String,
    pub path: String,
    pub params: BTreeMap<String, String>,
    pub headers: BTreeMap<String, String>,
    pub json_body: Option<Value>,
    pub raw_body: String,
    pub content_type: String,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct GCPAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub content_type: String,
    pub headers: BTreeMap<String, String>,
    pub body: String,
    pub data: Option<Value>,
}

pub fn list_contexts(settings: &GCPSettings) -> Vec<GCPContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, account) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(GCPContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(account.name.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            project_id: trim_or_empty(account.project_id.as_deref()),
            project_id_env: trim_or_empty(account.project_id_env.as_deref()),
            token_env: trim_or_empty(account.access_token_env.as_deref()),
            api_key_env: trim_or_empty(account.api_key_env.as_deref()),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[GCPContextListEntry]) -> String {
    if rows.is_empty() {
        return "no gcp accounts configured in settings\n".to_owned();
    }
    let headers =
        ["ALIAS", "DEFAULT", "PROJECT ID", "PROJECT ENV", "TOKEN ENV", "API KEY ENV", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.project_id).len());
        widths[3] = widths[3].max(or_dash(&row.project_id_env).len());
        widths[4] = widths[4].max(or_dash(&row.token_env).len());
        widths[5] = widths[5].max(or_dash(&row.api_key_env).len());
        widths[6] = widths[6].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.project_id),
            or_dash(&row.project_id_env),
            or_dash(&row.token_env),
            or_dash(&row.api_key_env),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &GCPSettings,
    env: &BTreeMap<String, String>,
) -> Result<GCPCurrentContext, String> {
    let runtime = resolve_runtime_context(settings, env, &GCPAuthOverrides::default(), false)?;
    Ok(GCPCurrentContext {
        account_alias: runtime.account_alias,
        environment: runtime.environment,
        project_id: runtime.project_id,
        base_url: runtime.base_url,
        source: runtime.source,
    })
}

pub fn resolve_auth_status(
    settings: &GCPSettings,
    env: &BTreeMap<String, String>,
    overrides: &GCPAuthOverrides,
) -> Result<GCPAuthStatus, String> {
    let runtime = resolve_runtime_context(settings, env, overrides, true)?;
    Ok(GCPAuthStatus {
        account_alias: runtime.account_alias,
        environment: runtime.environment,
        project_id: runtime.project_id,
        base_url: runtime.base_url,
        source: runtime.source,
        token_preview: preview_secret(&runtime.access_token),
    })
}

struct GCPRuntimeContext {
    account_alias: String,
    environment: String,
    project_id: String,
    base_url: String,
    access_token: String,
    source: String,
}

pub fn resolve_runtime(
    settings: &GCPSettings,
    env: &BTreeMap<String, String>,
    overrides: &GCPAuthOverrides,
    require_token: bool,
) -> Result<GCPRuntime, String> {
    let runtime = resolve_runtime_context(settings, env, overrides, require_token)?;
    Ok(GCPRuntime {
        account_alias: runtime.account_alias,
        environment: runtime.environment,
        project_id: runtime.project_id,
        base_url: runtime.base_url,
        access_token: runtime.access_token,
        source: runtime.source,
    })
}

pub fn execute_api_request(
    runtime: &GCPRuntime,
    request: &GCPAPIRequest,
) -> Result<GCPAPIResponse, String> {
    let method = Method::from_bytes(request.method.trim().as_bytes())
        .map_err(|err| format!("invalid gcp method {:?}: {err}", request.method))?;
    let path = normalize_path(&request.path);
    let url = format!("{}{}", runtime.base_url.trim_end_matches('/'), path);
    let client = Client::builder()
        .timeout(Duration::from_secs(60))
        .build()
        .map_err(|err| format!("failed to build gcp client: {err}"))?;
    let mut builder = client.request(method, url).header("User-Agent", "si-rs-provider-gcp/1.0");
    if !runtime.access_token.trim().is_empty() {
        builder = builder.bearer_auth(runtime.access_token.trim());
    }
    if !request.params.is_empty() {
        builder = builder.query(&request.params);
    }
    for (key, value) in &request.headers {
        let key = key.trim();
        if key.is_empty() {
            continue;
        }
        builder = builder.header(key, value.trim());
    }
    if let Some(body) = &request.json_body {
        builder = builder.json(body);
    } else if !request.raw_body.trim().is_empty() {
        let content_type = if request.content_type.trim().is_empty() {
            "application/json"
        } else {
            request.content_type.trim()
        };
        builder = builder
            .header(reqwest::header::CONTENT_TYPE, content_type)
            .body(request.raw_body.clone());
    }
    let response = builder.send().map_err(|err| format!("gcp request failed: {err}"))?;
    normalize_api_response(response)
}

fn normalize_path(path: &str) -> String {
    let trimmed = path.trim();
    if trimmed.is_empty() {
        "/".to_owned()
    } else if trimmed.starts_with('/') {
        trimmed.to_owned()
    } else {
        format!("/{trimmed}")
    }
}

fn normalize_api_response(response: reqwest::blocking::Response) -> Result<GCPAPIResponse, String> {
    let status = response.status();
    let headers = response.headers().clone();
    let request_id = headers
        .get("x-request-id")
        .or_else(|| headers.get("x-goog-request-id"))
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .trim()
        .to_owned();
    let content_type = headers
        .get(reqwest::header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .trim()
        .to_owned();
    let response_headers = headers
        .iter()
        .filter_map(|(key, value)| {
            Some((key.as_str().to_owned(), value.to_str().ok()?.trim().to_owned()))
        })
        .collect::<BTreeMap<_, _>>();
    let bytes =
        response.bytes().map_err(|err| format!("failed to read gcp response body: {err}"))?;
    let body = String::from_utf8_lossy(&bytes).into_owned();
    if !status.is_success() {
        let mut message = format!(
            "gcp request failed: {} {}",
            status.as_u16(),
            status.canonical_reason().unwrap_or_default().trim()
        );
        if !request_id.is_empty() {
            message.push_str(&format!(" [request_id={request_id}]"));
        }
        if !body.trim().is_empty() {
            message.push_str(": ");
            message.push_str(body.trim());
        }
        return Err(message);
    }
    let data = if content_type.contains("json")
        || body.trim_start().starts_with('{')
        || body.trim_start().starts_with('[')
    {
        serde_json::from_slice(&bytes).ok()
    } else {
        None
    };
    Ok(GCPAPIResponse {
        status_code: status.as_u16(),
        status: status.canonical_reason().unwrap_or_default().trim().to_owned(),
        request_id,
        content_type,
        headers: response_headers,
        body,
        data,
    })
}

fn resolve_runtime_context(
    settings: &GCPSettings,
    env: &BTreeMap<String, String>,
    overrides: &GCPAuthOverrides,
    require_token: bool,
) -> Result<GCPRuntimeContext, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let environment = resolve_environment(settings, env, &overrides.environment)?;
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        account.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("GCP_API_BASE_URL").map(String::as_str),
        Some("https://serviceusage.googleapis.com"),
    ])
    .trim_end_matches('/')
    .to_owned();

    let (project_id, project_source) =
        resolve_project_id(&alias, &account, env, &overrides.project_id);
    if project_id.trim().is_empty() {
        return Err(
            "gcp project id not found (set --project, GCP_PROJECT_ID, or account project settings)"
                .to_owned(),
        );
    }

    let (access_token, token_source) =
        resolve_access_token(&alias, &account, env, &overrides.access_token);
    if require_token && access_token.trim().is_empty() {
        let prefix = account_env_prefix(&alias, &account);
        let hint = if prefix.is_empty() { "GCP_<ACCOUNT>_".to_owned() } else { prefix };
        return Err(format!(
            "gcp access token not found (set --access-token, {hint}ACCESS_TOKEN, or GOOGLE_OAUTH_ACCESS_TOKEN)"
        ));
    }

    Ok(GCPRuntimeContext {
        account_alias: alias,
        environment,
        project_id,
        base_url,
        access_token,
        source: join_sources(&[project_source, token_source]),
    })
}

fn resolve_account_selection(
    settings: &GCPSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, GCPAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("GCP_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), GCPAccountEntry::default());
    }
    if let Some(account) = settings.accounts.get(&selected) {
        return (selected, account.clone());
    }
    (selected, GCPAccountEntry::default())
}

fn resolve_environment(
    settings: &GCPSettings,
    env: &BTreeMap<String, String>,
    override_environment: &str,
) -> Result<String, String> {
    let raw = first_non_empty(&[
        Some(override_environment),
        settings.default_env.as_deref(),
        env.get("GCP_DEFAULT_ENV").map(String::as_str),
        Some("prod"),
    ]);
    match raw.trim().to_ascii_lowercase().as_str() {
        "prod" => Ok("prod".to_owned()),
        "staging" => Ok("staging".to_owned()),
        "dev" => Ok("dev".to_owned()),
        _ => Err(format!("invalid environment {:?} (expected prod|staging|dev)", raw.trim())),
    }
}

fn resolve_project_id(
    alias: &str,
    account: &GCPAccountEntry,
    env: &BTreeMap<String, String>,
    override_project_id: &str,
) -> (String, String) {
    if !override_project_id.trim().is_empty() {
        return (override_project_id.trim().to_owned(), "flag:--project".to_owned());
    }
    if let Some(value) =
        account.project_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.project_id".to_owned());
    }
    if let Some(reference) =
        account.project_id_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = account_env(alias, account, "PROJECT_ID", env) {
        return (value, format!("env:{}PROJECT_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("GCP_PROJECT_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GCP_PROJECT_ID".to_owned());
    }
    if let Some(value) = env
        .get("GOOGLE_CLOUD_PROJECT")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GOOGLE_CLOUD_PROJECT".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_access_token(
    alias: &str,
    account: &GCPAccountEntry,
    env: &BTreeMap<String, String>,
    override_access_token: &str,
) -> (String, String) {
    if !override_access_token.trim().is_empty() {
        return (override_access_token.trim().to_owned(), "flag:--access-token".to_owned());
    }
    if let Some(reference) =
        account.access_token_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = account_env(alias, account, "ACCESS_TOKEN", env) {
        return (value, format!("env:{}ACCESS_TOKEN", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("GOOGLE_OAUTH_ACCESS_TOKEN")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GOOGLE_OAUTH_ACCESS_TOKEN".to_owned());
    }
    if let Some(value) = env
        .get("GCP_ACCESS_TOKEN")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GCP_ACCESS_TOKEN".to_owned());
    }
    (String::new(), String::new())
}

fn preview_secret(value: &str) -> String {
    let value = value.trim();
    if value.is_empty() {
        return "-".to_owned();
    }
    if value.len() <= 6 {
        return "*".repeat(value.len());
    }
    format!("{}{}{}", &value[..3], "*".repeat(value.len() - 6), &value[value.len() - 3..])
}

fn account_env_prefix(alias: &str, account: &GCPAccountEntry) -> String {
    if let Some(prefix) = account.vault_prefix.as_deref() {
        let trimmed = prefix.trim();
        if !trimmed.is_empty() {
            let upper = trimmed.to_ascii_uppercase();
            return if upper.ends_with('_') { upper } else { format!("{upper}_") };
        }
    }
    let alias = alias.trim();
    if alias.is_empty() {
        return String::new();
    }
    let mut slug = String::new();
    let mut last_underscore = false;
    for ch in alias.chars() {
        if ch.is_ascii_alphanumeric() {
            slug.push(ch.to_ascii_uppercase());
            last_underscore = false;
        } else if !last_underscore {
            slug.push('_');
            last_underscore = true;
        }
    }
    let slug = slug.trim_matches('_');
    if slug.is_empty() { String::new() } else { format!("GCP_{slug}_") }
}

fn account_env(
    alias: &str,
    account: &GCPAccountEntry,
    key: &str,
    env: &BTreeMap<String, String>,
) -> Option<String> {
    let prefix = account_env_prefix(alias, account);
    if prefix.is_empty() {
        return None;
    }
    env.get(&(prefix + key))
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
}

fn first_non_empty<'a>(values: &[Option<&'a str>]) -> &'a str {
    values
        .iter()
        .filter_map(|value| *value)
        .map(str::trim)
        .find(|value| !value.is_empty())
        .unwrap_or("")
}

fn join_sources(parts: &[String]) -> String {
    parts
        .iter()
        .filter_map(|part| {
            let trimmed = part.trim();
            if trimmed.is_empty() { None } else { Some(trimmed) }
        })
        .collect::<Vec<_>>()
        .join(",")
}

fn trim_or_empty(value: Option<&str>) -> String {
    value.unwrap_or_default().trim().to_owned()
}

fn bool_string(value: bool) -> String {
    if value { "true".to_owned() } else { "false".to_owned() }
}

fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() { "-" } else { value }
}

fn format_row<const N: usize>(columns: &[&str; N], widths: &[usize; N]) -> String {
    let mut row = String::new();
    for (index, column) in columns.iter().enumerate() {
        if index > 0 {
            row.push_str("  ");
        }
        row.push_str(&format!("{column:<width$}", width = widths[index]));
    }
    row.push('\n');
    row
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn list_contexts_marks_default_account() {
        let mut settings =
            GCPSettings { default_account: Some("core".to_owned()), ..GCPSettings::default() };
        settings.accounts.insert(
            "core".to_owned(),
            GCPAccountEntry {
                project_id: Some("proj_core".to_owned()),
                access_token_env: Some("CORE_GCP_ACCESS_TOKEN".to_owned()),
                ..GCPAccountEntry::default()
            },
        );
        let rows = list_contexts(&settings);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].default, "true");
    }

    #[test]
    fn auth_status_uses_env_sources() {
        let mut settings = GCPSettings {
            default_account: Some("core".to_owned()),
            default_env: Some("prod".to_owned()),
            ..GCPSettings::default()
        };
        settings.accounts.insert(
            "core".to_owned(),
            GCPAccountEntry {
                project_id_env: Some("CORE_PROJECT".to_owned()),
                access_token_env: Some("CORE_TOKEN".to_owned()),
                ..GCPAccountEntry::default()
            },
        );
        let env = BTreeMap::from([
            ("CORE_PROJECT".to_owned(), "proj_core".to_owned()),
            ("CORE_TOKEN".to_owned(), "ya29.token".to_owned()),
        ]);
        let status =
            resolve_auth_status(&settings, &env, &GCPAuthOverrides::default()).expect("status");
        assert_eq!(status.account_alias, "core");
        assert_eq!(status.environment, "prod");
        assert_eq!(status.source, "env:CORE_PROJECT,env:CORE_TOKEN");
        assert_eq!(status.token_preview, "ya2****ken");
    }
}
