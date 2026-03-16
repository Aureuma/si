use serde::Serialize;
use si_rs_config::settings::{StripeAccountEntry, StripeSettings};
use std::collections::BTreeMap;

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

struct StripeRuntimeContext {
    account_alias: String,
    account_id: String,
    environment: String,
    api_key: String,
    key_source: String,
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
    Ok(StripeRuntimeContext { account_alias: alias, account_id, environment, api_key, key_source })
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
