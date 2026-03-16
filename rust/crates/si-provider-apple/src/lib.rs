use serde::Serialize;
use si_rs_config::settings::{AppleAppStoreAccountEntry, AppleSettings};
use std::collections::BTreeMap;
use std::fs;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct AppleAppStoreContextListEntry {
    pub alias: String,
    pub name: String,
    pub project: String,
    pub default: String,
    pub bundle_id: String,
    pub platform: String,
    pub language: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct AppleAppStoreCurrentContext {
    pub account_alias: String,
    pub project_id: String,
    pub environment: String,
    pub source: String,
    pub token_source: String,
    pub bundle_id: String,
    pub locale: String,
    pub platform: String,
    pub base_url: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AppleAppStoreAuthOverrides {
    pub account: String,
    pub environment: String,
    pub bundle_id: String,
    pub locale: String,
    pub platform: String,
    pub issuer_id: String,
    pub key_id: String,
    pub private_key: String,
    pub private_key_file: String,
    pub project_id: String,
    pub base_url: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct AppleAppStoreAuthStatus {
    pub status: String,
    pub account_alias: String,
    pub project_id: String,
    pub environment: String,
    pub source: String,
    pub token_source: String,
    pub bundle_id: String,
    pub locale: String,
    pub platform: String,
    pub base_url: String,
}

pub fn list_appstore_contexts(settings: &AppleSettings) -> Vec<AppleAppStoreContextListEntry> {
    let mut rows = Vec::with_capacity(settings.appstore.accounts.len());
    for (alias, account) in &settings.appstore.accounts {
        let alias = alias.trim();
        if alias.is_empty() {
            continue;
        }
        rows.push(AppleAppStoreContextListEntry {
            alias: alias.to_owned(),
            name: trim_or_empty(account.name.as_deref()),
            project: trim_or_empty(account.project_id.as_deref()),
            default: bool_string(
                alias == settings.default_account.as_deref().unwrap_or_default().trim(),
            ),
            bundle_id: trim_or_empty(account.default_bundle_id.as_deref()),
            platform: trim_or_empty(account.default_platform.as_deref()),
            language: trim_or_empty(account.default_language.as_deref()),
        });
    }
    rows.sort_by(|left, right| left.alias.cmp(&right.alias));
    rows
}

pub fn render_appstore_context_list_text(rows: &[AppleAppStoreContextListEntry]) -> String {
    if rows.is_empty() {
        return "no apple appstore accounts configured in settings\n".to_owned();
    }
    let headers = ["ALIAS", "DEFAULT", "PROJECT", "BUNDLE ID", "PLATFORM", "LANG", "NAME"];
    let mut widths = headers.map(str::len);
    for row in rows {
        widths[0] = widths[0].max(row.alias.len());
        widths[1] = widths[1].max(row.default.len());
        widths[2] = widths[2].max(or_dash(&row.project).len());
        widths[3] = widths[3].max(or_dash(&row.bundle_id).len());
        widths[4] = widths[4].max(or_dash(&row.platform).len());
        widths[5] = widths[5].max(or_dash(&row.language).len());
        widths[6] = widths[6].max(or_dash(&row.name).len());
    }
    let mut out = String::new();
    out.push_str(&format_row(&headers, &widths));
    for row in rows {
        let cols = [
            row.alias.as_str(),
            row.default.as_str(),
            or_dash(&row.project),
            or_dash(&row.bundle_id),
            or_dash(&row.platform),
            or_dash(&row.language),
            or_dash(&row.name),
        ];
        out.push_str(&format_row(&cols, &widths));
    }
    out
}

pub fn resolve_current_context(
    settings: &AppleSettings,
    env: &BTreeMap<String, String>,
) -> Result<AppleAppStoreCurrentContext, String> {
    let (alias, account) = resolve_account_selection(settings, env, "");
    let environment = resolve_environment(settings, env, "")?;
    let base_url = first_non_empty(&[
        settings.appstore.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("APPLE_APPSTORE_API_BASE_URL").map(String::as_str),
        Some("https://api.appstoreconnect.apple.com"),
    ])
    .to_owned();

    let (project_id, project_source) = resolve_project_id(&alias, &account, env);
    let (bundle_id, bundle_source) = resolve_bundle_id(&alias, &account, env);
    let (locale, locale_source) = resolve_locale(&alias, &account, env);
    let (platform, platform_source) = resolve_platform(&alias, &account, env)?;
    let (_, issuer_source) = resolve_issuer_id(&alias, &account, env)?;
    let (_, key_source) = resolve_key_id(&alias, &account, env)?;
    let token_source = resolve_private_key_source(&alias, &account, env)?;
    let source = join_sources(&[
        project_source,
        bundle_source,
        locale_source,
        platform_source,
        issuer_source,
        key_source,
    ]);

    Ok(AppleAppStoreCurrentContext {
        account_alias: alias,
        project_id,
        environment,
        source,
        token_source,
        bundle_id,
        locale,
        platform,
        base_url,
    })
}

pub fn resolve_auth_status(
    settings: &AppleSettings,
    env: &BTreeMap<String, String>,
    overrides: &AppleAppStoreAuthOverrides,
) -> Result<AppleAppStoreAuthStatus, String> {
    let (alias, account) = resolve_account_selection(settings, env, &overrides.account);
    let environment = resolve_environment(settings, env, &overrides.environment)?;
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        settings.appstore.api_base_url.as_deref(),
        settings.api_base_url.as_deref(),
        env.get("APPLE_APPSTORE_API_BASE_URL").map(String::as_str),
        Some("https://api.appstoreconnect.apple.com"),
    ])
    .to_owned();

    let (project_id, project_source) =
        resolve_project_id_with_override(overrides.project_id.as_str(), &alias, &account, env);
    let (bundle_id, bundle_source) =
        resolve_bundle_id_with_override(overrides.bundle_id.as_str(), &alias, &account, env);
    let (locale, locale_source) =
        resolve_locale_with_override(overrides.locale.as_str(), &alias, &account, env);
    let (platform, platform_source) = resolve_platform_with_override(
        overrides.platform.as_str(),
        &alias,
        &account,
        env,
    )?;
    let (_, issuer_source) =
        resolve_issuer_id_with_override(overrides.issuer_id.as_str(), &alias, &account, env)?;
    let (_, key_source) =
        resolve_key_id_with_override(overrides.key_id.as_str(), &alias, &account, env)?;
    let token_source = resolve_private_key_source_with_override(
        overrides.private_key.as_str(),
        overrides.private_key_file.as_str(),
        &alias,
        &account,
        env,
    )?;
    let source = join_sources(&[
        project_source,
        bundle_source,
        locale_source,
        platform_source,
        issuer_source,
        key_source,
    ]);

    Ok(AppleAppStoreAuthStatus {
        status: "ready".to_owned(),
        account_alias: alias,
        project_id,
        environment,
        source,
        token_source,
        bundle_id,
        locale,
        platform,
        base_url,
    })
}

fn resolve_account_selection(
    settings: &AppleSettings,
    env: &BTreeMap<String, String>,
    override_account: &str,
) -> (String, AppleAppStoreAccountEntry) {
    let mut selected = first_non_empty(&[
        Some(override_account),
        settings.default_account.as_deref(),
        env.get("APPLE_DEFAULT_ACCOUNT").map(String::as_str),
    ])
    .to_owned();
    if selected.is_empty() && settings.appstore.accounts.len() == 1 {
        selected = settings.appstore.accounts.keys().next().cloned().unwrap_or_default();
    }
    if selected.is_empty() {
        return (String::new(), AppleAppStoreAccountEntry::default());
    }
    if let Some(account) = settings.appstore.accounts.get(&selected) {
        return (selected, account.clone());
    }
    (selected, AppleAppStoreAccountEntry::default())
}

fn resolve_environment(
    settings: &AppleSettings,
    env: &BTreeMap<String, String>,
    override_environment: &str,
) -> Result<String, String> {
    let raw = first_non_empty(&[
        Some(override_environment),
        settings.default_env.as_deref(),
        env.get("APPLE_DEFAULT_ENV").map(String::as_str),
        Some("prod"),
    ]);
    normalize_environment(Some(raw))
        .map(str::to_owned)
        .ok_or_else(|| "environment required (prod|staging|dev)".to_owned())
}

fn resolve_project_id_with_override(
    override_project_id: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    let override_project_id = override_project_id.trim();
    if !override_project_id.is_empty() {
        return (override_project_id.to_owned(), "flag:--project-id".to_owned());
    }
    resolve_project_id(alias, account, env)
}

fn resolve_project_id(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    if let Some(value) =
        account.project_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.apple.project_id".to_owned());
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
    if let Some(value) = account_env(alias, account, "PROJECT_ID", env) {
        return (value, format!("env:{}PROJECT_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("APPLE_PROJECT_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:APPLE_PROJECT_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_bundle_id_with_override(
    override_bundle_id: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    let override_bundle_id = override_bundle_id.trim();
    if !override_bundle_id.is_empty() {
        return (override_bundle_id.to_owned(), "flag:--bundle-id".to_owned());
    }
    resolve_bundle_id(alias, account, env)
}

fn resolve_bundle_id(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    if let Some(value) =
        account.default_bundle_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "settings.apple.default_bundle_id".to_owned());
    }
    if let Some(value) = account_env(alias, account, "APPSTORE_BUNDLE_ID", env) {
        return (value, format!("env:{}APPSTORE_BUNDLE_ID", account_env_prefix(alias, account)));
    }
    if let Some(value) = env
        .get("APPLE_APPSTORE_BUNDLE_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return (value.to_owned(), "env:APPLE_APPSTORE_BUNDLE_ID".to_owned());
    }
    (String::new(), String::new())
}

fn resolve_locale_with_override(
    override_locale: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    if let Some(locale) = normalize_locale(Some(override_locale)) {
        return (locale.to_owned(), "flag:--locale".to_owned());
    }
    resolve_locale(alias, account, env)
}

fn resolve_locale(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> (String, String) {
    if let Some(value) = normalize_locale(account.default_language.as_deref()) {
        return (value.to_owned(), "settings.apple.default_language".to_owned());
    }
    if let Some(value) = account_env(alias, account, "APPSTORE_LOCALE", env) {
        if let Some(locale) = normalize_locale(Some(value.as_str())) {
            return (
                locale.to_owned(),
                format!("env:{}APPSTORE_LOCALE", account_env_prefix(alias, account)),
            );
        }
    }
    if let Some(locale) = normalize_locale(env.get("APPLE_APPSTORE_LOCALE").map(String::as_str)) {
        return (locale.to_owned(), "env:APPLE_APPSTORE_LOCALE".to_owned());
    }
    ("en-US".to_owned(), "default".to_owned())
}

fn resolve_platform_with_override(
    override_platform: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<(String, String), String> {
    let override_platform = override_platform.trim();
    if !override_platform.is_empty() {
        if let Some(platform) = normalize_platform(Some(override_platform)) {
            return Ok((platform.to_owned(), "flag:--platform".to_owned()));
        }
        return Err(format!(
            "invalid --platform {override_platform:?} (expected IOS|MAC_OS|TV_OS|VISION_OS)"
        ));
    }
    resolve_platform(alias, account, env)
}

fn resolve_platform(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<(String, String), String> {
    if let Some(value) = normalize_platform(account.default_platform.as_deref()) {
        return Ok((value.to_owned(), "settings.apple.default_platform".to_owned()));
    }
    if let Some(value) = account_env(alias, account, "APPSTORE_PLATFORM", env) {
        if let Some(platform) = normalize_platform(Some(value.as_str())) {
            return Ok((
                platform.to_owned(),
                format!("env:{}APPSTORE_PLATFORM", account_env_prefix(alias, account)),
            ));
        }
        return Err(format!(
            "invalid APPSTORE_PLATFORM {value:?} (expected IOS|MAC_OS|TV_OS|VISION_OS)"
        ));
    }
    if let Some(value) = env.get("APPLE_APPSTORE_PLATFORM").map(String::as_str) {
        if let Some(platform) = normalize_platform(Some(value)) {
            return Ok((platform.to_owned(), "env:APPLE_APPSTORE_PLATFORM".to_owned()));
        }
        return Err(format!(
            "invalid APPLE_APPSTORE_PLATFORM {:?} (expected IOS|MAC_OS|TV_OS|VISION_OS)",
            value.trim()
        ));
    }
    Ok(("IOS".to_owned(), "default".to_owned()))
}

fn resolve_issuer_id_with_override(
    override_issuer_id: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<(String, String), String> {
    let override_issuer_id = override_issuer_id.trim();
    if !override_issuer_id.is_empty() {
        return Ok((override_issuer_id.to_owned(), "flag:--issuer-id".to_owned()));
    }
    resolve_issuer_id(alias, account, env)
}

fn resolve_issuer_id(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<(String, String), String> {
    if let Some(value) =
        account.issuer_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return Ok((value.to_owned(), "settings.apple.issuer_id".to_owned()));
    }
    if let Some(reference) =
        account.issuer_id_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return Ok((value.to_owned(), format!("env:{reference}")));
        }
    }
    if let Some(value) = account_env(alias, account, "APPSTORE_ISSUER_ID", env) {
        return Ok((
            value,
            format!("env:{}APPSTORE_ISSUER_ID", account_env_prefix(alias, account)),
        ));
    }
    if let Some(value) = env
        .get("APPLE_APPSTORE_ISSUER_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return Ok((value.to_owned(), "env:APPLE_APPSTORE_ISSUER_ID".to_owned()));
    }
    Err("apple appstore issuer id not found (set APPLE_<ACCOUNT>_APPSTORE_ISSUER_ID or APPLE_APPSTORE_ISSUER_ID)".to_owned())
}

fn resolve_key_id_with_override(
    override_key_id: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<(String, String), String> {
    let override_key_id = override_key_id.trim();
    if !override_key_id.is_empty() {
        return Ok((override_key_id.to_owned(), "flag:--key-id".to_owned()));
    }
    resolve_key_id(alias, account, env)
}

fn resolve_key_id(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<(String, String), String> {
    if let Some(value) = account.key_id.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return Ok((value.to_owned(), "settings.apple.key_id".to_owned()));
    }
    if let Some(reference) =
        account.key_id_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return Ok((value.to_owned(), format!("env:{reference}")));
        }
    }
    if let Some(value) = account_env(alias, account, "APPSTORE_KEY_ID", env) {
        return Ok((value, format!("env:{}APPSTORE_KEY_ID", account_env_prefix(alias, account))));
    }
    if let Some(value) = env
        .get("APPLE_APPSTORE_KEY_ID")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return Ok((value.to_owned(), "env:APPLE_APPSTORE_KEY_ID".to_owned()));
    }
    Err("apple appstore key id not found (set APPLE_<ACCOUNT>_APPSTORE_KEY_ID or APPLE_APPSTORE_KEY_ID)".to_owned())
}

fn resolve_private_key_source_with_override(
    override_private_key: &str,
    override_private_key_file: &str,
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<String, String> {
    let override_private_key = override_private_key.trim();
    if !override_private_key.is_empty() {
        return validate_private_key_value(override_private_key)
            .map(|_| "flag:--private-key".to_owned());
    }
    let override_private_key_file = override_private_key_file.trim();
    if !override_private_key_file.is_empty() {
        return validate_private_key_file(override_private_key_file)
            .map(|_| "flag:--private-key-file".to_owned());
    }
    resolve_private_key_source(alias, account, env)
}

fn resolve_private_key_source(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
    env: &BTreeMap<String, String>,
) -> Result<String, String> {
    if let Some(value) =
        account.private_key_pem.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return validate_private_key_value(value)
            .map(|_| "settings.apple.private_key_pem".to_owned());
    }
    if let Some(reference) =
        account.private_key_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(value) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return validate_private_key_value(value).map(|_| format!("env:{reference}"));
        }
    }
    if let Some(value) = account_env(alias, account, "APPSTORE_PRIVATE_KEY_PEM", env) {
        return validate_private_key_value(&value).map(|_| {
            format!("env:{}APPSTORE_PRIVATE_KEY_PEM", account_env_prefix(alias, account))
        });
    }
    if let Some(path) =
        account.private_key_file.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return validate_private_key_file(path)
            .map(|_| "settings.apple.private_key_file".to_owned());
    }
    if let Some(reference) =
        account.private_key_file_env.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        if let Some(path) =
            env.get(reference).map(String::as_str).map(str::trim).filter(|value| !value.is_empty())
        {
            return validate_private_key_file(path).map(|_| format!("env:{reference}"));
        }
    }
    if let Some(path) = account_env(alias, account, "APPSTORE_PRIVATE_KEY_FILE", env) {
        return validate_private_key_file(&path).map(|_| {
            format!("env:{}APPSTORE_PRIVATE_KEY_FILE", account_env_prefix(alias, account))
        });
    }
    if let Some(value) = env
        .get("APPLE_APPSTORE_PRIVATE_KEY_PEM")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return validate_private_key_value(value)
            .map(|_| "env:APPLE_APPSTORE_PRIVATE_KEY_PEM".to_owned());
    }
    if let Some(path) = env
        .get("APPLE_APPSTORE_PRIVATE_KEY_FILE")
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return validate_private_key_file(path)
            .map(|_| "env:APPLE_APPSTORE_PRIVATE_KEY_FILE".to_owned());
    }
    Err("apple appstore private key not found (set APPLE_<ACCOUNT>_APPSTORE_PRIVATE_KEY_PEM, APPLE_<ACCOUNT>_APPSTORE_PRIVATE_KEY_FILE, APPLE_APPSTORE_PRIVATE_KEY_PEM, or APPLE_APPSTORE_PRIVATE_KEY_FILE)".to_owned())
}

