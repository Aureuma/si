use chrono::Utc;
use hmac::{Hmac, Mac};
use reqwest::blocking::Client;
use serde::Serialize;
use serde_json::Value;
use sha2::{Digest, Sha256};
use si_rs_config::settings::{AWSAccountEntry, AWSSettings};
use std::{collections::BTreeMap, time::Duration};

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct AWSContextListEntry {
    pub alias: String,
    pub name: String,
    pub default: String,
    pub region: String,
    pub access_key_env: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AWSAuthOverrides {
    pub account: String,
    pub region: String,
    pub base_url: String,
    pub access_key: String,
    pub secret_key: String,
    pub session_token: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct AWSCurrentContext {
    pub account_alias: String,
    pub region: String,
    pub base_url: String,
    pub source: String,
    pub access_key: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct AWSAuthStatus {
    pub account_alias: String,
    pub region: String,
    pub base_url: String,
    pub source: String,
    pub access_key: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verify_status: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verify: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verify_error: Option<String>,
    pub status: String,
}

#[derive(Debug, Clone, Default)]
pub struct AWSAPIRequest {
    pub method: String,
    pub path: String,
    pub service: String,
    pub params: BTreeMap<String, String>,
    pub headers: BTreeMap<String, String>,
    pub body: String,
    pub content_type: String,
    pub accept: String,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct AWSAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub headers: BTreeMap<String, String>,
    pub body: String,
    pub data: Value,
}

pub fn list_contexts(settings: &AWSSettings) -> Vec<AWSContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, account) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(AWSContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(account.name.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            region: first_non_empty(&[
                account.region.as_deref(),
                settings.default_region.as_deref(),
            ])
            .to_owned(),
            access_key_env: trim_or_empty(account.access_key_id_env.as_deref()),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[AWSContextListEntry]) -> String {
    if rows.is_empty() {
        return "no aws accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "REGION", "ACCESS KEY ENV", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.region).len());
        widths[3] = widths[3].max(or_dash(&row.access_key_env).len());
        widths[4] = widths[4].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.region),
            or_dash(&row.access_key_env),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &AWSSettings,
    env: &BTreeMap<String, String>,
) -> Result<AWSCurrentContext, String> {
    let runtime = resolve_runtime_context(settings, env, &AWSAuthOverrides::default())?;
    Ok(AWSCurrentContext {
        account_alias: runtime.account_alias,
        region: runtime.region,
        base_url: runtime.base_url,
        source: runtime.source,
        access_key: preview_access_key(&runtime.access_key),
    })
}

pub fn resolve_auth_status(
    settings: &AWSSettings,
    env: &BTreeMap<String, String>,
    overrides: &AWSAuthOverrides,
) -> Result<AWSAuthStatus, String> {
    let runtime = resolve_runtime_context(settings, env, overrides)?;
    Ok(AWSAuthStatus {
        account_alias: runtime.account_alias,
        region: runtime.region,
        base_url: runtime.base_url,
        source: runtime.source,
        access_key: preview_access_key(&runtime.access_key),
        verify_status: None,
        verify: None,
        verify_error: None,
        status: "ready".to_owned(),
    })
}

pub struct AWSRuntimeContext {
    pub account_alias: String,
    pub region: String,
    pub base_url: String,
    pub access_key: String,
    pub secret_key: String,
    pub session_token: String,
    pub source: String,
}

fn resolve_runtime_context(
    settings: &AWSSettings,
    env: &BTreeMap<String, String>,
    overrides: &AWSAuthOverrides,
) -> Result<AWSRuntimeContext, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let region = first_non_empty(&[
        Some(overrides.region.as_str()),
        account.region.as_deref(),
        settings.default_region.as_deref(),
        env.get("AWS_REGION").map(String::as_str),
        env.get("AWS_DEFAULT_REGION").map(String::as_str),
        Some("us-east-1"),
    ])
    .to_owned();
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        settings.api_base_url.as_deref(),
        env.get("AWS_IAM_API_BASE_URL").map(String::as_str),
        Some("https://iam.amazonaws.com"),
    ])
    .trim_end_matches('/')
    .to_owned();

    let (access_key, access_source) =
        resolve_access_key(&alias, &account, env, &overrides.access_key);
    let (secret_key, secret_source) =
        resolve_secret_key(&alias, &account, env, &overrides.secret_key);
    let (session_token, session_source) =
        resolve_session_token(&alias, &account, env, &overrides.session_token);

    if access_key.trim().is_empty() || secret_key.trim().is_empty() {
        let prefix = account_env_prefix(&alias, &account);
        let hint = if prefix.is_empty() { "AWS_<ACCOUNT>_".to_owned() } else { prefix };
        return Err(format!(
            "aws credentials not found (set --access-key/--secret-key, {hint}ACCESS_KEY_ID, {hint}SECRET_ACCESS_KEY, or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY)"
        ));
    }

    Ok(AWSRuntimeContext {
        account_alias: alias,
        region,
        base_url,
        access_key,
        secret_key,
        session_token,
        source: join_sources(&[access_source, secret_source, session_source]),
    })
}

