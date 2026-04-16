use reqwest::blocking::Client;
use reqwest::header::{
    ACCEPT, AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderName, HeaderValue, USER_AGENT,
};
use serde::Serialize;
use serde_json::Value;
use si_rs_config::settings::{CloudflareAccountEntry, CloudflareSettings};
use std::collections::BTreeMap;
use std::time::Duration;
use url::Url;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct CloudflareContextListEntry {
    pub alias: String,
    pub name: String,
    pub account_id: String,
    pub default: String,
    pub prod_zone: String,
    pub staging_zone: String,
    pub dev_zone: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CloudflareContextOverrides {
    pub account: String,
    pub environment: String,
    pub zone_id: String,
    pub zone_name: String,
    pub base_url: String,
    pub account_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct CloudflareCurrentContext {
    pub account_alias: String,
    pub account_id: String,
    pub environment: String,
    pub zone_id: String,
    pub zone_name: String,
    pub base_url: String,
    pub source: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CloudflareAuthOverrides {
    pub account: String,
    pub environment: String,
    pub zone_id: String,
    pub zone_name: String,
    pub base_url: String,
    pub account_id: String,
    pub api_token: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CloudflareAuthRuntime {
    pub account_alias: String,
    pub account_id: String,
    pub environment: String,
    pub zone_id: String,
    pub zone_name: String,
    pub base_url: String,
    pub source: String,
    pub api_token: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct CloudflareAuthStatus {
    pub status: String,
    pub account_alias: String,
    pub account_id: String,
    pub environment: String,
    pub zone_id: String,
    pub zone_name: String,
    pub source: String,
    pub token_preview: String,
    pub base_url: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CloudflareAPIRequest {
    pub method: String,
    pub path: String,
    pub params: BTreeMap<String, String>,
    pub headers: BTreeMap<String, String>,
    pub raw_body: String,
    pub json_body: Option<Value>,
    pub content_type: String,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct CloudflareAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub success: bool,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub headers: BTreeMap<String, String>,
    pub body: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub list: Option<Vec<Value>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub messages: Option<Vec<Value>>,
}

pub fn list_contexts(settings: &CloudflareSettings) -> Vec<CloudflareContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, entry) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(CloudflareContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(entry.name.as_deref()),
            account_id: trim_or_empty(entry.account_id.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            prod_zone: dash_if_empty(entry.prod_zone_id.as_deref()),
            staging_zone: dash_if_empty(entry.staging_zone_id.as_deref()),
            dev_zone: dash_if_empty(entry.dev_zone_id.as_deref()),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[CloudflareContextListEntry]) -> String {
    if rows.is_empty() {
        return "no cloudflare accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "ACCOUNT", "PROD", "STAGING", "DEV", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.account_id).len());
        widths[3] = widths[3].max(or_dash(&row.prod_zone).len());
        widths[4] = widths[4].max(or_dash(&row.staging_zone).len());
        widths[5] = widths[5].max(or_dash(&row.dev_zone).len());
        widths[6] = widths[6].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.account_id),
            or_dash(&row.prod_zone),
            or_dash(&row.staging_zone),
            or_dash(&row.dev_zone),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &CloudflareSettings,
    env: &BTreeMap<String, String>,
    overrides: &CloudflareContextOverrides,
) -> Result<CloudflareCurrentContext, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let environment = resolve_environment(settings, env, &overrides.environment)?;
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        account.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("CLOUDFLARE_API_BASE_URL").map(String::as_str),
        Some("https://api.cloudflare.com/client/v4"),
    ])
    .to_owned();

    let (account_id, account_source) =
        resolve_account_id(&alias, &account, env, &overrides.account_id);
    let zone_name = first_non_empty(&[
        Some(overrides.zone_name.as_str()),
        account.default_zone_name.as_deref(),
        env_key(alias.as_str(), &account, "DEFAULT_ZONE_NAME", env).as_deref(),
    ])
    .to_owned();
    let (zone_id, zone_source) =
        resolve_zone_id(&alias, &account, env, &environment, &overrides.zone_id);
    let source = join_sources(&[account_source, zone_source]);

    Ok(CloudflareCurrentContext {
        account_alias: alias,
        account_id,
        environment,
        zone_id,
        zone_name,
        base_url,
        source,
    })
}

pub fn resolve_auth_runtime(
    settings: &CloudflareSettings,
    env: &BTreeMap<String, String>,
    overrides: &CloudflareAuthOverrides,
) -> Result<CloudflareAuthRuntime, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let environment = resolve_environment(settings, env, &overrides.environment)?;
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        account.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("CLOUDFLARE_API_BASE_URL").map(String::as_str),
        Some("https://api.cloudflare.com/client/v4"),
    ])
    .to_owned();

    let (account_id, account_source) =
        resolve_account_id(&alias, &account, env, &overrides.account_id);
    let zone_name = first_non_empty(&[
        Some(overrides.zone_name.as_str()),
        account.default_zone_name.as_deref(),
        env_key(alias.as_str(), &account, "DEFAULT_ZONE_NAME", env).as_deref(),
    ])
    .to_owned();
    let (zone_id, zone_source) =
        resolve_zone_id(&alias, &account, env, &environment, &overrides.zone_id);
    let (api_token, token_source) = resolve_api_token(&alias, &account, env, &overrides.api_token)?;
    let source = join_sources(&[account_source, zone_source, token_source]);

    Ok(CloudflareAuthRuntime {
        account_alias: alias,
        account_id,
        environment,
        zone_id,
        zone_name,
        base_url,
        source,
        api_token,
    })
}

