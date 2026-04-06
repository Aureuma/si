use anyhow::{Context, Result, anyhow, bail};
use base64::{Engine as _, engine::general_purpose::STANDARD as BASE64_STANDARD};
use clap::{ArgAction, Args, Subcommand};
use ecies::{decrypt as ecies_decrypt, encrypt as ecies_encrypt};
use serde::{Deserialize, Serialize};
use si_rs_config::runtime::git_repo_root_from;
use std::collections::{BTreeMap, HashSet};
use std::env;
use std::fs;
use std::io::{self, Read};
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Command as StdCommand;

use crate::{OutputFormat, show_vault_trust_lookup};

const SI_VAULT_PUBLIC_KEY: &str = "SI_VAULT_PUBLIC_KEY";
const SI_VAULT_ENCRYPTED_PREFIX: &str = "encrypted:si-vault:";
const LEGACY_ENCRYPTED_PREFIX: &str = "encrypted:";

#[derive(Debug, Subcommand)]
pub(crate) enum VaultCommand {
    #[command(name = "keypair", alias = "keygen")]
    Keypair {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long, action = ArgAction::SetTrue)]
        rotate: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Status {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Check {
        #[arg(long = "file")]
        file: Option<PathBuf>,
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
        #[arg(long, action = ArgAction::SetTrue)]
        staged: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        all: bool,
        #[arg(long = "include-examples", action = ArgAction::SetTrue)]
        include_examples: bool,
    },
    Hooks {
        #[command(subcommand)]
        command: VaultHooksCommand,
    },
    Encrypt {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long, action = ArgAction::SetTrue)]
        stdout: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        reencrypt: bool,
    },
    Decrypt {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long, action = ArgAction::SetTrue)]
        stdout: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        inplace: bool,
        #[arg(long = "in-place", action = ArgAction::SetTrue)]
        in_place: bool,
    },
    Restore {
        #[arg(long = "env-file")]
        env_file: Option<PathBuf>,
        #[arg(long = "file")]
        file: Option<PathBuf>,
    },
    Set {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long, hide = true)]
        section: Option<String>,
        #[arg(long, hide = true, action = ArgAction::SetTrue)]
        format: bool,
        key: String,
        value: Option<String>,
        #[arg(long, action = ArgAction::SetTrue)]
        stdin: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        plain: bool,
    },
    Unset {
        #[command(flatten)]
        target: VaultTargetArgs,
        key: String,
    },
    Get {
        #[command(flatten)]
        target: VaultTargetArgs,
        key: String,
        #[arg(long, action = ArgAction::SetTrue)]
        reveal: bool,
    },
    #[command(name = "list", alias = "ls")]
    List {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Run {
        #[command(flatten)]
        target: VaultTargetArgs,
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
        #[arg(long = "allow-plaintext", action = ArgAction::SetTrue)]
        allow_plaintext: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        shell: bool,
        #[arg(long = "shell-interactive", action = ArgAction::SetTrue)]
        shell_interactive: bool,
        #[arg(long = "shell-path")]
        shell_path: Option<PathBuf>,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
    },
    Trust {
        #[command(subcommand)]
        command: VaultTrustCommand,
    },
}

#[derive(Debug, Subcommand)]
pub(crate) enum VaultHooksCommand {
    Install {
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
        #[arg(long, action = ArgAction::SetTrue)]
        force: bool,
    },
    Status {
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
    },
    #[command(alias = "remove", alias = "rm")]
    Uninstall {
        #[arg(long = "vault-dir")]
        vault_dir: Option<PathBuf>,
    },
}

#[derive(Debug, Subcommand)]
pub(crate) enum VaultTrustCommand {
    Lookup {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        repo_root: String,
        #[arg(long)]
        file: String,
        #[arg(long)]
        fingerprint: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Clone, Args)]
pub(crate) struct VaultTargetArgs {
    #[arg(long = "env-file")]
    env_file: Option<PathBuf>,
    #[arg(long = "file")]
    file: Option<PathBuf>,
    #[arg(long)]
    repo: Option<String>,
    #[arg(long)]
    env: Option<String>,
    #[arg(long)]
    scope: Option<String>,
}

