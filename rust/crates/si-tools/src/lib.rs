use std::collections::BTreeSet;
use std::env;
use std::fs;
use std::io;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Command;

pub const USAGE_TEXT: &str = r#"Import plaintext .env files into si vault (native SI format).

Defaults:
  --src .
  --section default
  --identity-file ~/.si/vault/keys/age.key

Examples:
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin import-dotenv-to-si-vault -- --src .
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin import-dotenv-to-si-vault -- --src . --section app-dev
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin import-dotenv-to-si-vault -- --dry-run

Notes:
  - This reads plaintext .env files. Use for migration/bootstrap only.
  - Target env is inferred per file:
      *.prod*|*.production* -> prod
      otherwise             -> dev
  - Requires: the si binary on PATH."#;

pub fn infer_target_env(base: &str) -> &'static str {
    let lower = base.to_ascii_lowercase();
    if lower.contains(".prod") || lower.contains("production") { "prod" } else { "dev" }
}

pub fn list_env_files(src: &Path) -> Result<Vec<PathBuf>, std::io::Error> {
    let mut out = Vec::new();
    for entry in fs::read_dir(src)? {
        let entry = entry?;
        let path = entry.path();
        if path.is_dir() {
            continue;
        }
        let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
            continue;
        };
        if !name.starts_with(".env") || name == ".env.keys" || name == ".env.vault" {
            continue;
        }
        out.push(path);
    }
    out.sort();
    Ok(out)
}

pub fn parse_dotenv(content: &str) -> std::collections::BTreeMap<String, String> {
    let mut out = std::collections::BTreeMap::new();
    for raw_line in content.lines() {
        let mut line = raw_line.trim().to_string();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(rest) = line.strip_prefix("export ") {
            line = rest.trim().to_string();
        }
        let Some((key_raw, value_raw)) = line.split_once('=') else {
            continue;
        };
        let key = key_raw.trim();
        if !is_valid_key(key) {
            continue;
        }
        let mut value = value_raw.trim().to_string();
        if value.len() >= 2 {
            if value.starts_with('\'') && value.ends_with('\'') {
                value = value[1..value.len() - 1].to_string();
            } else if value.starts_with('"') && value.ends_with('"') {
                value = decode_double_quoted(&value[1..value.len() - 1]);
            }
        }
        out.insert(key.to_string(), value);
    }
    out
}

pub fn sync_codex_skills(src: &Path, dest: &Path) -> io::Result<()> {
    if !src.exists() {
        return Ok(());
    }
    if dest.exists() {
        clear_dir(dest)?;
    }
    copy_tree(src, dest)
}

pub fn sync_codex_auth(
    profile_id: Option<&str>,
    si_dir: &Path,
    codex_home: &Path,
) -> io::Result<()> {
    let Some(profile_id) = profile_id.map(str::trim).filter(|value| !value.is_empty()) else {
        return Ok(());
    };
    let source = si_dir.join("codex").join("profiles").join(profile_id).join("auth.json");
    let dest = codex_home.join("auth.json");
    if !source.exists() {
        if dest.exists() {
            fs::remove_file(dest)?;
        }
        return Ok(());
    }
    copy_file(&source, &dest, 0o600)
}

fn clear_dir(path: &Path) -> io::Result<()> {
    for entry in fs::read_dir(path)? {
        let entry = entry?;
        let entry_path = entry.path();
        let metadata = entry.metadata()?;
        if metadata.is_dir() {
            fs::remove_dir_all(entry_path)?;
        } else {
            fs::remove_file(entry_path)?;
        }
    }
    Ok(())
}

fn copy_tree(src: &Path, dest: &Path) -> io::Result<()> {
    fs::create_dir_all(dest)?;
    for entry in fs::read_dir(src)? {
        let entry = entry?;
        let src_path = entry.path();
        let dest_path = dest.join(entry.file_name());
        let metadata = entry.metadata()?;
        if metadata.is_dir() {
            copy_tree(&src_path, &dest_path)?;
        } else if metadata.is_file() {
            copy_file(&src_path, &dest_path, metadata.permissions().mode())?;
        }
    }
    Ok(())
}

fn copy_file(src: &Path, dest: &Path, mode: u32) -> io::Result<()> {
    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent)?;
    }
    fs::copy(src, dest)?;
    fs::set_permissions(dest, fs::Permissions::from_mode(mode))?;
    Ok(())
}

pub fn shell_escape(input: &str) -> String {
    if input.is_empty() {
        return "''".to_string();
    }
    let escaped = input.replace('\'', "'\"'\"'");
    format!("'{escaped}'")
}

pub fn command_exists(name: &str) -> bool {
    Command::new("sh")
        .arg("-lc")
        .arg(format!("command -v {} >/dev/null 2>&1", shell_escape(name)))
        .status()
        .map(|status| status.success())
        .unwrap_or(false)
}

