use reqwest::blocking::Client;
use serde::Serialize;
use serde_json::Value;
use si_rs_config::settings::{WorkOSAccountEntry, WorkOSSettings};
use std::{collections::BTreeMap, time::Duration};

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkOSContextListEntry {
    pub alias: String,
    pub name: String,
    pub default: String,
    pub api_key_env: String,
    pub client_id_env: String,
    pub organization_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct WorkOSAuthOverrides {
    pub account: String,
    pub environment: String,
    pub base_url: String,
    pub api_key: String,
    pub client_id: String,
    pub organization_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkOSCurrentContext {
    pub account_alias: String,
    pub environment: String,
    pub base_url: String,
    pub organization_id: String,
    pub client_id: String,
    pub source: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkOSAuthStatus {
    pub account_alias: String,
    pub environment: String,
    pub organization_id: String,
    pub client_id: String,
    pub source: String,
    pub base_url: String,
    pub key_preview: String,
}

#[derive(Debug, Clone, Default)]
pub struct WorkOSAPIRequest {
    pub method: String,
    pub path: String,
    pub params: BTreeMap<String, String>,
    pub headers: BTreeMap<String, String>,
    pub raw_body: Option<String>,
    pub json_body: Option<Value>,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct WorkOSAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub body: String,
    pub data: Value,
}

pub fn list_contexts(settings: &WorkOSSettings) -> Vec<WorkOSContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    let default_env = normalize_environment(settings.default_env.as_deref()).unwrap_or("prod");
    for (alias, entry) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(WorkOSContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(entry.name.as_deref()),
            default: yes_no(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            api_key_env: account_key_env_ref(default_env, entry),
            client_id_env: trim_or_empty(entry.client_id_env.as_deref()),
            organization_id: trim_or_empty(entry.organization_id.as_deref()),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[WorkOSContextListEntry]) -> String {
    if rows.is_empty() {
        return "no workos accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "API KEY ENV", "CLIENT ID ENV", "ORG ID", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.api_key_env).len());
        widths[3] = widths[3].max(or_dash(&row.client_id_env).len());
        widths[4] = widths[4].max(or_dash(&row.organization_id).len());
        widths[5] = widths[5].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.api_key_env),
            or_dash(&row.client_id_env),
            or_dash(&row.organization_id),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
) -> Result<WorkOSCurrentContext, String> {
    let runtime = resolve_runtime_context(settings, env, &WorkOSAuthOverrides::default())?;
    Ok(WorkOSCurrentContext {
        account_alias: runtime.account_alias,
        environment: runtime.environment,
        base_url: runtime.base_url,
        organization_id: runtime.organization_id,
        client_id: runtime.client_id,
        source: runtime.source,
    })
}

pub fn resolve_auth_status(
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
    overrides: &WorkOSAuthOverrides,
) -> Result<WorkOSAuthStatus, String> {
    let runtime = resolve_runtime_context(settings, env, overrides)?;
    Ok(WorkOSAuthStatus {
        account_alias: runtime.account_alias,
        environment: runtime.environment,
        organization_id: runtime.organization_id,
        client_id: runtime.client_id,
        source: runtime.source,
        base_url: runtime.base_url,
        key_preview: preview_secret(&runtime.api_key),
    })
}

pub struct WorkOSRuntimeContext {
    pub account_alias: String,
    pub environment: String,
    pub base_url: String,
    pub api_key: String,
    pub client_id: String,
    pub organization_id: String,
    pub source: String,
}

pub fn resolve_runtime(
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
    overrides: &WorkOSAuthOverrides,
) -> Result<WorkOSRuntimeContext, String> {
    resolve_runtime_context(settings, env, overrides)
}

pub fn execute_api_request(
    runtime: &WorkOSRuntimeContext,
    request: &WorkOSAPIRequest,
) -> Result<WorkOSAPIResponse, String> {
    let method = reqwest::Method::from_bytes(
        request
            .method
            .trim()
            .to_ascii_uppercase()
            .as_bytes(),
    )
    .map_err(|err| format!("invalid WorkOS method: {err}"))?;
    let url = if request.path.trim().starts_with("http://")
        || request.path.trim().starts_with("https://")
    {
        request.path.trim().to_owned()
    } else if request.path.trim().starts_with('/') {
        format!("{}{}", runtime.base_url.trim_end_matches('/'), request.path.trim())
    } else {
        format!("{}/{}", runtime.base_url.trim_end_matches('/'), request.path.trim())
    };
    let client = Client::builder()
        .timeout(Duration::from_secs(45))
        .build()
        .map_err(|err| format!("build WorkOS client: {err}"))?;
    let mut builder = client.request(method, &url).bearer_auth(runtime.api_key.trim());
    if !request.params.is_empty() {
        builder = builder.query(&request.params);
    }
    for (key, value) in &request.headers {
        if !key.trim().is_empty() && !value.trim().is_empty() {
            builder = builder.header(key.trim(), value.trim());
        }
    }
    if let Some(json_body) = &request.json_body {
        builder = builder.json(json_body);
    } else if let Some(raw_body) = &request.raw_body {
        builder = builder.body(raw_body.clone());
    }
    let response = builder.send().map_err(|err| format!("workos request failed: {err}"))?;
    let status = response.status();
    let request_id = first_header(response.headers(), &["x-request-id"]);
    let body = response
        .text()
        .map_err(|err| format!("read WorkOS response body: {err}"))?;
    let data = if body.trim().is_empty() {
        Value::Null
    } else {
        serde_json::from_str(body.trim()).unwrap_or_else(|_| Value::String(body.clone()))
    };
    if !status.is_success() {
        return Err(format!(
            "workos request failed: status={} request_id={} body={}",
            status.as_u16(),
            if request_id.trim().is_empty() { "-" } else { request_id.trim() },
            body.trim()
        ));
    }
    Ok(WorkOSAPIResponse {
        status_code: status.as_u16(),
        status: status.to_string(),
        request_id,
        body,
        data,
    })
}

fn resolve_runtime_context(
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
    overrides: &WorkOSAuthOverrides,
) -> Result<WorkOSRuntimeContext, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let environment = resolve_environment(settings, env, &overrides.environment)?;
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        account.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("WORKOS_API_BASE_URL").map(String::as_str),
        Some("https://api.workos.com"),
    ])
    .trim_end_matches('/')
    .to_owned();
    let (api_key, key_source) =
        resolve_api_key(&alias, &account, &environment, env, &overrides.api_key);
    if api_key.trim().is_empty() {
        let prefix = account_env_prefix(&alias, &account);
        let hint = if prefix.is_empty() {
            "WORKOS_<ACCOUNT>_API_KEY".to_owned()
        } else {
            format!("{prefix}API_KEY")
        };
        return Err(format!("workos api key not found (set --api-key, {hint}, or WORKOS_API_KEY)"));
    }
    let (client_id, client_id_source) =
        resolve_client_id(&alias, &account, env, &overrides.client_id);
    let (organization_id, org_source) =
        resolve_organization_id(&alias, &account, settings, env, &overrides.organization_id);
    Ok(WorkOSRuntimeContext {
        account_alias: alias,
        environment,
        base_url,
        api_key,
        client_id,
        organization_id,
        source: join_sources(&[key_source, client_id_source, org_source]),
    })
}

fn resolve_account_selection(
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, WorkOSAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("WORKOS_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), WorkOSAccountEntry::default());
    }
    if let Some(entry) = settings.accounts.get(&selected) {
        return (selected, entry.clone());
    }
    (selected, WorkOSAccountEntry::default())
}

fn resolve_environment(
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
    override_env: &str,
) -> Result<String, String> {
    let raw = first_non_empty(&[
        Some(override_env),
        settings.default_env.as_deref(),
        env.get("WORKOS_DEFAULT_ENV").map(String::as_str),
        Some("prod"),
    ]);
    normalize_environment(Some(raw)).map(str::to_owned).ok_or_else(|| {
        if raw.trim().eq_ignore_ascii_case("test") {
            "environment `test` is not supported; use `staging` or `dev`".to_owned()
        } else {
            format!("invalid environment {:?} (expected prod|staging|dev)", raw.trim())
        }
    })
}

fn normalize_environment(value: Option<&str>) -> Option<&'static str> {
    match value.unwrap_or_default().trim().to_ascii_lowercase().as_str() {
        "prod" => Some("prod"),
        "staging" => Some("staging"),
        "dev" => Some("dev"),
        _ => None,
    }
}