#[derive(Clone, Debug)]
struct VaultTarget {
    repo_root: PathBuf,
    repo: String,
    env: String,
    env_file: PathBuf,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
struct KeyringDoc {
    #[serde(default)]
    entries: BTreeMap<String, KeyringEntry>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
struct KeyringEntry {
    #[serde(default)]
    repo: String,
    #[serde(default)]
    env: String,
    #[serde(default)]
    public_key: String,
    #[serde(default)]
    private_key: String,
    #[serde(default)]
    backup_private_keys: Vec<String>,
}

#[derive(Clone, Debug)]
struct DotenvLine {
    text: String,
    nl: String,
}

#[derive(Clone, Debug)]
struct DotenvDoc {
    lines: Vec<DotenvLine>,
    default_nl: String,
}

impl Default for DotenvDoc {
    fn default() -> Self {
        Self { lines: Vec::new(), default_nl: "\n".to_owned() }
    }
}

#[derive(Clone, Debug, Serialize)]
struct VaultKeypairView {
    repo: String,
    env: String,
    env_file: String,
    keyring: String,
    public_key_name: String,
    public_key: String,
    backup_key_count: usize,
}

#[derive(Clone, Debug, Serialize)]
struct VaultStatusView {
    file: String,
    repo_root: String,
    repo: String,
    env: String,
    keyring: String,
    file_exists: bool,
    public_key_header: bool,
    encrypted_keys: usize,
    plaintext_keys: usize,
    empty_keys: usize,
    keypair_present: bool,
    keypair_scope_entry: bool,
    decrypt_ready: bool,
}

#[derive(Clone, Debug, Serialize)]
struct VaultListEntryView {
    key: String,
    state: String,
}

#[derive(Clone, Debug)]
struct DotenvEntry {
    key: String,
    value_raw: String,
}

#[derive(Clone, Debug, Default)]
struct DotenvScan {
    encrypted_keys: Vec<String>,
    plaintext_keys: Vec<String>,
    empty_keys: Vec<String>,
    public_key_header: bool,
}

#[derive(Clone, Debug)]
struct DecryptedEnv {
    values: BTreeMap<String, String>,
    plaintext_keys: Vec<String>,
}

#[derive(Default)]
struct EncryptStats {
    encrypted_keys: usize,
    reencrypted_keys: usize,
    skipped_encrypted: usize,
}

pub(crate) fn run_vault_command(command: VaultCommand) -> Result<()> {
    match command {
        VaultCommand::Keypair { target, rotate, format } => run_vault_keypair(target, rotate, format),
        VaultCommand::Status { target, vault_dir, format } => {
            run_vault_status(target, vault_dir, format)
        }
        VaultCommand::Check { file, vault_dir, staged, all, include_examples } => {
            run_vault_check(file, vault_dir, staged, all, include_examples)
        }
        VaultCommand::Hooks { command } => run_vault_hooks(command),
        VaultCommand::Encrypt { target, stdout, reencrypt } => {
            run_vault_encrypt(target, stdout, reencrypt)
        }
        VaultCommand::Decrypt { target, stdout, inplace, in_place } => {
            run_vault_decrypt(target, stdout, inplace || in_place)
        }
        VaultCommand::Restore { env_file, file } => run_vault_restore(env_file.or(file)),
        VaultCommand::Set { target, section: _, format: _, key, value, stdin, plain } => {
            run_vault_set(target, key, value, stdin, plain)
        }
        VaultCommand::Unset { target, key } => run_vault_unset(target, key),
        VaultCommand::Get { target, key, reveal } => run_vault_get(target, key, reveal),
        VaultCommand::List { target, vault_dir, format } => run_vault_list(target, vault_dir, format),
        VaultCommand::Run {
            target,
            vault_dir,
            allow_plaintext,
            shell,
            shell_interactive,
            shell_path,
            args,
        } => run_vault_run(
            target,
            vault_dir,
            allow_plaintext,
            shell,
            shell_interactive,
            shell_path,
            args,
        ),
        VaultCommand::Trust { command } => match command {
            VaultTrustCommand::Lookup { path, repo_root, file, fingerprint, format } => {
                show_vault_trust_lookup(path, &repo_root, &file, &fingerprint, format)
            }
        },
    }
}

fn run_vault_keypair(target: VaultTargetArgs, rotate: bool, format: OutputFormat) -> Result<()> {
    let target = resolve_vault_target(&target, None)?;
    let keyring_path = resolve_keyring_path();
    let entry = ensure_keyring_entry(&keyring_path, &target, rotate)?;
    let view = VaultKeypairView {
        repo: target.repo.clone(),
        env: target.env.clone(),
        env_file: target.env_file.display().to_string(),
        keyring: keyring_path.display().to_string(),
        public_key_name: SI_VAULT_PUBLIC_KEY.to_owned(),
        public_key: entry.public_key.clone(),
        backup_key_count: entry.backup_private_keys.len(),
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("repo={}", view.repo);
            println!("env={}", view.env);
            println!("env_file={}", view.env_file);
            println!("keyring={}", view.keyring);
            println!("public_key_name={}", view.public_key_name);
            println!("public_key={}", view.public_key);
            println!("backup_key_count={}", view.backup_key_count);
        }
    }
    Ok(())
}

fn run_vault_status(
    target: VaultTargetArgs,
    vault_dir: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let target = resolve_vault_target(&target, vault_dir)?;
    let keyring_path = resolve_keyring_path();
    let file_exists = target.env_file.exists();
    let scan = if file_exists {
        DotenvDoc::read(&target.env_file)?.scan()?
    } else {
        DotenvScan::default()
    };
    let keyring_doc = load_keyring_doc(&keyring_path)?;
    let view = VaultStatusView {
        file: target.env_file.display().to_string(),
        repo_root: target.repo_root.display().to_string(),
        repo: target.repo.clone(),
        env: target.env.clone(),
        keyring: keyring_path.display().to_string(),
        file_exists,
        public_key_header: scan.public_key_header,
        encrypted_keys: scan.encrypted_keys.len(),
        plaintext_keys: scan.plaintext_keys.len(),
        empty_keys: scan.empty_keys.len(),
        keypair_present: canonical_keypair(&keyring_doc)?.is_some(),
        keypair_scope_entry: find_keyring_entry(&keyring_doc, &target).is_some(),
        decrypt_ready: keyring_present_for_target(&keyring_doc, &target),
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("file={}", view.file);
            println!("repo_root={}", view.repo_root);
            println!("repo={}", view.repo);
            println!("env={}", view.env);
            println!("keyring={}", view.keyring);
            println!("file_exists={}", view.file_exists);
            println!("public_key_header={}", view.public_key_header);
            println!("encrypted_keys={}", view.encrypted_keys);
            println!("plaintext_keys={}", view.plaintext_keys);
            println!("empty_keys={}", view.empty_keys);
            println!("keypair_present={}", view.keypair_present);
            println!("keypair_scope_entry={}", view.keypair_scope_entry);
            println!("decrypt_ready={}", view.decrypt_ready);
        }
    }
    Ok(())
}

fn run_vault_check(
    file: Option<PathBuf>,
    vault_dir: Option<PathBuf>,
    staged: bool,
    all: bool,
    include_examples: bool,
) -> Result<()> {
    let cwd = env::current_dir().context("read current dir")?;
    let repo_root = git_repo_root_from(&cwd).unwrap_or_else(|_| cwd.clone());
    let scan_root = absolutize_path(vault_dir.as_deref().unwrap_or(repo_root.as_path()), &cwd);
    let explicit_file = file.as_deref().map(|path| absolutize_path(path, &cwd));

    let files = if staged {
        if all {
            staged_dotenv_files(&repo_root, &scan_root, include_examples)?
        } else {
            explicit_file.into_iter().collect()
        }
    } else if all {
        discover_dotenv_files(&scan_root, include_examples)?
    } else {
        vec![explicit_file.unwrap_or_else(|| scan_root.join(".env"))]
    };

    let mut findings = Vec::new();
    for path in files {
        let display = if staged {
            match path.strip_prefix(&repo_root) {
                Ok(rel) => rel.display().to_string(),
                Err(_) => path.display().to_string(),
            }
        } else {
            path.display().to_string()
        };
        let doc = if staged {
            let rel = path
                .strip_prefix(&repo_root)
                .with_context(|| format!("resolve staged path {}", path.display()))?;
            match git_show_index_file(&repo_root, rel) {
                Ok(bytes) => DotenvDoc::parse(&bytes),
                Err(_) => continue,
            }
        } else if path.exists() {
            DotenvDoc::read(&path)?
        } else {
            continue;
        };
        let scan = doc.scan()?;
        if !scan.plaintext_keys.is_empty() {
            findings.push((display, scan.plaintext_keys));
        }
    }

    if findings.is_empty() {
        return Ok(());
    }

    let mut message = String::from("[si vault] plaintext values detected; encrypt before committing.\n");
    for (file, keys) in findings {
        message.push_str("  - ");
        message.push_str(&file);
        message.push_str(": ");
        message.push_str(&keys.join(", "));
        message.push('\n');
    }
    message.push_str("\nFix:\n");
    message.push_str("  si vault encrypt --env-file <path>\n");
    message.push_str("\nBypass (not recommended): git commit --no-verify\n");
    bail!("{message}");
}

