use serde::Serialize;
use si_rs_config::settings::{CloudflareAccountEntry, CloudflareSettings};
use std::collections::BTreeMap;

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
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|v| !v.is_empty())
        {
            return (value.to_owned(), format!("env:{reference}"));
        }
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
    if !key.is_empty() {
        if let Some(value) = env_key(alias, account, key, env) {
            return (value, format!("env:{}{}", env_prefix(alias, account), key));
        }
    }
    (String::new(), String::new())
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
}