pub fn verify_auth_status(runtime: &CloudflareAuthRuntime) -> Result<Value, String> {
    let url = format!("{}/user/tokens/verify", runtime.base_url.trim_end_matches('/'));
    let response = reqwest::blocking::Client::new()
        .get(url)
        .header("Authorization", format!("Bearer {}", runtime.api_token))
        .send()
        .map_err(|err| format!("cloudflare auth verification request failed: {err}"))?;
    let status = response.status();
    let body = response
        .text()
        .map_err(|err| format!("read cloudflare auth verification response: {err}"))?;
    if !status.is_success() {
        let detail = body.trim();
        return Err(if detail.is_empty() {
            format!("cloudflare auth verification failed with status {}", status.as_u16())
        } else {
            format!(
                "cloudflare auth verification failed with status {}: {}",
                status.as_u16(),
                detail
            )
        });
    }
    serde_json::from_str(&body)
        .map_err(|err| format!("decode cloudflare auth verification response: {err}"))
}

pub fn execute_api_request(
    runtime: &CloudflareAuthRuntime,
    request: &CloudflareAPIRequest,
) -> Result<CloudflareAPIResponse, String> {
    let method = if request.method.trim().is_empty() { "GET" } else { request.method.trim() };
    let url = resolve_api_url(&runtime.base_url, &request.path, &request.params)?;
    let client = Client::builder()
        .timeout(Duration::from_secs(45))
        .build()
        .map_err(|err| format!("build cloudflare http client: {err}"))?;
    let mut headers = HeaderMap::new();
    headers.insert(
        AUTHORIZATION,
        HeaderValue::from_str(&format!("Bearer {}", runtime.api_token.trim()))
            .map_err(|err| format!("build cloudflare auth header: {err}"))?,
    );
    headers.insert(ACCEPT, HeaderValue::from_static("application/json"));
    headers.insert(USER_AGENT, HeaderValue::from_static("si-rs"));
    for (key, value) in &request.headers {
        let key = key.trim();
        if key.is_empty() {
            continue;
        }
        let name = HeaderName::from_bytes(key.as_bytes())
            .map_err(|err| format!("build cloudflare header name {key:?}: {err}"))?;
        headers.insert(
            name,
            HeaderValue::from_str(value.trim())
                .map_err(|err| format!("build cloudflare header value for {key:?}: {err}"))?,
        );
    }

    let body = if !request.raw_body.trim().is_empty() {
        Some(request.raw_body.trim().as_bytes().to_vec())
    } else {
        request
            .json_body
            .as_ref()
            .map(serde_json::to_vec)
            .transpose()
            .map_err(|err| format!("encode cloudflare request body: {err}"))?
    };
    if let Some(value) = body.as_ref() {
        let content_type = if request.content_type.trim().is_empty() {
            "application/json"
        } else {
            request.content_type.trim()
        };
        headers.insert(
            CONTENT_TYPE,
            HeaderValue::from_str(content_type)
                .map_err(|err| format!("build cloudflare content-type header: {err}"))?,
        );
        let method = reqwest::Method::from_bytes(method.as_bytes())
            .map_err(|err| format!("build cloudflare http method: {err}"))?;
        let response = client
            .request(method, &url)
            .headers(headers)
            .body(value.clone())
            .send()
            .map_err(|err| format!("cloudflare request failed: {err}"))?;
        return normalize_api_response(response);
    }

    let method = reqwest::Method::from_bytes(method.as_bytes())
        .map_err(|err| format!("build cloudflare http method: {err}"))?;
    let response = client
        .request(method, &url)
        .headers(headers)
        .send()
        .map_err(|err| format!("cloudflare request failed: {err}"))?;
    normalize_api_response(response)
}