fn run_vault_hooks(command: VaultHooksCommand) -> Result<()> {
    match command {
        VaultHooksCommand::Install { vault_dir, force } => run_vault_hooks_install(vault_dir, force),
        VaultHooksCommand::Status { vault_dir } => run_vault_hooks_status(vault_dir),
        VaultHooksCommand::Uninstall { vault_dir } => run_vault_hooks_uninstall(vault_dir),
    }
}

fn run_vault_hooks_install(vault_dir: Option<PathBuf>, force: bool) -> Result<()> {
    let repo_root = resolve_hook_repo_root(vault_dir)?;
    let hooks_dir = git_hooks_dir(&repo_root)?;
    fs::create_dir_all(&hooks_dir).with_context(|| format!("create {}", hooks_dir.display()))?;
    let hook_path = hooks_dir.join("pre-commit");
    if hook_path.exists() {
        let existing = fs::read(&hook_path).with_context(|| format!("read {}", hook_path.display()))?;
        if !is_managed_hook(&existing) && !force {
            bail!("hook already exists (use --force): {}", hook_path.display());
        }
    }
    write_executable_file(&hook_path, render_vault_pre_commit_hook().as_bytes())?;
    println!("installed: {}", hook_path.display());
    Ok(())
}

fn run_vault_hooks_status(vault_dir: Option<PathBuf>) -> Result<()> {
    let repo_root = resolve_hook_repo_root(vault_dir)?;
    let hook_path = git_hooks_dir(&repo_root)?.join("pre-commit");
    match fs::read(&hook_path) {
        Ok(bytes) => {
            if is_managed_hook(&bytes) {
                println!("pre-commit: installed ({})", hook_path.display());
            } else {
                println!("pre-commit: present (not managed by si) ({})", hook_path.display());
            }
        }
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            println!("pre-commit: missing ({})", hook_path.display());
        }
        Err(err) => return Err(err).with_context(|| format!("read {}", hook_path.display())),
    }
    Ok(())
}

fn run_vault_hooks_uninstall(vault_dir: Option<PathBuf>) -> Result<()> {
    let repo_root = resolve_hook_repo_root(vault_dir)?;
    let hook_path = git_hooks_dir(&repo_root)?.join("pre-commit");
    let bytes = match fs::read(&hook_path) {
        Ok(bytes) => bytes,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(()),
        Err(err) => return Err(err).with_context(|| format!("read {}", hook_path.display())),
    };
    if !is_managed_hook(&bytes) {
        bail!("refusing to remove non-si hook: {}", hook_path.display());
    }
    fs::remove_file(&hook_path).with_context(|| format!("remove {}", hook_path.display()))?;
    println!("removed: {}", hook_path.display());
    Ok(())
}

fn run_vault_encrypt(target: VaultTargetArgs, stdout: bool, reencrypt: bool) -> Result<()> {
    let target = resolve_vault_target(&target, None)?;
    let keyring_path = resolve_keyring_path();
    let entry = ensure_keyring_entry(&keyring_path, &target, false)?;
    let mut doc = read_or_empty(&target.env_file)?;
    doc.ensure_public_key(&entry.public_key);
    let stats = doc.encrypt(&entry.public_key, &private_key_candidates(&entry), reencrypt)?;
    if stdout {
        print!("{}", String::from_utf8_lossy(&doc.bytes()));
    } else {
        write_dotenv_atomic(&target.env_file, &doc.bytes())?;
        println!("file={}", target.env_file.display());
        println!("repo={}", target.repo);
        println!("env={}", target.env);
        println!("encrypted={}", stats.encrypted_keys);
        println!("reencrypted={}", stats.reencrypted_keys);
        println!("skipped_encrypted={}", stats.skipped_encrypted);
    }
    Ok(())
}

fn run_vault_decrypt(target: VaultTargetArgs, stdout: bool, inplace: bool) -> Result<()> {
    let target = resolve_vault_target(&target, None)?;
    let keyring_path = resolve_keyring_path();
    let entry = load_keyring_entry_or_canonical(&keyring_path, &target)?;
    let mut doc = DotenvDoc::read(&target.env_file)?;
    let original = fs::read(&target.env_file).with_context(|| format!("read {}", target.env_file.display()))?;
    let decrypted = doc.decrypt(&private_key_candidates(&entry))?;
    let print_stdout = stdout || !inplace;
    if inplace {
        save_restore_backup(&target.env_file, &original)?;
        write_dotenv_atomic(&target.env_file, &doc.bytes())?;
        println!("file={}", target.env_file.display());
        println!("repo={}", target.repo);
        println!("env={}", target.env);
        println!("decrypted={}", decrypted);
        println!("backup={}", restore_backup_path(&target.env_file).display());
    }
    if print_stdout {
        print!("{}", String::from_utf8_lossy(&doc.bytes()));
    }
    Ok(())
}

fn run_vault_restore(path: Option<PathBuf>) -> Result<()> {
    let cwd = env::current_dir().context("read current dir")?;
    let env_file = absolutize_path(path.as_deref().unwrap_or(Path::new(".env")), &cwd);
    let backup_path = restore_backup_path(&env_file);
    let bytes = fs::read(&backup_path).with_context(|| format!("read {}", backup_path.display()))?;
    write_dotenv_atomic(&env_file, &bytes)?;
    let _ = fs::remove_file(&backup_path);
    println!("restored: {}", env_file.display());
    Ok(())
}