pub fn browser_mcp_url_from_env() -> Option<String> {
    for key in ["SI_BROWSER_MCP_URL", "BROWSER_MCP_URL", "CODEX_BROWSER_MCP_URL"] {
        if let Ok(value) = env::var(key) {
            let trimmed = value.trim();
            if !trimmed.is_empty() {
                return Some(trimmed.to_string());
            }
        }
    }

    let host = ["SI_BROWSER_MCP_HOST", "BROWSER_MCP_HOST", "CODEX_BROWSER_MCP_HOST"]
        .into_iter()
        .find_map(|key| env::var(key).ok())
        .filter(|value| !value.trim().is_empty())
        .unwrap_or_else(|| "127.0.0.1".to_string());
    let port = ["SI_BROWSER_MCP_PORT", "BROWSER_MCP_PORT", "CODEX_BROWSER_MCP_PORT"]
        .into_iter()
        .find_map(|key| env::var(key).ok())
        .filter(|value| !value.trim().is_empty())?;
    Some(format!("http://{}:{}/mcp", host.trim(), port.trim()))
}

pub fn write_codex_config(config_path: &Path, template_path: Option<&Path>) -> io::Result<()> {
    if config_path.exists() {
        return Ok(());
    }
    if let Some(parent) = config_path.parent() {
        fs::create_dir_all(parent)?;
    }
    let body = match template_path {
        Some(path) if path.exists() => fs::read_to_string(path)?,
        _ => default_codex_config(),
    };
    fs::write(config_path, body)?;
    fs::set_permissions(config_path, fs::Permissions::from_mode(0o600))?;
    Ok(())
}

fn default_codex_config() -> String {
    r#"# managed by si-codex-init
approval_policy = "never"
sandbox_mode = "danger-full-access"
"#
    .to_string()
}

pub fn collect_git_safe_directories(cwd: Option<&Path>) -> io::Result<Vec<PathBuf>> {
    let mut dirs = BTreeSet::new();
    for path in ["/workspace", "/workspaces", "/mnt"] {
        let path = PathBuf::from(path);
        if path.exists() {
            dirs.insert(path);
        }
    }
    if let Some(path) = cwd {
        let mut current = Some(path);
        while let Some(candidate) = current {
            if candidate.exists() {
                dirs.insert(candidate.to_path_buf());
            }
            current = candidate.parent().filter(|parent| {
                parent.starts_with("/workspace") || parent.starts_with("/workspaces")
            });
        }
    }
    for mount in read_mount_points()? {
        if mount.starts_with("/workspace")
            || mount.starts_with("/workspaces")
            || mount.starts_with("/mnt")
            || mount.starts_with("/home/si")
        {
            dirs.insert(mount);
        }
    }
    Ok(dirs.into_iter().collect())
}

pub fn ensure_git_safe_directories(home: &Path, dirs: &[PathBuf]) -> io::Result<()> {
    if !command_exists("git") {
        return Ok(());
    }
    for dir in dirs {
        let _ = Command::new("git")
            .env("HOME", home)
            .arg("config")
            .arg("--global")
            .arg("--add")
            .arg("safe.directory")
            .arg(dir)
            .status();
    }
    Ok(())
}

fn read_mount_points() -> io::Result<Vec<PathBuf>> {
    let content = fs::read_to_string("/proc/self/mountinfo")?;
    let mut mounts = Vec::new();
    for line in content.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if let Some(raw_mount) = fields.get(4) {
            mounts.push(PathBuf::from(decode_mount_field(raw_mount)));
        }
    }
    Ok(mounts)
}

fn decode_mount_field(raw: &str) -> String {
    let mut out = String::new();
    let bytes = raw.as_bytes();
    let mut idx = 0;
    while idx < bytes.len() {
        if bytes[idx] == b'\\' && idx + 3 < bytes.len() {
            let octal = &raw[idx + 1..idx + 4];
            if octal.chars().all(|ch| ('0'..='7').contains(&ch)) {
                if let Ok(value) = u8::from_str_radix(octal, 8) {
                    out.push(value as char);
                    idx += 4;
                    continue;
                }
            }
        }
        out.push(bytes[idx] as char);
        idx += 1;
    }
    out
}

fn is_valid_key(key: &str) -> bool {
    let mut chars = key.chars();
    match chars.next() {
        Some(first) if first == '_' || first.is_ascii_alphabetic() => {}
        _ => return false,
    }
    chars.all(|ch| ch == '_' || ch.is_ascii_alphanumeric())
}