fn account_env_prefix(alias: &str, account: &WorkOSAccountEntry) -> String {
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
    if slug.is_empty() { String::new() } else { format!("WORKOS_{slug}_") }
}

fn resolve_env(
    alias: &str,
    account: &WorkOSAccountEntry,
    key: &str,
    env: &BTreeMap<String, String>,
) -> String {
    let prefix = account_env_prefix(alias, account);
    if prefix.is_empty() {
        return String::new();
    }
    env.get(&(prefix + key)).map(String::as_str).map(str::trim).unwrap_or_default().to_owned()
}

fn resolve_api_key(
    alias: &str,
    account: &WorkOSAccountEntry,
    environment: &str,
    env: &BTreeMap<String, String>,
    override_key: &str,
) -> (String, String) {
    if !override_key.trim().is_empty() {
        return (override_key.trim().to_owned(), "flag:--api-key".to_owned());
    }
    match environment {
        "prod" => {
            if let Some(reference) =
                account.prod_api_key_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
            {
                if let Some(value) =
                    env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
                {
                    return (value.to_owned(), format!("env:{reference}"));
                }
            }
            let value = resolve_env(alias, account, "PROD_API_KEY", env);
            if !value.is_empty() {
                return (value, format!("env:{}PROD_API_KEY", account_env_prefix(alias, account)));
            }
        }
        "staging" => {
            if let Some(reference) =
                account.staging_api_key_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
            {
                if let Some(value) =
                    env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
                {
                    return (value.to_owned(), format!("env:{reference}"));
                }
            }
            let value = resolve_env(alias, account, "STAGING_API_KEY", env);
            if !value.is_empty() {
                return (
                    value,
                    format!("env:{}STAGING_API_KEY", account_env_prefix(alias, account)),
                );
            }
        }
        "dev" => {
            if let Some(reference) =
                account.dev_api_key_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
            {
                if let Some(value) =
                    env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
                {
                    return (value.to_owned(), format!("env:{reference}"));
                }
            }
            let value = resolve_env(alias, account, "DEV_API_KEY", env);
            if !value.is_empty() {
                return (value, format!("env:{}DEV_API_KEY", account_env_prefix(alias, account)));
            }
        }
        _ => {}
    }

    if let Some(reference) = account.api_key_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    let value = resolve_env(alias, account, "API_KEY", env);
    if !value.is_empty() {
        return (value, format!("env:{}API_KEY", account_env_prefix(alias, account)));
    }
    for key in ["WORKOS_API_KEY", "WORKOS_MANAGEMENT_API_KEY"] {
        if let Some(value) =
            env.get(key).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
        {
            return (value.to_owned(), format!("env:{key}"));
        }
    }
    (String::new(), String::new())
}