fn run_vault_set(
    target: VaultTargetArgs,
    key: String,
    value: Option<String>,
    stdin: bool,
    plain: bool,
) -> Result<()> {
    validate_key_name(&key)?;
    let target = resolve_vault_target(&target, None)?;
    let mut value = if stdin {
        let mut raw = String::new();
        io::stdin().read_to_string(&mut raw).context("read stdin")?;
        raw.trim_end_matches(['\n', '\r']).to_owned()
    } else {
        value.ok_or_else(|| anyhow!("value required (or use --stdin)"))?
    };
    let mut doc = read_or_empty(&target.env_file)?;
    if !plain && key.trim() != SI_VAULT_PUBLIC_KEY {
        let keyring_path = resolve_keyring_path();
        let entry = ensure_keyring_entry(&keyring_path, &target, false)?;
        doc.ensure_public_key(&entry.public_key);
        value = encrypt_value(&value, &entry.public_key)?;
        doc.set(&key, value)?;
    } else {
        doc.set(&key, render_dotenv_plain(&value)?)?;
    }
    write_dotenv_atomic(&target.env_file, &doc.bytes())?;
    println!("file={}", target.env_file.display());
    println!("repo={}", target.repo);
    println!("env={}", target.env);
    println!("set={} ({})", key.trim(), if plain { "plaintext" } else { "encrypted" });
    Ok(())
}

fn run_vault_unset(target: VaultTargetArgs, key: String) -> Result<()> {
    validate_key_name(&key)?;
    let target = resolve_vault_target(&target, None)?;
    let mut doc = DotenvDoc::read(&target.env_file)?;
    if !doc.unset(&key) {
        println!("key not found: {}", key.trim());
        return Ok(());
    }
    write_dotenv_atomic(&target.env_file, &doc.bytes())?;
    println!("file={}", target.env_file.display());
    println!("unset={}", key.trim());
    Ok(())
}

fn run_vault_get(target: VaultTargetArgs, key: String, reveal: bool) -> Result<()> {
    validate_key_name(&key)?;
    let target = resolve_vault_target(&target, None)?;
    let doc = DotenvDoc::read(&target.env_file)?;
    let value = doc.lookup(&key).ok_or_else(|| anyhow!("key not found: {}", key.trim()))?;
    if !reveal {
        println!(
            "{}: {}",
            key.trim(),
            if is_encrypted_value(&value) { "encrypted" } else { "plaintext" }
        );
        return Ok(());
    }
    if !is_encrypted_value(&value) {
        println!("{value}");
        return Ok(());
    }
    let keyring_path = resolve_keyring_path();
    let entry = load_keyring_entry_or_canonical(&keyring_path, &target)?;
    println!("{}", decrypt_value(&value, &private_key_candidates(&entry))?);
    Ok(())
}

fn run_vault_list(
    target: VaultTargetArgs,
    vault_dir: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let target = resolve_vault_target(&target, vault_dir)?;
    let doc = DotenvDoc::read(&target.env_file)?;
    let items = doc
        .entries()?
        .into_iter()
        .filter(|entry| entry.key != SI_VAULT_PUBLIC_KEY)
        .map(|entry| VaultListEntryView {
            key: entry.key,
            state: if entry.value_raw.is_empty() {
                "empty".to_owned()
            } else if is_encrypted_value(&entry.value_raw) {
                "encrypted".to_owned()
            } else {
                "plaintext".to_owned()
            },
        })
        .collect::<Vec<_>>();
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&items)?),
        OutputFormat::Text => {
            println!("file: {}", target.env_file.display());
            for item in items {
                println!("{}\t({})", item.key, item.state);
            }
        }
    }
    Ok(())
}

fn run_vault_run(
    target: VaultTargetArgs,
    vault_dir: Option<PathBuf>,
    allow_plaintext: bool,
    shell: bool,
    shell_interactive: bool,
    shell_path: Option<PathBuf>,
    args: Vec<String>,
) -> Result<()> {
    let target = resolve_vault_target(&target, vault_dir)?;
    let args = normalize_trailing_args(args);
    if args.is_empty() {
        bail!(
            "usage: si vault run [--env-file <path>] [--allow-plaintext] [--shell] [--shell-interactive] [--shell-path <path>] -- <cmd...>"
        );
    }
    let doc = DotenvDoc::read(&target.env_file)?;
    let keyring_path = resolve_keyring_path();
    let entry = load_keyring_entry_or_canonical(&keyring_path, &target)?;
    let decrypted = doc.decrypt_values(&private_key_candidates(&entry))?;
    if !allow_plaintext && !decrypted.plaintext_keys.is_empty() {
        bail!(
            "vault file contains plaintext keys: {} (run `si vault encrypt` or pass --allow-plaintext)",
            decrypted.plaintext_keys.join(", ")
        );
    }

    let mut command = if shell {
        let shell_path = shell_path
            .or_else(|| env::var_os("SHELL").map(PathBuf::from))
            .unwrap_or_else(|| PathBuf::from("/bin/bash"));
        let mut command = StdCommand::new(&shell_path);
        command.arg(if shell_interactive { "-ic" } else { "-lc" });
        command.arg(args.join(" "));
        command
    } else {
        let mut command = StdCommand::new(&args[0]);
        command.args(&args[1..]);
        command
    };
    command.current_dir(&target.repo_root);
    command.envs(decrypted.values);
    command.stdin(std::process::Stdio::inherit());
    command.stdout(std::process::Stdio::inherit());
    command.stderr(std::process::Stdio::inherit());
    let status = command.status().context("run vault command")?;
    if !status.success() {
        bail!("vault command exited with {}", status);
    }
    Ok(())
}

fn resolve_vault_target(target: &VaultTargetArgs, vault_dir: Option<PathBuf>) -> Result<VaultTarget> {
    let cwd = env::current_dir().context("read current dir")?;
    let explicit_env_name = target
        .env
        .as_deref()
        .or(target.scope.as_deref())
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(normalize_env_name);
    let env_file = target
        .env_file
        .as_deref()
        .or(target.file.as_deref())
        .map(|path| absolutize_path(path, &cwd))
        .unwrap_or_else(|| {
            let base = vault_dir
                .as_deref()
                .map(|path| absolutize_path(path, &cwd))
                .unwrap_or_else(|| cwd.clone());
            match explicit_env_name.as_deref() {
                Some("dev") => base.join(".env.dev"),
                Some("prod") => base.join(".env.prod"),
                _ => base.join(".env"),
            }
        });
    let repo_root = env_file
        .parent()
        .and_then(|path| git_repo_root_from(path).ok())
        .unwrap_or_else(|| {
            env_file
                .parent()
                .map(Path::to_path_buf)
                .unwrap_or_else(|| cwd.clone())
        });
    let repo = target
        .repo
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned)
        .or_else(|| repo_root.file_name().map(|value| value.to_string_lossy().trim().to_string()))
        .filter(|value| !value.is_empty())
        .ok_or_else(|| anyhow!("unable to determine vault repo name"))?;
    let env_name =
        explicit_env_name.unwrap_or_else(|| infer_env_name(&env_file).unwrap_or_else(|| "dev".to_owned()));
    Ok(VaultTarget { repo_root, repo, env: env_name, env_file })
}

