use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::fs;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use tempfile::NamedTempFile;
use thiserror::Error;

pub const WARMUP_STATE_VERSION: i32 = 3;

#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct WarmupState {
    pub version: i32,
    #[serde(default)]
    pub updated_at: String,
    #[serde(default)]
    pub profiles: BTreeMap<String, WarmupProfileState>,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq, Serialize)]
pub struct WarmupProfileState {
    pub profile_id: String,
    #[serde(default)]
    pub last_attempt: String,
    #[serde(default)]
    pub last_result: String,
    #[serde(default)]
    pub last_error: String,
    #[serde(default)]
    pub last_weekly_used_pct: f64,
    #[serde(default)]
    pub last_weekly_used_ok: bool,
    #[serde(default)]
    pub last_weekly_reset: String,
    #[serde(default)]
    pub last_warmed_reset: String,
    #[serde(default)]
    pub last_usage_delta: f64,
    #[serde(default)]
    pub next_due: String,
    #[serde(default)]
    pub failure_count: i32,
    #[serde(default)]
    pub paused: bool,
}

#[derive(Debug, Deserialize)]
struct RawWarmupState {
    #[serde(default)]
    version: i32,
    #[serde(default)]
    updated_at: String,
    #[serde(default)]
    profiles: BTreeMap<String, Option<WarmupProfileState>>,
}