fn decode_double_quoted(raw: &str) -> String {
    let mut out = String::new();
    let mut chars = raw.chars();
    while let Some(ch) = chars.next() {
        if ch != '\\' {
            out.push(ch);
            continue;
        }
        match chars.next() {
            Some('n') => out.push('\n'),
            Some('r') => out.push('\r'),
            Some('t') => out.push('\t'),
            Some('\\') => out.push('\\'),
            Some('"') => out.push('"'),
            Some(other) => {
                out.push('\\');
                out.push(other);
            }
            None => out.push('\\'),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn infer_target_env_cases() {
        let cases = [
            (".env", "dev"),
            (".env.prod", "prod"),
            (".env.PRODUCTION.local", "prod"),
            (".env.development", "dev"),
        ];
        for (input, want) in cases {
            assert_eq!(infer_target_env(input), want);
        }
    }

    #[test]
    fn parse_dotenv_cases() {
        let input = r#"
# comment
export FOO=bar
BAD-KEY=bad
QUOTED_SINGLE='hello world'
QUOTED_DOUBLE="line1\nline2"
RAW="unterminated
SPACE_KEY = value
"#;
        let got = parse_dotenv(input);
        let want = std::collections::BTreeMap::from([
            ("FOO".to_string(), "bar".to_string()),
            ("QUOTED_SINGLE".to_string(), "hello world".to_string()),
            ("QUOTED_DOUBLE".to_string(), "line1\nline2".to_string()),
            ("SPACE_KEY".to_string(), "value".to_string()),
            ("RAW".to_string(), "\"unterminated".to_string()),
        ]);
        assert_eq!(got, want);
    }

    #[test]
    fn list_env_files_filters() {
        let dir = tempfile::tempdir().unwrap();
        let names = [".env", ".env.dev", ".env.prod", ".env.keys", ".env.vault", "notenv"];
        for name in names {
            std::fs::write(dir.path().join(name), "x").unwrap();
        }
        std::fs::create_dir_all(dir.path().join(".envdir")).unwrap();
        let got = list_env_files(dir.path()).unwrap();
        let want = vec![
            dir.path().join(".env"),
            dir.path().join(".env.dev"),
            dir.path().join(".env.prod"),
        ];
        assert_eq!(got, want);
    }

    #[test]
    fn shell_escape_handles_quotes() {
        assert_eq!(shell_escape("plain"), "'plain'");
        assert_eq!(shell_escape("foo'bar"), "'foo'\"'\"'bar'");
    }

    #[test]
    fn decode_mount_field_unescapes_octal() {
        assert_eq!(decode_mount_field("/workspace/foo\\040bar"), "/workspace/foo bar");
    }

    #[test]
    fn sync_codex_skills_replaces_existing_contents_without_removing_root() {
        let src = tempfile::tempdir().expect("src tempdir");
        let dest_parent = tempfile::tempdir().expect("dest parent");
        let dest = dest_parent.path().join("skills");
        std::fs::create_dir_all(src.path().join("alpha")).expect("mkdir src alpha");
        std::fs::write(src.path().join("alpha").join("SKILL.md"), "alpha\n")
            .expect("write src skill");
        std::fs::create_dir_all(dest.join("stale")).expect("mkdir stale");
        std::fs::write(dest.join("stale").join("SKILL.md"), "stale\n").expect("write stale skill");

        sync_codex_skills(src.path(), &dest).expect("sync skills");

        assert!(dest.exists());
        assert!(dest.join("alpha").join("SKILL.md").exists());
        assert!(!dest.join("stale").exists());
    }

    #[test]
    fn sync_codex_auth_copies_selected_profile_into_codex_home() {
        let home = tempfile::tempdir().expect("home tempdir");
        let codex_home = home.path().join(".codex");
        let si_dir = home.path().join(".si");
        let source = si_dir.join("codex").join("profiles").join("profile-alpha").join("auth.json");
        std::fs::create_dir_all(source.parent().expect("source parent")).expect("mkdir source");
        std::fs::write(&source, "{\"tokens\":{\"access_token\":\"abc\"}}\n").expect("write auth");

        sync_codex_auth(Some("profile-alpha"), &si_dir, &codex_home).expect("sync auth");

        assert_eq!(
            std::fs::read_to_string(codex_home.join("auth.json")).expect("read synced auth"),
            "{\"tokens\":{\"access_token\":\"abc\"}}\n"
        );
    }

    #[test]
    fn sync_codex_auth_removes_stale_auth_when_selected_profile_is_missing() {
        let home = tempfile::tempdir().expect("home tempdir");
        let codex_home = home.path().join(".codex");
        std::fs::create_dir_all(&codex_home).expect("mkdir codex home");
        std::fs::write(codex_home.join("auth.json"), "stale\n").expect("write stale auth");

        sync_codex_auth(Some("profile-alpha"), &home.path().join(".si"), &codex_home)
            .expect("sync auth");

        assert!(!codex_home.join("auth.json").exists());
    }
}