fn resolve_keyring_path() -> PathBuf {
    env::var_os("SI_VAULT_KEYRING_FILE")
        .filter(|value| !value.is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(|| {
            super::default_home_dir().join(".si").join("vault").join("si-vault-keyring.json")
        })
}

fn ensure_keyring_entry(path: &Path, target: &VaultTarget, rotate: bool) -> Result<KeyringEntry> {
    let mut doc = load_keyring_doc(path)?;
    let canonical_before = canonical_keypair(&doc)?;
    let map_key = keyring_scope_key(target);
    let mut changed = false;

    let (public_key, private_key) = if rotate || canonical_before.is_none() {
        let (public_key, private_key) = generate_keypair();
        if let Some((_, old_private)) = canonical_before.clone() {
            for entry in doc.entries.values_mut() {
                let normalized_old = normalize_hex(&old_private);
                if !normalized_old.is_empty()
                    && !entry
                        .backup_private_keys
                        .iter()
                        .any(|candidate| normalize_hex(candidate) == normalized_old)
                {
                    entry.backup_private_keys.push(old_private.clone());
                }
                entry.public_key = public_key.clone();
                entry.private_key = private_key.clone();
            }
        }
        changed = true;
        (public_key, private_key)
    } else {
        canonical_before.unwrap_or_default()
    };

    let result_entry = {
        let entry = doc.entries.entry(map_key).or_insert_with(|| KeyringEntry {
            repo: target.repo.clone(),
            env: target.env.clone(),
            public_key: public_key.clone(),
            private_key: private_key.clone(),
            backup_private_keys: Vec::new(),
        });
        if entry.repo.trim().is_empty() {
            entry.repo = target.repo.clone();
            changed = true;
        }
        if entry.env.trim().is_empty() {
            entry.env = target.env.clone();
            changed = true;
        }
        if normalize_hex(&entry.public_key) != normalize_hex(&public_key) {
            entry.public_key = public_key.clone();
            changed = true;
        }
        if normalize_hex(&entry.private_key) != normalize_hex(&private_key) {
            if !entry.private_key.trim().is_empty()
                && !entry
                    .backup_private_keys
                    .iter()
                    .any(|candidate| normalize_hex(candidate) == normalize_hex(&entry.private_key))
            {
                entry.backup_private_keys.push(entry.private_key.clone());
            }
            entry.private_key = private_key.clone();
            changed = true;
        }
        entry.clone()
    };
    if changed {
        save_keyring_doc(path, &doc)?;
    }
    Ok(result_entry)
}

fn load_keyring_entry_or_canonical(path: &Path, target: &VaultTarget) -> Result<KeyringEntry> {
    let doc = load_keyring_doc(path)?;
    if let Some(entry) = find_keyring_entry(&doc, target) {
        return Ok(entry);
    }
    if let Some((public_key, private_key)) = canonical_keypair(&doc)? {
        return Ok(KeyringEntry {
            repo: target.repo.clone(),
            env: target.env.clone(),
            public_key,
            private_key,
            backup_private_keys: Vec::new(),
        });
    }
    bail!("vault keypair not found for {}/{}", target.repo, target.env);
}

fn keyring_present_for_target(doc: &KeyringDoc, target: &VaultTarget) -> bool {
    find_keyring_entry(doc, target).is_some() || canonical_keypair(doc).ok().flatten().is_some()
}

fn find_keyring_entry(doc: &KeyringDoc, target: &VaultTarget) -> Option<KeyringEntry> {
    let key = keyring_scope_key(target);
    if let Some(entry) = doc.entries.get(&key) {
        return Some(entry.clone());
    }
    doc.entries.values().find(|entry| {
        entry.repo.eq_ignore_ascii_case(&target.repo) && entry.env.eq_ignore_ascii_case(&target.env)
    }).cloned()
}

fn canonical_keypair(doc: &KeyringDoc) -> Result<Option<(String, String)>> {
    let mut expected: Option<(String, String)> = None;
    for entry in doc.entries.values() {
        let public_key = normalize_hex(&entry.public_key);
        let private_key = normalize_hex(&entry.private_key);
        if public_key.is_empty() && private_key.is_empty() {
            continue;
        }
        if public_key.is_empty() || private_key.is_empty() {
            bail!("vault keyring is invalid");
        }
        match &expected {
            Some((expected_public, expected_private))
                if expected_public != &public_key || expected_private != &private_key =>
            {
                bail!("vault keyring violates single-key-material invariant")
            }
            None => expected = Some((public_key, private_key)),
            _ => {}
        }
    }
    Ok(expected)
}

fn load_keyring_doc(path: &Path) -> Result<KeyringDoc> {
    match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes).with_context(|| format!("parse {}", path.display())),
        Err(err) if err.kind() == io::ErrorKind::NotFound => Ok(KeyringDoc::default()),
        Err(err) => Err(err).with_context(|| format!("read {}", path.display())),
    }
}

fn save_keyring_doc(path: &Path, doc: &KeyringDoc) -> Result<()> {
    let mut bytes = serde_json::to_vec_pretty(doc)?;
    bytes.push(b'\n');
    write_secret_file(path, &bytes)
}

fn generate_keypair() -> (String, String) {
    let (secret, public) = ecies::utils::generate_keypair();
    (hex::encode(public.serialize_compressed()), hex::encode(secret.serialize()))
}

fn private_key_candidates(entry: &KeyringEntry) -> Vec<String> {
    let mut out = Vec::new();
    for candidate in std::iter::once(&entry.private_key).chain(entry.backup_private_keys.iter()) {
        let candidate = normalize_hex(candidate);
        if !candidate.is_empty() && !out.contains(&candidate) {
            out.push(candidate);
        }
    }
    out
}

fn keyring_scope_key(target: &VaultTarget) -> String {
    format!("{}/{}", target.repo.to_ascii_lowercase(), target.env.to_ascii_lowercase())
}

fn normalize_hex(value: &str) -> String {
    value.trim().to_ascii_lowercase()
}

fn normalize_env_name(value: &str) -> String {
    match value.trim().to_ascii_lowercase().as_str() {
        "production" => "prod".to_owned(),
        "development" => "dev".to_owned(),
        other => other.to_owned(),
    }
}

fn infer_env_name(path: &Path) -> Option<String> {
    let file_name = path.file_name()?.to_string_lossy().to_ascii_lowercase();
    match file_name.as_str() {
        ".env.dev" | ".env.development" => Some("dev".to_owned()),
        ".env.prod" | ".env.production" => Some("prod".to_owned()),
        _ => None,
    }
}