fn validate_private_key_value(raw: &str) -> Result<(), String> {
    let value = raw.trim();
    if value.is_empty() {
        return Err("apple appstore private key is empty".to_owned());
    }
    if let Some(path) = value.strip_prefix('@') {
        return validate_private_key_file(path.trim());
    }
    if looks_like_key_path(value) {
        return validate_private_key_file(value);
    }
    Ok(())
}

fn validate_private_key_file(path: &str) -> Result<(), String> {
    let content = fs::read_to_string(path)
        .map_err(|err| format!("read apple appstore private key file {path}: {err}"))?;
    if content.trim().is_empty() {
        return Err(format!("apple appstore private key file {path} is empty"));
    }
    Ok(())
}

fn looks_like_key_path(value: &str) -> bool {
    let lower = value.to_ascii_lowercase();
    (lower.ends_with(".p8") || lower.ends_with(".pem")) && fs::metadata(value).is_ok()
}

fn normalize_environment(value: Option<&str>) -> Option<&'static str> {
    match value.unwrap_or_default().trim().to_ascii_lowercase().as_str() {
        "prod" => Some("prod"),
        "staging" => Some("staging"),
        "dev" => Some("dev"),
        _ => None,
    }
}

fn normalize_platform(value: Option<&str>) -> Option<&'static str> {
    match value.unwrap_or_default().trim().to_ascii_uppercase().as_str() {
        "IOS" => Some("IOS"),
        "MAC_OS" => Some("MAC_OS"),
        "TV_OS" => Some("TV_OS"),
        "VISION_OS" => Some("VISION_OS"),
        _ => None,
    }
}

