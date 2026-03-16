use serde::Serialize;
use si_rs_config::settings::{AWSAccountEntry, AWSSettings};
use std::collections::BTreeMap;

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
    })
}

struct AWSRuntimeContext {
    account_alias: String,
    region: String,
    base_url: String,
    access_key: String,
    source: String,
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
    let (_session_token, session_source) =
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

fn preview_access_key(value: &str) -> String {
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