fn resolve_account_selection(
    settings: &AWSSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, AWSAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("AWS_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), AWSAccountEntry::default());
    }
    if let Some(account) = settings.accounts.get(&selected) {
        return (selected, account.clone());
    }
    (selected, AWSAccountEntry::default())
}

fn resolve_access_key(
    alias: &str,
    account: &AWSAccountEntry,
    env: &BTreeMap<String, String>,
    override_access_key: &str,
) -> (String, String) {
    if !override_access_key.trim().is_empty() {
        return (override_access_key.trim().to_owned(), "flag:--access-key".to_owned());
    }
    if let Some(reference) =
        account.access_key_id_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = account_env(alias, account, "ACCESS_KEY_ID", env) {
        return (value, format!("env:{}ACCESS_KEY_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("AWS_ACCESS_KEY_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:AWS_ACCESS_KEY_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_secret_key(
    alias: &str,
    account: &AWSAccountEntry,
    env: &BTreeMap<String, String>,
    override_secret_key: &str,
) -> (String, String) {
    if !override_secret_key.trim().is_empty() {
        return (override_secret_key.trim().to_owned(), "flag:--secret-key".to_owned());
    }
    if let Some(reference) =
        account.secret_access_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = account_env(alias, account, "SECRET_ACCESS_KEY", env) {
        return (value, format!("env:{}SECRET_ACCESS_KEY", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("AWS_SECRET_ACCESS_KEY")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:AWS_SECRET_ACCESS_KEY".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_session_token(
    alias: &str,
    account: &AWSAccountEntry,
    env: &BTreeMap<String, String>,
    override_session_token: &str,
) -> (String, String) {
    if !override_session_token.trim().is_empty() {
        return (override_session_token.trim().to_owned(), "flag:--session-token".to_owned());
    }
    if let Some(reference) =
        account.session_token_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = account_env(alias, account, "SESSION_TOKEN", env) {
        return (value, format!("env:{}SESSION_TOKEN", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("AWS_SESSION_TOKEN")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:AWS_SESSION_TOKEN".to_owned());
    }
    (String::new(), String::new())
}

pub fn preview_access_key(value: &str) -> String {
    let value = value.trim();
    if value.is_empty() {
        return "-".to_owned();
    }
    if value.len() <= 8 {
        return "*".repeat(value.len());
    }
    format!("{}{}{}", &value[..4], "*".repeat(value.len() - 8), &value[value.len() - 4..])
}

fn account_env_prefix(alias: &str, account: &AWSAccountEntry) -> String {
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
    if slug.is_empty() { String::new() } else { format!("AWS_{slug}_") }
}

fn account_env(
    alias: &str,
    account: &AWSAccountEntry,
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

fn sign_request(
    url: &str,
    method: &str,
    body: &str,
    runtime: &AWSRuntimeContext,
    service: &str,
    headers: &mut BTreeMap<String, String>,
) -> Result<(), String> {
    if runtime.access_key.trim().is_empty() || runtime.secret_key.trim().is_empty() {
        return Err("aws access key and secret key are required for signing".to_owned());
    }
    let parsed = reqwest::Url::parse(url).map_err(|err| format!("parse AWS url: {err}"))?;
    let host = parsed.host_str().unwrap_or_default().trim().to_ascii_lowercase();
    if host.is_empty() {
        return Err("request host is required for aws signing".to_owned());
    }
    let now = Utc::now();
    let amz_date = now.format("%Y%m%dT%H%M%SZ").to_string();
    let date_stamp = now.format("%Y%m%d").to_string();
    let payload_hash = hex::encode(Sha256::digest(body.as_bytes()));
    headers.insert("host".to_owned(), host.clone());
    headers.insert("x-amz-date".to_owned(), amz_date.clone());
    headers.insert("x-amz-content-sha256".to_owned(), payload_hash.clone());
    if !runtime.session_token.trim().is_empty() {
        headers.insert("x-amz-security-token".to_owned(), runtime.session_token.clone());
    }
    let mut signed_header_names = headers
        .keys()
        .filter(|name| {
            matches!(
                name.as_str(),
                "host" | "x-amz-content-sha256" | "x-amz-date" | "x-amz-security-token"
            )
        })
        .cloned()
        .collect::<Vec<_>>();
    signed_header_names.sort();
    let canonical_headers = signed_header_names
        .iter()
        .map(|name| {
            format!(
                "{name}:{}",
                canonical_header_value(headers.get(name).map(String::as_str).unwrap_or_default())
            )
        })
        .collect::<Vec<_>>()
        .join("\n");
    let signed_headers = signed_header_names.join(";");
    let canonical_request = [
        method.trim().to_ascii_uppercase(),
        canonical_uri(&parsed),
        canonical_query(parsed.query().unwrap_or_default()),
        canonical_headers,
        String::new(),
        signed_headers.clone(),
        payload_hash,
    ]
    .join("\n");
    let canonical_request_hash = hex::encode(Sha256::digest(canonical_request.as_bytes()));
    let credential_scope = format!(
        "{}/{}/{}/aws4_request",
        date_stamp,
        runtime.region.trim(),
        service.trim()
    );
    let string_to_sign = [
        "AWS4-HMAC-SHA256".to_owned(),
        amz_date,
        credential_scope.clone(),
        canonical_request_hash,
    ]
    .join("\n");
    let signature = hex::encode(signature_key(
        runtime.secret_key.trim(),
        &date_stamp,
        runtime.region.trim(),
        service.trim(),
        &string_to_sign,
    )?);
    headers.insert(
        "authorization".to_owned(),
        format!(
            "AWS4-HMAC-SHA256 Credential={}/{}, SignedHeaders={}, Signature={}",
            runtime.access_key.trim(),
            credential_scope,
            signed_headers,
            signature
        ),
    );
    Ok(())
}

fn canonical_uri(url: &reqwest::Url) -> String {
    let path = url.path();
    if path.trim().is_empty() { "/".to_owned() } else { path.to_owned() }
}

fn canonical_query(raw: &str) -> String {
    if raw.trim().is_empty() {
        return String::new();
    }
    let mut parsed = url::form_urlencoded::parse(raw.as_bytes())
        .map(|(k, v)| (k.into_owned(), v.into_owned()))
        .collect::<Vec<_>>();
    parsed.sort();
    parsed
        .into_iter()
        .map(|(k, v)| format!("{}={}", percent_encode(&k), percent_encode(&v)))
        .collect::<Vec<_>>()
        .join("&")
}

fn percent_encode(value: &str) -> String {
    let encoded = url::form_urlencoded::byte_serialize(value.as_bytes()).collect::<String>();
    encoded.replace('+', "%20").replace('*', "%2A").replace("%7E", "~")
}

fn canonical_header_value(value: &str) -> String {
    value.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn signature_key(
    secret_key: &str,
    date: &str,
    region: &str,
    service: &str,
    string_to_sign: &str,
) -> Result<Vec<u8>, String> {
    let k_date = hmac_sha256(format!("AWS4{secret_key}").as_bytes(), date)?;
    let k_region = hmac_sha256(&k_date, region)?;
    let k_service = hmac_sha256(&k_region, service)?;
    let k_signing = hmac_sha256(&k_service, "aws4_request")?;
    hmac_sha256(&k_signing, string_to_sign)
}

fn hmac_sha256(key: &[u8], payload: &str) -> Result<Vec<u8>, String> {
    let mut mac =
        Hmac::<Sha256>::new_from_slice(key).map_err(|err| format!("build hmac: {err}"))?;
    mac.update(payload.as_bytes());
    Ok(mac.finalize().into_bytes().to_vec())
}

fn url_encode_params(params: &BTreeMap<String, String>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    for (key, value) in params {
        serializer.append_pair(key, value);
    }
    serializer.finish()
}

fn normalize_response(
    status_code: u16,
    status: String,
    headers: &reqwest::header::HeaderMap,
    body: &str,
) -> AWSAPIResponse {
    let mut header_map = BTreeMap::new();
    for (key, value) in headers {
        if let Ok(value) = value.to_str() {
            header_map.insert(key.as_str().to_owned(), value.to_owned());
        }
    }
    let request_id = first_header(headers, &["x-amzn-requestid", "x-amz-request-id"]);
    let data = parse_body_data(body, &request_id);
    AWSAPIResponse {
        status_code,
        status,
        request_id,
        headers: header_map,
        body: body.trim().to_owned(),
        data,
    }
}

fn normalize_http_error(
    status_code: u16,
    headers: &reqwest::header::HeaderMap,
    body: &str,
) -> String {
    let request_id = first_header(headers, &["x-amzn-requestid", "x-amz-request-id"]);
    format!(
        "aws iam api error: status_code={}, request_id={}, message={}",
        status_code,
        if request_id.trim().is_empty() { "-" } else { request_id.trim() },
        if body.trim().is_empty() { "request failed" } else { body.trim() }
    )
}

fn first_header(headers: &reqwest::header::HeaderMap, names: &[&str]) -> String {
    for name in names {
        if let Some(value) = headers.get(*name) {
            if let Ok(value) = value.to_str() {
                let trimmed = value.trim();
                if !trimmed.is_empty() {
                    return trimmed.to_owned();
                }
            }
        }
    }
    String::new()
}

fn parse_body_data(body: &str, request_id: &str) -> Value {
    let trimmed = body.trim();
    if trimmed.is_empty() {
        return Value::Null;
    }
    if let Ok(value) = serde_json::from_str::<Value>(trimmed) {
        return value;
    }
    let response_name = xml_root_name(trimmed).unwrap_or_default();
    let mut map = serde_json::Map::new();
    if !response_name.trim().is_empty() {
        map.insert("response".to_owned(), Value::String(response_name));
    }
    if !request_id.trim().is_empty() {
        map.insert("request_id".to_owned(), Value::String(request_id.to_owned()));
    }
    if map.is_empty() {
        Value::String(trimmed.to_owned())
    } else {
        Value::Object(map)
    }
}

fn xml_root_name(body: &str) -> Option<String> {
    let start = body.find('<')?;
    let rest = &body[start + 1..];
    let end = rest.find([' ', '>'])?;
    Some(rest[..end].trim_matches('/').to_owned())
}

pub fn resolve_runtime(
    settings: &AWSSettings,
    env: &BTreeMap<String, String>,
    overrides: &AWSAuthOverrides,
) -> Result<AWSRuntimeContext, String> {
    resolve_runtime_context(settings, env, overrides)
}

pub fn execute_api_request(
    runtime: &AWSRuntimeContext,
    request: &AWSAPIRequest,
) -> Result<AWSAPIResponse, String> {
    let method = request.method.trim().to_ascii_uppercase();
    if method.is_empty() {
        return Err("aws method is required".to_owned());
    }
    let service = if request.service.trim().is_empty() {
        "iam".to_owned()
    } else {
        request.service.trim().to_ascii_lowercase()
    };
    let base = runtime.base_url.trim_end_matches('/');
    let url = if request.path.trim().starts_with("http://")
        || request.path.trim().starts_with("https://")
    {
        request.path.trim().to_owned()
    } else if request.path.trim().is_empty() || request.path.trim() == "/" {
        format!("{base}/")
    } else if request.path.trim().starts_with('/') {
        format!("{base}{}", request.path.trim())
    } else {
        format!("{base}/{}", request.path.trim())
    };
    let client = Client::builder()
        .timeout(Duration::from_secs(45))
        .build()
        .map_err(|err| format!("build AWS client: {err}"))?;
    let mut headers = request.headers.clone();
    let accept = if request.accept.trim().is_empty() {
        "application/xml"
    } else {
        request.accept.trim()
    };
    let content_type = if request.content_type.trim().is_empty() {
        "application/x-www-form-urlencoded; charset=utf-8"
    } else {
        request.content_type.trim()
    };
    headers.insert("accept".to_owned(), accept.to_owned());
    headers.insert("content-type".to_owned(), content_type.to_owned());
    sign_request(&url, &method, &request.body, runtime, &service, &mut headers)?;

    let mut req = client
        .request(
            reqwest::Method::from_bytes(method.as_bytes())
                .map_err(|err| format!("invalid AWS method: {err}"))?,
            &url,
        )
        .body(request.body.clone());
    if !request.params.is_empty() {
        req = req.query(&request.params);
    }
    for (key, value) in &headers {
        req = req.header(key, value);
    }

    let response = req.send().map_err(|err| format!("aws request failed: {err}"))?;
    let status = response.status();
    let headers = response.headers().clone();
    let body = response.text().map_err(|err| format!("read AWS response body: {err}"))?;
    if !status.is_success() {
        return Err(normalize_http_error(status.as_u16(), &headers, &body));
    }
    Ok(normalize_response(status.as_u16(), status.to_string(), &headers, &body))
}

pub fn verify_auth_status(runtime: &AWSRuntimeContext) -> AWSAuthStatus {
    let mut form = BTreeMap::new();
    form.insert("Action".to_owned(), "GetUser".to_owned());
    form.insert("Version".to_owned(), "2010-05-08".to_owned());
    match execute_api_request(
        runtime,
        &AWSAPIRequest {
            method: "POST".to_owned(),
            path: "/".to_owned(),
            service: "iam".to_owned(),
            body: url_encode_params(&form),
            ..AWSAPIRequest::default()
        },
    ) {
        Ok(response) => AWSAuthStatus {
            account_alias: runtime.account_alias.clone(),
            region: runtime.region.clone(),
            base_url: runtime.base_url.clone(),
            source: runtime.source.clone(),
            access_key: preview_access_key(&runtime.access_key),
            verify_status: Some(response.status_code),
            verify: Some(response.data),
            verify_error: None,
            status: "ready".to_owned(),
        },
        Err(err) => AWSAuthStatus {
            account_alias: runtime.account_alias.clone(),
            region: runtime.region.clone(),
            base_url: runtime.base_url.clone(),
            source: runtime.source.clone(),
            access_key: preview_access_key(&runtime.access_key),
            verify_status: None,
            verify: None,
            verify_error: Some(err),
            status: "error".to_owned(),
        },
    }
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
            AWSSettings { default_account: Some("core".to_owned()), ..AWSSettings::default() };
        settings.accounts.insert(
            "core".to_owned(),
            AWSAccountEntry {
                region: Some("us-west-2".to_owned()),
                access_key_id_env: Some("CORE_AWS_ACCESS_KEY_ID".to_owned()),
                ..AWSAccountEntry::default()
            },
        );
        let rows = list_contexts(&settings);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].default, "true");
        assert_eq!(rows[0].region, "us-west-2");
    }

    #[test]
    fn auth_status_uses_env_sources() {
        let mut settings = AWSSettings {
            default_account: Some("core".to_owned()),
            default_region: Some("us-east-1".to_owned()),
            ..AWSSettings::default()
        };
        settings.accounts.insert(
            "core".to_owned(),
            AWSAccountEntry {
                access_key_id_env: Some("CORE_ACCESS".to_owned()),
                secret_access_key_env: Some("CORE_SECRET".to_owned()),
                session_token_env: Some("CORE_SESSION".to_owned()),
                ..AWSAccountEntry::default()
            },
        );
        let env = BTreeMap::from([
            ("CORE_ACCESS".to_owned(), "AKIA1234567890ABCD".to_owned()),
            ("CORE_SECRET".to_owned(), "secret".to_owned()),
            ("CORE_SESSION".to_owned(), "session".to_owned()),
        ]);
        let status =
            resolve_auth_status(&settings, &env, &AWSAuthOverrides::default()).expect("status");
        assert_eq!(status.account_alias, "core");
        assert_eq!(status.region, "us-east-1");
        assert_eq!(status.source, "env:CORE_ACCESS,env:CORE_SECRET,env:CORE_SESSION");
        assert_eq!(status.access_key, "AKIA**********ABCD");
    }
}
