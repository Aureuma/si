use base64::{Engine as _, engine::general_purpose::STANDARD as BASE64_STANDARD};
use httpdate::fmt_http_date;
use reqwest::blocking::Client;
use reqwest::header::{
    ACCEPT, AUTHORIZATION, CONTENT_LENGTH, CONTENT_TYPE, DATE, HOST, HeaderMap, HeaderName,
    HeaderValue, USER_AGENT,
};
use rsa::pkcs1::DecodeRsaPrivateKey;
use rsa::pkcs1v15::Pkcs1v15Sign;
use rsa::pkcs8::DecodePrivateKey;
use rsa::RsaPrivateKey;
use serde::Serialize;
use serde_json::Value;
use sha2::{Digest, Sha256};
use si_rs_config::settings::{OCIAccountEntry, OCISettings};
use std::collections::BTreeMap;
use std::env;
use std::fs;
use std::path::Path;
use std::time::Duration;
use url::Url;

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

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct OCIAuthOverrides {
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

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OCIAuthStatus {
    pub status: String,
    pub account_alias: String,
    pub profile: String,
    pub config_file: String,
    pub region: String,
    pub base_url: String,
    pub auth_style: String,
    pub tenancy_ocid: String,
    pub user_ocid: String,
    pub fingerprint: String,
    pub source: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OCIAPIService {
    Core,
    Identity,
}

#[derive(Debug, Clone, PartialEq)]
pub struct OCIAPIRequest {
    pub method: String,
    pub path: String,
    pub params: BTreeMap<String, String>,
    pub headers: BTreeMap<String, String>,
    pub raw_body: String,
    pub json_body: Option<Value>,
    pub service: OCIAPIService,
}

impl Default for OCIAPIRequest {
    fn default() -> Self {
        Self {
            method: "GET".to_owned(),
            path: String::new(),
            params: BTreeMap::new(),
            headers: BTreeMap::new(),
            raw_body: String::new(),
            json_body: None,
            service: OCIAPIService::Core,
        }
    }
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct OCIAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub headers: BTreeMap<String, String>,
    pub body: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub list: Option<Vec<Value>>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OCIAPIErrorDetails {
    pub status_code: u16,
    pub code: String,
    pub message: String,
    pub request_id: String,
    pub raw_body: String,
}

impl std::fmt::Display for OCIAPIErrorDetails {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let mut parts = Vec::new();
        if self.status_code > 0 {
            parts.push(format!("status_code={}", self.status_code));
        }
        if !self.code.trim().is_empty() {
            parts.push(format!("code={}", self.code.trim()));
        }
        if !self.message.trim().is_empty() {
            parts.push(format!("message={}", self.message.trim()));
        }
        if !self.request_id.trim().is_empty() {
            parts.push(format!("request_id={}", self.request_id.trim()));
        }
        if parts.is_empty() {
            f.write_str("oci api error")
        } else {
            write!(f, "oci api error: {}", parts.join(", "))
        }
    }
}

impl std::error::Error for OCIAPIErrorDetails {}

#[derive(Debug, Clone)]
struct OCIRuntimeContext {
    account_alias: String,
    profile: String,
    config_file: String,
    region: String,
    base_url: String,
    auth_style: String,
    tenancy_ocid: String,
    user_ocid: String,
    fingerprint: String,
    private_key: Option<RsaPrivateKey>,
    source: String,
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

pub fn resolve_auth_status(
    settings: &OCISettings,
    env_map: &BTreeMap<String, String>,
    overrides: &OCIAuthOverrides,
) -> Result<OCIAuthStatus, String> {
    let runtime = resolve_runtime(settings, env_map, overrides, true)?;
    Ok(OCIAuthStatus {
        status: "ready".to_owned(),
        account_alias: runtime.account_alias,
        profile: runtime.profile,
        config_file: runtime.config_file,
        region: runtime.region,
        base_url: runtime.base_url,
        auth_style: runtime.auth_style,
        tenancy_ocid: runtime.tenancy_ocid,
        user_ocid: runtime.user_ocid,
        fingerprint: runtime.fingerprint,
        source: runtime.source,
    })
}

pub fn execute_api_request(
    settings: &OCISettings,
    env_map: &BTreeMap<String, String>,
    overrides: &OCIAuthOverrides,
    request: &OCIAPIRequest,
) -> Result<OCIAPIResponse, String> {
    let runtime = resolve_runtime(settings, env_map, overrides, true)?;
    let method = request.method.trim().to_uppercase();
    let method = if method.is_empty() { "GET".to_owned() } else { method };
    let service_base_url = match request.service {
        OCIAPIService::Core => runtime.base_url.clone(),
        OCIAPIService::Identity => oci_identity_url(&runtime.region),
    };
    let endpoint = resolve_url(&service_base_url, &request.path, &request.params)?;

    let body = if !request.raw_body.trim().is_empty() {
        request.raw_body.as_bytes().to_vec()
    } else if let Some(json_body) = &request.json_body {
        serde_json::to_vec(json_body).map_err(|err| format!("encode oci request body: {err}"))?
    } else {
        Vec::new()
    };

    let client = Client::builder()
        .timeout(Duration::from_secs(60))
        .build()
        .map_err(|err| format!("build oci http client: {err}"))?;
    let req_method = reqwest::Method::from_bytes(method.as_bytes())
        .map_err(|err| format!("oci request method: {err}"))?;
    let mut builder = client.request(req_method, endpoint.as_str());
    builder = builder.header(ACCEPT, "application/json");
    builder = builder.header(USER_AGENT, "si-rs-provider-oci/1.0");
    for (key, value) in &request.headers {
        let key = key.trim();
        if key.is_empty() {
            continue;
        }
        let header = HeaderName::from_bytes(key.as_bytes())
            .map_err(|err| format!("invalid oci header name {key:?}: {err}"))?;
        let value = HeaderValue::from_str(value.trim())
            .map_err(|err| format!("invalid oci header value for {key:?}: {err}"))?;
        builder = builder.header(header, value);
    }
    if !body.is_empty() {
        builder = builder.header(CONTENT_TYPE, "application/json").body(body.clone());
    }
    let mut req = builder.build().map_err(|err| format!("build oci request: {err}"))?;
    if runtime.auth_style == "signature" {
        sign_request(&mut req, &runtime, &body)?;
    }
    let resp = client
        .execute(req)
        .map_err(|err| format!("oci request failed: {err}"))?;
    let status_code = resp.status().as_u16();
    let status = resp.status().to_string();
    let headers = headers_to_map(resp.headers());
    let request_id = first_header(resp.headers(), &["opc-request-id", "opc-client-request-id"]);
    let body = resp
        .text()
        .map_err(|err| format!("read oci response body: {err}"))?;
    if (200..300).contains(&status_code) {
        let mut out = OCIAPIResponse {
            status_code,
            status,
            request_id,
            headers,
            body: body.trim().to_owned(),
            data: None,
            list: None,
        };
        if !out.body.is_empty() {
            if let Ok(parsed) = serde_json::from_str::<Value>(&out.body) {
                match parsed {
                    Value::Object(_) => out.data = Some(parsed),
                    Value::Array(values) => out.list = Some(values),
                    _ => {}
                }
            }
        }
        return Ok(out);
    }

    let mut details = OCIAPIErrorDetails {
        status_code,
        code: String::new(),
        message: "oci request failed".to_owned(),
        request_id,
        raw_body: body.trim().to_owned(),
    };
    if let Ok(parsed) = serde_json::from_str::<Value>(&details.raw_body) {
        if let Some(code) = parsed.get("code").and_then(Value::as_str) {
            details.code = code.trim().to_owned();
        }
        if let Some(message) = parsed.get("message").and_then(Value::as_str) {
            details.message = message.trim().to_owned();
        }
    }
    if details.message == "oci request failed" {
        details.message = if details.raw_body.trim().is_empty() {
            status.clone()
        } else {
            details.raw_body.clone()
        };
    }
    Err(details.to_string())
}

pub fn build_oracular_cloud_init_user_data(ssh_port: u16) -> Result<String, String> {
    if ssh_port == 22 {
        return Err("refusing to configure OCI SSH port to 22; use a non-22 port".to_owned());
    }
    if ssh_port == 0 {
        return Err("invalid ssh port 0".to_owned());
    }
    let cloud_config = format!(
        r#"#cloud-config
write_files:
  - path: /etc/cloud/cloud.cfg.d/99-oracular-hostname.cfg
    permissions: '0644'
    content: |
      preserve_hostname: true
  - path: /etc/ssh/sshd_config.d/oracular-port.conf
    permissions: '0644'
    content: |
      Port {ssh_port}
  - path: /etc/iptables/rules.v4
    permissions: '0644'
    content: |
      *filter
      :INPUT ACCEPT [0:0]
      :FORWARD ACCEPT [0:0]
      :OUTPUT ACCEPT [0:0]
      :InstanceServices - [0:0]
      -A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT
      -A INPUT -p icmp -j ACCEPT
      -A INPUT -i lo -j ACCEPT
      -A INPUT -p tcp -m state --state NEW -m tcp --dport {ssh_port} -j ACCEPT
      -A INPUT -p tcp -m state --state NEW -m tcp --dport 80 -j ACCEPT
      -A INPUT -p tcp -m state --state NEW -m tcp --dport 443 -j ACCEPT
      -A INPUT -j REJECT --reject-with icmp-host-prohibited
      -A FORWARD -j REJECT --reject-with icmp-host-prohibited
      -A OUTPUT -d 169.254.0.0/16 -j InstanceServices
      -A InstanceServices -d 169.254.0.2/32 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.2.0/24 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.4.0/24 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.5.0/24 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.0.2/32 -p tcp -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp -m udp --dport 53 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p tcp -m tcp --dport 53 -j ACCEPT
      -A InstanceServices -d 169.254.0.3/32 -p tcp -m owner --uid-owner 0 -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.0.4/32 -p tcp -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p tcp -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp -m udp --dport 67 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp -m udp --dport 69 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp --dport 123 -j ACCEPT
      -A InstanceServices -d 169.254.0.0/16 -p tcp -m tcp -j REJECT --reject-with tcp-reset
      -A InstanceServices -d 169.254.0.0/16 -p udp -m udp -j REJECT --reject-with icmp-port-unreachable
      COMMIT
runcmd:
  - bash -lc "hostnamectl set-hostname oracular"
  - bash -lc "systemctl enable --now ssh || true"
  - bash -lc "systemctl restart ssh.socket || true"
  - bash -lc "systemctl restart ssh || true"
  - bash -lc "iptables-restore < /etc/iptables/rules.v4"
  - bash -lc "systemctl enable --now netfilter-persistent || true"
  - bash -lc "systemctl restart netfilter-persistent || true"
"#
    );
    Ok(BASE64_STANDARD.encode(cloud_config.as_bytes()))
}

fn resolve_runtime(
    settings: &OCISettings,
    env_map: &BTreeMap<String, String>,
    overrides: &OCIAuthOverrides,
    require_auth: bool,
) -> Result<OCIRuntimeContext, String> {
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
    let (mut tenancy_ocid, tenancy_source) = resolve_account_value(
        account.tenancy_ocid.as_deref(),
        account.tenancy_ocid_env.as_deref(),
        env_map,
    );
    let (mut user_ocid, user_source) =
        resolve_account_value(account.user_ocid.as_deref(), account.user_ocid_env.as_deref(), env_map);
    let (mut fingerprint, fingerprint_source) = resolve_account_value(
        account.fingerprint.as_deref(),
        account.fingerprint_env.as_deref(),
        env_map,
    );
    let (mut private_key_path, private_key_source) = resolve_account_value(
        account.private_key_path.as_deref(),
        account.private_key_path_env.as_deref(),
        env_map,
    );
    let mut passphrase = resolve_env_reference(account.passphrase_env.as_deref(), env_map).unwrap_or_default();
    source.extend(
        [tenancy_source, user_source, fingerprint_source, private_key_source]
            .into_iter()
            .flatten(),
    );
    if auth_style == "signature" {
        let profile_values = parse_config_profile(&config_file, &profile)?;
        source.push(format!("profile:{profile}"));
        if tenancy_ocid.is_empty() {
            tenancy_ocid = trim_or_empty(profile_values.get("tenancy").map(String::as_str));
        }
        if user_ocid.is_empty() {
            user_ocid = trim_or_empty(profile_values.get("user").map(String::as_str));
        }
        if fingerprint.is_empty() {
            fingerprint = trim_or_empty(profile_values.get("fingerprint").map(String::as_str));
        }
        if private_key_path.is_empty() {
            private_key_path = trim_or_empty(profile_values.get("key_file").map(String::as_str));
        }
        if passphrase.is_empty() {
            passphrase = trim_or_empty(profile_values.get("pass_phrase").map(String::as_str));
        }
        if region.is_empty() {
            region = trim_or_empty(profile_values.get("region").map(String::as_str));
        }
    }
    if region.is_empty() {
        region = "us-ashburn-1".to_owned();
    }
    private_key_path = normalize_private_key_path(&config_file, &private_key_path);
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        settings.api_base_url.as_deref(),
        Some(oci_core_url(&region).as_str()),
    ])
    .trim_end_matches('/')
    .to_owned();
    if require_auth && auth_style != "signature" {
        return Err("oci signature auth is required for this command (set --auth signature)".to_owned());
    }
    let private_key = if auth_style == "signature" {
        Some(load_rsa_private_key(&private_key_path, &passphrase)?)
    } else {
        None
    };
    if require_auth {
        if tenancy_ocid.is_empty() {
            return Err("oci signature auth requires tenancy ocid".to_owned());
        }
        if user_ocid.is_empty() {
            return Err("oci signature auth requires user ocid".to_owned());
        }
        if fingerprint.is_empty() {
            return Err("oci signature auth requires fingerprint".to_owned());
        }
        if private_key.is_none() {
            return Err("oci signature private key is not configured".to_owned());
        }
    }
    Ok(OCIRuntimeContext {
        account_alias: alias,
        profile,
        config_file,
        region,
        base_url,
        auth_style,
        tenancy_ocid,
        user_ocid,
        fingerprint,
        private_key,
        source: join_sources(&source),
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

fn parse_config_profile(config_file: &str, profile: &str) -> Result<BTreeMap<String, String>, String> {
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
        let split_idx = line.find(['=', ':']).unwrap_or(usize::MAX);
        if split_idx == usize::MAX || split_idx == 0 {
            continue;
        }
        if current.is_empty() {
            continue;
        }
        let key = line[..split_idx].trim();
        let value = line[split_idx + 1..].trim().trim_matches('"').trim_matches('\'');
        profiles
            .entry(current.clone())
            .or_default()
            .insert(key.to_owned(), value.to_owned());
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

fn resolve_account_value(
    value: Option<&str>,
    env_reference: Option<&str>,
    env_map: &BTreeMap<String, String>,
) -> (String, Option<String>) {
    let direct = trim_or_empty(value);
    if !direct.is_empty() {
        return (direct, None);
    }
    let reference = env_reference.map(str::trim).unwrap_or_default();
    if reference.is_empty() {
        return (String::new(), None);
    }
    let env_value = env_map
        .get(reference)
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or_default()
        .to_owned();
    if env_value.is_empty() {
        (String::new(), None)
    } else {
        (env_value, Some(format!("env:{reference}")))
    }
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

fn normalize_private_key_path(config_file: &str, private_key_path: &str) -> String {
    let expanded = expand_tilde(private_key_path);
    if expanded.trim().is_empty() {
        return String::new();
    }
    let path = Path::new(&expanded);
    if path.is_absolute() {
        return path.display().to_string();
    }
    Path::new(config_file)
        .parent()
        .unwrap_or_else(|| Path::new(""))
        .join(path)
        .display()
        .to_string()
}

fn load_rsa_private_key(path: &str, passphrase: &str) -> Result<RsaPrivateKey, String> {
    if path.trim().is_empty() {
        return Err("oci private key path is required".to_owned());
    }
    let raw = fs::read_to_string(path).map_err(|err| format!("read oci private key {:?}: {err}", path))?;
    if passphrase.trim().is_empty() {
        if private_key_needs_passphrase(&raw) {
            return Err("encrypted oci private key requires passphrase".to_owned());
        }
        return RsaPrivateKey::from_pkcs8_pem(&raw)
            .or_else(|_| RsaPrivateKey::from_pkcs1_pem(&raw))
            .map_err(|err| format!("parse oci private key: {err}"));
    }
    if private_key_needs_passphrase(&raw) {
        return Err(
            "encrypted oci private key format is not yet supported by the Rust OCI provider"
                .to_owned(),
        );
    }
    Err(
        format!(
            "parse oci private key: passphrase was provided but the key is not a supported encrypted PKCS#8 PEM"
        ),
    )
}

fn private_key_needs_passphrase(raw: &str) -> bool {
    let upper = raw.to_uppercase();
    upper.contains("-----BEGIN ENCRYPTED PRIVATE KEY-----")
        || upper.contains("PROC-TYPE: 4,ENCRYPTED")
        || upper.contains("DEK-INFO:")
}

fn resolve_url(base_url: &str, path: &str, params: &BTreeMap<String, String>) -> Result<String, String> {
    let path = path.trim();
    if path.starts_with("http://") || path.starts_with("https://") {
        let mut url = Url::parse(path).map_err(|err| format!("parse oci url {:?}: {err}", path))?;
        if !params.is_empty() {
            let mut pairs = url.query_pairs_mut();
            for (key, value) in params {
                let key = key.trim();
                if key.is_empty() {
                    continue;
                }
                pairs.append_pair(key, value.trim());
            }
        }
        return Ok(url.to_string());
    }
    let mut url = Url::parse(base_url).map_err(|err| format!("parse oci base url {:?}: {err}", base_url))?;
    if !path.starts_with('/') {
        url.set_path(&format!("/{path}"));
    } else {
        url.set_path(path);
    }
    if !params.is_empty() {
        let mut pairs = url.query_pairs_mut();
        for (key, value) in params {
            let key = key.trim();
            if key.is_empty() {
                continue;
            }
            pairs.append_pair(key, value.trim());
        }
    }
    Ok(url.to_string())
}

fn sign_request(
    request: &mut reqwest::blocking::Request,
    runtime: &OCIRuntimeContext,
    body: &[u8],
) -> Result<(), String> {
    let private_key = runtime
        .private_key
        .as_ref()
        .ok_or_else(|| "oci signature auth requires a private key".to_owned())?;
    let key_id = format!(
        "{}/{}/{}",
        runtime.tenancy_ocid.trim(),
        runtime.user_ocid.trim(),
        runtime.fingerprint.trim()
    );
    if key_id.contains("//") {
        return Err("oci signature auth requires tenancy/user/fingerprint values".to_owned());
    }
    request.headers_mut().insert(
        DATE,
        HeaderValue::from_str(&fmt_http_date(std::time::SystemTime::now())).unwrap(),
    );
    let host = request
        .url()
        .host_str()
        .ok_or_else(|| "oci request host is required".to_owned())?
        .to_owned();
    request.headers_mut().insert(
        HOST,
        HeaderValue::from_str(&host).map_err(|err| format!("oci request host: {err}"))?,
    );
    let method = request.method().as_str().to_lowercase();
    let mut headers_to_sign = vec!["date".to_owned(), "(request-target)".to_owned(), "host".to_owned()];
    if matches!(method.as_str(), "post" | "put" | "patch") {
        if !request.headers().contains_key(CONTENT_TYPE) {
            request
                .headers_mut()
                .insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
        }
        let content_sha = BASE64_STANDARD.encode(Sha256::digest(body));
        request.headers_mut().insert(
            HeaderName::from_static("x-content-sha256"),
            HeaderValue::from_str(&content_sha)
                .map_err(|err| format!("oci request content digest header: {err}"))?,
        );
        request.headers_mut().insert(
            CONTENT_LENGTH,
            HeaderValue::from_str(&body.len().to_string())
                .map_err(|err| format!("oci request content-length header: {err}"))?,
        );
        headers_to_sign.extend([
            "content-length".to_owned(),
            "content-type".to_owned(),
            "x-content-sha256".to_owned(),
        ]);
    }
    let request_target = format!("{} {}", method, path_and_query(request.url()));
    let signing_parts = headers_to_sign
        .iter()
        .map(|header| {
            let value = match header.as_str() {
                "(request-target)" => request_target.clone(),
                "host" => host.to_lowercase(),
                _ => request
                    .headers()
                    .get(header.as_str())
                    .and_then(|value| value.to_str().ok())
                    .unwrap_or_default()
                    .trim()
                    .to_owned(),
            };
            format!("{header}: {value}")
        })
        .collect::<Vec<_>>();
    let signing_string = signing_parts.join("\n");
    let digest = Sha256::digest(signing_string.as_bytes());
    let signature = private_key
        .sign(Pkcs1v15Sign::new::<Sha256>(), &digest)
        .map_err(|err| format!("oci request signing failed: {err}"))?;
    let auth_header = format!(
        r#"Signature version="1",keyId="{}",algorithm="rsa-sha256",headers="{}",signature="{}""#,
        key_id,
        headers_to_sign.join(" "),
        BASE64_STANDARD.encode(signature)
    );
    request.headers_mut().insert(
        AUTHORIZATION,
        HeaderValue::from_str(&auth_header)
            .map_err(|err| format!("oci request authorization header: {err}"))?,
    );
    Ok(())
}

fn path_and_query(url: &Url) -> String {
    let path = if url.path().trim().is_empty() { "/" } else { url.path() };
    if url.query().unwrap_or_default().trim().is_empty() {
        path.to_owned()
    } else {
        format!("{path}?{}", url.query().unwrap_or_default())
    }
}

fn headers_to_map(headers: &HeaderMap) -> BTreeMap<String, String> {
    headers
        .iter()
        .filter_map(|(key, value)| {
            value
                .to_str()
                .ok()
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .map(|value| (key.as_str().to_owned(), value.to_owned()))
        })
        .collect()
}

fn first_header(headers: &HeaderMap, keys: &[&str]) -> String {
    keys.iter()
        .filter_map(|key| headers.get(*key))
        .filter_map(|value| value.to_str().ok())
        .map(str::trim)
        .find(|value| !value.is_empty())
        .unwrap_or_default()
        .to_owned()
}

fn oci_core_url(region: &str) -> String {
    let region = region.trim();
    if region.is_empty() {
        "https://iaas.us-ashburn-1.oraclecloud.com".to_owned()
    } else {
        format!("https://iaas.{region}.oraclecloud.com")
    }
}

fn oci_identity_url(region: &str) -> String {
    let region = region.trim();
    if region.is_empty() {
        "https://identity.us-ashburn-1.oraclecloud.com".to_owned()
    } else {
        format!("https://identity.{region}.oraclecloud.com")
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

fn join_sources(values: &[String]) -> String {
    values
        .iter()
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
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

    #[test]
    fn auth_status_reads_signature_material() {
        let temp = env::temp_dir().join(format!("si-rs-oci-auth-{}", std::process::id()));
        let _ = fs::create_dir_all(&temp);
        let config = temp.join("config");
        let key = temp.join("keys").join("oci.pem");
        fs::create_dir_all(key.parent().expect("key parent")).expect("mkdir key parent");
        fs::write(
            &config,
            "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..profile\nuser=ocid1.user.oc1..profile\nfingerprint=aa:bb:cc\nkey_file=keys/oci.pem\nregion=us-phoenix-1\n",
        )
        .expect("write config");
        fs::write(
            &key,
            "-----BEGIN PRIVATE KEY-----\nMIICdgIBADANBgkqhkiG9w0BAQEFAASCAmAwggJcAgEAAoGBAMHdWNb6AMmJKYK2\nAtBSIA5dld4B22eLwBBeQaqsbqyZj3Wpu4lgs2Hu/PBRIgqN/VT83RRyhLjp1PTL\n9fNTlykVRd3aBOj8QwIWsVS+10a/8GuPx5N4vZlzsiplkIOEwcrpCQs30uNPtJqv\nbr2DSoulEAzFiboOri2wsY+MIbKxAgMBAAECgYAn0+mkgMgYn20/xVTep4CecuuP\nKKKCq1tSAYtMHRC/tOycJ7q3hn5T6F1eocx0jqc1Bp4EzWIm+yMdB6oHy2yKUH/f\nN5zX1Hi/pulp5zO6c8ANaHjb48fBiBOTck7FQ9c/uppCleBESdE773zk6fN7XKgm\nz6Y9EegeBYMrAP5DYQJBAOtaAtKsQYKiPoQM6EiskBfO3kpRS7C4WgrJchgArY74\n+tBk5s0Bf6ibSxSyNfSZ4gZyyF7kLNDR3CWAxFp9EX8CQQDS34pEuKVSEYz41uiS\nMzM+hQJiszF8M2NPj9IzqT8EmvXIvveK29f6C6nxkzllKB6WyjnB0PcbYqHnCsGv\nG/PPAkBw6m+eShzoIxVhX5v2eixr78mA2H47HEe/EyVVVMXwaY5Ue4SsaQKpj1A3\nbsUqRMZHl7yAonLKAVXg/GW4kHbbAkBkqCXFJepsIUqMYXFEkEIOvsjjuiuN4K2w\nBbPNyyT0ms9l0pow4z3V8oldcew8uAjZ64/kT04U+WDU+1J2tr4LAkEAo2Jr+HY3\nn7bZhk8wZV/UBPJY/hjPoMGweaYAz8Vx4OujBqJhYaVd4XHFSH8cOGiXGsj5IVfE\nytNZBG2qI/IOCw==\n-----END PRIVATE KEY-----\n",
        )
        .expect("write key");

        let mut settings = OCISettings::default();
        settings.default_account = Some("team".to_owned());

        let status = resolve_auth_status(
            &settings,
            &BTreeMap::new(),
            &OCIAuthOverrides {
                config_file: config.display().to_string(),
                ..OCIAuthOverrides::default()
            },
        )
        .expect("auth status");

        assert_eq!(status.status, "ready");
        assert_eq!(status.profile, "DEFAULT");
        assert_eq!(status.region, "us-phoenix-1");
        assert_eq!(status.tenancy_ocid, "ocid1.tenancy.oc1..profile");
        assert_eq!(status.user_ocid, "ocid1.user.oc1..profile");
        assert_eq!(status.fingerprint, "aa:bb:cc");
        assert_eq!(status.source, "profile:DEFAULT");
    }

    #[test]
    fn auth_status_requires_signature_auth() {
        let err = resolve_auth_status(
            &OCISettings::default(),
            &BTreeMap::new(),
            &OCIAuthOverrides {
                auth_style: "none".to_owned(),
                ..OCIAuthOverrides::default()
            },
        )
        .expect_err("expected signature auth error");
        assert_eq!(
            err,
            "oci signature auth is required for this command (set --auth signature)"
        );
    }

    #[test]
    fn build_cloud_init_rejects_port_22() {
        let err = build_oracular_cloud_init_user_data(22).expect_err("expected port-22 rejection");
        assert_eq!(
            err,
            "refusing to configure OCI SSH port to 22; use a non-22 port"
        );
    }
}
