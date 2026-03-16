use reqwest::blocking::Client;
use reqwest::header::{AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue};
use serde::Serialize;
use serde_json::Value;
use si_rs_config::settings::{OpenAIAccountEntry, OpenAISettings};
use std::collections::BTreeMap;
use std::time::Duration;
use url::Url;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OpenAIContextListEntry {
    pub alias: String,
    pub name: String,
    pub default: String,
    pub api_key_env: String,
    pub admin_api_key_env: String,
    pub org_id: String,
    pub project_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct OpenAIContextOverrides {
    pub account: String,
    pub base_url: String,
    pub api_key: String,
    pub admin_api_key: String,
    pub org_id: String,
    pub project_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OpenAICurrentContext {
    pub account_alias: String,
    pub base_url: String,
    pub organization_id: String,
    pub project_id: String,
    pub source: String,
    pub admin_key_set: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OpenAIRuntime {
    pub account_alias: String,
    pub base_url: String,
    pub api_key: String,
    pub admin_api_key: String,
    pub organization_id: String,
    pub project_id: String,
    pub source: String,
    pub admin_key_set: bool,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct OpenAIAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub body: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct OpenAIAuthStatus {
    pub status: String,
    pub account_alias: String,
    pub organization_id: String,
    pub project_id: String,
    pub source: String,
    pub base_url: String,
    pub api_key_preview: String,
    pub admin_key_set: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verify_status: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verify: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verify_error: Option<String>,
}

pub fn list_contexts(settings: &OpenAISettings) -> Vec<OpenAIContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, account) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(OpenAIContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(account.name.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            api_key_env: trim_or_empty(account.api_key_env.as_deref()),
            admin_api_key_env: trim_or_empty(account.admin_api_key_env.as_deref()),
            org_id: trim_or_empty(first_non_empty_ref(&[
                account.organization_id.as_deref(),
                account.organization_id_env.as_deref(),
            ])),
            project_id: trim_or_empty(first_non_empty_ref(&[
                account.project_id.as_deref(),
                account.project_id_env.as_deref(),
            ])),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[OpenAIContextListEntry]) -> String {
    if rows.is_empty() {
        return "no openai accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "API KEY ENV", "ADMIN KEY ENV", "ORG", "PROJECT", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.api_key_env).len());
        widths[3] = widths[3].max(or_dash(&row.admin_api_key_env).len());
        widths[4] = widths[4].max(or_dash(&row.org_id).len());
        widths[5] = widths[5].max(or_dash(&row.project_id).len());
        widths[6] = widths[6].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.api_key_env),
            or_dash(&row.admin_api_key_env),
            or_dash(&row.org_id),
            or_dash(&row.project_id),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &OpenAISettings,
    env: &BTreeMap<String, String>,
    overrides: &OpenAIContextOverrides,
) -> Result<OpenAICurrentContext, String> {
    let runtime = resolve_runtime(settings, env, overrides)?;
    Ok(OpenAICurrentContext {
        account_alias: runtime.account_alias,
        base_url: runtime.base_url,
        organization_id: runtime.organization_id,
        project_id: runtime.project_id,
        source: runtime.source,
        admin_key_set: runtime.admin_key_set,
    })
}

pub fn resolve_runtime(
    settings: &OpenAISettings,
    env: &BTreeMap<String, String>,
    overrides: &OpenAIContextOverrides,
) -> Result<OpenAIRuntime, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        account.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("OPENAI_API_BASE_URL").map(String::as_str),
        Some("https://api.openai.com"),
    ])
    .trim_end_matches('/')
    .to_owned();
    let (api_key, api_key_source) = resolve_api_key(&alias, &account, env, &overrides.api_key);
    let (admin_key, admin_source) =
        resolve_admin_api_key(&alias, &account, env, &overrides.admin_api_key);
    let admin_key_set = !admin_key.trim().is_empty();
    let auth_token = if api_key.trim().is_empty() {
        if admin_key.trim().is_empty() {
            let prefix = account_env_prefix(&alias, &account);
            let hint = if prefix.is_empty() { "OPENAI_<ACCOUNT>_".to_owned() } else { prefix };
            return Err(format!(
                "openai api key not found (set --api-key, {hint}API_KEY, or OPENAI_API_KEY)"
            ));
        }
        admin_key.clone()
    } else {
        api_key.clone()
    };
    let (org_id, org_source) = resolve_org_id(&alias, &account, settings, env, &overrides.org_id);
    let (project_id, project_source) =
        resolve_project_id(&alias, &account, settings, env, &overrides.project_id);

    Ok(OpenAIRuntime {
        account_alias: alias,
        base_url,
        api_key: auth_token,
        admin_api_key: admin_key,
        organization_id: org_id,
        project_id,
        source: join_sources(&[api_key_source, admin_source, org_source, project_source]),
        admin_key_set,
    })
}

fn resolve_account_selection(
    settings: &OpenAISettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, OpenAIAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("OPENAI_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), OpenAIAccountEntry::default());
    }
    let account = settings.accounts.get(&selected).cloned().unwrap_or_default();
    (selected, account)
}

fn resolve_api_key(
    alias: &str,
    account: &OpenAIAccountEntry,
    env: &BTreeMap<String, String>,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--api-key".to_owned());
    }
    if let Some(reference) =
        account.api_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = resolve_openai_env(alias, account, env, "API_KEY") {
        return (value, format!("env:{}API_KEY", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("OPENAI_API_KEY")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:OPENAI_API_KEY".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_admin_api_key(
    alias: &str,
    account: &OpenAIAccountEntry,
    env: &BTreeMap<String, String>,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--admin-api-key".to_owned());
    }
    if let Some(reference) =
        account.admin_api_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = resolve_openai_env(alias, account, env, "ADMIN_API_KEY") {
        return (value, format!("env:{}ADMIN_API_KEY", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("OPENAI_ADMIN_API_KEY")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:OPENAI_ADMIN_API_KEY".to_owned());
    }
    if let Some(value) = env
        .get("OPENAI_ADMIN_KEY")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:OPENAI_ADMIN_KEY".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_org_id(
    alias: &str,
    account: &OpenAIAccountEntry,
    settings: &OpenAISettings,
    env: &BTreeMap<String, String>,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--org-id".to_owned());
    }
    if let Some(value) =
        account.organization_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.organization_id".to_owned());
    }
    if let Some(reference) =
        account.organization_id_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if let Some(value) = resolve_openai_env(alias, account, env, "ORG_ID") {
        return (value, format!("env:{}ORG_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) =
        settings.default_organization_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.default_organization_id".to_owned());
    }
    if let Some(value) = env
        .get("OPENAI_ORG_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:OPENAI_ORG_ID".to_owned());
    }
    if let Some(value) = env
        .get("OPENAI_ORGANIZATION")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:OPENAI_ORGANIZATION".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_project_id(
    alias: &str,
    account: &OpenAIAccountEntry,
    settings: &OpenAISettings,
    env: &BTreeMap<String, String>,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--project-id".to_owned());
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
    if let Some(value) = resolve_openai_env(alias, account, env, "PROJECT_ID") {
        return (value, format!("env:{}PROJECT_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) =
        settings.default_project_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.default_project_id".to_owned());
    }
    if let Some(value) = env
        .get("OPENAI_PROJECT_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:OPENAI_PROJECT_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_openai_env(
    alias: &str,
    account: &OpenAIAccountEntry,
    env: &BTreeMap<String, String>,
    key: &str,
) -> Option<String> {
    let prefix = account_env_prefix(alias, account);
    if prefix.is_empty() {
        return None;
    }
    env.get(&format!("{prefix}{key}"))
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
}

fn account_env_prefix(alias: &str, account: &OpenAIAccountEntry) -> String {
    if let Some(prefix) =
        account.vault_prefix.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if prefix.ends_with('_') {
            return prefix.to_uppercase();
        }
        return format!("{}_", prefix.to_uppercase());
    }
    let alias = alias
        .trim()
        .chars()
        .map(|ch| match ch {
            'a'..='z' => ch.to_ascii_uppercase(),
            'A'..='Z' | '0'..='9' => ch,
            _ => '_',
        })
        .collect::<String>()
        .trim_matches('_')
        .to_owned();
    if alias.is_empty() { String::new() } else { format!("OPENAI_{alias}_") }
}

pub fn list_models(
    runtime: &OpenAIRuntime,
    limit: Option<usize>,
) -> Result<OpenAIAPIResponse, String> {
    let mut params = Vec::new();
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit", limit.to_string()));
    }
    openai_get(runtime, "/v1/models", &params, false)
}

pub fn get_model(runtime: &OpenAIRuntime, id: &str) -> Result<OpenAIAPIResponse, String> {
    let id = id.trim();
    if id.is_empty() {
        return Err("model id is required".to_owned());
    }
    let escaped = url::form_urlencoded::byte_serialize(id.as_bytes()).collect::<String>();
    openai_get(runtime, &format!("/v1/models/{escaped}"), &[], false)
}

pub fn list_projects(
    runtime: &OpenAIRuntime,
    limit: Option<usize>,
    after: &str,
    include_archived: bool,
) -> Result<OpenAIAPIResponse, String> {
    let mut params = Vec::new();
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit", limit.to_string()));
    }
    if !after.trim().is_empty() {
        params.push(("after", after.trim().to_owned()));
    }
    if include_archived {
        params.push(("include_archived", "true".to_owned()));
    }
    openai_get(runtime, "/v1/organization/projects", &params, true)
}

pub fn get_project(runtime: &OpenAIRuntime, id: &str) -> Result<OpenAIAPIResponse, String> {
    let id = id.trim();
    if id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let escaped = url::form_urlencoded::byte_serialize(id.as_bytes()).collect::<String>();
    openai_get(runtime, &format!("/v1/organization/projects/{escaped}"), &[], true)
}

pub fn create_project(runtime: &OpenAIRuntime, payload: &Value) -> Result<OpenAIAPIResponse, String> {
    openai_send_json(runtime, "POST", "/v1/organization/projects", payload, true)
}

pub fn update_project(
    runtime: &OpenAIRuntime,
    id: &str,
    payload: &Value,
) -> Result<OpenAIAPIResponse, String> {
    let id = id.trim();
    if id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let escaped = url::form_urlencoded::byte_serialize(id.as_bytes()).collect::<String>();
    openai_send_json(runtime, "POST", &format!("/v1/organization/projects/{escaped}"), payload, true)
}

pub fn archive_project(runtime: &OpenAIRuntime, id: &str) -> Result<OpenAIAPIResponse, String> {
    let id = id.trim();
    if id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let escaped = url::form_urlencoded::byte_serialize(id.as_bytes()).collect::<String>();
    openai_send_json(
        runtime,
        "POST",
        &format!("/v1/organization/projects/{escaped}/archive"),
        &Value::Object(Default::default()),
        true,
    )
}

pub fn list_admin_api_keys(
    runtime: &OpenAIRuntime,
    limit: Option<usize>,
    after: &str,
    order: &str,
) -> Result<OpenAIAPIResponse, String> {
    let mut params = Vec::new();
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit", limit.to_string()));
    }
    if !after.trim().is_empty() {
        params.push(("after", after.trim().to_owned()));
    }
    if !order.trim().is_empty() {
        params.push(("order", order.trim().to_ascii_lowercase()));
    }
    openai_get(runtime, "/v1/organization/admin_api_keys", &params, true)
}

pub fn get_admin_api_key(
    runtime: &OpenAIRuntime,
    key_id: &str,
) -> Result<OpenAIAPIResponse, String> {
    let key_id = key_id.trim();
    if key_id.is_empty() {
        return Err("key id is required".to_owned());
    }
    let escaped = url::form_urlencoded::byte_serialize(key_id.as_bytes()).collect::<String>();
    openai_get(runtime, &format!("/v1/organization/admin_api_keys/{escaped}"), &[], true)
}

pub fn create_admin_api_key(
    runtime: &OpenAIRuntime,
    payload: &Value,
) -> Result<OpenAIAPIResponse, String> {
    openai_send_json(runtime, "POST", "/v1/organization/admin_api_keys", payload, true)
}

pub fn delete_admin_api_key(
    runtime: &OpenAIRuntime,
    key_id: &str,
) -> Result<OpenAIAPIResponse, String> {
    let key_id = key_id.trim();
    if key_id.is_empty() {
        return Err("key id is required".to_owned());
    }
    let escaped = url::form_urlencoded::byte_serialize(key_id.as_bytes()).collect::<String>();
    openai_delete(runtime, &format!("/v1/organization/admin_api_keys/{escaped}"), true)
}

pub fn get_usage_metric(
    runtime: &OpenAIRuntime,
    metric: &str,
    params: &[(String, String)],
) -> Result<OpenAIAPIResponse, String> {
    let metric = normalize_usage_metric(metric)?;
    let path = if metric == "costs" {
        "/v1/organization/costs".to_owned()
    } else {
        format!("/v1/organization/usage/{metric}")
    };
    let params =
        params.iter().map(|(key, value)| (key.as_str(), value.clone())).collect::<Vec<_>>();
    openai_get(runtime, &path, &params, true)
}

pub fn list_project_api_keys(
    runtime: &OpenAIRuntime,
    project_id: &str,
    limit: Option<usize>,
    after: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let mut params = Vec::new();
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit", limit.to_string()));
    }
    if !after.trim().is_empty() {
        params.push(("after", after.trim().to_owned()));
    }
    let escaped = url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    openai_get(runtime, &format!("/v1/organization/projects/{escaped}/api_keys"), &params, true)
}

pub fn get_project_api_key(
    runtime: &OpenAIRuntime,
    project_id: &str,
    key_id: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let key_id = key_id.trim();
    if key_id.is_empty() {
        return Err("key id is required".to_owned());
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    let key_id = url::form_urlencoded::byte_serialize(key_id.as_bytes()).collect::<String>();
    openai_get(
        runtime,
        &format!("/v1/organization/projects/{project_id}/api_keys/{key_id}"),
        &[],
        true,
    )
}

pub fn delete_project_api_key(
    runtime: &OpenAIRuntime,
    project_id: &str,
    key_id: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let key_id = key_id.trim();
    if key_id.is_empty() {
        return Err("key id is required".to_owned());
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    let key_id = url::form_urlencoded::byte_serialize(key_id.as_bytes()).collect::<String>();
    openai_delete(
        runtime,
        &format!("/v1/organization/projects/{project_id}/api_keys/{key_id}"),
        true,
    )
}

pub fn list_project_service_accounts(
    runtime: &OpenAIRuntime,
    project_id: &str,
    limit: Option<usize>,
    after: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let mut params = Vec::new();
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit", limit.to_string()));
    }
    if !after.trim().is_empty() {
        params.push(("after", after.trim().to_owned()));
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    openai_get(
        runtime,
        &format!("/v1/organization/projects/{project_id}/service_accounts"),
        &params,
        true,
    )
}

pub fn create_project_service_account(
    runtime: &OpenAIRuntime,
    project_id: &str,
    payload: &Value,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    openai_send_json(
        runtime,
        "POST",
        &format!("/v1/organization/projects/{project_id}/service_accounts"),
        payload,
        true,
    )
}

pub fn list_project_rate_limits(
    runtime: &OpenAIRuntime,
    project_id: &str,
    limit: Option<usize>,
    after: &str,
    before: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let mut params = Vec::new();
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit", limit.to_string()));
    }
    if !after.trim().is_empty() {
        params.push(("after", after.trim().to_owned()));
    }
    if !before.trim().is_empty() {
        params.push(("before", before.trim().to_owned()));
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    openai_get(
        runtime,
        &format!("/v1/organization/projects/{project_id}/rate_limits"),
        &params,
        true,
    )
}

pub fn update_project_rate_limit(
    runtime: &OpenAIRuntime,
    project_id: &str,
    rate_limit_id: &str,
    payload: &Value,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let rate_limit_id = rate_limit_id.trim();
    if rate_limit_id.is_empty() {
        return Err("rate limit id is required".to_owned());
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    let rate_limit_id =
        url::form_urlencoded::byte_serialize(rate_limit_id.as_bytes()).collect::<String>();
    openai_send_json(
        runtime,
        "POST",
        &format!("/v1/organization/projects/{project_id}/rate_limits/{rate_limit_id}"),
        payload,
        true,
    )
}

pub fn get_project_service_account(
    runtime: &OpenAIRuntime,
    project_id: &str,
    service_account_id: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let service_account_id = service_account_id.trim();
    if service_account_id.is_empty() {
        return Err("service account id is required".to_owned());
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    let service_account_id =
        url::form_urlencoded::byte_serialize(service_account_id.as_bytes()).collect::<String>();
    openai_get(
        runtime,
        &format!("/v1/organization/projects/{project_id}/service_accounts/{service_account_id}"),
        &[],
        true,
    )
}

pub fn delete_project_service_account(
    runtime: &OpenAIRuntime,
    project_id: &str,
    service_account_id: &str,
) -> Result<OpenAIAPIResponse, String> {
    let project_id = project_id.trim();
    if project_id.is_empty() {
        return Err("project id is required".to_owned());
    }
    let service_account_id = service_account_id.trim();
    if service_account_id.is_empty() {
        return Err("service account id is required".to_owned());
    }
    let project_id =
        url::form_urlencoded::byte_serialize(project_id.as_bytes()).collect::<String>();
    let service_account_id =
        url::form_urlencoded::byte_serialize(service_account_id.as_bytes()).collect::<String>();
    openai_delete(
        runtime,
        &format!("/v1/organization/projects/{project_id}/service_accounts/{service_account_id}"),
        true,
    )
}

pub fn render_api_response_text(response: &OpenAIAPIResponse, raw: bool) -> String {
    if raw {
        if response.body.trim().is_empty() {
            return String::new();
        }
        return ensure_trailing_newline(response.body.clone());
    }
    let mut out = format!("Status: {} {}\n", response.status_code, response.status.trim());
    if !response.request_id.trim().is_empty() {
        out.push_str(&format!("Request ID: {}\n", response.request_id.trim()));
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

fn normalize_usage_metric(metric: &str) -> Result<&str, String> {
    match metric.trim().to_ascii_lowercase().as_str() {
        "completion" | "completions" => Ok("completions"),
        "embedding" | "embeddings" => Ok("embeddings"),
        "image" | "images" => Ok("images"),
        "audio-speeches" | "audio_speeches" | "speeches" => Ok("audio_speeches"),
        "audio-transcriptions" | "audio_transcriptions" | "transcriptions" => {
            Ok("audio_transcriptions")
        }
        "moderation" | "moderations" => Ok("moderations"),
        "vector-store" | "vector-stores" | "vector_stores" => Ok("vector_stores"),
        "code-interpreter-sessions"
        | "code_interpreter_sessions"
        | "code-interpreter"
        | "code_interpreter" => Ok("code_interpreter_sessions"),
        "cost" | "costs" => Ok("costs"),
        _ => Err("unsupported usage metric".to_owned()),
    }
}

pub fn verify_auth_status(runtime: &OpenAIRuntime) -> OpenAIAuthStatus {
    match list_models(runtime, Some(1)) {
        Ok(response) => OpenAIAuthStatus {
            status: "ready".to_owned(),
            account_alias: runtime.account_alias.clone(),
            organization_id: runtime.organization_id.clone(),
            project_id: runtime.project_id.clone(),
            source: runtime.source.clone(),
            base_url: runtime.base_url.clone(),
            api_key_preview: preview_secret(&runtime.api_key),
            admin_key_set: runtime.admin_key_set,
            verify_status: Some(response.status_code),
            verify: response.data,
            verify_error: None,
        },
        Err(err) => OpenAIAuthStatus {
            status: "error".to_owned(),
            account_alias: runtime.account_alias.clone(),
            organization_id: runtime.organization_id.clone(),
            project_id: runtime.project_id.clone(),
            source: runtime.source.clone(),
            base_url: runtime.base_url.clone(),
            api_key_preview: preview_secret(&runtime.api_key),
            admin_key_set: runtime.admin_key_set,
            verify_status: None,
            verify: None,
            verify_error: Some(err),
        },
    }
}

pub fn render_auth_status_text(status: &OpenAIAuthStatus) -> String {
    let mut out = String::new();
    out.push_str(&format!("OpenAI auth: {}\n", status.status.trim()));
    out.push_str(&format!(
        "Context: account={} base={} org={} project={}\n",
        if status.account_alias.trim().is_empty() {
            "(default)"
        } else {
            status.account_alias.trim()
        },
        status.base_url.trim(),
        or_dash(&status.organization_id),
        or_dash(&status.project_id),
    ));
    if status.status == "ready" {
        out.push_str(&format!("Source: {}\n", or_dash(&status.source)));
        out.push_str(&format!("API key preview: {}\n", or_dash(&status.api_key_preview)));
    } else if let Some(err) = &status.verify_error {
        out.push_str(&format!("OpenAI error: {}\n", err.trim()));
    }
    out
}

fn openai_get(
    runtime: &OpenAIRuntime,
    path: &str,
    params: &[(&str, String)],
    use_admin_key: bool,
) -> Result<OpenAIAPIResponse, String> {
    openai_request(runtime, "GET", path, params, None, use_admin_key)
}

fn openai_send_json(
    runtime: &OpenAIRuntime,
    method: &str,
    path: &str,
    payload: &Value,
    use_admin_key: bool,
) -> Result<OpenAIAPIResponse, String> {
    openai_request(
        runtime,
        method,
        path,
        &[],
        Some(
            serde_json::to_vec(payload)
                .map_err(|err| format!("encode openai request body: {err}"))?,
        ),
        use_admin_key,
    )
}

fn openai_delete(
    runtime: &OpenAIRuntime,
    path: &str,
    use_admin_key: bool,
) -> Result<OpenAIAPIResponse, String> {
    openai_request(runtime, "DELETE", path, &[], None, use_admin_key)
}

fn openai_request(
    runtime: &OpenAIRuntime,
    method: &str,
    path: &str,
    params: &[(&str, String)],
    body: Option<Vec<u8>>,
    use_admin_key: bool,
) -> Result<OpenAIAPIResponse, String> {
    let url = resolve_url(&runtime.base_url, path, params)?;
    let client = Client::builder()
        .timeout(Duration::from_secs(60))
        .build()
        .map_err(|err| format!("build openai http client: {err}"))?;
    let mut headers = HeaderMap::new();
    let token = if use_admin_key {
        let value = runtime.admin_api_key.trim();
        if value.is_empty() {
            return Err("admin api key is required for this command".to_owned());
        }
        value
    } else {
        runtime.api_key.trim()
    };
    let auth_value = HeaderValue::from_str(&format!("Bearer {token}"))
        .map_err(|err| format!("build openai auth header: {err}"))?;
    headers.insert(AUTHORIZATION, auth_value);
    if !runtime.organization_id.trim().is_empty() {
        headers.insert(
            "OpenAI-Organization",
            HeaderValue::from_str(runtime.organization_id.trim())
                .map_err(|err| format!("build openai organization header: {err}"))?,
        );
    }
    if !runtime.project_id.trim().is_empty() {
        headers.insert(
            "OpenAI-Project",
            HeaderValue::from_str(runtime.project_id.trim())
                .map_err(|err| format!("build openai project header: {err}"))?,
        );
    }
    if body.is_some() {
        headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
    }

    let method = reqwest::Method::from_bytes(method.trim().as_bytes())
        .map_err(|err| format!("build openai http method: {err}"))?;
    let mut request = client.request(method, url).headers(headers);
    if let Some(body) = body {
        request = request.body(body);
    }
    let response = request
        .send()
        .map_err(|err| format!("openai request failed: {err}"))?;
    let status = response.status();
    let status_text = status.to_string();
    let request_id = first_header(response.headers(), &["x-request-id", "openai-processing-ms"]);
    let body = response.text().map_err(|err| format!("read openai response body: {err}"))?;
    if !status.is_success() {
        return Err(format!(
            "openai request failed: status={} request_id={} body={}",
            status.as_u16(),
            if request_id.is_empty() { "-" } else { request_id.as_str() },
            body.trim()
        ));
    }
    let data = serde_json::from_str::<Value>(body.trim()).ok();
    Ok(OpenAIAPIResponse {
        status_code: status.as_u16(),
        status: status_text,
        request_id,
        body: body.trim().to_owned(),
        data,
    })
}

fn resolve_url(base_url: &str, path: &str, params: &[(&str, String)]) -> Result<String, String> {
    let mut url = if path.starts_with("http://") || path.starts_with("https://") {
        Url::parse(path).map_err(|err| format!("parse openai url {:?}: {err}", path))?
    } else {
        let base = Url::parse(base_url)
            .map_err(|err| format!("parse openai base url {:?}: {err}", base_url))?;
        let trimmed = if path.starts_with('/') { path.to_owned() } else { format!("/{path}") };
        base.join(&trimmed).map_err(|err| format!("resolve openai path {:?}: {err}", path))?
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
        if let Some(value) = headers.get(*name) {
            if let Ok(value) = value.to_str() {
                let value = value.trim();
                if !value.is_empty() {
                    return value.to_owned();
                }
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

fn preview_secret(value: &str) -> String {
    let value = value.trim();
    if value.is_empty() {
        return String::new();
    }
    if value.len() <= 8 {
        return "****".to_owned();
    }
    format!("{}…{}", &value[..4], &value[value.len() - 4..])
}

fn bool_string(value: bool) -> String {
    if value { "true".to_owned() } else { "false".to_owned() }
}

fn trim_or_empty(value: Option<&str>) -> String {
    value.unwrap_or_default().trim().to_owned()
}

fn first_non_empty(values: &[Option<&str>]) -> String {
    values
        .iter()
        .filter_map(|value| value.map(str::trim))
        .find(|value| !value.is_empty())
        .unwrap_or_default()
        .to_owned()
}

fn first_non_empty_ref<'a>(values: &[Option<&'a str>]) -> Option<&'a str> {
    values.iter().copied().flatten().map(str::trim).find(|value| !value.is_empty())
}

fn join_sources(parts: &[String]) -> String {
    parts
        .iter()
        .map(String::as_str)
        .map(str::trim)
        .filter(|part| !part.is_empty())
        .collect::<Vec<_>>()
        .join(",")
}

fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() { "-" } else { value }
}

fn format_row(columns: &[&str], widths: &[usize]) -> String {
    let mut line = String::new();
    for (idx, column) in columns.iter().enumerate() {
        if idx > 0 {
            line.push_str("  ");
        }
        line.push_str(&format!("{:<width$}", column, width = widths[idx]));
    }
    line.push('\n');
    line
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn current_context_uses_env_and_defaults() {
        let mut env = BTreeMap::new();
        env.insert("OPENAI_CORE_API_KEY".to_owned(), "sk-test".to_owned());
        env.insert("OPENAI_CORE_ORG_ID".to_owned(), "org_123".to_owned());
        let current = resolve_current_context(
            &OpenAISettings {
                default_account: Some("core".to_owned()),
                default_project_id: Some("proj_123".to_owned()),
                ..OpenAISettings::default()
            },
            &env,
            &OpenAIContextOverrides::default(),
        )
        .expect("current context");
        assert_eq!(current.account_alias, "core");
        assert_eq!(current.organization_id, "org_123");
        assert_eq!(current.project_id, "proj_123");
        assert!(!current.source.is_empty());
    }

    #[test]
    fn resolve_runtime_falls_back_to_admin_key_when_api_key_missing() {
        let mut env = BTreeMap::new();
        env.insert("OPENAI_CORE_ADMIN_API_KEY".to_owned(), "sk-admin".to_owned());
        let runtime = resolve_runtime(
            &OpenAISettings {
                default_account: Some("core".to_owned()),
                ..OpenAISettings::default()
            },
            &env,
            &OpenAIContextOverrides::default(),
        )
        .expect("runtime");
        assert_eq!(runtime.api_key, "sk-admin");
        assert!(runtime.admin_key_set);
    }

    #[test]
    fn render_api_response_text_pretty_prints_data() {
        let rendered = render_api_response_text(
            &OpenAIAPIResponse {
                status_code: 200,
                status: "200 OK".to_owned(),
                request_id: "req_123".to_owned(),
                body: "{\"id\":\"gpt-test\"}".to_owned(),
                data: Some(serde_json::json!({"id": "gpt-test"})),
            },
            false,
        );
        assert!(rendered.contains("Status: 200 200 OK"));
        assert!(rendered.contains("\"id\": \"gpt-test\""));
    }

    #[test]
    fn render_auth_status_text_includes_error() {
        let rendered = render_auth_status_text(&OpenAIAuthStatus {
            status: "error".to_owned(),
            account_alias: "core".to_owned(),
            organization_id: String::new(),
            project_id: String::new(),
            source: "env:OPENAI_API_KEY".to_owned(),
            base_url: "https://api.openai.com".to_owned(),
            api_key_preview: "sk-t…1234".to_owned(),
            admin_key_set: false,
            verify_status: None,
            verify: None,
            verify_error: Some("boom".to_owned()),
        });
        assert!(rendered.contains("OpenAI auth: error"));
        assert!(rendered.contains("OpenAI error: boom"));
    }
}
