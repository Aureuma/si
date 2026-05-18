use std::env;
use std::path::PathBuf;
use std::process::{Command, ExitCode};

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    let checks: &[&[&str]] = &[
        &["test", "-p", "si-rs-fort"],
        &[
            "test",
            "-p",
            "si-rs-codex",
            "tests::codex_fort_agent_id_is_slot_aware",
            "--",
            "--exact",
        ],
        &[
            "test",
            "-p",
            "si-rs-cli",
            "codex_repair_auth_all_provisions_slot_specific_agents_with_30d_ttl",
            "--",
            "--exact",
        ],
        &[
            "test",
            "-p",
            "si-rs-cli",
            "codex_profile_resolution_forms_are_consistent_across_lifecycle_commands",
            "--",
            "--exact",
        ],
        &[
            "test",
            "-p",
            "si-rs-cli",
            "codex_lifecycle_commands_reject_dual_profile_forms_and_shell_legacy_positional_profile",
            "--",
            "--exact",
        ],
        &[
            "test",
            "-p",
            "si-rs-cli",
            "fort_wrapper_rejects_profile_refresh_token_rotation_to_noncanonical_path",
            "--",
            "--exact",
        ],
        &[
            "test",
            "-p",
            "si-rs-cli",
            "fort_wrapper_rejects_nonprimary_profile_refresh_token_rotation_to_noncanonical_path",
            "--",
            "--exact",
        ],
    ];

    for args in checks {
        println!("[fort-spawn-matrix] cargo {}", args.join(" "));
        match Command::new("cargo").current_dir(&root).args(*args).status() {
            Ok(status) if status.success() => {}
            Ok(status) => return ExitCode::from(status.code().unwrap_or(1) as u8),
            Err(err) => {
                eprintln!("{err}");
                return ExitCode::from(1);
            }
        }
    }
    ExitCode::SUCCESS
}

fn repo_root() -> Result<PathBuf, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("Cargo.toml").is_file() {
        Ok(cwd)
    } else {
        Err("repo root not found; run from the si workspace root".to_string())
    }
}