pub fn render_api_response_text(response: &CloudflareAPIResponse, raw: bool) -> String {
    if raw {
        if response.body.trim().is_empty() {
            return "{}\n".to_owned();
        }
        return ensure_trailing_newline(response.body.clone());
    }
    let mut out =
        format!("Cloudflare API: {} ({})\n", response.status.trim(), response.status_code);
    if !response.request_id.trim().is_empty() {
        out.push_str(&format!("Request ID: {}\n", response.request_id.trim()));
    }
    if let Some(data) = &response.data
        && let Ok(pretty) = serde_json::to_string_pretty(data)
    {
        out.push_str(&pretty);
        out.push('\n');
        return out;
    }
    if let Some(list) = &response.list
        && let Ok(pretty) = serde_json::to_string_pretty(list)
    {
        out.push_str(&pretty);
        out.push('\n');
        return out;
    }
    if !response.body.trim().is_empty() {
        out.push_str(response.body.trim());
        out.push('\n');
    }
    out
}

impl From<&CloudflareAuthRuntime> for CloudflareAuthStatus {
    fn from(value: &CloudflareAuthRuntime) -> Self {
        Self {
            status: "ready".to_owned(),
            account_alias: value.account_alias.clone(),
            account_id: value.account_id.clone(),
            environment: value.environment.clone(),
            zone_id: value.zone_id.clone(),
            zone_name: value.zone_name.clone(),
            source: value.source.clone(),
            token_preview: preview_api_token(&value.api_token),
            base_url: value.base_url.clone(),
        }
    }
}

fn resolve_account_selection(
    settings: &CloudflareSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, CloudflareAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("CLOUDFLARE_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), CloudflareAccountEntry::default());
    }
    if let Some(entry) = settings.accounts.get(&selected) {
        return (selected, entry.clone());
    }
    (selected, CloudflareAccountEntry::default())
}

fn normalize_api_response(
    response: reqwest::blocking::Response,
) -> Result<CloudflareAPIResponse, String> {
    let status = response.status();
    let status_text = status.to_string();
    let headers = response.headers().clone();
    let request_id = first_header(&headers, &["cf-ray", "x-request-id"]);
    let body = response.text().map_err(|err| format!("read cloudflare response body: {err}"))?;
    let mut payload = CloudflareAPIResponse {
        status_code: status.as_u16(),
        status: status_text,
        request_id,
        success: status.is_success(),
        headers: normalize_headers(&headers),
        body: body.trim().to_owned(),
        data: None,
        list: None,
        messages: None,
    };
    if let Ok(value) = serde_json::from_str::<Value>(body.trim())
        && let Some(object) = value.as_object()
    {
        if let Some(success) = object.get("success").and_then(Value::as_bool) {
            payload.success = success;
        }
        if let Some(messages) = object.get("messages").and_then(Value::as_array) {
            payload.messages = Some(messages.clone());
        }
        if let Some(result) = object.get("result") {
            if let Some(list) = result.as_array() {
                payload.list = Some(list.clone());
            } else {
                payload.data = Some(result.clone());
            }
        } else {
            payload.data = Some(value);
        }
    }
    if !status.is_success() || !payload.success {
        return Err(format!(
            "cloudflare request failed: status={} request_id={} body={}",
            payload.status_code,
            if payload.request_id.is_empty() { "-" } else { payload.request_id.as_str() },
            payload.body.trim()
        ));
    }
    Ok(payload)
}

fn normalize_headers(headers: &HeaderMap) -> BTreeMap<String, String> {
    let mut normalized = BTreeMap::new();
    for (key, value) in headers {
        let value = value.to_str().unwrap_or_default().trim().to_owned();
        normalized.insert(key.as_str().to_owned(), value);
    }
    normalized
}

