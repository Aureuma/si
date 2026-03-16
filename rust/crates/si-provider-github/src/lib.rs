use serde::Serialize;
use si_rs_config::settings::{GitHubAccountEntry, GitHubSettings};
use std::collections::BTreeMap;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GitHubContextListEntry {
    pub alias: String,
    pub name: String,
    pub owner: String,
    pub auth_mode: String,
    pub default: String,
    pub api_base_url: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GitHubCurrentContext {
    pub account_alias: String,
    pub owner: String,
    pub auth_mode: String,
    pub base_url: String,
    pub source: String,
}

pub fn list_contexts(settings: &GitHubSettings) -> Vec<GitHubContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, entry) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(build_list_entry(alias, entry, settings));
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn resolve_current_context(
    settings: &GitHubSettings,
    env: &BTreeMap<String, String>,
) -> GitHubCurrentContext {
    let selected_account = resolve_account_selection(settings, env);
    let entry = selected_account.as_ref().and_then(|alias| settings.accounts.get(alias));

    let owner = first_non_empty(&[
        entry.and_then(|item| item.owner.as_deref()),
        settings.default_owner.as_deref(),
        env.get("GITHUB_DEFAULT_OWNER").map(String::as_str),
    ])
    .to_owned();
    let base_url = first_non_empty(&[
        entry.and_then(|item| item.api_base_url.as_deref()),
        settings.api_base_url.as_deref(),
        env.get("GITHUB_API_BASE_URL").map(String::as_str),
        Some("https://api.github.com"),
    ])
    .to_owned();
    let auth_mode = first_non_empty(&[
        entry.and_then(|item| item.auth_mode.as_deref()),
        env.get("GITHUB_AUTH_MODE").map(String::as_str),
        env.get("GITHUB_DEFAULT_AUTH_MODE").map(String::as_str),
        settings.default_auth_mode.as_deref(),
        Some("app"),
    ])
    .to_owned();

    let mut source = Vec::new();
    if let Some(alias) = selected_account.as_deref() {
        if settings.default_account.as_deref().map(str::trim) == Some(alias) {
            source.push("settings.default_account".to_owned());
        } else if env
            .get("GITHUB_DEFAULT_ACCOUNT")
            .map(|value| value.trim() == alias)
            .unwrap_or(false)
        {
            source.push("env:GITHUB_DEFAULT_ACCOUNT".to_owned());
        }
    }
    if entry.and_then(|item| item.auth_mode.as_deref()).map(str::trim) == Some(auth_mode.as_str()) {
        source.push("settings.auth_mode".to_owned());
    } else if env.get("GITHUB_AUTH_MODE").map(|value| value.trim() == auth_mode).unwrap_or(false) {
        source.push("env:GITHUB_AUTH_MODE".to_owned());
    } else if env
        .get("GITHUB_DEFAULT_AUTH_MODE")
        .map(|value| value.trim() == auth_mode)
        .unwrap_or(false)
    {
        source.push("env:GITHUB_DEFAULT_AUTH_MODE".to_owned());
    } else if settings.default_auth_mode.as_deref().map(str::trim) == Some(auth_mode.as_str()) {
        source.push("settings.default_auth_mode".to_owned());
    }

    GitHubCurrentContext {
        account_alias: selected_account.unwrap_or_default(),
        owner,
        auth_mode,
        base_url,
        source: source.join(","),
    }
}

fn build_list_entry(
    alias: &str,
    entry: &GitHubAccountEntry,
    settings: &GitHubSettings,
) -> GitHubContextListEntry {
    let auth_mode = first_non_empty(&[
        entry.auth_mode.as_deref(),
        settings.default_auth_mode.as_deref(),
        Some("app"),
    ]);
    GitHubContextListEntry {
        alias: alias.to_owned(),
        name: trim_or_empty(entry.name.as_deref()),
        owner: trim_or_empty(entry.owner.as_deref()),
        auth_mode: auth_mode.to_owned(),
        default: bool_string(
            alias == settings.default_account.as_deref().unwrap_or_default().trim(),
        ),
        api_base_url: trim_or_empty(entry.api_base_url.as_deref()),
    }
}

