use serde::Serialize;
use si_rs_config::settings::{OCIAccountEntry, OCISettings};
use std::collections::BTreeMap;
use std::env;
use std::fs;
use std::path::Path;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OCIContextListEntry {
    pub alias: String,
    pub name: String,
    pub default: String,
    pub profile: String,
    pub region: String,
    pub config_file: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct OCIContextOverrides {
    pub account: String,
    pub profile: String,
    pub config_file: String,
    pub region: String,
    pub base_url: String,
    pub auth_style: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OCICurrentContext {
    pub account_alias: String,
    pub profile: String,
    pub config_file: String,
    pub region: String,
    pub base_url: String,
    pub auth_style: String,
    pub source: String,
    pub tenancy_ocid: String,
}

pub fn list_contexts(settings: &OCISettings) -> Vec<OCIContextListEntry> {
    let mut rows = Vec::with_capacity(settings.accounts.len());
    for (alias, account) in &settings.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(OCIContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(account.name.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            profile: first_non_empty(&[account.profile.as_deref(), settings.profile.as_deref()]),
            region: first_non_empty(&[account.region.as_deref(), settings.region.as_deref()]),
            config_file: first_non_empty(&[
                account.config_file.as_deref(),
                settings.config_file.as_deref(),
            ]),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_context_list_text(rows: &[OCIContextListEntry]) -> String {
    if rows.is_empty() {
        return "no oci accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "PROFILE", "REGION", "CONFIG FILE", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.profile).len());
        widths[3] = widths[3].max(or_dash(&row.region).len());
        widths[4] = widths[4].max(or_dash(&row.config_file).len());
        widths[5] = widths[5].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.profile),
            or_dash(&row.region),
            or_dash(&row.config_file),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &OCISettings,
    env_map: &BTreeMap<String, String>,
    overrides: &OCIContextOverrides,
) -> Result<OCICurrentContext, String> {
    let (alias, account) = resolve_account_selection(settings, env_map, &overrides.account);
    let auth_style = normalize_auth_style(&overrides.auth_style)?;

    let profile = {
        let value = first_non_empty(&[
            Some(overrides.profile.as_str()),
            account.profile.as_deref(),
            env_map.get("OCI_CLI_PROFILE").map(String::as_str),
            settings.profile.as_deref(),
            Some("DEFAULT"),
        ]);
        if value.is_empty() { "DEFAULT".to_owned() } else { value }
    };
    let config_file = expand_tilde(&first_non_empty(&[
        Some(overrides.config_file.as_str()),
        account.config_file.as_deref(),
        env_map.get("OCI_CONFIG_FILE").map(String::as_str),
        settings.config_file.as_deref(),
        Some("~/.oci/config"),
    ]));
    let mut region = first_non_empty(&[
        Some(overrides.region.as_str()),
        account.region.as_deref(),
        env_map.get("OCI_CLI_REGION").map(String::as_str),
        settings.region.as_deref(),
    ]);
    let mut source = Vec::new();
    let mut tenancy_ocid = first_non_empty(&[
        account.tenancy_ocid.as_deref(),
        resolve_env_reference(account.tenancy_ocid_env.as_deref(), env_map).as_deref(),
    ]);

    if auth_style == "signature" {
        let profile_values = parse_config_profile(&config_file, &profile)?;
        source.push(format!("profile:{profile}"));
        if tenancy_ocid.is_empty() {
            tenancy_ocid = trim_or_empty(profile_values.get("tenancy").map(String::as_str));
        }
        if region.is_empty() {
            region = trim_or_empty(profile_values.get("region").map(String::as_str));
        }
    }
    if region.is_empty() {
        region = "us-ashburn-1".to_owned();
    }
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        settings.api_base_url.as_deref(),
        Some(oci_core_url(&region).as_str()),
    ])
    .trim_end_matches('/')
    .to_owned();

    Ok(OCICurrentContext {
        account_alias: alias,
        profile,
        config_file,
        region: region.clone(),
        base_url,
        auth_style,
        source: source.join(","),
        tenancy_ocid,
    })
}

fn resolve_account_selection(
    settings: &OCISettings,
    env_map: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, OCIAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env_map.get("OCI_DEFAULT_ACCOUNT").map(String::as_str),
    ]);
    if selected.is_empty() && settings.accounts.len() == 1 {
        selected = settings.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), OCIAccountEntry::default());
    }
    let account = settings.accounts.get(&selected).cloned().unwrap_or_default();
    (selected, account)
}