fn resolve_api_url(
    base_url: &str,
    path: &str,
    params: &BTreeMap<String, String>,
) -> Result<String, String> {
    let path = path.trim();
    if path.is_empty() {
        return Err("request path is required".to_owned());
    }
    let mut url = if path.starts_with("http://") || path.starts_with("https://") {
        Url::parse(path).map_err(|err| format!("parse cloudflare url {:?}: {err}", path))?
    } else {
        let mut base = Url::parse(base_url)
            .map_err(|err| format!("parse cloudflare base url {:?}: {err}", base_url))?;
        let base_path = base.path().trim_end_matches('/');
        let relative_path = path.trim_start_matches('/');
        let resolved_path = if base_path.is_empty() || base_path == "/" {
            format!("/{relative_path}")
        } else if relative_path.is_empty() {
            base_path.to_owned()
        } else {
            format!("{base_path}/{relative_path}")
        };
        base.set_path(&resolved_path);
        base
    };
    if params.iter().any(|(key, value)| !key.trim().is_empty() && !value.trim().is_empty()) {
        let mut pairs = url.query_pairs_mut();
        for (key, value) in params {
            let key = key.trim();
            let value = value.trim();
            if key.is_empty() || value.is_empty() {
                continue;
            }
            pairs.append_pair(key, value);
        }
    }
    Ok(url.to_string())
}

fn first_header(headers: &HeaderMap, names: &[&str]) -> String {
    for name in names {
        if let Some(value) = headers.get(*name)
            && let Ok(text) = value.to_str()
        {
            let text = text.trim();
            if !text.is_empty() {
                return text.to_owned();
            }
        }
    }
    String::new()
}

fn ensure_trailing_newline(mut value: String) -> String {
    if !value.ends_with('\n') {
        value.push('\n');
    }
    value
}

fn resolve_environment(
    settings: &CloudflareSettings,
    env: &BTreeMap<String, String>,
    override_env: &str,
) -> Result<String, String> {
    let raw = first_non_empty(&[
        Some(override_env),
        settings.default_env.as_deref(),
        env.get("CLOUDFLARE_DEFAULT_ENV").map(String::as_str),
        Some("prod"),
    ]);
    match raw.trim().to_ascii_lowercase().as_str() {
        "prod" => Ok("prod".to_owned()),
        "staging" => Ok("staging".to_owned()),
        "dev" => Ok("dev".to_owned()),
        "test" => Err("environment `test` is not supported; use `staging` or `dev`".to_owned()),
        value => Err(format!("invalid environment {value:?} (expected prod|staging|dev)")),
    }
}

