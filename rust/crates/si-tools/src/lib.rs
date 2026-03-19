use std::fs;
use std::path::{Path, PathBuf};

pub const USAGE_TEXT: &str = r#"Import plaintext .env files into si vault (native SI format).

Defaults:
  --src .
  --section default
  --identity-file ~/.si/vault/keys/age.key

Examples:
  tools/vault/import-dotenv-to-si-vault.sh --src .
  tools/vault/import-dotenv-to-si-vault.sh --src . --section app-dev
  tools/vault/import-dotenv-to-si-vault.sh --src . --dry-run

Notes:
  - This reads plaintext .env files. Use for migration/bootstrap only.
  - Target env is inferred per file:
      *.prod*|*.production* -> prod
      otherwise             -> dev
  - Requires: the si binary on PATH."#;

pub fn infer_target_env(base: &str) -> &'static str {
    let lower = base.to_ascii_lowercase();
    if lower.contains(".prod") || lower.contains("production") {
        "prod"
    } else {
        "dev"
    }
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
}