fn normalize_auth_style(value: &str) -> Result<String, String> {
    let value = value.trim().to_lowercase();
    let normalized = if value.is_empty() { "signature".to_owned() } else { value.clone() };
    match normalized.as_str() {
        "signature" | "none" => Ok(normalized),
        _ => Err(format!("invalid oci auth style {:?} (expected signature|none)", value.trim())),
    }
}

fn parse_config_profile(
    config_file: &str,
    profile: &str,
) -> Result<BTreeMap<String, String>, String> {
    if config_file.trim().is_empty() {
        return Err("oci config file path is required".to_owned());
    }
    let raw = fs::read_to_string(config_file)
        .map_err(|err| format!("read oci config {:?}: {err}", config_file))?;
    let profile = if profile.trim().is_empty() { "DEFAULT" } else { profile.trim() };
    let mut profiles: BTreeMap<String, BTreeMap<String, String>> = BTreeMap::new();
    let mut current = String::new();
    for line in raw.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with(';') {
            continue;
        }
        if line.starts_with('[') && line.ends_with(']') {
            current = line[1..line.len() - 1].trim().to_owned();
            profiles.entry(current.clone()).or_default();
            continue;
        }
        let Some((key, value)) = line.split_once('=') else { continue };
        if current.is_empty() {
            continue;
        }
        profiles
            .entry(current.clone())
            .or_default()
            .insert(key.trim().to_owned(), value.trim().to_owned());
    }
    profiles
        .remove(profile)
        .ok_or_else(|| format!("oci profile {:?} not found in {:?}", profile, config_file))
}

fn resolve_env_reference(
    reference: Option<&str>,
    env_map: &BTreeMap<String, String>,
) -> Option<String> {
    let reference = reference?.trim();
    if reference.is_empty() {
        return None;
    }
    env_map
        .get(reference)
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
}

fn expand_tilde(value: &str) -> String {
    let value = value.trim();
    if value == "~" {
        return home_dir().unwrap_or_else(|| "~".to_owned());
    }
    if let Some(rest) = value.strip_prefix("~/") {
        return Path::new(&home_dir().unwrap_or_else(|| "~".to_owned()))
            .join(rest)
            .display()
            .to_string();
    }
    value.to_owned()
}

fn home_dir() -> Option<String> {
    env::var("HOME").ok().filter(|value| !value.trim().is_empty())
}

fn oci_core_url(region: &str) -> String {
    let region = region.trim();
    if region.is_empty() {
        "https://iaas.us-ashburn-1.oraclecloud.com".to_owned()
    } else {
        format!("https://iaas.{region}.oraclecloud.com")
    }
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
    fn current_context_reads_profile_values() {
        let temp = env::temp_dir().join(format!("si-rs-oci-{}", std::process::id()));
        let _ = fs::create_dir_all(&temp);
        let config = temp.join("config");
        fs::write(&config, "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nregion=us-phoenix-1\n")
            .expect("write config");
        let current = resolve_current_context(
            &OCISettings::default(),
            &BTreeMap::new(),
            &OCIContextOverrides {
                config_file: config.display().to_string(),
                ..OCIContextOverrides::default()
            },
        )
        .expect("current context");
        assert_eq!(current.profile, "DEFAULT");
        assert_eq!(current.region, "us-phoenix-1");
        assert_eq!(current.tenancy_ocid, "ocid1.tenancy.oc1..example");
    }
}