fn resolve_account_id(
    alias: &str,
    account: &CloudflareAccountEntry,
    env: &BTreeMap<String, String>,
    override_account_id: &str,
) -> (String, String) {
    if !override_account_id.trim().is_empty() {
        return (override_account_id.trim().to_owned(), "flag:--account-id".to_owned());
    }
    if let Some(value) = account.account_id.as_deref().map(str::trim).filter(|v| !v.is_empty()) {
        return (value.to_owned(), "settings.account_id".to_owned());
    }
    if let Some(reference) =
        account.account_id_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
        && let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
    {
        return (value.to_owned(), format!("env:{reference}"));
    }
    if let Some(value) = env_key(alias, account, "ACCOUNT_ID", env) {
        return (value, format!("env:{}ACCOUNT_ID", env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("CLOUDFLARE_ACCOUNT_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "env:CLOUDFLARE_ACCOUNT_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_zone_id(
    alias: &str,
    account: &CloudflareAccountEntry,
    env: &BTreeMap<String, String>,
    environment: &str,
    override_zone_id: &str,
) -> (String, String) {
    if !override_zone_id.trim().is_empty() {
        return (override_zone_id.trim().to_owned(), "flag:--zone-id".to_owned());
    }
    let from_settings = match environment {
        "prod" => account.prod_zone_id.as_deref(),
        "staging" => account.staging_zone_id.as_deref(),
        "dev" => account.dev_zone_id.as_deref(),
        _ => None,
    }
    .map(str::trim)
    .filter(|v| !v.is_empty());
    if let Some(value) = from_settings {
        let source = match environment {
            "prod" => "settings.prod_zone_id",
            "staging" => "settings.staging_zone_id",
            "dev" => "settings.dev_zone_id",
            _ => "",
        };
        return (value.to_owned(), source.to_owned());
    }
    if let Some(value) = account.default_zone_id.as_deref().map(str::trim).filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "settings.default_zone_id".to_owned());
    }
    if let Some(value) =
        env.get("CLOUDFLARE_ZONE_ID").map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "env:CLOUDFLARE_ZONE_ID".to_owned());
    }
    let key = match environment {
        "prod" => "PROD_ZONE_ID",
        "staging" => "STAGING_ZONE_ID",
        "dev" => "DEV_ZONE_ID",
        _ => "",
    };
    if !key.is_empty()
        && let Some(value) = env_key(alias, account, key, env)
    {
        return (value, format!("env:{}{}", env_prefix(alias, account), key));
    }
    (String::new(), String::new())
}

fn resolve_api_token(
    alias: &str,
    account: &CloudflareAccountEntry,
    env: &BTreeMap<String, String>,
    override_api_token: &str,
) -> Result<(String, String), String> {
    if !override_api_token.trim().is_empty() {
        return Ok((override_api_token.trim().to_owned(), "flag:--api-token".to_owned()));
    }
    if let Some(reference) =
        account.api_token_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
        && let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
    {
        return Ok((value.to_owned(), format!("env:{reference}")));
    }
    if let Some(value) = env_key(alias, account, "API_TOKEN", env) {
        return Ok((value, format!("env:{}API_TOKEN", env_prefix(alias, account))));
    }
    if let Some(value) =
        env.get("CLOUDFLARE_API_TOKEN").map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
    {
        return Ok((value.to_owned(), "env:CLOUDFLARE_API_TOKEN".to_owned()));
    }
    Err("cloudflare api token not found (set --api-token, CLOUDFLARE_<ACCOUNT>_API_TOKEN, or CLOUDFLARE_API_TOKEN)".to_owned())
}

fn preview_api_token(value: &str) -> String {
    let value = value.trim();
    if value.is_empty() {
        return String::new();
    }
    let prefix: String = value.chars().take(6).collect();
    format!("{prefix}...")
}

fn env_prefix(alias: &str, account: &CloudflareAccountEntry) -> String {
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
    if slug.is_empty() { String::new() } else { format!("CLOUDFLARE_{slug}_") }
}

fn env_key(
    alias: &str,
    account: &CloudflareAccountEntry,
    key: &str,
    env: &BTreeMap<String, String>,
) -> Option<String> {
    let env_key = format!("{}{}", env_prefix(alias, account), key);
    env.get(&env_key)
        .map(String::as_str)
        .map(str::trim)
        .filter(|v| !v.is_empty())
        .map(ToOwned::to_owned)
}

fn trim_or_empty(value: Option<&str>) -> String {
    value.unwrap_or_default().trim().to_owned()
}

fn dash_if_empty(value: Option<&str>) -> String {
    let trimmed = value.unwrap_or_default().trim();
    if trimmed.is_empty() { "-".to_owned() } else { trimmed.to_owned() }
}

fn first_non_empty<'a>(values: &[Option<&'a str>]) -> &'a str {
    for value in values.iter().flatten() {
        let trimmed = value.trim();
        if !trimmed.is_empty() {
            return trimmed;
        }
    }
    ""
}

fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() { "-" } else { value }
}

fn bool_string(value: bool) -> String {
    if value { "true".to_owned() } else { "false".to_owned() }
}

fn format_row<const N: usize>(cols: &[&str; N], widths: &[usize; N]) -> String {
    let mut line = String::new();
    for (idx, col) in cols.iter().enumerate() {
        if idx > 0 {
            line.push_str("  ");
        }
        line.push_str(&format!("{col:<width$}", width = widths[idx]));
    }
    line.push('\n');
    line
}

fn join_sources(values: &[String]) -> String {
    values
        .iter()
        .map(|value| value.trim())
        .filter(|value| !value.is_empty())
        .collect::<Vec<_>>()
        .join(",")
}

#[cfg(test)]
mod tests {
    use super::*;
    use si_rs_config::settings::{CloudflareAccountEntry, CloudflareSettings};