impl DotenvDoc {
    fn read(path: &Path) -> Result<Self> {
        let bytes = fs::read(path).with_context(|| format!("read {}", path.display()))?;
        Ok(Self::parse(&bytes))
    }

    fn parse(bytes: &[u8]) -> Self {
        if bytes.is_empty() {
            return Self::default();
        }
        let mut lines = Vec::new();
        let mut start = 0;
        let mut default_nl = "\n".to_owned();
        while start < bytes.len() {
            if let Some(relative_end) = bytes[start..].iter().position(|byte| *byte == b'\n') {
                let end = start + relative_end;
                let mut line = &bytes[start..end];
                let nl = if line.ends_with(b"\r") {
                    line = &line[..line.len() - 1];
                    "\r\n"
                } else {
                    "\n"
                };
                if default_nl == "\n" {
                    default_nl = nl.to_owned();
                }
                lines.push(DotenvLine {
                    text: String::from_utf8_lossy(line).into_owned(),
                    nl: nl.to_owned(),
                });
                start = end + 1;
            } else {
                lines.push(DotenvLine {
                    text: String::from_utf8_lossy(&bytes[start..]).into_owned(),
                    nl: String::new(),
                });
                break;
            }
        }
        Self { lines, default_nl }
    }

    fn bytes(&self) -> Vec<u8> {
        let mut out = Vec::new();
        for line in &self.lines {
            out.extend_from_slice(line.text.as_bytes());
            out.extend_from_slice(line.nl.as_bytes());
        }
        out
    }

    fn ensure_public_key(&mut self, public_key: &str) {
        let assignment = format!("{SI_VAULT_PUBLIC_KEY}={}", public_key.trim());
        let mut last_index = None;
        for (index, line) in self.lines.iter().enumerate() {
            if let Some((key, _)) = parse_assignment(&line.text)
                && key == SI_VAULT_PUBLIC_KEY
            {
                last_index = Some(index);
            }
        }
        if let Some(index) = last_index {
            self.lines[index].text = assignment;
            return;
        }
        self.lines.insert(
            0,
            DotenvLine { text: assignment, nl: self.default_nl.clone() },
        );
    }

    fn lookup(&self, key: &str) -> Option<String> {
        let key = key.trim();
        let mut value = None;
        for line in &self.lines {
            let Some((line_key, value_raw)) = parse_assignment(&line.text) else {
                continue;
            };
            if line_key == key {
                value = normalize_dotenv_value(&value_raw).ok();
            }
        }
        value
    }

    fn set(&mut self, key: &str, value_raw: String) -> Result<()> {
        validate_key_name(key)?;
        let rendered = format!("{}={}", key.trim(), value_raw);
        let mut last_index = None;
        for (index, line) in self.lines.iter().enumerate() {
            let Some((line_key, _)) = parse_assignment(&line.text) else {
                continue;
            };
            if line_key == key.trim() {
                last_index = Some(index);
            }
        }
        if let Some(index) = last_index {
            self.lines[index].text = rendered;
            return Ok(());
        }
        if let Some(line) = self.lines.last_mut()
            && line.nl.is_empty()
        {
            line.nl = self.default_nl.clone();
        }
        self.lines.push(DotenvLine { text: rendered, nl: self.default_nl.clone() });
        Ok(())
    }

    fn unset(&mut self, key: &str) -> bool {
        let key = key.trim();
        let original_len = self.lines.len();
        self.lines.retain(|line| {
            parse_assignment(&line.text).map(|(line_key, _)| line_key != key).unwrap_or(true)
        });
        self.lines.len() != original_len
    }

    fn entries(&self) -> Result<Vec<DotenvEntry>> {
        let mut order = Vec::new();
        let mut seen = HashSet::new();
        let mut entries = BTreeMap::new();
        for line in &self.lines {
            let Some((key, value_raw)) = parse_assignment(&line.text) else {
                continue;
            };
            validate_key_name(&key)?;
            let value_raw = normalize_dotenv_value(&value_raw)?;
            if seen.insert(key.clone()) {
                order.push(key.clone());
            }
            entries.insert(key.clone(), DotenvEntry { key, value_raw });
        }
        Ok(order
            .into_iter()
            .filter_map(|key| entries.remove(&key))
            .collect())
    }

    fn scan(&self) -> Result<DotenvScan> {
        let mut scan = DotenvScan::default();
        for entry in self.entries()? {
            if entry.key == SI_VAULT_PUBLIC_KEY {
                scan.public_key_header = !entry.value_raw.trim().is_empty();
                continue;
            }
            if entry.value_raw.is_empty() {
                scan.empty_keys.push(entry.key);
            } else if is_encrypted_value(&entry.value_raw) {
                scan.encrypted_keys.push(entry.key);
            } else {
                scan.plaintext_keys.push(entry.key);
            }
        }
        Ok(scan)
    }

    fn encrypt(
        &mut self,
        public_key: &str,
        private_key_hexes: &[String],
        reencrypt: bool,
    ) -> Result<EncryptStats> {
        let mut stats = EncryptStats::default();
        for line in &mut self.lines {
            let Some((key, value_raw)) = parse_assignment(&line.text) else {
                continue;
            };
            if key == SI_VAULT_PUBLIC_KEY {
                continue;
            }
            validate_key_name(&key)?;
            let value = normalize_dotenv_value(&value_raw)?;
            if value.is_empty() {
                continue;
            }
            if is_encrypted_value(&value) {
                if !reencrypt {
                    stats.skipped_encrypted += 1;
                    continue;
                }
                let plain = decrypt_value(&value, private_key_hexes)?;
                line.text = format!("{key}={}", encrypt_value(&plain, public_key)?);
                stats.reencrypted_keys += 1;
                continue;
            }
            line.text = format!("{key}={}", encrypt_value(&value, public_key)?);
            stats.encrypted_keys += 1;
        }
        Ok(stats)
    }

    fn decrypt(&mut self, private_key_hexes: &[String]) -> Result<usize> {
        let mut decrypted = 0_usize;
        for line in &mut self.lines {
            let Some((key, value_raw)) = parse_assignment(&line.text) else {
                continue;
            };
            if key == SI_VAULT_PUBLIC_KEY {
                continue;
            }
            let value = normalize_dotenv_value(&value_raw)?;
            if !is_encrypted_value(&value) {
                continue;
            }
            line.text = format!("{key}={}", render_dotenv_plain(&decrypt_value(&value, private_key_hexes)?)?);
            decrypted += 1;
        }
        Ok(decrypted)
    }