pub fn render_context_list_text(rows: &[GitHubContextListEntry]) -> String {
    if rows.is_empty() {
        return "no github accounts configured in settings\n".to_owned();
    }

    let headers = ["ALIAS", "DEFAULT", "AUTH", "OWNER", "BASE URL", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(or_dash(&row.default).len());
        widths[2] = widths[2].max(or_dash(&row.auth_mode).len());
        widths[3] = widths[3].max(or_dash(&row.owner).len());
        widths[4] = widths[4].max(or_dash(&row.api_base_url).len());
        widths[5] = widths[5].max(or_dash(&row.name).len());
    }

    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            or_dash(&row.default),
            or_dash(&row.auth_mode),
            or_dash(&row.owner),
            or_dash(&row.api_base_url),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

fn format_row<const N: usize>(cols: &[&str; N], widths: &[usize; N]) -> String {
    let mut line = String::new();
    for (idx, col) in cols.iter().enumerate() {
        if idx > 0 {
            line.push_str("  ");
        }
        let padded = format!("{col:<width$}", width = widths[idx]);
        line.push_str(&padded);
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

fn bool_string(value: bool) -> String {
    if value { "true".to_owned() } else { "false".to_owned() }
}

fn resolve_account_selection(
    settings: &GitHubSettings,
    env: &BTreeMap<String, String>,
) -> Option<String> {
    let selected = first_non_empty(&[
        settings.default_account.as_deref(),
        env.get("GITHUB_DEFAULT_ACCOUNT").map(String::as_str),
    ]);
    if !selected.is_empty() {
        return Some(selected.to_owned());
    }
    if settings.accounts.len() == 1 {
        return settings.accounts.keys().next().cloned();
    }
    None
}

fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() { "-" } else { value }
}

#[cfg(test)]
mod tests {
    use super::*;
    use si_rs_config::settings::{GitHubAccountEntry, GitHubSettings};

    #[test]
    fn list_contexts_applies_defaults_and_sorts() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "beta".to_owned(),
            GitHubAccountEntry {
                owner: Some("BetaOrg".to_owned()),
                ..GitHubAccountEntry::default()
            },
        );
        accounts.insert(
            "alpha".to_owned(),
            GitHubAccountEntry {
                name: Some("Alpha".to_owned()),
                auth_mode: Some("oauth".to_owned()),
                api_base_url: Some("https://ghe.example/api/v3".to_owned()),
                ..GitHubAccountEntry::default()
            },
        );
        let settings = GitHubSettings {
            default_account: Some("beta".to_owned()),
            default_auth_mode: Some("app".to_owned()),
            accounts,
            ..GitHubSettings::default()
        };

        let rows = list_contexts(&settings);
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0].alias, "alpha");
        assert_eq!(rows[0].auth_mode, "oauth");
        assert_eq!(rows[1].alias, "beta");
        assert_eq!(rows[1].auth_mode, "app");
        assert_eq!(rows[1].default, "true");
    }

    #[test]
    fn render_context_list_text_handles_empty() {
        let text = render_context_list_text(&[]);
        assert_eq!(text, "no github accounts configured in settings\n");
    }

    #[test]
    fn resolve_current_context_uses_settings_and_env() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            GitHubAccountEntry {
                owner: Some("Aureuma".to_owned()),
                auth_mode: Some("oauth".to_owned()),
                api_base_url: Some("https://ghe.example/api/v3".to_owned()),
                ..GitHubAccountEntry::default()
            },
        );
        let settings = GitHubSettings {
            default_account: Some("core".to_owned()),
            default_auth_mode: Some("app".to_owned()),
            accounts,
            ..GitHubSettings::default()
        };
        let env = BTreeMap::new();

        let resolved = resolve_current_context(&settings, &env);
        assert_eq!(resolved.account_alias, "core");
        assert_eq!(resolved.owner, "Aureuma");
        assert_eq!(resolved.auth_mode, "oauth");
        assert_eq!(resolved.base_url, "https://ghe.example/api/v3");
        assert!(resolved.source.contains("settings.default_account"));
        assert!(resolved.source.contains("settings.auth_mode"));
    }
}
