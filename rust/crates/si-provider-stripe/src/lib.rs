use reqwest::blocking::Client;
use reqwest::header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue, USER_AGENT};
use serde::Serialize;
use serde_json::Value;
use si_rs_config::settings::{StripeAccountEntry, StripeSettings};
use std::collections::BTreeMap;
use std::time::Duration;
use url::Url;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct StripeContextListEntry {
    pub alias: String,
    pub id: String,
    pub name: String,
    pub default: String,
    pub live_key_config: String,
    pub sandbox_key_config: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct StripeAuthOverrides {
    pub account: String,
    pub environment: String,
    pub api_key: String,
    pub base_url: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct StripeCurrentContext {
    pub account_alias: String,
    pub account_id: String,
    pub environment: String,
    pub key_source: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct StripeAuthStatus {
    pub account_alias: String,
    pub account_id: String,
    pub environment: String,
    pub key_source: String,
    pub key_preview: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct StripeAPIRequest {
    pub method: String,
    pub path: String,
    pub params: BTreeMap<String, String>,
    pub raw_body: String,
    pub idempotency_key: String,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct StripeAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub idempotency_key: String,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub headers: BTreeMap<String, String>,
    pub body: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
}

pub fn list_contexts(settings: &StripeSettings) -> Vec<StripeContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, account) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(StripeContextListEntry {
            alias: alias.to_owned(),
            id: trim_or_empty(account.id.as_deref()),
            name: trim_or_empty(account.name.as_deref()),
            default: yes_no(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            live_key_config: yes_no(has_key_config(
                account.live_key.as_deref(),
                account.live_key_env.as_deref(),
            )),
            sandbox_key_config: yes_no(has_key_config(
                account.sandbox_key.as_deref(),
                account.sandbox_key_env.as_deref(),
            )),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[StripeContextListEntry]) -> String {
    if rows.is_empty() {
        return "no stripe accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "ACCOUNT", "DEFAULT", "LIVE", "SANDBOX", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(or_dash(&row.id).len());
        widths[2] = widths[2].max(row.default.len());
        widths[3] = widths[3].max(row.live_key_config.len());
        widths[4] = widths[4].max(row.sandbox_key_config.len());
        widths[5] = widths[5].max(or_dash(&row.name).len());
    }

    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            or_dash(&row.id),
            row.default.as_str(),
            row.live_key_config.as_str(),
            row.sandbox_key_config.as_str(),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &StripeSettings,
    env: &BTreeMap<String, String>,
) -> Result<StripeCurrentContext, String> {
    let runtime = resolve_runtime_context(settings, env, &StripeAuthOverrides::default())?;
    Ok(StripeCurrentContext {
        account_alias: runtime.account_alias,
        account_id: runtime.account_id,
        environment: runtime.environment,
        key_source: runtime.key_source,
    })
}

pub fn resolve_auth_status(
    settings: &StripeSettings,
    env: &BTreeMap<String, String>,
    overrides: &StripeAuthOverrides,
) -> Result<StripeAuthStatus, String> {
    let runtime = resolve_runtime_context(settings, env, overrides)?;
    Ok(StripeAuthStatus {
        account_alias: runtime.account_alias,
        account_id: runtime.account_id,
        environment: runtime.environment,
        key_source: runtime.key_source,
        key_preview: preview_secret(&runtime.api_key),
    })
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StripeRuntimeContext {
    pub account_alias: String,
    pub account_id: String,
    pub environment: String,
    pub api_key: String,
    pub key_source: String,
    pub base_url: String,
}

fn resolve_runtime_context(
    settings: &StripeSettings,
    env: &BTreeMap<String, String>,
    overrides: &StripeAuthOverrides,
) -> Result<StripeRuntimeContext, String> {
    let (alias, account, account_id) = resolve_account_selection(settings, env, &overrides.account);
    let environment = resolve_environment(settings, env, &overrides.environment)?;
    let (api_key, key_source) = if !overrides.api_key.trim().is_empty() {
        (overrides.api_key.trim().to_owned(), "flag".to_owned())
    } else {
        resolve_api_key(&account, &environment, env)
    };
    if api_key.trim().is_empty() {
        return Err(format!(
            "stripe api key not found for env={} (set --api-key, [stripe.accounts.<alias>] key, or SI_STRIPE_API_KEY)",
            environment
        ));
    }
    Ok(StripeRuntimeContext {
        account_alias: alias,
        account_id,
        environment,
        api_key,
        key_source,
        base_url: first_non_empty(&[
            Some(overrides.base_url.as_str()),
            Some("https://api.stripe.com"),
        ])
        .to_owned(),
    })
}

pub fn resolve_runtime(
    settings: &StripeSettings,
    env: &BTreeMap<String, String>,
    overrides: &StripeAuthOverrides,
) -> Result<StripeRuntimeContext, String> {
    resolve_runtime_context(settings, env, overrides)
}

fn resolve_account_selection(
    settings: &StripeSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, StripeAccountEntry, String) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("SI_STRIPE_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), StripeAccountEntry::default(), String::new());
    }
    if let Some(account) = settings.accounts.get(&selected) {
        return (selected, account.clone(), trim_or_empty(account.id.as_deref()));
    }
    if selected.to_ascii_lowercase().starts_with("acct_") {
        return (String::new(), StripeAccountEntry::default(), selected);
    }
    (selected, StripeAccountEntry::default(), String::new())
}

fn resolve_environment(
    settings: &StripeSettings,
    env: &BTreeMap<String, String>,
    override_env: &str,
) -> Result<String, String> {
    let raw = first_non_empty(&[
        Some(override_env),
        settings.default_env.as_deref(),
        env.get("SI_STRIPE_ENV").map(String::as_str),
        Some("sandbox"),
    ]);
    match raw.trim().to_ascii_lowercase().as_str() {
        "live" => Ok("live".to_owned()),
        "sandbox" => Ok("sandbox".to_owned()),
        value => Err(format!("invalid stripe environment {value:?} (expected live|sandbox)")),
    }
}

fn resolve_api_key(
    account: &StripeAccountEntry,
    environment: &str,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    match environment {
        "live" => {
            if let Some(value) =
                account.live_key.as_deref().map(str::trim).filter(|value| !value.is_empty())
            {
                return (value.to_owned(), "settings.live_key".to_owned());
            }
            if let Some(reference) =
                account.live_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
            {
                if let Some(value) = env
                    .get(reference)
                    .map(String::as_str)
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                {
                    return (value.to_owned(), format!("env:{reference}"));
                }
            }
            if let Some(value) = env
                .get("SI_STRIPE_LIVE_API_KEY")
                .map(String::as_str)
                .map(str::trim)
                .filter(|value| !value.is_empty())
            {
                return (value.to_owned(), "env:SI_STRIPE_LIVE_API_KEY".to_owned());
            }
        }
        "sandbox" => {
            if let Some(value) =
                account.sandbox_key.as_deref().map(str::trim).filter(|value| !value.is_empty())
            {
                return (value.to_owned(), "settings.sandbox_key".to_owned());
            }
            if let Some(reference) =
                account.sandbox_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
            {
                if let Some(value) = env
                    .get(reference)
                    .map(String::as_str)
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                {
                    return (value.to_owned(), format!("env:{reference}"));
                }
            }
            if let Some(value) = env
                .get("SI_STRIPE_SANDBOX_API_KEY")
                .map(String::as_str)
                .map(str::trim)
                .filter(|value| !value.is_empty())
            {
                return (value.to_owned(), "env:SI_STRIPE_SANDBOX_API_KEY".to_owned());
            }
        }
        _ => {}
    }

    if let Some(value) = env
        .get("SI_STRIPE_API_KEY")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:SI_STRIPE_API_KEY".to_owned());
    }
    (String::new(), String::new())
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

fn trim_or_empty(value: Option<&str>) -> String {
    value.unwrap_or_default().trim().to_owned()
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

fn has_key_config(value: Option<&str>, reference: Option<&str>) -> bool {
    value.map(str::trim).filter(|value| !value.is_empty()).is_some()
        || reference.map(str::trim).filter(|value| !value.is_empty()).is_some()
}

fn yes_no(value: bool) -> String {
    if value { "yes".to_owned() } else { "no".to_owned() }
}

fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() { "-" } else { value }
}

fn preview_secret(secret: &str) -> String {
    let trimmed = secret.trim();
    if trimmed.is_empty() {
        return "-".to_owned();
    }
    let preview: String = trimmed.chars().take(8).collect();
    if trimmed.chars().count() <= 10 { preview } else { format!("{preview}...") }
}

pub fn execute_api_request(
    runtime: &StripeRuntimeContext,
    request: &StripeAPIRequest,
) -> Result<StripeAPIResponse, String> {
    let method = if request.method.trim().is_empty() { "GET" } else { request.method.trim() };
    let (endpoint, body) = build_request_target(
        &runtime.base_url,
        &request.path,
        method,
        &request.params,
        &request.raw_body,
    )?;
    let client = Client::builder()
        .timeout(Duration::from_secs(30))
        .build()
        .map_err(|err| format!("build stripe http client: {err}"))?;
    let mut headers = HeaderMap::new();
    headers.insert(
        AUTHORIZATION,
        HeaderValue::from_str(&format!("Bearer {}", runtime.api_key.trim()))
            .map_err(|err| format!("build stripe auth header: {err}"))?,
    );
    headers.insert(ACCEPT, HeaderValue::from_static("application/json"));
    headers.insert(USER_AGENT, HeaderValue::from_static("si-rs"));
    if !runtime.account_id.trim().is_empty() {
        headers.insert(
            "Stripe-Account",
            HeaderValue::from_str(runtime.account_id.trim())
                .map_err(|err| format!("build stripe account header: {err}"))?,
        );
    }
    if !request.idempotency_key.trim().is_empty() {
        headers.insert(
            "Idempotency-Key",
            HeaderValue::from_str(request.idempotency_key.trim())
                .map_err(|err| format!("build stripe idempotency header: {err}"))?,
        );
    }
    let method = reqwest::Method::from_bytes(method.as_bytes())
        .map_err(|err| format!("build stripe http method: {err}"))?;
    let mut request_builder = client.request(method, endpoint).headers(headers);
    if let Some(body) = body {
        request_builder = request_builder
            .header(CONTENT_TYPE, detect_stripe_content_type(&request.path, &request.raw_body))
            .body(body);
    }
    let response = request_builder.send().map_err(|err| format!("stripe request failed: {err}"))?;
    normalize_response(response)
}

pub fn list_all(
    runtime: &StripeRuntimeContext,
    path: &str,
    params: &BTreeMap<String, String>,
    limit: usize,
) -> Result<Vec<Value>, String> {
    let target_limit = if limit == 0 { 100 } else { limit };
    let mut out = Vec::new();
    let mut cursor = String::new();
    loop {
        let remaining = target_limit.saturating_sub(out.len());
        if remaining == 0 {
            break;
        }
        let mut page_params = params.clone();
        let page_size = remaining.min(100);
        page_params.entry("limit".to_owned()).or_insert_with(|| page_size.to_string());
        if !cursor.trim().is_empty() {
            page_params.insert("starting_after".to_owned(), cursor.clone());
        }
        let response = execute_api_request(
            runtime,
            &StripeAPIRequest {
                method: "GET".to_owned(),
                path: path.to_owned(),
                params: page_params,
                ..StripeAPIRequest::default()
            },
        )?;
        let object = response
            .data
            .as_ref()
            .and_then(Value::as_object)
            .ok_or_else(|| "stripe list response missing json body".to_owned())?;
        let items = object.get("data").and_then(Value::as_array).cloned().unwrap_or_default();
        if items.is_empty() {
            break;
        }
        cursor = items
            .last()
            .and_then(Value::as_object)
            .and_then(|value| value.get("id"))
            .and_then(Value::as_str)
            .unwrap_or_default()
            .trim()
            .to_owned();
        out.extend(items);
        let has_more = object.get("has_more").and_then(Value::as_bool).unwrap_or(false);
        if !has_more || cursor.is_empty() {
            break;
        }
    }
    if out.len() > target_limit {
        out.truncate(target_limit);
    }
    Ok(out)
}

pub fn render_api_response_text(response: &StripeAPIResponse, raw: bool) -> String {
    if raw {
        if response.body.trim().is_empty() {
            return "{}\n".to_owned();
        }
        return ensure_trailing_newline(response.body.clone());
    }
    let mut out = format!("Stripe API: {} ({})\n", response.status.trim(), response.status_code);
    if !response.request_id.trim().is_empty() {
        out.push_str(&format!("Request ID: {}\n", response.request_id.trim()));
    }
    if !response.idempotency_key.trim().is_empty() {
        out.push_str(&format!("Idempotency Key: {}\n", response.idempotency_key.trim()));
    }
    if let Some(data) = &response.data {
        if let Ok(pretty) = serde_json::to_string_pretty(data) {
            out.push_str(&pretty);
            out.push('\n');
            return out;
        }
    }
    if !response.body.trim().is_empty() {
        out.push_str(response.body.trim());
        out.push('\n');
    }
    out
}

fn build_request_target(
    base_url: &str,
    path: &str,
    method: &str,
    params: &BTreeMap<String, String>,
    raw_body: &str,
) -> Result<(String, Option<Vec<u8>>), String> {
    let method = method.trim().to_ascii_uppercase();
    let mut path = path.trim().to_owned();
    if path.is_empty() {
        return Err("request path is required".to_owned());
    }
    if !path.starts_with('/') && !path.starts_with("http://") && !path.starts_with("https://") {
        path = format!("/{path}");
    }
    if matches!(method.as_str(), "GET" | "DELETE") {
        let mut url = resolve_url(base_url, &path)?;
        append_query(&mut url, params);
        return Ok((url.to_string(), None));
    }
    let body = if !raw_body.trim().is_empty() {
        raw_body.trim().as_bytes().to_vec()
    } else if path.starts_with("/v2/") {
        let payload = params
            .iter()
            .map(|(key, value)| (key.clone(), Value::String(value.clone())))
            .collect::<serde_json::Map<String, Value>>();
        serde_json::to_vec(&Value::Object(payload))
            .map_err(|err| format!("encode stripe request body: {err}"))?
    } else {
        let mut serializer = url::form_urlencoded::Serializer::new(String::new());
        for (key, value) in params {
            serializer.append_pair(key.trim(), value.trim());
        }
        serializer.finish().into_bytes()
    };
    Ok((resolve_url(base_url, &path)?.to_string(), Some(body)))
}

fn resolve_url(base_url: &str, path: &str) -> Result<Url, String> {
    if path.starts_with("http://") || path.starts_with("https://") {
        return Url::parse(path).map_err(|err| format!("parse stripe url {:?}: {err}", path));
    }
    let base = Url::parse(base_url)
        .map_err(|err| format!("parse stripe base url {:?}: {err}", base_url))?;
    base.join(path).map_err(|err| format!("resolve stripe path {:?}: {err}", path))
}

fn append_query(url: &mut Url, params: &BTreeMap<String, String>) {
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
}

fn detect_stripe_content_type(path: &str, raw_body: &str) -> &'static str {
    if path.trim().starts_with("/v2/") {
        return "application/json";
    }
    let raw = raw_body.trim();
    if raw.starts_with('{') || raw.starts_with('[') {
        return "application/json";
    }
    "application/x-www-form-urlencoded"
}

fn normalize_response(response: reqwest::blocking::Response) -> Result<StripeAPIResponse, String> {
    let status = response.status();
    let status_text = status.to_string();
    let headers = response.headers().clone();
    let request_id = headers
        .get("Request-Id")
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .trim()
        .to_owned();
    let idempotency_key = headers
        .get("Idempotency-Key")
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .trim()
        .to_owned();
    let body = response.text().map_err(|err| format!("read stripe response body: {err}"))?;
    if !status.is_success() {
        return Err(format!(
            "stripe request failed: status={} request_id={} body={}",
            status.as_u16(),
            if request_id.is_empty() { "-" } else { request_id.as_str() },
            body.trim()
        ));
    }
    Ok(StripeAPIResponse {
        status_code: status.as_u16(),
        status: status_text,
        request_id,
        idempotency_key,
        headers: normalize_headers(&headers),
        body: body.trim().to_owned(),
        data: serde_json::from_str::<Value>(body.trim()).ok(),
    })
}

fn normalize_headers(headers: &HeaderMap) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    for (key, value) in headers {
        out.insert(key.as_str().to_owned(), value.to_str().unwrap_or_default().trim().to_owned());
    }
    out
}

fn ensure_trailing_newline(mut value: String) -> String {
    if !value.ends_with('\n') {
        value.push('\n');
    }
    value
}

#[cfg(test)]
mod tests {
    use super::*;
    use si_rs_config::settings::{StripeAccountEntry, StripeSettings};

    #[test]
    fn list_contexts_applies_defaults_and_sorts() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "beta".to_owned(),
            StripeAccountEntry {
                id: Some("acct_beta".to_owned()),
                live_key_env: Some("BETA_LIVE".to_owned()),
                ..StripeAccountEntry::default()
            },
        );
        accounts.insert(
            "alpha".to_owned(),
            StripeAccountEntry {
                id: Some("acct_alpha".to_owned()),
                sandbox_key: Some("sk_test_alpha".to_owned()),
                ..StripeAccountEntry::default()
            },
        );
        let settings = StripeSettings {
            default_account: Some("beta".to_owned()),
            accounts,
            ..StripeSettings::default()
        };

        let rows = list_contexts(&settings);
        assert_eq!(rows[0].alias, "alpha");
        assert_eq!(rows[0].sandbox_key_config, "yes");
        assert_eq!(rows[1].alias, "beta");
        assert_eq!(rows[1].default, "yes");
        assert_eq!(rows[1].live_key_config, "yes");
    }

    #[test]
    fn resolve_current_context_uses_defaults() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            StripeAccountEntry {
                id: Some("acct_core".to_owned()),
                sandbox_key: Some("sk_test_core".to_owned()),
                ..StripeAccountEntry::default()
            },
        );
        let settings = StripeSettings {
            default_account: Some("core".to_owned()),
            default_env: Some("sandbox".to_owned()),
            accounts,
            ..StripeSettings::default()
        };
        let env = BTreeMap::new();

        let current = resolve_current_context(&settings, &env).unwrap();

        assert_eq!(current.account_alias, "core");
        assert_eq!(current.account_id, "acct_core");
        assert_eq!(current.environment, "sandbox");
        assert_eq!(current.key_source, "settings.sandbox_key");
    }

    #[test]
    fn resolve_auth_status_uses_env_key_override() {
        let mut env = BTreeMap::new();
        env.insert("SI_STRIPE_API_KEY".to_owned(), "sk_test_shared".to_owned());

        let status = resolve_auth_status(
            &StripeSettings::default(),
            &env,
            &StripeAuthOverrides {
                account: "acct_123".to_owned(),
                environment: "sandbox".to_owned(),
                ..StripeAuthOverrides::default()
            },
        )
        .unwrap();

        assert_eq!(status.account_alias, "");
        assert_eq!(status.account_id, "acct_123");
        assert_eq!(status.environment, "sandbox");
        assert_eq!(status.key_source, "env:SI_STRIPE_API_KEY");
        assert_eq!(status.key_preview, "sk_test_...");
    }
}