fn normalize_locale(value: Option<&str>) -> Option<&str> {
    let value = value.unwrap_or_default().trim();
    if value.is_empty() { None } else { Some(value) }
}

fn account_env_prefix(alias: &str, account: &AppleAppStoreAccountEntry) -> String {
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
    if slug.is_empty() { String::new() } else { format!("APPLE_{slug}_") }
}

fn account_env(
    alias: &str,
    account: &AppleAppStoreAccountEntry,
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
            AppleSettings { default_account: Some("core".to_owned()), ..AppleSettings::default() };
        settings.appstore.accounts.insert(
            "core".to_owned(),
            AppleAppStoreAccountEntry {
                project_id: Some("proj_core".to_owned()),
                ..AppleAppStoreAccountEntry::default()
            },
        );

        let rows = list_appstore_contexts(&settings);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].alias, "core");
        assert_eq!(rows[0].default, "true");
    }

    #[test]
    fn current_context_uses_settings_and_env_sources() {
        let mut settings = AppleSettings {
            default_account: Some("core".to_owned()),
            default_env: Some("prod".to_owned()),
            ..AppleSettings::default()
        };
        settings.appstore.accounts.insert(
            "core".to_owned(),
            AppleAppStoreAccountEntry {
                project_id: Some("proj_core".to_owned()),
                issuer_id_env: Some("CORE_ISSUER".to_owned()),
                key_id_env: Some("CORE_KEY".to_owned()),
                private_key_env: Some("CORE_PRIVATE_KEY".to_owned()),
                default_bundle_id: Some("com.example.app".to_owned()),
                default_language: Some("en-US".to_owned()),
                default_platform: Some("IOS".to_owned()),
                ..AppleAppStoreAccountEntry::default()
            },
        );
        let env = BTreeMap::from([
            ("CORE_ISSUER".to_owned(), "issuer_123".to_owned()),
            ("CORE_KEY".to_owned(), "key_123".to_owned()),
            ("CORE_PRIVATE_KEY".to_owned(), "-----BEGIN PRIVATE KEY-----".to_owned()),
        ]);

        let current = resolve_current_context(&settings, &env).expect("current context");
        assert_eq!(current.account_alias, "core");
        assert_eq!(current.project_id, "proj_core");
        assert_eq!(current.token_source, "env:CORE_PRIVATE_KEY");
        assert_eq!(
            current.source,
            "settings.apple.project_id,settings.apple.default_bundle_id,settings.apple.default_language,settings.apple.default_platform,env:CORE_ISSUER,env:CORE_KEY"
        );
    }

    #[test]
    fn auth_status_uses_flag_overrides() {
        let status = resolve_auth_status(
            &AppleSettings::default(),
            &BTreeMap::new(),
            &AppleAppStoreAuthOverrides {
                account: "mobile".to_owned(),
                environment: "staging".to_owned(),
                bundle_id: "com.example.mobile".to_owned(),
                locale: "fr-FR".to_owned(),
                platform: "MAC_OS".to_owned(),
                issuer_id: "issuer_123".to_owned(),
                key_id: "key_123".to_owned(),
                private_key: "-----BEGIN PRIVATE KEY-----".to_owned(),
                project_id: "proj_mobile".to_owned(),
                base_url: "https://example.invalid".to_owned(),
                ..AppleAppStoreAuthOverrides::default()
            },
        )
        .expect("auth status");

        assert_eq!(status.status, "ready");
        assert_eq!(status.account_alias, "mobile");
        assert_eq!(status.environment, "staging");
        assert_eq!(status.project_id, "proj_mobile");
        assert_eq!(status.bundle_id, "com.example.mobile");
        assert_eq!(status.locale, "fr-FR");
        assert_eq!(status.platform, "MAC_OS");
        assert_eq!(status.base_url, "https://example.invalid");
        assert_eq!(status.token_source, "flag:--private-key");
        assert_eq!(
            status.source,
            "flag:--project-id,flag:--bundle-id,flag:--locale,flag:--platform,flag:--issuer-id,flag:--key-id"
        );
    }
}