    fn decrypt_values(&self, private_key_hexes: &[String]) -> Result<DecryptedEnv> {
        let mut values = BTreeMap::new();
        let mut plaintext_keys = Vec::new();
        for entry in self.entries()? {
            if entry.key == SI_VAULT_PUBLIC_KEY {
                continue;
            }
            if is_encrypted_value(&entry.value_raw) {
                values.insert(entry.key.clone(), decrypt_value(&entry.value_raw, private_key_hexes)?);
            } else {
                values.insert(entry.key.clone(), entry.value_raw.clone());
                if !entry.value_raw.is_empty() {
                    plaintext_keys.push(entry.key);
                }
            }
        }
        plaintext_keys.sort();
        Ok(DecryptedEnv { values, plaintext_keys })
    }
}

fn parse_assignment(line: &str) -> Option<(String, String)> {
    let trimmed = line.trim_start();
    if trimmed.is_empty() || trimmed.starts_with('#') {
        return None;
    }
    let trimmed = trimmed.strip_prefix("export ").unwrap_or(trimmed);
    let (key, value) = trimmed.split_once('=')?;
    let key = key.trim();
    if key.is_empty() {
        return None;
    }
    Some((key.to_owned(), value.to_owned()))
}

fn normalize_dotenv_value(raw: &str) -> Result<String> {
    let raw = raw.trim();
    if raw.is_empty() {
        return Ok(String::new());
    }
    if raw.starts_with('\'') {
        if !raw.ends_with('\'') || raw.len() < 2 {
            bail!("invalid quoted value: missing closing single quote");
        }
        return Ok(raw[1..raw.len() - 1].to_owned());
    }
    if raw.starts_with('"') {
        if !raw.ends_with('"') || raw.len() < 2 {
            bail!("invalid quoted value: missing closing double quote");
        }
        return serde_json::from_str(raw).context("invalid quoted value");
    }
    Ok(raw.to_owned())
}

fn render_dotenv_plain(value: &str) -> Result<String> {
    if value.is_empty() {
        return Ok(String::new());
    }
    if needs_dotenv_quotes(value) {
        return serde_json::to_string(value).context("render dotenv value");
    }
    Ok(value.to_owned())
}

fn needs_dotenv_quotes(value: &str) -> bool {
    if value.starts_with('#') || value.contains(['\n', '\r']) {
        return true;
    }
    if value.starts_with(' ')
        || value.starts_with('\t')
        || value.ends_with(' ')
        || value.ends_with('\t')
    {
        return true;
    }
    let bytes = value.as_bytes();
    for index in 1..bytes.len() {
        if bytes[index] == b'#' && (bytes[index - 1] == b' ' || bytes[index - 1] == b'\t') {
            return true;
        }
    }
    false
}

fn validate_key_name(key: &str) -> Result<()> {
    let key = key.trim();
    if key.is_empty() {
        bail!("key required");
    }
    if key.len() > 512 {
        bail!("invalid key {:?}: too long", key);
    }
    for ch in key.chars() {
        if matches!(ch, '=' | '\0' | '\n' | '\r') || ch.is_whitespace() || !ch.is_ascii_graphic() {
            bail!("invalid key {:?}: contains forbidden character", key);
        }
    }
    Ok(())
}

fn is_encrypted_value(raw: &str) -> bool {
    let raw = raw.trim();
    raw.starts_with(SI_VAULT_ENCRYPTED_PREFIX) || raw.starts_with(LEGACY_ENCRYPTED_PREFIX)
}

fn encrypt_value(plain: &str, public_key_hex: &str) -> Result<String> {
    let public_key_hex = public_key_hex.trim();
    if public_key_hex.is_empty() {
        bail!("public key is required");
    }
    if plain.is_empty() {
        return Ok(SI_VAULT_ENCRYPTED_PREFIX.to_owned());
    }
    let public = hex::decode(public_key_hex).context("decode public key")?;
    let cipher = ecies_encrypt(&public, plain.as_bytes())
        .map_err(|err| anyhow!("encrypt value: {err}"))?;
    Ok(format!("{SI_VAULT_ENCRYPTED_PREFIX}{}", BASE64_STANDARD.encode(cipher)))
}

fn decrypt_value(ciphertext: &str, private_key_hexes: &[String]) -> Result<String> {
    let ciphertext = ciphertext.trim();
    let encoded = if let Some(value) = ciphertext.strip_prefix(SI_VAULT_ENCRYPTED_PREFIX) {
        value.trim()
    } else if let Some(value) = ciphertext.strip_prefix(LEGACY_ENCRYPTED_PREFIX) {
        value.trim()
    } else {
        return Ok(ciphertext.to_owned());
    };
    if encoded.is_empty() {
        return Ok(String::new());
    }
    let blob = BASE64_STANDARD.decode(encoded).context("decode ciphertext")?;
    if private_key_hexes.is_empty() {
        bail!("private key is required");
    }
    let mut last_error = None;
    for candidate in private_key_hexes {
        let private = match hex::decode(candidate.trim()) {
            Ok(value) => value,
            Err(err) => {
                last_error = Some(anyhow!(err).context("decode private key"));
                continue;
            }
        };
        match ecies_decrypt(&private, &blob) {
            Ok(plain) => return String::from_utf8(plain).context("decode plaintext"),
            Err(err) => last_error = Some(anyhow!("decrypt value: {err}")),
        }
    }
    Err(last_error.unwrap_or_else(|| anyhow!("private key is required")))
}

fn read_or_empty(path: &Path) -> Result<DotenvDoc> {
    match fs::read(path) {
        Ok(bytes) => Ok(DotenvDoc::parse(&bytes)),
        Err(err) if err.kind() == io::ErrorKind::NotFound => Ok(DotenvDoc::default()),
        Err(err) => Err(err).with_context(|| format!("read {}", path.display())),
    }
}

fn write_dotenv_atomic(path: &Path, bytes: &[u8]) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    let tmp = path.with_extension("tmp");
    fs::write(&tmp, bytes).with_context(|| format!("write {}", tmp.display()))?;
    #[cfg(unix)]
    fs::set_permissions(&tmp, fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {}", tmp.display()))?;
    fs::rename(&tmp, path).with_context(|| format!("rename {}", path.display()))?;
    Ok(())
}