    #[test]
    fn list_contexts_sorts_and_marks_default() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "beta".to_owned(),
            CloudflareAccountEntry {
                account_id: Some("acc_beta".to_owned()),
                prod_zone_id: Some("zone_beta_prod".to_owned()),
                ..CloudflareAccountEntry::default()
            },
        );
        accounts.insert(
            "alpha".to_owned(),
            CloudflareAccountEntry {
                account_id: Some("acc_alpha".to_owned()),
                staging_zone_id: Some("zone_alpha_stg".to_owned()),
                ..CloudflareAccountEntry::default()
            },
        );
        let settings = CloudflareSettings {
            default_account: Some("beta".to_owned()),
            accounts,
            ..CloudflareSettings::default()
        };
        let rows = list_contexts(&settings);
        assert_eq!(rows[0].alias, "alpha");
        assert_eq!(rows[1].default, "true");
    }

    #[test]
    fn resolve_current_context_uses_settings_and_env() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            CloudflareAccountEntry {
                account_id: Some("acc_core".to_owned()),
                default_zone_name: Some("example.com".to_owned()),
                prod_zone_id: Some("zone_prod".to_owned()),
                ..CloudflareAccountEntry::default()
            },
        );
        let settings = CloudflareSettings {
            default_account: Some("core".to_owned()),
            default_env: Some("prod".to_owned()),
            accounts,
            ..CloudflareSettings::default()
        };
        let env = BTreeMap::new();
        let current =
            resolve_current_context(&settings, &env, &CloudflareContextOverrides::default())
                .unwrap();
        assert_eq!(current.account_alias, "core");
        assert_eq!(current.account_id, "acc_core");
        assert_eq!(current.environment, "prod");
        assert_eq!(current.zone_id, "zone_prod");
        assert_eq!(current.zone_name, "example.com");
        assert_eq!(current.source, "settings.account_id,settings.prod_zone_id");
    }

    #[test]
    fn resolve_auth_runtime_uses_flag_token_override() {
        let runtime = resolve_auth_runtime(
            &CloudflareSettings::default(),
            &BTreeMap::new(),
            &CloudflareAuthOverrides {
                account: "core".to_owned(),
                environment: "staging".to_owned(),
                zone_id: "zone_staging".to_owned(),
                account_id: "acc_core".to_owned(),
                api_token: "cf-token-123".to_owned(),
                base_url: "https://api.example.invalid".to_owned(),
                ..CloudflareAuthOverrides::default()
            },
        )
        .expect("auth runtime");

        assert_eq!(runtime.account_alias, "core");
        assert_eq!(runtime.environment, "staging");
        assert_eq!(runtime.zone_id, "zone_staging");
        assert_eq!(runtime.account_id, "acc_core");
        assert_eq!(runtime.base_url, "https://api.example.invalid");
        assert_eq!(runtime.source, "flag:--account-id,flag:--zone-id,flag:--api-token");
        let status = CloudflareAuthStatus::from(&runtime);
        assert_eq!(status.token_preview, "cf-tok...");
    }

    #[test]
    fn resolve_api_url_preserves_base_path_for_leading_slash_paths() {
        let url = resolve_api_url(
            "https://api.cloudflare.com/client/v4",
            "/zones/abc/dns_records",
            &BTreeMap::new(),
        )
        .expect("url");

        assert_eq!(url, "https://api.cloudflare.com/client/v4/zones/abc/dns_records");
    }

    #[test]
    fn resolve_api_url_preserves_base_path_for_relative_paths() {
        let url = resolve_api_url(
            "https://api.cloudflare.com/client/v4",
            "accounts/abc/r2/buckets",
            &BTreeMap::new(),
        )
        .expect("url");

        assert_eq!(url, "https://api.cloudflare.com/client/v4/accounts/abc/r2/buckets");
    }

    #[test]
    fn resolve_api_url_handles_trailing_base_slash_and_query_params() {
        let params = BTreeMap::from([
            ("page".to_owned(), "2".to_owned()),
            ("per_page".to_owned(), "50".to_owned()),
        ]);
        let url = resolve_api_url(
            "https://api.cloudflare.com/client/v4/",
            "/zones/abc/dns_records",
            &params,
        )
        .expect("url");

        assert_eq!(
            url,
            "https://api.cloudflare.com/client/v4/zones/abc/dns_records?page=2&per_page=50"
        );
    }

    #[test]
    fn resolve_api_url_passes_through_absolute_urls() {
        let params = BTreeMap::from([("page".to_owned(), "1".to_owned())]);
        let url = resolve_api_url(
            "https://api.cloudflare.com/client/v4",
            "https://api.cloudflare.com/client/v4/certificates",
            &params,
        )
        .expect("url");

        assert_eq!(url, "https://api.cloudflare.com/client/v4/certificates?page=1");
    }
}
