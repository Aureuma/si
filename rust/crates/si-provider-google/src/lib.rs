use serde::Serialize;
use si_rs_config::settings::{GoogleAccountEntry, GoogleSettings};
use std::collections::BTreeMap;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GooglePlacesContextListEntry {
    pub alias: String,
    pub name: String,
    pub project: String,
    pub default: String,
    pub language: String,
    pub region: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GooglePlacesOverrides {
    pub account: String,
    pub environment: String,
    pub api_key: String,
    pub base_url: String,
    pub project_id: String,
    pub language: String,
    pub region: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GooglePlacesCurrentContext {
    pub account_alias: String,
    pub project_id: String,
    pub environment: String,
    pub language_code: String,
    pub region_code: String,
    pub source: String,
    pub base_url: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GooglePlacesAuthStatus {
    pub account_alias: String,
    pub project_id: String,
    pub environment: String,
    pub language_code: String,
    pub region_code: String,
    pub source: String,
    pub key_preview: String,
    pub base_url: String,
}

pub fn list_places_contexts(settings: &GoogleSettings) -> Vec<GooglePlacesContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, account) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(GooglePlacesContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(account.name.as_deref()),
            project: trim_or_empty(account.project_id.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            language: trim_or_empty(account.default_language_code.as_deref()),
            region: trim_or_empty(account.default_region_code.as_deref()),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_places_context_list_text(rows: &[GooglePlacesContextListEntry]) -> String {
    if rows.is_empty() {
        return "no google accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "PROJECT", "LANGUAGE", "REGION", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.project).len());
        widths[3] = widths[3].max(or_dash(&row.language).len());
        widths[4] = widths[4].max(or_dash(&row.region).len());
        widths[5] = widths[5].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.project),
            or_dash(&row.language),
            or_dash(&row.region),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_places_current_context(
    settings: &GoogleSettings,
    env: &BTreeMap<String, String>,
    overrides: &GooglePlacesOverrides,
) -> Result<GooglePlacesCurrentContext, String> {
    let runtime = resolve_places_runtime_context(settings, env, overrides)?;
    Ok(GooglePlacesCurrentContext {
        account_alias: runtime.account_alias,
        project_id: runtime.project_id,
        environment: runtime.environment,
        language_code: runtime.language_code,
        region_code: runtime.region_code,
        source: runtime.source,
        base_url: runtime.base_url,
    })
}

pub fn resolve_places_auth_status(
    settings: &GoogleSettings,
    env: &BTreeMap<String, String>,
    overrides: &GooglePlacesOverrides,
) -> Result<GooglePlacesAuthStatus, String> {
    let runtime = resolve_places_runtime_context(settings, env, overrides)?;
    Ok(GooglePlacesAuthStatus {
        account_alias: runtime.account_alias,
        project_id: runtime.project_id,
        environment: runtime.environment,
        language_code: runtime.language_code,
        region_code: runtime.region_code,
        source: runtime.source,
        key_preview: preview_secret(&runtime.api_key),
        base_url: runtime.base_url,
    })
}

struct GooglePlacesRuntimeContext {
    account_alias: String,
    project_id: String,
    environment: String,
    language_code: String,
    region_code: String,
    source: String,
    api_key: String,
    base_url: String,
}

fn resolve_places_runtime_context(
    settings: &GoogleSettings,
    env: &BTreeMap<String, String>,
    overrides: &GooglePlacesOverrides,
) -> Result<GooglePlacesRuntimeContext, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let environment = parse_environment(&first_non_empty(&[
        Some(overrides.environment.as_str()),
        settings.default_env.as_deref(),
        env.get("GOOGLE_DEFAULT_ENV").map(String::as_str),
        Some("prod"),
    ]))?;
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        account.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("GOOGLE_API_BASE_URL").map(String::as_str),
        Some("https://places.googleapis.com"),
    ])
    .trim_end_matches('/')
    .to_owned();
    let (project_id, project_source) =
        resolve_project_id(&alias, &account, env, &overrides.project_id);
    let (api_key, key_source) =
        resolve_places_api_key(&alias, &account, env, &environment, &overrides.api_key);
    if api_key.trim().is_empty() {
        let prefix = account_env_prefix(&alias, &account);
        let hint = if prefix.is_empty() { "GOOGLE_<ACCOUNT>_".to_owned() } else { prefix };
        return Err(format!(
            "google places api key not found (set --api-key, {hint}PLACES_API_KEY, or GOOGLE_PLACES_API_KEY)"
        ));
    }
    let (language_code, language_source) =
        resolve_language_code(&alias, &account, env, &overrides.language);
    let (region_code, region_source) =
        resolve_region_code(&alias, &account, env, &overrides.region);
    Ok(GooglePlacesRuntimeContext {
        account_alias: alias,
        project_id,
        environment,
        language_code,
        region_code,
        source: join_sources(&[key_source, project_source, language_source, region_source]),
        api_key,
        base_url,
    })
}

fn resolve_account_selection(
    settings: &GoogleSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, GoogleAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("GOOGLE_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), GoogleAccountEntry::default());
    }
    let account = settings.accounts.get(&selected).cloned().unwrap_or_default();
    (selected, account)
}

fn resolve_project_id(
    alias: &str,
    account: &GoogleAccountEntry,
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
    let prefix = account_env_prefix(alias, account);
    if !prefix.is_empty() {
        let key = format!("{prefix}PROJECT_ID");
        if let Some(value) =
            env.get(&key).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{key}"));
        }
    }
    if let Some(value) = env
        .get("GOOGLE_PROJECT_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GOOGLE_PROJECT_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_places_api_key(
    alias: &str,
    account: &GoogleAccountEntry,
    env: &BTreeMap<String, String>,
    environment: &str,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--api-key".to_owned());
    }
    let prefix = account_env_prefix(alias, account);
    let env_specific = match environment {
        "prod" => account.prod_places_api_key_env.as_deref(),
        "staging" => account.staging_places_api_key_env.as_deref(),
        "dev" => account.dev_places_api_key_env.as_deref(),
        _ => None,
    };
    if let Some(reference) = env_specific.map(str::trim).filter(|value| !value.is_empty()) {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if !prefix.is_empty() {
        let key = match environment {
            "prod" => format!("{prefix}PROD_PLACES_API_KEY"),
            "staging" => format!("{prefix}STAGING_PLACES_API_KEY"),
            "dev" => format!("{prefix}DEV_PLACES_API_KEY"),
            _ => String::new(),
        };
        if !key.is_empty() {
            if let Some(value) =
                env.get(&key).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
            {
                return (value.to_owned(), format!("env:{key}"));
            }
        }
    }
    if let Some(reference) =
        account.places_api_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
    }
    if !prefix.is_empty() {
        let key = format!("{prefix}PLACES_API_KEY");
        if let Some(value) =
            env.get(&key).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{key}"));
        }
    }
    if let Some(value) = env
        .get("GOOGLE_PLACES_API_KEY")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GOOGLE_PLACES_API_KEY".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_language_code(
    alias: &str,
    account: &GoogleAccountEntry,
    env: &BTreeMap<String, String>,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--language".to_owned());
    }
    if let Some(value) =
        account.default_language_code.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.default_language_code".to_owned());
    }
    let prefix = account_env_prefix(alias, account);
    if !prefix.is_empty() {
        let key = format!("{prefix}DEFAULT_LANGUAGE_CODE");
        if let Some(value) =
            env.get(&key).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{key}"));
        }
    }
    if let Some(value) = env
        .get("GOOGLE_DEFAULT_LANGUAGE_CODE")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GOOGLE_DEFAULT_LANGUAGE_CODE".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_region_code(
    alias: &str,
    account: &GoogleAccountEntry,
    env: &BTreeMap<String, String>,
    override_value: &str,
) -> (String, String) {
    if !override_value.trim().is_empty() {
        return (override_value.trim().to_owned(), "flag:--region".to_owned());
    }
    if let Some(value) =
        account.default_region_code.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.default_region_code".to_owned());
    }
    let prefix = account_env_prefix(alias, account);
    if !prefix.is_empty() {
        let key = format!("{prefix}DEFAULT_REGION_CODE");
        if let Some(value) =
            env.get(&key).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return (value.to_owned(), format!("env:{key}"));
        }
    }
    if let Some(value) = env
        .get("GOOGLE_DEFAULT_REGION_CODE")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:GOOGLE_DEFAULT_REGION_CODE".to_owned());
    }
    (String::new(), String::new())
}