fn resolve_client_id(
    alias: &str,
    account: &WorkOSAccountEntry,
    env: &BTreeMap<String, String>,
    override_client_id: &str,
) -> (String, String) {
    if !override_client_id.trim().is_empty() {
        return (override_client_id.trim().to_owned(), "flag:--client-id".to_owned());
    }
    if let Some(reference) =
        account.client_id_env.as_deref().map(str::trim).filter(|v| !v.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    let value = resolve_env(alias, account, "CLIENT_ID", env);
    if !value.is_empty() {
        return (value, format!("env:{}CLIENT_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) =
        env.get("WORKOS_CLIENT_ID").map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "env:WORKOS_CLIENT_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_organization_id(
    alias: &str,
    account: &WorkOSAccountEntry,
    settings: &WorkOSSettings,
    env: &BTreeMap<String, String>,
    override_org: &str,
) -> (String, String) {
    if !override_org.trim().is_empty() {
        return (override_org.trim().to_owned(), "flag:--org-id".to_owned());
    }
    if let Some(value) = account.organization_id.as_deref().map(str::trim).filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "settings.organization_id".to_owned());
    }
    let value = resolve_env(alias, account, "ORGANIZATION_ID", env);
    if !value.is_empty() {
        return (value, format!("env:{}ORGANIZATION_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) =
        settings.default_organization_id.as_deref().map(str::trim).filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "settings.default_organization_id".to_owned());
    }
    if let Some(value) = env
        .get("WORKOS_ORGANIZATION_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|v| !v.is_empty())
    {
        return (value.to_owned(), "env:WORKOS_ORGANIZATION_ID".to_owned());
    }
    (String::new(), String::new())
}

fn account_key_env_ref(environment: &str, account: &WorkOSAccountEntry) -> String {
    match environment {
        "prod" => account.prod_api_key_env.as_deref(),
        "staging" => account.staging_api_key_env.as_deref(),
        "dev" => account.dev_api_key_env.as_deref(),
        _ => None,
    }
    .unwrap_or_default()
    .trim()
    .to_owned()
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

fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() { "-" } else { value }
}

fn yes_no(value: bool) -> String {
    if value { "true".to_owned() } else { "false".to_owned() }
}

fn preview_secret(secret: &str) -> String {
    let trimmed = secret.trim();
    if trimmed.is_empty() {
        return "-".to_owned();
    }
    let preview: String = trimmed.chars().take(8).collect();
    if trimmed.chars().count() <= 10 { preview } else { format!("{preview}...") }
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
    use si_rs_config::settings::{WorkOSAccountEntry, WorkOSSettings};

    #[test]
    fn list_contexts_applies_defaults() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            WorkOSAccountEntry {
                prod_api_key_env: Some("CORE_PROD".to_owned()),
                organization_id: Some("org_123".to_owned()),
                ..WorkOSAccountEntry::default()
            },
        );
        let settings = WorkOSSettings {
            default_account: Some("core".to_owned()),
            accounts,
            ..WorkOSSettings::default()
        };
        let rows = list_contexts(&settings);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].default, "true");
        assert_eq!(rows[0].api_key_env, "CORE_PROD");
        assert_eq!(rows[0].organization_id, "org_123");
    }

    #[test]
    fn resolve_current_context_uses_defaults() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            WorkOSAccountEntry {
                prod_api_key_env: Some("CORE_PROD".to_owned()),
                ..WorkOSAccountEntry::default()
            },
        );
        let settings = WorkOSSettings {
            default_account: Some("core".to_owned()),
            default_env: Some("prod".to_owned()),
            accounts,
            ..WorkOSSettings::default()
        };
        let mut env = BTreeMap::new();
        env.insert("CORE_PROD".to_owned(), "sk_workos_prod".to_owned());
        let current = resolve_current_context(&settings, &env).unwrap();
        assert_eq!(current.account_alias, "core");
        assert_eq!(current.environment, "prod");
        assert_eq!(current.source, "env:CORE_PROD");
    }

    #[test]
    fn resolve_auth_status_uses_env_sources() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            WorkOSAccountEntry {
                prod_api_key_env: Some("CORE_PROD".to_owned()),
                client_id_env: Some("CORE_CLIENT".to_owned()),
                organization_id: Some("org_123".to_owned()),
                ..WorkOSAccountEntry::default()
            },
        );
        let settings = WorkOSSettings {
            default_account: Some("core".to_owned()),
            accounts,
            ..WorkOSSettings::default()
        };
        let mut env = BTreeMap::new();
        env.insert("CORE_PROD".to_owned(), "sk_workos_prod".to_owned());
        env.insert("CORE_CLIENT".to_owned(), "client_123".to_owned());
        let status = resolve_auth_status(&settings, &env, &WorkOSAuthOverrides::default()).unwrap();
        assert_eq!(status.account_alias, "core");
        assert_eq!(status.environment, "prod");
        assert_eq!(status.source, "env:CORE_PROD,env:CORE_CLIENT,settings.organization_id");
        assert_eq!(status.key_preview, "sk_worko...");
    }
}