fn write_secret_file(path: &Path, bytes: &[u8]) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(path, bytes).with_context(|| format!("write {}", path.display()))?;
    #[cfg(unix)]
    fs::set_permissions(path, fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {}", path.display()))?;
    Ok(())
}

fn restore_backup_path(env_file: &Path) -> PathBuf {
    use sha2::{Digest, Sha256};

    let digest = Sha256::digest(env_file.display().to_string().as_bytes());
    let name = format!("{}.enc", hex::encode(&digest[..16]));
    env_file
        .parent()
        .unwrap_or_else(|| Path::new("."))
        .join(".si-vault-restore")
        .join(name)
}

fn save_restore_backup(env_file: &Path, bytes: &[u8]) -> Result<()> {
    write_secret_file(&restore_backup_path(env_file), bytes)
}

fn resolve_hook_repo_root(vault_dir: Option<PathBuf>) -> Result<PathBuf> {
    let cwd = env::current_dir().context("read current dir")?;
    let start = vault_dir
        .as_deref()
        .map(|path| absolutize_path(path, &cwd))
        .unwrap_or(cwd);
    git_repo_root_from(&start).map_err(|_| anyhow!("vault dir not found (run inside a git repo or set --vault-dir)"))
}

fn git_hooks_dir(repo_root: &Path) -> Result<PathBuf> {
    let output = StdCommand::new("git")
        .arg("-C")
        .arg(repo_root)
        .args(["rev-parse", "--git-path", "hooks"])
        .output()
        .with_context(|| format!("run git hooks dir in {}", repo_root.display()))?;
    if !output.status.success() {
        bail!("unable to resolve git hooks dir for {}", repo_root.display());
    }
    let relative = String::from_utf8_lossy(&output.stdout).trim().to_owned();
    Ok(absolutize_path(Path::new(&relative), repo_root))
}

fn render_vault_pre_commit_hook() -> String {
    [
        "#!/bin/sh",
        "set -e",
        "# si-vault:hook pre-commit v2",
        "if [ -n \"${SI_BIN:-}\" ] && [ -x \"$SI_BIN\" ]; then",
        "  exec \"$SI_BIN\" vault check --staged --all",
        "fi",
        "if repo_root=$(git rev-parse --show-toplevel 2>/dev/null); then",
        "  if [ -x \"$repo_root/si\" ]; then",
        "    exec \"$repo_root/si\" vault check --staged --all",
        "  fi",
        "fi",
        "if command -v si >/dev/null 2>&1; then",
        "  exec si vault check --staged --all",
        "fi",
        "echo \"[si vault] error: si not found (install si or set SI_BIN)\" >&2",
        "exit 1",
        "",
    ]
    .join("\n")
}

fn is_managed_hook(bytes: &[u8]) -> bool {
    String::from_utf8_lossy(bytes).contains("si-vault:hook pre-commit")
}

fn write_executable_file(path: &Path, bytes: &[u8]) -> Result<()> {
    fs::write(path, bytes).with_context(|| format!("write {}", path.display()))?;
    #[cfg(unix)]
    fs::set_permissions(path, fs::Permissions::from_mode(0o700))
        .with_context(|| format!("chmod {}", path.display()))?;
    Ok(())
}

fn staged_dotenv_files(repo_root: &Path, scan_root: &Path, include_examples: bool) -> Result<Vec<PathBuf>> {
    let staged = git_staged_files(repo_root)?;
    let mut files = Vec::new();
    for relative in staged {
        if !is_dotenv_path(&relative, include_examples) {
            continue;
        }
        let absolute = repo_root.join(&relative);
        if path_is_within(&absolute, scan_root) {
            files.push(absolute);
        }
    }
    Ok(files)
}

fn git_staged_files(repo_root: &Path) -> Result<Vec<PathBuf>> {
    let output = StdCommand::new("git")
        .arg("-C")
        .arg(repo_root)
        .args(["diff", "--cached", "--name-only", "--diff-filter=ACMR"])
        .output()
        .with_context(|| format!("run git staged files in {}", repo_root.display()))?;
    if !output.status.success() {
        bail!("unable to list staged files in {}", repo_root.display());
    }
    Ok(String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .map(PathBuf::from)
        .collect())
}

fn git_show_index_file(repo_root: &Path, relative: &Path) -> Result<Vec<u8>> {
    let spec = format!(":{}", relative.display());
    let output = StdCommand::new("git")
        .arg("-C")
        .arg(repo_root)
        .arg("show")
        .arg(spec)
        .output()
        .with_context(|| format!("run git show in {}", repo_root.display()))?;
    if !output.status.success() {
        bail!("unable to read staged file {}", relative.display());
    }
    Ok(output.stdout)
}

fn discover_dotenv_files(root: &Path, include_examples: bool) -> Result<Vec<PathBuf>> {
    let mut out = Vec::new();
    discover_dotenv_files_inner(root, include_examples, &mut out)?;
    out.sort();
    Ok(out)
}

fn discover_dotenv_files_inner(
    root: &Path,
    include_examples: bool,
    out: &mut Vec<PathBuf>,
) -> Result<()> {
    for entry in fs::read_dir(root).with_context(|| format!("read {}", root.display()))? {
        let entry = entry.with_context(|| format!("read {}", root.display()))?;
        let path = entry.path();
        if entry.file_type().with_context(|| format!("stat {}", path.display()))?.is_dir() {
            if path.file_name().map(|name| name == ".git").unwrap_or(false) {
                continue;
            }
            discover_dotenv_files_inner(&path, include_examples, out)?;
            continue;
        }
        if is_dotenv_path(&path, include_examples) {
            out.push(path);
        }
    }
    Ok(())
}

fn is_dotenv_path(path: &Path, include_examples: bool) -> bool {
    let Some(file_name) = path.file_name() else {
        return false;
    };
    let file_name = file_name.to_string_lossy().to_ascii_lowercase();
    if file_name == ".env" || file_name.starts_with(".env.") {
        if include_examples {
            return true;
        }
        return !matches!(
            file_name.as_str(),
            ".env.example" | ".env.sample" | ".env.template" | ".env.dist"
        );
    }
    false
}

fn path_is_within(child: &Path, parent: &Path) -> bool {
    child == parent || child.starts_with(parent)
}

fn absolutize_path(path: &Path, base: &Path) -> PathBuf {
    if path.is_absolute() {
        path.to_path_buf()
    } else {
        base.join(path)
    }
}

fn normalize_trailing_args(mut args: Vec<String>) -> Vec<String> {
    if args.first().map(String::as_str) == Some("--") {
        args.remove(0);
    }
    args
}