fn parse_environment(value: &str) -> Result<String, String> {
    let normalized = value.trim().to_lowercase();
    match normalized.as_str() {
        "" => Ok("prod".to_owned()),
        "prod" | "staging" | "dev" => Ok(normalized),
        _ => Err(format!("invalid google environment {value:?} (expected prod|staging|dev)")),
    }
}

fn account_env_prefix(alias: &str, account: &GoogleAccountEntry) -> String {
    let candidate = first_non_empty(&[account.vault_prefix.as_deref(), Some(alias)])
        .replace('-', "_")
        .to_uppercase();
    if candidate.is_empty() { String::new() } else { format!("GOOGLE_{candidate}_") }
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

fn preview_secret(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return String::new();
    }
    if trimmed.len() <= 6 {
        return "*".repeat(trimmed.len());
    }
    format!("{}{}{}", &trimmed[..3], "*".repeat(trimmed.len() - 6), &trimmed[trimmed.len() - 3..])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn places_api_key_uses_environment_specific_env() {
        let mut env = BTreeMap::new();
        env.insert("GOOGLE_CORE_STAGING_PLACES_API_KEY".to_owned(), "stage-key".to_owned());
        let (value, source) =
            resolve_places_api_key("core", &GoogleAccountEntry::default(), &env, "staging", "");
        assert_eq!(value, "stage-key");
        assert_eq!(source, "env:GOOGLE_CORE_STAGING_PLACES_API_KEY");
    }

    #[test]
    fn current_context_uses_project_and_language_env() {
        let mut env = BTreeMap::new();
        env.insert("GOOGLE_CORE_PROJECT_ID".to_owned(), "proj-core".to_owned());
        env.insert("GOOGLE_CORE_PLACES_API_KEY".to_owned(), "AIza-123456".to_owned());
        env.insert("GOOGLE_CORE_DEFAULT_LANGUAGE_CODE".to_owned(), "en".to_owned());
        let current = resolve_places_current_context(
            &GoogleSettings {
                default_account: Some("core".to_owned()),
                ..GoogleSettings::default()
            },
            &env,
            &GooglePlacesOverrides::default(),
        )
        .expect("current context");
        assert_eq!(current.account_alias, "core");
        assert_eq!(current.project_id, "proj-core");
        assert_eq!(current.language_code, "en");
    }
}
