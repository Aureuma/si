use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use reqwest::blocking::{Client, Response};
use reqwest::header::{ACCEPT, AUTHORIZATION, HeaderMap, HeaderName, HeaderValue, USER_AGENT};
use serde::{Serialize, Serializer};
use serde_json::Value;
use si_rs_config::settings::{GitHubAccountEntry, GitHubSettings};
use std::collections::BTreeMap;
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use url::Url;

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

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct GitHubAuthOverrides {
    pub account: String,
    pub owner: String,
    pub base_url: String,
    pub auth_mode: String,
    pub token: String,
    pub app_id: Option<i64>,
    pub app_key: String,
    pub installation_id: Option<i64>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct GitHubAuthStatus {
    pub account_alias: String,
    pub owner: String,
    pub auth_mode: String,
    pub base_url: String,
    pub source: String,
    pub token_preview: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GitHubRuntime {
    pub account_alias: String,
    pub owner: String,
    pub auth_mode: String,
    pub base_url: String,
    pub source: String,
    credentials: GitHubCredentials,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum GitHubCredentials {
    OAuth {
        access_token: String,
    },
    App {
        app_id: i64,
        app_key: String,
        installation_id: i64,
    },
}

#[derive(Debug, Clone, PartialEq)]
pub struct GitHubAPIResponse {
    pub status_code: u16,
    pub status: String,
    pub request_id: String,
    pub headers: BTreeMap<String, String>,
    pub body: String,
    pub data: Option<Value>,
    pub list: Vec<Value>,
}

impl Serialize for GitHubAPIResponse {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        use serde::ser::SerializeMap;

        let mut map = serializer.serialize_map(None)?;
        map.serialize_entry("status_code", &self.status_code)?;
        map.serialize_entry("status", &self.status)?;
        if !self.request_id.trim().is_empty() {
            map.serialize_entry("request_id", &self.request_id)?;
        }
        if !self.headers.is_empty() {
            map.serialize_entry("headers", &self.headers)?;
        }
        if !self.body.is_empty() {
            map.serialize_entry("body", &self.body)?;
        }
        if let Some(data) = &self.data {
            map.serialize_entry("data", data)?;
        }
        if !self.list.is_empty() {
            map.serialize_entry("list", &self.list)?;
        }
        map.end()
    }
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
    let selected_account = resolve_account_selection(settings, env, "");
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

pub fn resolve_auth_status(
    settings: &GitHubSettings,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> Result<GitHubAuthStatus, String> {
    let selected_account = resolve_account_selection(settings, env, &overrides.account);
    let entry = selected_account.as_ref().and_then(|alias| settings.accounts.get(alias));

    let owner = first_non_empty(&[
        Some(overrides.owner.as_str()),
        entry.and_then(|item| item.owner.as_deref()),
        settings.default_owner.as_deref(),
        env.get("GITHUB_DEFAULT_OWNER").map(String::as_str),
    ])
    .to_owned();
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        entry.and_then(|item| item.api_base_url.as_deref()),
        settings.api_base_url.as_deref(),
        env.get("GITHUB_API_BASE_URL").map(String::as_str),
        Some("https://api.github.com"),
    ])
    .to_owned();

    let mut source = Vec::new();
    if let Some(alias) = selected_account.as_deref() {
        if !overrides.account.trim().is_empty() && overrides.account.trim() == alias {
            source.push("flag:--account".to_owned());
        } else if settings.default_account.as_deref().map(str::trim) == Some(alias) {
            source.push("settings.default_account".to_owned());
        } else if env
            .get("GITHUB_DEFAULT_ACCOUNT")
            .map(|value| value.trim() == alias)
            .unwrap_or(false)
        {
            source.push("env:GITHUB_DEFAULT_ACCOUNT".to_owned());
        }
    }

    let (auth_mode, auth_mode_source) = resolve_auth_mode(settings, entry, env, overrides)?;
    source.extend(auth_mode_source);
    let token_preview = if auth_mode == "oauth" {
        let (token, token_source) =
            resolve_oauth_token(selected_account.as_deref(), entry, env, overrides);
        if token.trim().is_empty() {
            return Err("github oauth token not found".to_owned());
        }
        source.push(token_source);
        preview_secret(&token)
    } else {
        let (app_id, app_id_source) =
            resolve_app_id(selected_account.as_deref(), entry, env, overrides);
        let (app_key, app_key_source) =
            resolve_app_key(selected_account.as_deref(), entry, env, overrides);
        let (_, installation_source) =
            resolve_installation_id(selected_account.as_deref(), entry, env, overrides);
        if app_id <= 0 || app_key.trim().is_empty() {
            return Err("github app auth requires app id and private key".to_owned());
        }
        source.push(app_id_source);
        source.push(app_key_source);
        source.push(installation_source);
        "-".to_owned()
    };

    Ok(GitHubAuthStatus {
        account_alias: selected_account.unwrap_or_default(),
        owner,
        auth_mode,
        base_url,
        source: join_sources(&source),
        token_preview,
    })
}

pub fn resolve_runtime(
    settings: &GitHubSettings,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> Result<GitHubRuntime, String> {
    let selected_account = resolve_account_selection(settings, env, &overrides.account);
    let entry = selected_account.as_ref().and_then(|alias| settings.accounts.get(alias));

    let owner = first_non_empty(&[
        Some(overrides.owner.as_str()),
        entry.and_then(|item| item.owner.as_deref()),
        settings.default_owner.as_deref(),
        env.get("GITHUB_DEFAULT_OWNER").map(String::as_str),
    ])
    .to_owned();
    let base_url = first_non_empty(&[
        Some(overrides.base_url.as_str()),
        entry.and_then(|item| item.api_base_url.as_deref()),
        settings.api_base_url.as_deref(),
        env.get("GITHUB_API_BASE_URL").map(String::as_str),
        Some("https://api.github.com"),
    ])
    .to_owned();

    let mut source = Vec::new();
    if let Some(alias) = selected_account.as_deref() {
        if !overrides.account.trim().is_empty() && overrides.account.trim() == alias {
            source.push("flag:--account".to_owned());
        } else if settings.default_account.as_deref().map(str::trim) == Some(alias) {
            source.push("settings.default_account".to_owned());
        } else if env
            .get("GITHUB_DEFAULT_ACCOUNT")
            .map(|value| value.trim() == alias)
            .unwrap_or(false)
        {
            source.push("env:GITHUB_DEFAULT_ACCOUNT".to_owned());
        }
    }
    if !overrides.owner.trim().is_empty() {
        source.push("flag:--owner".to_owned());
    } else if entry.and_then(|item| item.owner.as_deref()).is_some_and(|value| value.trim() == owner)
    {
        source.push("settings.owner".to_owned());
    } else if settings.default_owner.as_deref().is_some_and(|value| value.trim() == owner) {
        source.push("settings.default_owner".to_owned());
    } else if env
        .get("GITHUB_DEFAULT_OWNER")
        .map(|value| value.trim() == owner)
        .unwrap_or(false)
    {
        source.push("env:GITHUB_DEFAULT_OWNER".to_owned());
    }
    if !overrides.base_url.trim().is_empty() {
        source.push("flag:--base-url".to_owned());
    } else if entry
        .and_then(|item| item.api_base_url.as_deref())
        .is_some_and(|value| value.trim() == base_url)
    {
        source.push("settings.api_base_url".to_owned());
    } else if settings.api_base_url.as_deref().is_some_and(|value| value.trim() == base_url) {
        source.push("settings.api_base_url".to_owned());
    } else if env
        .get("GITHUB_API_BASE_URL")
        .map(|value| value.trim() == base_url)
        .unwrap_or(false)
    {
        source.push("env:GITHUB_API_BASE_URL".to_owned());
    }

    let (auth_mode, auth_mode_source) = resolve_auth_mode(settings, entry, env, overrides)?;
    source.extend(auth_mode_source);
    let credentials = if auth_mode == "oauth" {
        let (token, token_source) =
            resolve_oauth_token(selected_account.as_deref(), entry, env, overrides);
        if token.trim().is_empty() {
            return Err("github oauth token not found".to_owned());
        }
        source.push(token_source);
        GitHubCredentials::OAuth { access_token: normalize_bearer_token(&token) }
    } else {
        let (app_id, app_id_source) =
            resolve_app_id(selected_account.as_deref(), entry, env, overrides);
        let (app_key, app_key_source) =
            resolve_app_key(selected_account.as_deref(), entry, env, overrides);
        let (installation_id, installation_source) =
            resolve_installation_id(selected_account.as_deref(), entry, env, overrides);
        if app_id <= 0 || app_key.trim().is_empty() {
            return Err("github app auth requires app id and private key".to_owned());
        }
        source.push(app_id_source);
        source.push(app_key_source);
        if !installation_source.trim().is_empty() {
            source.push(installation_source);
        }
        GitHubCredentials::App {
            app_id,
            app_key: normalize_private_key(&app_key),
            installation_id,
        }
    };

    Ok(GitHubRuntime {
        account_alias: selected_account.unwrap_or_default(),
        owner,
        auth_mode,
        base_url,
        source: join_sources(&source),
        credentials,
    })
}

pub fn list_releases(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    params: &BTreeMap<String, String>,
    max_pages: usize,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let mut merged = params.clone();
    merged.entry("per_page".to_owned()).or_insert_with(|| "100".to_owned());
    let mut items = Vec::new();
    let mut last_response = GitHubAPIResponse {
        status_code: 200,
        status: "200 OK".to_owned(),
        request_id: String::new(),
        headers: BTreeMap::new(),
        body: String::new(),
        data: None,
        list: Vec::new(),
    };
    let total_pages = if max_pages == 0 { 5 } else { max_pages };
    for page in 1..=total_pages {
        merged.insert("page".to_owned(), page.to_string());
        let response = github_get(
            &client,
            &runtime.base_url,
            &format!("/repos/{owner}/{repo}/releases"),
            &merged,
            &token,
        )?;
        let next = parse_next_link(response.headers());
        let payload = normalize_response(response)?;
        items.extend(payload.list.iter().cloned());
        last_response = payload;
        if next.is_none() || last_response.list.is_empty() {
            break;
        }
    }
    last_response.data = None;
    last_response.list = items;
    Ok(last_response)
}

pub fn list_repos(
    runtime: &GitHubRuntime,
    owner: &str,
    params: &BTreeMap<String, String>,
    max_pages: usize,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, "")?;
    let mut merged = params.clone();
    merged.entry("per_page".to_owned()).or_insert_with(|| "100".to_owned());
    let mut items = Vec::new();
    let mut last_response = GitHubAPIResponse {
        status_code: 200,
        status: "200 OK".to_owned(),
        request_id: String::new(),
        headers: BTreeMap::new(),
        body: String::new(),
        data: None,
        list: Vec::new(),
    };
    let total_pages = if max_pages == 0 { 10 } else { max_pages };
    for page in 1..=total_pages {
        merged.insert("page".to_owned(), page.to_string());
        let response = github_get(
            &client,
            &runtime.base_url,
            &format!("/users/{owner}/repos"),
            &merged,
            &token,
        )?;
        let next = parse_next_link(response.headers());
        let payload = normalize_response(response)?;
        items.extend(payload.list.iter().cloned());
        last_response = payload;
        if next.is_none() || last_response.list.is_empty() {
            break;
        }
    }
    last_response.data = None;
    last_response.list = items;
    Ok(last_response)
}

pub fn get_repo(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!("/repos/{owner}/{repo}");
    normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)
}

pub fn list_projects(
    runtime: &GitHubRuntime,
    organization: &str,
    limit: usize,
) -> Result<GitHubAPIResponse, String> {
    let query = r#"
query($org:String!,$first:Int!){
  organization(login:$org) {
    projectsV2(first:$first, orderBy:{field:UPDATED_AT,direction:DESC}) {
      nodes {
        id
        number
        title
        shortDescription
        public
        closed
        url
        updatedAt
      }
    }
  }
}
"#;
    let variables = serde_json::json!({
        "org": organization.trim(),
        "first": if limit == 0 { 30 } else { limit },
    });
    github_graphql(runtime, organization, query, variables)
}

pub fn resolve_project_id(
    runtime: &GitHubRuntime,
    organization: &str,
    number: i64,
) -> Result<String, String> {
    let query = r#"
query($org:String!,$number:Int!){
  organization(login:$org) {
    projectV2(number:$number) {
      id
      number
      title
      url
      closed
      public
    }
  }
}
"#;
    let response = github_graphql(
        runtime,
        organization,
        query,
        serde_json::json!({
            "org": organization.trim(),
            "number": number,
        }),
    )?;
    let project_id = response
        .data
        .as_ref()
        .and_then(|data| data.get("organization"))
        .and_then(|organization| organization.get("projectV2"))
        .and_then(|project| project.get("id"))
        .and_then(Value::as_str)
        .unwrap_or_default()
        .trim()
        .to_owned();
    if project_id.is_empty() {
        return Err(format!("project not found: {}/{}", organization.trim(), number));
    }
    Ok(project_id)
}

pub fn get_project(runtime: &GitHubRuntime, project_id: &str) -> Result<GitHubAPIResponse, String> {
    let query = r#"
query($id:ID!){
  node(id:$id) {
    ... on ProjectV2 {
      id
      number
      title
      shortDescription
      readme
      public
      closed
      url
      updatedAt
      items(first:1) { totalCount }
      fields(first:1) { totalCount }
    }
  }
}
"#;
    github_graphql(
        runtime,
        &runtime.owner,
        query,
        serde_json::json!({
            "id": project_id.trim(),
        }),
    )
}

pub fn list_project_fields(
    runtime: &GitHubRuntime,
    project_id: &str,
    limit: usize,
) -> Result<GitHubAPIResponse, String> {
    let query = r#"
query($id:ID!,$first:Int!){
  node(id:$id) {
    ... on ProjectV2 {
      id
      fields(first:$first) {
        nodes {
          ... on ProjectV2Field {
            id
            name
            dataType
          }
          ... on ProjectV2SingleSelectField {
            id
            name
            dataType
            options {
              id
              name
            }
          }
          ... on ProjectV2IterationField {
            id
            name
            dataType
            configuration {
              iterations {
                id
                title
                startDate
                duration
              }
            }
          }
        }
      }
    }
  }
}
"#;
    github_graphql(
        runtime,
        &runtime.owner,
        query,
        serde_json::json!({
            "id": project_id.trim(),
            "first": if limit == 0 { 100 } else { limit },
        }),
    )
}

pub fn list_project_items(
    runtime: &GitHubRuntime,
    project_id: &str,
    limit: usize,
    include_archived: bool,
) -> Result<GitHubAPIResponse, String> {
    let query = r#"
query($id:ID!,$first:Int!,$includeArchived:Boolean!){
  node(id:$id) {
    ... on ProjectV2 {
      id
      items(first:$first, includeArchived:$includeArchived) {
        nodes {
          id
          isArchived
          type
          content {
            __typename
            ... on DraftIssue {
              title
              body
            }
            ... on Issue {
              id
              number
              title
              state
              url
              repository {
                name
                owner {
                  login
                }
              }
            }
            ... on PullRequest {
              id
              number
              title
              state
              url
              repository {
                name
                owner {
                  login
                }
              }
            }
          }
          fieldValues(first:20) {
            nodes {
              ... on ProjectV2ItemFieldTextValue {
                text
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldNumberValue {
                number
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldDateValue {
                date
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldSingleSelectValue {
                name
                optionId
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldIterationValue {
                title
                iterationId
                startDate
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
"#;
    github_graphql(
        runtime,
        &runtime.owner,
        query,
        serde_json::json!({
            "id": project_id.trim(),
            "first": if limit == 0 { 50 } else { limit },
            "includeArchived": include_archived,
        }),
    )
}

pub fn list_workflows(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!("/repos/{owner}/{repo}/actions/workflows");
    let mut response =
        normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)?;
    response.list = response
        .data
        .as_ref()
        .and_then(|data| data.get("workflows"))
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    Ok(response)
}

pub fn list_workflow_runs(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    workflow: &str,
    params: &BTreeMap<String, String>,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = if workflow.trim().is_empty() {
        format!("/repos/{owner}/{repo}/actions/runs")
    } else {
        format!("/repos/{owner}/{repo}/actions/workflows/{}/runs", workflow.trim())
    };
    let mut response =
        normalize_response(github_get(&client, &runtime.base_url, &path, params, &token)?)?;
    response.list = response
        .data
        .as_ref()
        .and_then(|data| data.get("workflow_runs"))
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    Ok(response)
}

pub fn get_workflow_run(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    run_id: i64,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!("/repos/{owner}/{repo}/actions/runs/{run_id}");
    normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)
}

pub fn get_workflow_logs(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    run_id: i64,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!("/repos/{owner}/{repo}/actions/runs/{run_id}/logs");
    normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)
}

pub fn list_issues(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    params: &BTreeMap<String, String>,
    max_pages: usize,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let mut merged = params.clone();
    merged.entry("per_page".to_owned()).or_insert_with(|| "100".to_owned());
    let mut items = Vec::new();
    let mut last_response = GitHubAPIResponse {
        status_code: 200,
        status: "200 OK".to_owned(),
        request_id: String::new(),
        headers: BTreeMap::new(),
        body: String::new(),
        data: None,
        list: Vec::new(),
    };
    let total_pages = if max_pages == 0 { 5 } else { max_pages };
    for page in 1..=total_pages {
        merged.insert("page".to_owned(), page.to_string());
        let path = format!("/repos/{owner}/{repo}/issues");
        let response = github_get(&client, &runtime.base_url, &path, &merged, &token)?;
        let next = parse_next_link(response.headers());
        let payload = normalize_response(response)?;
        items.extend(payload.list.iter().cloned());
        last_response = payload;
        if next.is_none() || last_response.list.is_empty() {
            break;
        }
    }
    last_response.data = None;
    last_response.list = items;
    Ok(last_response)
}

pub fn get_issue(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    number: i64,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!("/repos/{owner}/{repo}/issues/{number}");
    normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)
}

pub fn list_pull_requests(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    params: &BTreeMap<String, String>,
    max_pages: usize,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let mut merged = params.clone();
    merged.entry("per_page".to_owned()).or_insert_with(|| "100".to_owned());
    let mut items = Vec::new();
    let mut last_response = GitHubAPIResponse {
        status_code: 200,
        status: "200 OK".to_owned(),
        request_id: String::new(),
        headers: BTreeMap::new(),
        body: String::new(),
        data: None,
        list: Vec::new(),
    };
    let total_pages = if max_pages == 0 { 5 } else { max_pages };
    for page in 1..=total_pages {
        merged.insert("page".to_owned(), page.to_string());
        let path = format!("/repos/{owner}/{repo}/pulls");
        let response = github_get(&client, &runtime.base_url, &path, &merged, &token)?;
        let next = parse_next_link(response.headers());
        let payload = normalize_response(response)?;
        items.extend(payload.list.iter().cloned());
        last_response = payload;
        if next.is_none() || last_response.list.is_empty() {
            break;
        }
    }
    last_response.data = None;
    last_response.list = items;
    Ok(last_response)
}

pub fn get_pull_request(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    number: i64,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!("/repos/{owner}/{repo}/pulls/{number}");
    normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)
}

pub fn list_branches(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    params: &BTreeMap<String, String>,
    max_pages: usize,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let mut merged = params.clone();
    merged.entry("per_page".to_owned()).or_insert_with(|| "100".to_owned());
    let mut items = Vec::new();
    let mut last_response = GitHubAPIResponse {
        status_code: 200,
        status: "200 OK".to_owned(),
        request_id: String::new(),
        headers: BTreeMap::new(),
        body: String::new(),
        data: None,
        list: Vec::new(),
    };
    let total_pages = if max_pages == 0 { 10 } else { max_pages };
    for page in 1..=total_pages {
        merged.insert("page".to_owned(), page.to_string());
        let path = format!("/repos/{owner}/{repo}/branches");
        let response = github_get(&client, &runtime.base_url, &path, &merged, &token)?;
        let next = parse_next_link(response.headers());
        let payload = normalize_response(response)?;
        items.extend(payload.list.iter().cloned());
        last_response = payload;
        if next.is_none() || last_response.list.is_empty() {
            break;
        }
    }
    last_response.data = None;
    last_response.list = items;
    Ok(last_response)
}

pub fn get_branch(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    branch: &str,
    params: &BTreeMap<String, String>,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let path = format!(
        "/repos/{owner}/{repo}/branches/{}",
        percent_encode_path_segment(branch.trim())
    );
    normalize_response(github_get(&client, &runtime.base_url, &path, params, &token)?)
}

pub fn get_release(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
    release_ref: &str,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, repo)?;
    let trimmed = release_ref.trim();
    if trimmed.is_empty() {
        return Err("github release ref is required".to_owned());
    }
    let path = if trimmed.parse::<u64>().is_ok() {
        format!("/repos/{owner}/{repo}/releases/{trimmed}")
    } else {
        format!("/repos/{owner}/{repo}/releases/tags/{trimmed}")
    };
    normalize_response(github_get(&client, &runtime.base_url, &path, &BTreeMap::new(), &token)?)
}

pub fn resolve_access_token(
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
) -> Result<String, String> {
    let client = build_http_client()?;
    github_access_token(&client, runtime, owner, repo)
}

pub fn raw_get(
    runtime: &GitHubRuntime,
    path: &str,
    params: &BTreeMap<String, String>,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, &runtime.owner, "")?;
    normalize_response(github_get(&client, &runtime.base_url, path.trim(), params, &token)?)
}

pub fn graphql_query(
    runtime: &GitHubRuntime,
    query: &str,
    variables: Value,
) -> Result<GitHubAPIResponse, String> {
    github_graphql(runtime, &runtime.owner, query, variables)
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
    override_account: &str,
) -> Option<String> {
    let selected = first_non_empty(&[
        Some(override_account),
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

fn resolve_auth_mode(
    settings: &GitHubSettings,
    entry: Option<&GitHubAccountEntry>,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> Result<(String, Vec<String>), String> {
    let raw = first_non_empty(&[
        Some(overrides.auth_mode.as_str()),
        entry.and_then(|item| item.auth_mode.as_deref()),
        env.get("GITHUB_AUTH_MODE").map(String::as_str),
        env.get("GITHUB_DEFAULT_AUTH_MODE").map(String::as_str),
        settings.default_auth_mode.as_deref(),
        Some("app"),
    ]);
    let auth_mode = normalize_auth_mode(raw)?;
    let source = if !overrides.auth_mode.trim().is_empty() {
        vec!["flag:--auth-mode".to_owned()]
    } else if entry.and_then(|item| item.auth_mode.as_deref()).map(str::trim) == Some(raw) {
        vec!["settings.auth_mode".to_owned()]
    } else if env.get("GITHUB_AUTH_MODE").map(|value| value.trim() == raw).unwrap_or(false) {
        vec!["env:GITHUB_AUTH_MODE".to_owned()]
    } else if env.get("GITHUB_DEFAULT_AUTH_MODE").map(|value| value.trim() == raw).unwrap_or(false)
    {
        vec!["env:GITHUB_DEFAULT_AUTH_MODE".to_owned()]
    } else if settings.default_auth_mode.as_deref().map(str::trim) == Some(raw) {
        vec!["settings.default_auth_mode".to_owned()]
    } else {
        Vec::new()
    };
    Ok((auth_mode.to_owned(), source))
}

fn normalize_auth_mode(raw: &str) -> Result<&'static str, String> {
    match raw.trim().to_ascii_lowercase().as_str() {
        "" | "app" => Ok("app"),
        "oauth" | "token" | "pat" => Ok("oauth"),
        value => Err(format!("invalid auth mode {value:?} (expected app|oauth)")),
    }
}

fn resolve_oauth_token(
    account_alias: Option<&str>,
    entry: Option<&GitHubAccountEntry>,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> (String, String) {
    if !overrides.token.trim().is_empty() {
        return (overrides.token.trim().to_owned(), "flag:--token".to_owned());
    }
    if let Some(value) = entry.and_then(|item| item.oauth_access_token.as_deref()) {
        if !value.trim().is_empty() {
            return (value.trim().to_owned(), "settings.oauth_access_token".to_owned());
        }
    }
    if let Some(reference) = entry.and_then(|item| item.oauth_token_env.as_deref()) {
        if let Some(value) = env.get(reference.trim()) {
            if !value.trim().is_empty() {
                return (value.trim().to_owned(), format!("env:{}", reference.trim()));
            }
        }
    }
    let prefix = github_account_env_prefix(account_alias, entry);
    for key in ["OAUTH_ACCESS_TOKEN", "TOKEN"] {
        let env_key = format!("{prefix}{key}");
        if let Some(value) = env.get(&env_key) {
            if !value.trim().is_empty() {
                return (value.trim().to_owned(), format!("env:{env_key}"));
            }
        }
    }
    for key in ["GITHUB_OAUTH_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PAT", "GH_PAT"] {
        if let Some(value) = env.get(key) {
            if !value.trim().is_empty() {
                return (value.trim().to_owned(), format!("env:{key}"));
            }
        }
    }
    (String::new(), String::new())
}

fn resolve_app_id(
    account_alias: Option<&str>,
    entry: Option<&GitHubAccountEntry>,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> (i64, String) {
    if let Some(value) = overrides.app_id.filter(|value| *value > 0) {
        return (value, "flag:--app-id".to_owned());
    }
    if let Some(value) = entry.and_then(|item| item.app_id).filter(|value| *value > 0) {
        return (value, "settings.app_id".to_owned());
    }
    if let Some(reference) = entry.and_then(|item| item.app_id_env.as_deref()) {
        if let Some(value) = env
            .get(reference.trim())
            .and_then(|raw| raw.trim().parse::<i64>().ok())
            .filter(|value| *value > 0)
        {
            return (value, format!("env:{}", reference.trim()));
        }
    }
    let env_key = format!("{}APP_ID", github_account_env_prefix(account_alias, entry));
    if let Some(value) =
        env.get(&env_key).and_then(|raw| raw.trim().parse::<i64>().ok()).filter(|value| *value > 0)
    {
        return (value, format!("env:{env_key}"));
    }
    if let Some(value) = env
        .get("GITHUB_APP_ID")
        .and_then(|raw| raw.trim().parse::<i64>().ok())
        .filter(|value| *value > 0)
    {
        return (value, "env:GITHUB_APP_ID".to_owned());
    }
    (0, String::new())
}

fn resolve_app_key(
    account_alias: Option<&str>,
    entry: Option<&GitHubAccountEntry>,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> (String, String) {
    if !overrides.app_key.trim().is_empty() {
        return (overrides.app_key.trim().to_owned(), "flag:--app-key".to_owned());
    }
    if let Some(value) = entry.and_then(|item| item.app_private_key_pem.as_deref()) {
        if !value.trim().is_empty() {
            return (value.trim().to_owned(), "settings.app_private_key_pem".to_owned());
        }
    }
    if let Some(reference) = entry.and_then(|item| item.app_private_key_env.as_deref()) {
        if let Some(value) = env.get(reference.trim()) {
            if !value.trim().is_empty() {
                return (value.trim().to_owned(), format!("env:{}", reference.trim()));
            }
        }
    }
    let env_key = format!("{}APP_PRIVATE_KEY_PEM", github_account_env_prefix(account_alias, entry));
    if let Some(value) = env.get(&env_key) {
        if !value.trim().is_empty() {
            return (value.trim().to_owned(), format!("env:{env_key}"));
        }
    }
    if let Some(value) = env.get("GITHUB_APP_PRIVATE_KEY_PEM") {
        if !value.trim().is_empty() {
            return (value.trim().to_owned(), "env:GITHUB_APP_PRIVATE_KEY_PEM".to_owned());
        }
    }
    (String::new(), String::new())
}

fn resolve_installation_id(
    account_alias: Option<&str>,
    entry: Option<&GitHubAccountEntry>,
    env: &BTreeMap<String, String>,
    overrides: &GitHubAuthOverrides,
) -> (i64, String) {
    if let Some(value) = overrides.installation_id.filter(|value| *value > 0) {
        return (value, "flag:--installation-id".to_owned());
    }
    if let Some(value) = entry.and_then(|item| item.installation_id).filter(|value| *value > 0) {
        return (value, "settings.installation_id".to_owned());
    }
    if let Some(reference) = entry.and_then(|item| item.installation_env.as_deref()) {
        if let Some(value) = env
            .get(reference.trim())
            .and_then(|raw| raw.trim().parse::<i64>().ok())
            .filter(|value| *value > 0)
        {
            return (value, format!("env:{}", reference.trim()));
        }
    }
    let env_key = format!("{}INSTALLATION_ID", github_account_env_prefix(account_alias, entry));
    if let Some(value) =
        env.get(&env_key).and_then(|raw| raw.trim().parse::<i64>().ok()).filter(|value| *value > 0)
    {
        return (value, format!("env:{env_key}"));
    }
    if let Some(value) = env
        .get("GITHUB_INSTALLATION_ID")
        .and_then(|raw| raw.trim().parse::<i64>().ok())
        .filter(|value| *value > 0)
    {
        return (value, "env:GITHUB_INSTALLATION_ID".to_owned());
    }
    (0, String::new())
}

fn github_account_env_prefix(
    account_alias: Option<&str>,
    entry: Option<&GitHubAccountEntry>,
) -> String {
    if let Some(prefix) = entry.and_then(|item| item.vault_prefix.as_deref()) {
        let trimmed = prefix.trim();
        if !trimmed.is_empty() {
            let upper = trimmed.to_ascii_uppercase();
            return if upper.ends_with('_') { upper } else { format!("{upper}_") };
        }
    }
    let alias = account_alias.unwrap_or_default().trim();
    if alias.is_empty() {
        return String::new();
    }
    let mut slug = String::new();
    let mut last_underscore = false;
    for ch in alias.chars() {
        let next = if ch.is_ascii_alphanumeric() {
            last_underscore = false;
            ch.to_ascii_uppercase()
        } else {
            if last_underscore {
                continue;
            }
            last_underscore = true;
            '_'
        };
        slug.push(next);
    }
    let slug = slug.trim_matches('_');
    if slug.is_empty() { String::new() } else { format!("GITHUB_{slug}_") }
}

fn normalize_bearer_token(value: &str) -> String {
    let trimmed = value.trim();
    trimmed
        .strip_prefix("Bearer ")
        .or_else(|| trimmed.strip_prefix("bearer "))
        .unwrap_or(trimmed)
        .trim()
        .to_owned()
}

fn normalize_private_key(value: &str) -> String {
    value.trim().replace("\\n", "\n")
}

fn build_http_client() -> Result<Client, String> {
    Client::builder()
        .timeout(Duration::from_secs(30))
        .build()
        .map_err(|err| format!("build github http client: {err}"))
}

fn github_access_token(
    client: &Client,
    runtime: &GitHubRuntime,
    owner: &str,
    repo: &str,
) -> Result<String, String> {
    match &runtime.credentials {
        GitHubCredentials::OAuth { access_token } => Ok(access_token.clone()),
        GitHubCredentials::App { app_id, app_key, installation_id } => {
            let jwt = github_app_jwt(*app_id, app_key)?;
            let resolved_installation_id = if *installation_id > 0 {
                *installation_id
            } else {
                lookup_installation_id(client, &runtime.base_url, owner, repo, &jwt)?
            };
            exchange_installation_token(
                client,
                &runtime.base_url,
                resolved_installation_id,
                &jwt,
            )
        }
    }
}

#[derive(Debug, Serialize)]
struct GitHubAppClaims {
    iat: i64,
    exp: i64,
    iss: String,
}

fn github_app_jwt(app_id: i64, app_key: &str) -> Result<String, String> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|err| format!("github jwt clock error: {err}"))?
        .as_secs() as i64;
    let claims = GitHubAppClaims {
        iat: now - 60,
        exp: now + 9 * 60,
        iss: app_id.to_string(),
    };
    encode(
        &Header::new(Algorithm::RS256),
        &claims,
        &EncodingKey::from_rsa_pem(app_key.as_bytes())
            .map_err(|err| format!("github app private key invalid: {err}"))?,
    )
    .map_err(|err| format!("sign github app jwt: {err}"))
}

fn exchange_installation_token(
    client: &Client,
    base_url: &str,
    installation_id: i64,
    jwt: &str,
) -> Result<String, String> {
    let url = resolve_url(
        base_url,
        &format!("/app/installations/{installation_id}/access_tokens"),
        &BTreeMap::new(),
    )?;
    let response = client
        .post(url)
        .headers(default_headers(&format!("Bearer {jwt}"))?)
        .send()
        .map_err(|err| format!("github installation token request failed: {err}"))?;
    let status = response.status();
    let request_id = response
        .headers()
        .get("x-github-request-id")
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .to_owned();
    let body = response
        .text()
        .map_err(|err| format!("read github installation token response: {err}"))?;
    if !status.is_success() {
        return Err(format!(
            "github installation token request failed: status={} request_id={} body={}",
            status.as_u16(),
            request_id,
            body.trim()
        ));
    }
    let payload: Value = serde_json::from_str(&body)
        .map_err(|err| format!("decode github installation token response: {err}"))?;
    let token = payload
        .get("token")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .trim()
        .to_owned();
    if token.is_empty() {
        return Err("github installation token response missing token".to_owned());
    }
    Ok(token)
}

fn lookup_installation_id(
    client: &Client,
    base_url: &str,
    owner: &str,
    repo: &str,
    jwt: &str,
) -> Result<i64, String> {
    let mut candidates = Vec::new();
    if !owner.trim().is_empty() && !repo.trim().is_empty() {
        candidates.push(format!("/repos/{owner}/{repo}/installation"));
    }
    if !owner.trim().is_empty() {
        candidates.push(format!("/orgs/{owner}/installation"));
        candidates.push(format!("/users/{owner}/installation"));
    }
    for path in candidates {
        let url = resolve_url(base_url, &path, &BTreeMap::new())?;
        let response = client
            .get(url)
            .headers(default_headers(&format!("Bearer {jwt}"))?)
            .send()
            .map_err(|err| format!("github installation lookup failed: {err}"))?;
        if !response.status().is_success() {
            continue;
        }
        let body = response
            .text()
            .map_err(|err| format!("read github installation lookup response: {err}"))?;
        let payload: Value = match serde_json::from_str(&body) {
            Ok(value) => value,
            Err(_) => continue,
        };
        if let Some(id) = payload.get("id").and_then(Value::as_i64).filter(|value| *value > 0) {
            return Ok(id);
        }
    }
    Err(format!(
        "unable to resolve github app installation id for owner={} repo={}",
        owner.trim(),
        repo.trim()
    ))
}

fn github_get(
    client: &Client,
    base_url: &str,
    path: &str,
    params: &BTreeMap<String, String>,
    token: &str,
) -> Result<Response, String> {
    let url = resolve_url(base_url, path, params)?;
    client
        .get(url)
        .headers(default_headers(&format!("Bearer {}", normalize_bearer_token(token)))?)
        .send()
        .map_err(|err| format!("github request failed: {err}"))
}

fn github_graphql(
    runtime: &GitHubRuntime,
    owner: &str,
    query: &str,
    variables: Value,
) -> Result<GitHubAPIResponse, String> {
    let client = build_http_client()?;
    let token = github_access_token(&client, runtime, owner, "")?;
    let url = resolve_url(&runtime.base_url, "/graphql", &BTreeMap::new())?;
    let response = client
        .post(url)
        .headers(default_headers(&format!("Bearer {}", normalize_bearer_token(&token)))?)
        .json(&serde_json::json!({
            "query": query.trim(),
            "variables": variables,
        }))
        .send()
        .map_err(|err| format!("github request failed: {err}"))?;
    let mut payload = normalize_response(response)?;
    let Some(root) = payload.data.as_ref() else {
        return Err("graphql response missing body".to_owned());
    };
    if let Some(errors) = root.get("errors").and_then(Value::as_array) {
        let messages = errors
            .iter()
            .filter_map(|item| item.get("message").and_then(Value::as_str))
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .collect::<Vec<_>>();
        if messages.is_empty() {
            return Err("graphql returned errors".to_owned());
        }
        return Err(format!("graphql returned errors: {}", messages.join("; ")));
    }
    let data = root
        .get("data")
        .cloned()
        .ok_or_else(|| "graphql response missing data".to_owned())?;
    payload.data = Some(data);
    Ok(payload)
}

fn default_headers(auth_value: &str) -> Result<HeaderMap, String> {
    let mut headers = HeaderMap::new();
    headers.insert(ACCEPT, HeaderValue::from_static("application/vnd.github+json"));
    headers.insert(
        HeaderName::from_static("x-github-api-version"),
        HeaderValue::from_static("2022-11-28"),
    );
    headers.insert(USER_AGENT, HeaderValue::from_static("si-rs"));
    headers.insert(
        AUTHORIZATION,
        HeaderValue::from_str(auth_value)
            .map_err(|err| format!("build github auth header: {err}"))?,
    );
    Ok(headers)
}

fn normalize_response(response: Response) -> Result<GitHubAPIResponse, String> {
    let status_code = response.status().as_u16();
    let status = response.status().to_string();
    let headers = response.headers().clone();
    let request_id = first_header(&headers, "x-github-request-id");
    let body = response
        .text()
        .map_err(|err| format!("read github response body: {err}"))?;
    if status_code < 200 || status_code >= 300 {
        return Err(format!(
            "github request failed: status={} request_id={} body={}",
            status_code,
            request_id,
            body.trim()
        ));
    }
    let mut payload = GitHubAPIResponse {
        status_code,
        status,
        request_id,
        headers: BTreeMap::new(),
        body,
        data: None,
        list: Vec::new(),
    };
    for (key, value) in &headers {
        if let Ok(text) = value.to_str() {
            payload.headers.insert(key.as_str().to_owned(), text.to_owned());
        }
    }
    if payload.body.trim().is_empty() {
        return Ok(payload);
    }
    if let Ok(parsed) = serde_json::from_str::<Value>(&payload.body) {
        match parsed {
            Value::Array(items) => payload.list = items,
            Value::Object(_) => payload.data = Some(parsed),
            _ => {}
        }
    }
    Ok(payload)
}

fn resolve_url(
    base_url: &str,
    path: &str,
    params: &BTreeMap<String, String>,
) -> Result<Url, String> {
    let mut url = Url::parse(base_url).map_err(|err| format!("invalid github base url: {err}"))?;
    let existing_path = url.path().trim_end_matches('/');
    let next_path = path.trim_start_matches('/');
    let joined = if existing_path.is_empty() || existing_path == "/" {
        format!("/{}", next_path)
    } else if next_path.is_empty() {
        existing_path.to_owned()
    } else {
        format!("{existing_path}/{next_path}")
    };
    url.set_path(&joined);
    if !params.is_empty() {
        let mut pairs = url.query_pairs_mut();
        pairs.clear();
        for (key, value) in params {
            pairs.append_pair(key, value);
        }
    }
    Ok(url)
}

fn first_header(headers: &HeaderMap, name: &str) -> String {
    headers
        .get(name)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .trim()
        .to_owned()
}

fn parse_next_link(headers: &HeaderMap) -> Option<String> {
    let link = first_header(headers, "link");
    for part in link.split(',') {
        let trimmed = part.trim();
        if !trimmed.contains("rel=\"next\"") {
            continue;
        }
        let start = trimmed.find('<')?;
        let end = trimmed[start + 1..].find('>')?;
        return Some(trimmed[start + 1..start + 1 + end].to_owned());
    }
    None
}

fn percent_encode_path_segment(value: &str) -> String {
    let mut out = String::new();
    for byte in value.as_bytes() {
        let ch = *byte as char;
        if ch.is_ascii_alphanumeric() || matches!(ch, '-' | '_' | '.' | '~') {
            out.push(ch);
        } else {
            out.push('%');
            out.push_str(&format!("{byte:02X}"));
        }
    }
    out
}

fn preview_secret(secret: &str) -> String {
    let trimmed = secret.trim();
    if trimmed.is_empty() {
        return "-".to_owned();
    }
    let preview: String = trimmed.chars().take(8).collect();
    if trimmed.chars().count() <= 10 { preview } else { format!("{preview}...") }
}

fn join_sources(values: &[String]) -> String {
    values
        .iter()
        .map(|value| value.trim())
        .filter(|value| !value.is_empty())
        .collect::<Vec<_>>()
        .join(",")
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

    #[test]
    fn resolve_auth_status_uses_oauth_sources() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            GitHubAccountEntry {
                owner: Some("Aureuma".to_owned()),
                auth_mode: Some("oauth".to_owned()),
                ..GitHubAccountEntry::default()
            },
        );
        let settings = GitHubSettings {
            default_account: Some("core".to_owned()),
            accounts,
            ..GitHubSettings::default()
        };
        let mut env = BTreeMap::new();
        env.insert("GITHUB_TOKEN".to_owned(), "gho_example_token".to_owned());

        let resolved =
            resolve_auth_status(&settings, &env, &GitHubAuthOverrides::default()).unwrap();

        assert_eq!(resolved.account_alias, "core");
        assert_eq!(resolved.owner, "Aureuma");
        assert_eq!(resolved.auth_mode, "oauth");
        assert_eq!(resolved.base_url, "https://api.github.com");
        assert_eq!(resolved.source, "settings.default_account,settings.auth_mode,env:GITHUB_TOKEN");
        assert_eq!(resolved.token_preview, "gho_exam...");
    }

    #[test]
    fn resolve_auth_status_uses_app_sources() {
        let mut accounts = BTreeMap::new();
        accounts.insert(
            "core".to_owned(),
            GitHubAccountEntry {
                owner: Some("Aureuma".to_owned()),
                ..GitHubAccountEntry::default()
            },
        );
        let settings = GitHubSettings {
            default_account: Some("core".to_owned()),
            default_auth_mode: Some("app".to_owned()),
            accounts,
            ..GitHubSettings::default()
        };
        let mut env = BTreeMap::new();
        env.insert("GITHUB_CORE_APP_ID".to_owned(), "42".to_owned());
        env.insert(
            "GITHUB_CORE_APP_PRIVATE_KEY_PEM".to_owned(),
            "-----BEGIN PRIVATE KEY-----abc".to_owned(),
        );
        env.insert("GITHUB_CORE_INSTALLATION_ID".to_owned(), "99".to_owned());

        let resolved =
            resolve_auth_status(&settings, &env, &GitHubAuthOverrides::default()).unwrap();

        assert_eq!(resolved.account_alias, "core");
        assert_eq!(resolved.owner, "Aureuma");
        assert_eq!(resolved.auth_mode, "app");
        assert_eq!(resolved.base_url, "https://api.github.com");
        assert_eq!(
            resolved.source,
            "settings.default_account,settings.default_auth_mode,env:GITHUB_CORE_APP_ID,env:GITHUB_CORE_APP_PRIVATE_KEY_PEM,env:GITHUB_CORE_INSTALLATION_ID"
        );
        assert_eq!(resolved.token_preview, "-");
    }
}
