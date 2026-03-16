use serde::Serialize;
use si_rs_config::settings::{OpenAIAccountEntry, OpenAISettings};
use std::collections::BTreeMap;

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
    if api_key.trim().is_empty() {
        let prefix = account_env_prefix(&alias, &account);
        let hint = if prefix.is_empty() { "OPENAI_<ACCOUNT>_".to_owned() } else { prefix };
        return Err(format!(
            "openai api key not found (set --api-key, {hint}API_KEY, or OPENAI_API_KEY)"
        ));
    }
    let (admin_key, admin_source) =
        resolve_admin_api_key(&alias, &account, env, &overrides.admin_api_key);
    let (org_id, org_source) = resolve_org_id(&alias, &account, settings, env, &overrides.org_id);
    let (project_id, project_source) =
        resolve_project_id(&alias, &account, settings, env, &overrides.project_id);

    Ok(OpenAICurrentContext {
        account_alias: alias,
        base_url,
        organization_id: org_id,
        project_id,
        source: join_sources(&[api_key_source, admin_source, org_source, project_source]),
        admin_key_set: !admin_key.trim().is_empty(),
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
}