#[derive(Debug, Error)]
pub enum WarmupStateError {
    #[error("state path required")]
    MissingPath,
    #[error("home directory required")]
    MissingHomeDirectory,
    #[error("state file must be a regular file")]
    NotRegularFile,
    #[error("stat state file: {0}")]
    Stat(#[source] std::io::Error),
    #[error("read state file: {0}")]
    Read(#[source] std::io::Error),
    #[error("parse state file: {0}")]
    Parse(#[source] serde_json::Error),
    #[error("create state directory: {0}")]
    CreateDirectory(#[source] std::io::Error),
    #[error("serialize state file: {0}")]
    Serialize(#[source] serde_json::Error),
    #[error("create temp state file: {0}")]
    CreateTemp(#[source] std::io::Error),
    #[error("write temp state file: {0}")]
    WriteTemp(#[source] std::io::Error),
    #[error("persist temp state file: {0}")]
    Persist(#[source] std::io::Error),
    #[error("set state file permissions: {0}")]
    SetPermissions(#[source] std::io::Error),
}

pub fn default_state_path(home: Option<&Path>) -> Result<PathBuf, WarmupStateError> {
    let root = match home {
        Some(path) => path.to_path_buf(),
        None => std::env::var_os("HOME")
            .map(PathBuf::from)
            .ok_or(WarmupStateError::MissingHomeDirectory)?,
    };
    Ok(root.join(".si").join("warmup").join("state.json"))
}

pub fn load_state(path: impl AsRef<Path>) -> Result<WarmupState, WarmupStateError> {
    let path = clean_path(path.as_ref())?;
    match fs::metadata(path) {
        Ok(metadata) => {
            if !metadata.is_file() {
                return Err(WarmupStateError::NotRegularFile);
            }
        }
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
            return Ok(WarmupState {
                version: WARMUP_STATE_VERSION,
                updated_at: String::new(),
                profiles: BTreeMap::new(),
            });
        }
        Err(err) => return Err(WarmupStateError::Stat(err)),
    }

    let raw = fs::read(path).map_err(WarmupStateError::Read)?;
    let state: RawWarmupState = serde_json::from_slice(&raw).map_err(WarmupStateError::Parse)?;
    Ok(normalize_state(state))
}

pub fn save_state(path: impl AsRef<Path>, state: &WarmupState) -> Result<(), WarmupStateError> {
    let path = clean_path(path.as_ref())?;
    let dir = path.parent().ok_or(WarmupStateError::MissingPath)?;
    fs::create_dir_all(dir).map_err(WarmupStateError::CreateDirectory)?;

    let mut payload =
        serde_json::to_vec_pretty(&state.normalized()).map_err(WarmupStateError::Serialize)?;
    payload.push(b'\n');

    let mut tmp = NamedTempFile::new_in(dir).map_err(WarmupStateError::CreateTemp)?;
    set_file_mode(tmp.path(), 0o600).map_err(WarmupStateError::SetPermissions)?;
    use std::io::Write as _;
    tmp.write_all(&payload).map_err(WarmupStateError::WriteTemp)?;
    tmp.flush().map_err(WarmupStateError::WriteTemp)?;
    tmp.persist(path).map_err(|err| WarmupStateError::Persist(err.error))?;
    set_file_mode(path, 0o600).map_err(WarmupStateError::SetPermissions)?;
    Ok(())
}

pub fn render_state_text(state: &WarmupState, now: DateTime<Utc>) -> String {
    if state.profiles.is_empty() {
        return "warmup state is empty\n".to_owned();
    }

    let headers = ["PROFILE", "RESULT", "USED", "DELTA", "NEXT", "ERROR"];
    let mut rows = Vec::with_capacity(state.profiles.len());
    for row in state.profiles.values() {
        let used = if row.last_weekly_used_ok {
            format!("{:.2}%", row.last_weekly_used_pct)
        } else {
            "-".to_owned()
        };
        let delta = if row.last_usage_delta != 0.0 {
            format!("{:.3}", row.last_usage_delta)
        } else {
            "-".to_owned()
        };
        let result = non_empty_or_dash(&row.last_result);
        let next = format_next_due(&row.next_due, now);
        rows.push([
            row.profile_id.clone(),
            result,
            used,
            delta,
            next,
            row.last_error.trim().to_owned(),
        ]);
    }

    let mut widths = headers.map(str::len);
    for row in &rows {
        for (index, value) in row.iter().enumerate() {
            widths[index] = widths[index].max(value.len());
        }
    }

    let mut out = String::new();
    out.push_str(&format_row(&headers.map(str::to_owned), &widths));
    for row in rows {
        out.push_str(&format_row(&row, &widths));
    }
    out
}

fn normalize_state(raw: RawWarmupState) -> WarmupState {
    let mut state = WarmupState {
        version: if raw.version == 0 { WARMUP_STATE_VERSION } else { raw.version },
        updated_at: raw.updated_at.trim().to_owned(),
        profiles: raw
            .profiles
            .into_iter()
            .filter_map(|(key, value)| value.map(|row| (key, row)))
            .collect(),
    };

    if state.version < WARMUP_STATE_VERSION {
        for row in state.profiles.values_mut() {
            *row = row.normalized();
            if !row.last_weekly_used_ok
                && (!row.last_weekly_reset.trim().is_empty() || row.last_weekly_used_pct != 0.0)
            {
                row.last_weekly_used_ok = true;
            }
            if state.version < 3
                && row.last_result.eq_ignore_ascii_case("warmed")
                && row.last_warmed_reset.trim().is_empty()
            {
                row.last_warmed_reset = row.last_weekly_reset.trim().to_owned();
            }
        }
        state.version = WARMUP_STATE_VERSION;
    } else {
        for row in state.profiles.values_mut() {
            *row = row.normalized();
        }
    }

    state
}

impl WarmupState {
    fn normalized(&self) -> Self {
        Self {
            version: self.version.max(WARMUP_STATE_VERSION),
            updated_at: self.updated_at.trim().to_owned(),
            profiles: self
                .profiles
                .iter()
                .map(|(key, row)| (key.trim().to_owned(), row.normalized()))
                .collect(),
        }
    }
}

impl WarmupProfileState {
    fn normalized(&self) -> Self {
        Self {
            profile_id: self.profile_id.trim().to_owned(),
            last_attempt: self.last_attempt.trim().to_owned(),
            last_result: self.last_result.trim().to_owned(),
            last_error: self.last_error.trim().to_owned(),
            last_weekly_used_pct: self.last_weekly_used_pct,
            last_weekly_used_ok: self.last_weekly_used_ok,
            last_weekly_reset: self.last_weekly_reset.trim().to_owned(),
            last_warmed_reset: self.last_warmed_reset.trim().to_owned(),
            last_usage_delta: self.last_usage_delta,
            next_due: self.next_due.trim().to_owned(),
            failure_count: self.failure_count,
            paused: self.paused,
        }
    }
}

fn clean_path(path: &Path) -> Result<&Path, WarmupStateError> {
    if path.as_os_str().is_empty() {
        return Err(WarmupStateError::MissingPath);
    }
    Ok(path)
}

fn non_empty_or_dash(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() { "-".to_owned() } else { trimmed.to_owned() }
}

fn format_row(values: &[String; 6], widths: &[usize; 6]) -> String {
    let mut row = String::new();
    for (index, value) in values.iter().enumerate() {
        if index > 0 {
            row.push_str("  ");
        }
        row.push_str(value);
        let padding = widths[index].saturating_sub(value.len());
        if padding > 0 {
            row.push_str(&" ".repeat(padding));
        }
    }
    row.push('\n');
    row
}

fn format_next_due(raw: &str, now: DateTime<Utc>) -> String {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return "-".to_owned();
    }
    match DateTime::parse_from_rfc3339(trimmed) {
        Ok(value) => {
            let value = value.with_timezone(&Utc);
            let delta = value.signed_duration_since(now);
            let human = if delta.num_seconds().abs() < 60 {
                "now".to_owned()
            } else if delta.num_hours().abs() < 24 {
                if delta.num_seconds() >= 0 {
                    format!("in {}h", delta.num_hours())
                } else {
                    format!("{}h ago", delta.num_hours().abs())
                }
            } else if delta.num_seconds() >= 0 {
                format!("in {}d", delta.num_days())
            } else {
                format!("{}d ago", delta.num_days().abs())
            };
            format!("{} ({})", value.format("%Y-%m-%d %H:%M UTC"), human)
        }
        Err(_) => trimmed.to_owned(),
    }
}

fn set_file_mode(path: &Path, mode: u32) -> Result<(), std::io::Error> {
    #[cfg(unix)]
    {
        fs::set_permissions(path, PermissionsExt::from_mode(mode))?;
    }
    #[cfg(not(unix))]
    {
        let _ = (path, mode);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{
        WARMUP_STATE_VERSION, WarmupProfileState, default_state_path, load_state,
        render_state_text, save_state,
    };
    use chrono::{TimeZone, Utc};
    use std::fs;
    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;
    use tempfile::tempdir;

    #[test]
    fn load_state_returns_empty_when_missing() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("state.json");

        let state = load_state(&path).expect("load state");
        assert_eq!(state.version, WARMUP_STATE_VERSION);
        assert!(state.profiles.is_empty());
    }

    #[test]
    fn load_state_upgrades_legacy_warmed_rows() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("state.json");
        fs::write(
            &path,
            r#"{
  "version": 2,
  "profiles": {
    "ferma": {
      "profile_id": " ferma ",
      "last_result": "warmed",
      "last_weekly_used_pct": 12.5,
      "last_weekly_reset": "2030-03-20T00:00:00Z"
    }
  }
}
"#,
        )
        .expect("write state");

        let state = load_state(&path).expect("load state");
        let profile = state.profiles.get("ferma").expect("profile");
        assert_eq!(state.version, WARMUP_STATE_VERSION);
        assert_eq!(profile.profile_id, "ferma");
        assert!(profile.last_weekly_used_ok);
        assert_eq!(profile.last_warmed_reset, "2030-03-20T00:00:00Z");
    }

    #[test]
    fn render_state_text_orders_rows_and_formats_fields() {
        let mut state = super::WarmupState {
            version: WARMUP_STATE_VERSION,
            updated_at: String::new(),
            profiles: Default::default(),
        };
        state.profiles.insert(
            "zeta".to_owned(),
            WarmupProfileState {
                profile_id: "zeta".to_owned(),
                last_result: "ready".to_owned(),
                last_weekly_used_ok: true,
                last_weekly_used_pct: 25.0,
                next_due: "2030-03-20T12:00:00Z".to_owned(),
                ..WarmupProfileState::default()
            },
        );
        state.profiles.insert(
            "alpha".to_owned(),
            WarmupProfileState {
                profile_id: "alpha".to_owned(),
                last_result: "failed".to_owned(),
                last_error: "boom".to_owned(),
                last_usage_delta: 0.125,
                ..WarmupProfileState::default()
            },
        );

        let output =
            render_state_text(&state, Utc.with_ymd_and_hms(2030, 3, 19, 12, 0, 0).unwrap());
        let lines: Vec<&str> = output.lines().collect();
        assert!(lines[0].contains("PROFILE"));
        assert!(lines[1].starts_with("alpha"));
        assert!(lines[2].starts_with("zeta"));
        assert!(output.contains("25.00%"));
        assert!(output.contains("0.125"));
        assert!(output.contains("boom"));
    }

    #[test]
    fn default_state_path_uses_home() {
        let dir = tempdir().expect("tempdir");
        let path = default_state_path(Some(dir.path())).expect("path");
        assert!(path.ends_with(".si/warmup/state.json"));
    }

    #[test]
    fn save_state_round_trips_with_strict_permissions() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("state.json");
        let mut state = super::WarmupState {
            version: 0,
            updated_at: " 2030-03-19T12:00:00Z ".to_owned(),
            profiles: Default::default(),
        };
        state.profiles.insert(
            " ferma ".to_owned(),
            WarmupProfileState {
                profile_id: " ferma ".to_owned(),
                last_result: " ready ".to_owned(),
                ..WarmupProfileState::default()
            },
        );

        save_state(&path, &state).expect("save state");
        let loaded = load_state(&path).expect("load state");
        assert_eq!(loaded.version, WARMUP_STATE_VERSION);
        assert_eq!(loaded.updated_at, "2030-03-19T12:00:00Z");
        assert_eq!(loaded.profiles["ferma"].profile_id, "ferma");
        assert_eq!(loaded.profiles["ferma"].last_result, "ready");
        #[cfg(unix)]
        {
            let mode = fs::metadata(&path).expect("stat state").permissions().mode() & 0o777;
            assert_eq!(mode, 0o600);
        }
    }
}
