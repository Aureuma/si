use std::env;
use std::process::{Command, ExitCode};

use si_agents::repo_root;

fn normalize_cargo_http_timeout(raw: &str) -> Result<String, String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err("timeout must not be empty".to_owned());
    }
    if trimmed.bytes().all(|b| b.is_ascii_digit()) {
        return Ok(trimmed.to_owned());
    }
    let (number, unit) = trimmed.split_at(trimmed.len().saturating_sub(1));
    let multiplier = match unit {
        "s" => 1_u64,
        "m" => 60_u64,
        "h" => 3600_u64,
        _ => return Err(format!("unsupported timeout unit in {:?}", raw)),
    };
    let seconds = number
        .parse::<u64>()
        .map_err(|err| format!("invalid timeout {:?}: {}", raw, err))?
        .checked_mul(multiplier)
        .ok_or_else(|| format!("timeout {:?} is too large", raw))?;
    Ok(seconds.to_string())
}

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    let cargo_timeout = match normalize_cargo_http_timeout(
        &env::var("SI_CODEX_UPGRADE_CARGO_TEST_TIMEOUT").unwrap_or_else(|_| "15m".to_string()),
    ) {
        Ok(value) => value,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };
    let test_groups = [
        ("si-rs-codex", "[preflight] cargo test -p si-rs-codex"),
        ("si-tools", "[preflight] cargo test -p si-tools"),
    ];

    println!("[preflight] codex upgrade compatibility checks");
    for (pkg, label) in test_groups {
        println!("{label}");
        let status = Command::new("cargo")
            .current_dir(&root)
            .args(["test", "-p", pkg])
            .env("CARGO_TERM_COLOR", "always")
            .env("CARGO_BUILD_JOBS", "1")
            .env("CARGO_NET_RETRY", "2")
            .env("CARGO_HTTP_TIMEOUT", cargo_timeout.as_str())
            .status();
        match status {
            Ok(status) if status.success() => {}
            Ok(status) => return ExitCode::from(status.code().unwrap_or(1) as u8),
            Err(err) => {
                eprintln!("{err}");
                return ExitCode::from(1);
            }
        }
    }

    println!("[preflight] codex upgrade compatibility checks passed");
    ExitCode::SUCCESS
}

#[cfg(test)]
mod tests {
    use super::normalize_cargo_http_timeout;

    #[test]
    fn accepts_integer_seconds() {
        assert_eq!(normalize_cargo_http_timeout("900").expect("seconds"), "900");
    }

    #[test]
    fn converts_minute_suffix_to_seconds() {
        assert_eq!(normalize_cargo_http_timeout("15m").expect("minutes"), "900");
    }

    #[test]
    fn converts_hour_suffix_to_seconds() {
        assert_eq!(normalize_cargo_http_timeout("2h").expect("hours"), "7200");
    }
}
