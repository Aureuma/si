//! Tests for the development-only `si-release` binary (extracted from `si build`).

use assert_cmd::Command;
use std::fs;
use std::fs::File;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::Path;
use std::thread;
use tar::Archive;
use tempfile::tempdir;

fn release_bin() -> Command {
    Command::cargo_bin("si-release").expect("si-release binary should build")
}

fn write_workspace_manifest(repo: &Path, version: &str) {
    fs::create_dir_all(repo.join("rust/crates/si-cli")).expect("mkdir cli crate");
    fs::write(
        repo.join("Cargo.toml"),
        format!(
            "[workspace]\nmembers = [\"rust/crates/si-cli\"]\nresolver = \"2\"\n\n[workspace.package]\nversion = \"{}\"\nedition = \"2024\"\nlicense = \"AGPL-3.0-only\"\nrepository = \"https://example.invalid/si\"\nrust-version = \"1.94\"\n",
            version.trim_start_matches('v')
        ),
    )
    .expect("write Cargo.toml");
}

fn shell_escape_for_test(path: &Path) -> String {
    format!("'{}'", path.display().to_string().replace('\'', "'\"'\"'"))
}

fn write_executable_shell_script(path: &Path, body: &str) {
    fs::write(path, body).expect("write shell script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(path).expect("stat shell script").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(path, perms).expect("chmod shell script");
    }
}

#[test]
fn build_homebrew_render_core_formula_writes_formula() {
    let dir = tempdir().expect("repo tempdir");
    let out = dir.path().join("Formula/si.rb");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let url = format!("http://{addr}");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /Aureuma/si/archive/refs/tags/v1.2.3.tar.gz"));
        let body = b"archive";
        let response = format!("HTTP/1.1 200 OK\r\nContent-Length: {}\r\n\r\n", body.len());
        stream.write_all(response.as_bytes()).expect("write head");
        stream.write_all(body).expect("write body");
    });

    release_bin()
        .args([
            "homebrew",
            "render-core-formula",
            "--version",
            "v1.2.3",
            "--output",
            out.to_str().expect("out"),
        ])
        .env("SI_RUST_HOMEBREW_SOURCE_BASE_URL", url)
        .assert()
        .success();

    let rendered = fs::read_to_string(out).expect("read formula");
    assert!(rendered.contains("homepage \"https://github.com/Aureuma/si\""));
    assert!(rendered.contains("url \"http://"));
    assert!(rendered.contains("sha256 \""));
    assert!(rendered.contains("depends_on \"rust\" => :build"));
    assert!(rendered.contains("cargo\", \"install\", \"--locked\""));
    assert!(rendered.contains("std_cargo_args(path: \"rust/crates/si-cli\")"));
    assert!(rendered.contains("\"--bin\", \"si\""));
}

#[test]
fn build_homebrew_render_core_formula_without_version_uses_workspace_version() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let out = repo.path().join("Formula/si.rb");
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let addr = listener.local_addr().expect("local addr");
    let url = format!("http://{addr}");

    thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut buffer = [0_u8; 4096];
        let read = stream.read(&mut buffer).expect("read request");
        let request = String::from_utf8_lossy(&buffer[..read]);
        assert!(request.contains("GET /Aureuma/si/archive/refs/tags/v1.2.3.tar.gz"));
        let body = b"archive";
        let response = format!("HTTP/1.1 200 OK\r\nContent-Length: {}\r\n\r\n", body.len());
        stream.write_all(response.as_bytes()).expect("write head");
        stream.write_all(body).expect("write body");
    });

    release_bin()
        .current_dir(repo.path())
        .args(["homebrew", "render-core-formula", "--output", out.to_str().expect("out")])
        .env("SI_RUST_HOMEBREW_SOURCE_BASE_URL", url)
        .assert()
        .success();

    let rendered = fs::read_to_string(out).expect("read formula");
    assert!(rendered.contains("url \"http://"));
}

#[test]
fn build_homebrew_render_tap_formula_writes_formula() {
    let dir = tempdir().expect("tempdir");
    let checksums = dir.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");
    let output = dir.path().join("Formula/si.rb");

    release_bin()
        .args([
            "homebrew",
            "render-tap-formula",
            "--version",
            "v1.2.3",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--output",
            output.to_str().expect("output"),
        ])
        .assert()
        .success();

    let rendered = fs::read_to_string(output).expect("read formula");
    assert!(rendered.contains("version \"1.2.3\""));
    assert!(rendered.contains("si_1.2.3_linux_amd64.tar.gz"));
    assert!(rendered.contains("sha4"));
    assert!(rendered.contains("bin.install binary => \"si\""));
}

#[test]
fn build_homebrew_render_tap_formula_without_version_uses_workspace_version() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let checksums = repo.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");
    let output = repo.path().join("Formula/si.rb");

    release_bin()
        .current_dir(repo.path())
        .args([
            "homebrew",
            "render-tap-formula",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--output",
            output.to_str().expect("output"),
        ])
        .assert()
        .success();

    let rendered = fs::read_to_string(output).expect("read formula");
    assert!(rendered.contains("version \"1.2.3\""));
}

#[test]
fn build_self_verify_release_assets_checks_archives() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::write(repo.path().join("README.md"), "readme\n").expect("write readme");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("write license");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\ntarget=\"\"\nprev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--target\" ]; then target=\"$arg\"; fi\n  prev=\"$arg\"\ndone\nout=\"$CARGO_TARGET_DIR/release\"\nif [ -n \"$target\" ]; then out=\"$CARGO_TARGET_DIR/$target/release\"; fi\nmkdir -p \"$out\"\nprintf '#!/bin/sh\\necho si\\n' > \"$out/si\"\nchmod 755 \"$out/si\"\n",
    );
    let file_path = bin_dir.path().join("file");
    write_executable_shell_script(
        &file_path,
        "#!/bin/sh\ncase \"$1\" in\n  *linux_amd64*) echo \"$1: ELF 64-bit LSB executable, x86-64\" ;;\n  *linux_arm64*) echo \"$1: ELF 64-bit LSB executable, ARM aarch64\" ;;\n  *darwin_amd64*) echo \"$1: Mach-O 64-bit executable x86_64\" ;;\n  *darwin_arm64*) echo \"$1: Mach-O 64-bit executable arm64\" ;;\n  *) echo \"$1: unknown\" ;;\nesac\n",
    );
    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    release_bin()
        .current_dir(repo.path())
        .args([
            "self",
            "release-assets",
            "--repo",
            repo.path().to_str().expect("repo path"),
            "--out-dir",
            out_dir.to_str().expect("out path"),
        ])
        .env("PATH", &path_env)
        .assert()
        .success();

    release_bin()
        .current_dir(repo.path())
        .args([
            "self",
            "verify-release-assets",
            "--version",
            "v1.2.3",
            "--out-dir",
            out_dir.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .assert()
        .success();
}

#[test]
fn build_homebrew_update_tap_repo_writes_formula_without_commit() {
    let dir = tempdir().expect("tempdir");
    let tap_dir = dir.path().join("homebrew-si");
    fs::create_dir_all(&tap_dir).expect("mkdir tap dir");
    let checksums = dir.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    release_bin()
        .args([
            "homebrew",
            "update-tap-repo",
            "--version",
            "v1.2.3",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--tap-dir",
            tap_dir.to_str().expect("tap dir"),
        ])
        .assert()
        .success();

    assert!(tap_dir.join("Formula/si.rb").exists());
}

#[test]
fn build_homebrew_update_tap_repo_without_version_uses_workspace_version() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let tap_dir = repo.path().join("homebrew-si");
    fs::create_dir_all(&tap_dir).expect("mkdir tap dir");
    let checksums = repo.path().join("checksums.txt");
    fs::write(
        &checksums,
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    release_bin()
        .current_dir(repo.path())
        .args([
            "homebrew",
            "update-tap-repo",
            "--checksums",
            checksums.to_str().expect("checksums"),
            "--tap-dir",
            tap_dir.to_str().expect("tap dir"),
        ])
        .assert()
        .success();

    let rendered = fs::read_to_string(tap_dir.join("Formula/si.rb")).expect("read formula");
    assert!(rendered.contains("version \"1.2.3\""));
}

#[test]
fn build_installer_run_dry_run_reports_rust_usage() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v0.1.0");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(&cargo_path, "#!/bin/sh\necho cargo 1.94.0\n");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = release_bin()
        .args([
            "installer",
            "run",
            "--dry-run",
            "--source-dir",
            repo.path().to_str().expect("repo"),
            "--force",
        ])
        .env("PATH", path_env)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let output = String::from_utf8_lossy(&output);
    assert!(output.contains("rust: using system cargo"));
}

#[test]
fn build_installer_run_installs_fake_binary() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v0.1.0");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo cargo 1.94.0\n  exit 0\nfi\nmkdir -p \"$CARGO_TARGET_DIR/release\"\nprintf '#!/bin/sh\\necho installed\\n' > \"$CARGO_TARGET_DIR/release/si\"\nchmod 755 \"$CARGO_TARGET_DIR/release/si\"\n",
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let install_dir = repo.path().join("bin");

    release_bin()
        .args([
            "installer",
            "run",
            "--source-dir",
            repo.path().to_str().expect("repo"),
            "--install-dir",
            install_dir.to_str().expect("install dir"),
            "--force",
            "--quiet",
        ])
        .env("PATH", path_env)
        .assert()
        .success();

    let installed = install_dir.join("si");
    assert!(installed.exists());
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(
            fs::metadata(&installed).expect("stat installed").permissions().mode() & 0o111,
            0o111
        );
    }
}

#[test]
fn build_installer_smoke_host_runs_rust_or_override_commands() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let installer = repo.path().join("installer-fixture");
    let settings = repo.path().join("settings-fixture");
    fs::write(
        &installer,
        "#!/bin/sh\nprev=\nbackend=\nsource_dir=\ninstall_dir=\ninstall_path=\nuninstall=0\ndry_run=0\nfor i in \"$@\"; do\n  if [ \"$prev\" = \"--backend\" ]; then backend=\"$i\"; fi\n  if [ \"$prev\" = \"--source-dir\" ]; then source_dir=\"$i\"; fi\n  if [ \"$prev\" = \"--install-dir\" ]; then install_dir=\"$i\"; fi\n  if [ \"$prev\" = \"--install-path\" ]; then install_path=\"$i\"; fi\n  [ \"$i\" = \"--uninstall\" ] && uninstall=1\n  [ \"$i\" = \"--dry-run\" ] && dry_run=1\n  [ \"$i\" = \"--help\" ] && exit 0\n  prev=\"$i\"\ndone\nif [ -n \"$backend\" ] && [ \"$backend\" != \"local\" ]; then exit 1; fi\nif [ -n \"$install_dir\" ] && [ -n \"$install_path\" ]; then exit 1; fi\nif [ -n \"$source_dir\" ] && [ ! -d \"$source_dir\" ]; then exit 1; fi\ncase \"$source_dir\" in *missing-source*) exit 1;; esac\nif [ -n \"$install_dir\" ]; then target=\"$install_dir/si\"; else target=\"$install_path\"; fi\nif [ \"$uninstall\" = 1 ]; then rm -f \"$target\"; exit 0; fi\nif [ \"$dry_run\" = 1 ]; then exit 0; fi\nmkdir -p \"$(dirname \"$target\")\"\nprintf '#!/bin/sh\\nexit 0\\n' > \"$target\"\nchmod 755 \"$target\"\n",
    )
    .expect("write installer");
    fs::write(&settings, "#!/bin/sh\nexit 0\n").expect("write settings");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&installer, &settings] {
            let mut perms = fs::metadata(path).expect("stat path").permissions();
            perms.set_mode(0o755);
            fs::set_permissions(path, perms).expect("chmod path");
        }
    }
    let bin_dir = tempdir().expect("bin tempdir");
    let git_path = bin_dir.path().join("git");
    fs::write(&git_path, "#!/bin/sh\nexit 0\n").expect("write git");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&git_path).expect("stat git").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&git_path, perms).expect("chmod git");
    }
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .current_dir(repo.path())
        .args(["installer", "smoke-host"])
        .env("PATH", path_env)
        .env("SI_INSTALLER_RUNNER", &installer)
        .env("SI_INSTALLER_SETTINGS_TEST", &settings)
        .assert()
        .success();
}

#[test]
fn build_installer_smoke_npm_runs_rust_or_override_commands() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let build_assets = repo.path().join("build-assets-fixture");
    let build_npm = repo.path().join("build-npm-fixture");
    fs::write(&build_assets, "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\nexit 0\n").expect("write assets script");
    fs::write(&build_npm, "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\ntouch \"$out/aureuma-si-1.2.3.tgz\"\nexit 0\n").expect("write npm script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&build_assets, &build_npm] {
            let mut perms = fs::metadata(path).expect("stat script").permissions();
            perms.set_mode(0o755);
            fs::set_permissions(path, perms).expect("chmod script");
        }
    }
    let bin_dir = tempdir().expect("bin tempdir");
    let npm_path = bin_dir.path().join("npm");
    fs::write(&npm_path, "#!/bin/sh\nprefix=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--prefix\" ]; then prefix=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$prefix/bin\"\nprintf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"\nchmod 755 \"$prefix/bin/si\"\nexit 0\n").expect("write npm");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&npm_path).expect("stat npm").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&npm_path, perms).expect("chmod npm");
    }
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .current_dir(repo.path())
        .args(["installer", "smoke-npm"])
        .env("PATH", path_env)
        .env("SI_BUILD_ASSETS_EXEC", &build_assets)
        .env("SI_BUILD_NPM_PACKAGE_EXEC", &build_npm)
        .assert()
        .success();
}

#[test]
fn build_installer_smoke_homebrew_runs_rust_or_override_commands() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let build_assets = repo.path().join("build-assets-fixture");
    fs::write(
        &build_assets,
        "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\ncat > \"$out/checksums.txt\" <<'EOF'\nsha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\nEOF\nexit 0\n",
    )
    .expect("write assets script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&build_assets).expect("stat assets script").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&build_assets, perms).expect("chmod assets script");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"trust\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf trusted > \"$prefix/trusted\"; exit 0; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q 'file://' \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix)
        ),
    )
    .expect("write brew");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&brew).expect("stat brew").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&brew, perms).expect("chmod brew");
    }

    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .current_dir(repo.path())
        .args(["installer", "smoke-homebrew"])
        .env("PATH", path_env)
        .env("SI_BUILD_ASSETS_EXEC", &build_assets)
        .assert()
        .success();
    assert!(brew_prefix.join("trusted").is_file(), "smoke must trust the tap");
}

#[test]
fn build_installer_smoke_homebrew_tolerates_brew_without_trust() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    let build_assets = repo.path().join("build-assets-fixture");
    fs::write(
        &build_assets,
        "#!/bin/sh\nout=\nprev=\nfor i in \"$@\"; do if [ \"$prev\" = \"--out-dir\" ]; then out=\"$i\"; fi; prev=\"$i\"; done\nmkdir -p \"$out\"\ncat > \"$out/checksums.txt\" <<'EOF'\nsha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\nEOF\nexit 0\n",
    )
    .expect("write assets script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&build_assets).expect("stat assets script").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&build_assets, perms).expect("chmod assets script");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"trust\" ]; then echo 'Error: Unknown command: trust' >&2; exit 1; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q 'file://' \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix)
        ),
    )
    .expect("write brew");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&brew).expect("stat brew").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&brew, perms).expect("chmod brew");
    }

    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .current_dir(repo.path())
        .args(["installer", "smoke-homebrew"])
        .env("PATH", path_env)
        .env("SI_BUILD_ASSETS_EXEC", &build_assets)
        .assert()
        .success();
}

#[test]
fn build_installer_smoke_homebrew_uses_provided_assets_dir() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    let provided_assets = repo.path().join("dist");
    fs::create_dir_all(&provided_assets).expect("mkdir dist");
    fs::write(
        provided_assets.join("checksums.txt"),
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nexpected_path={expected_path}\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"trust\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf trusted > \"$prefix/trusted\"; exit 0; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q \"file://$expected_path\" \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix),
            expected_path = shell_escape_for_test(&provided_assets),
        ),
    )
    .expect("write brew");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&brew).expect("stat brew").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&brew, perms).expect("chmod brew");
    }

    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .current_dir(repo.path())
        .args(["installer", "smoke-homebrew"])
        .env("PATH", path_env)
        .env("SI_INSTALL_SMOKE_ASSETS_DIR", &provided_assets)
        .assert()
        .success();
    assert!(brew_prefix.join("trusted").is_file(), "smoke must trust the tap");
}

#[test]
fn build_installer_smoke_homebrew_resolves_relative_assets_dir() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    let provided_assets = repo.path().join("dist");
    fs::create_dir_all(&provided_assets).expect("mkdir dist");
    fs::write(
        provided_assets.join("checksums.txt"),
        "sha1  si_1.2.3_darwin_arm64.tar.gz\nsha2  si_1.2.3_darwin_amd64.tar.gz\nsha3  si_1.2.3_linux_arm64.tar.gz\nsha4  si_1.2.3_linux_amd64.tar.gz\n",
    )
    .expect("write checksums");

    let bin_dir = tempdir().expect("bin tempdir");
    let brew_prefix = repo.path().join("fake-brew-prefix");
    let brew = bin_dir.path().join("brew");
    fs::write(
        &brew,
        format!(
            "#!/bin/sh\nset -eu\nprefix={prefix}\nmkdir -p \"$prefix\"\nstate=\"$prefix/tap-path\"\nformula_ref=\"si/homebrew-si-smoke/si-smoke\"\nexpected_path={expected_path}\nif [ \"$1\" = \"--version\" ]; then echo Homebrew 4.0.0; exit 0; fi\nif [ \"$1\" = \"tap\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf '%s\\n' \"$3\" > \"$state\"; exit 0; fi\nif [ \"$1\" = \"trust\" ]; then [ \"$2\" = \"si/homebrew-si-smoke\" ]; printf trusted > \"$prefix/trusted\"; exit 0; fi\nif [ \"$1\" = \"install\" ]; then [ \"$2\" = \"$formula_ref\" ]; tap_path=\"$(cat \"$state\")\"; formula=\"$tap_path/Formula/si-smoke.rb\"; grep -q 'class SiSmoke < Formula' \"$formula\"; grep -q \"file://$expected_path\" \"$formula\"; mkdir -p \"$prefix/bin\"; printf '#!/bin/sh\\nexit 0\\n' > \"$prefix/bin/si\"; chmod 755 \"$prefix/bin/si\"; exit 0; fi\nif [ \"$1\" = \"--prefix\" ] && [ \"$2\" = \"$formula_ref\" ]; then printf '%s\\n' \"$prefix\"; exit 0; fi\nif [ \"$1\" = \"uninstall\" ] && [ \"$3\" = \"$formula_ref\" ]; then rm -rf \"$prefix/bin\"; exit 0; fi\nif [ \"$1\" = \"untap\" ] && [ \"$2\" = \"si/homebrew-si-smoke\" ]; then rm -f \"$state\"; exit 0; fi\nexit 1\n",
            prefix = shell_escape_for_test(&brew_prefix),
            expected_path = shell_escape_for_test(&provided_assets),
        ),
    )
    .expect("write brew");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&brew).expect("stat brew").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&brew, perms).expect("chmod brew");
    }

    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .current_dir(repo.path())
        .args(["installer", "smoke-homebrew"])
        .env("PATH", path_env)
        .env("SI_INSTALL_SMOKE_ASSETS_DIR", "dist")
        .assert()
        .success();
    assert!(brew_prefix.join("trusted").is_file(), "smoke must trust the tap");
}

#[test]
fn build_self_validate_release_version_accepts_matching_tag() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    release_bin()
        .current_dir(repo.path())
        .args(["self", "validate-release-version", "--tag", "v1.2.3"])
        .assert()
        .success();
}

#[test]
fn build_self_validate_release_version_rejects_mismatch() {
    let repo = tempdir().expect("repo tempdir");
    write_workspace_manifest(repo.path(), "v1.2.3");

    release_bin()
        .current_dir(repo.path())
        .args(["self", "validate-release-version", "--tag", "v1.2.4"])
        .assert()
        .failure();
}

#[test]
fn build_self_release_asset_creates_single_archive() {
    let repo = tempdir().expect("repo tempdir");
    fs::create_dir_all(repo.path().join(".git")).expect("mkdir git dir");
    write_workspace_manifest(repo.path(), "v1.2.3");
    fs::write(repo.path().join("README.md"), "readme\n").expect("readme");
    fs::write(repo.path().join("LICENSE"), "license\n").expect("license");
    let toolchain_dir = tempdir().expect("toolchain tempdir");
    let cargo_path = toolchain_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        "#!/bin/sh\ntarget=\"\"\nprev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--target\" ]; then target=\"$arg\"; fi\n  prev=\"$arg\"\ndone\nout=\"$CARGO_TARGET_DIR/release\"\nif [ -n \"$target\" ]; then out=\"$CARGO_TARGET_DIR/$target/release\"; fi\nmkdir -p \"$out\"\nprintf '#!/bin/sh\\necho si\\n' > \"$out/si\"\nchmod 755 \"$out/si\"\n",
    );
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&cargo_path).expect("stat tool").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&cargo_path, perms).expect("chmod tool");
    }
    let out_dir = repo.path().join("out");
    let path_env =
        format!("{}:{}", toolchain_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    release_bin()
        .args([
            "self",
            "release-asset",
            "--repo-root",
            repo.path().to_str().expect("repo"),
            "--version",
            "v1.2.3",
            "--target",
            "linux-amd64",
            "--out-dir",
            out_dir.to_str().expect("out"),
        ])
        .env("PATH", path_env)
        .assert()
        .success();
    let archive = out_dir.join("si_1.2.3_linux_amd64.tar.gz");
    assert!(archive.exists());
    let file = File::open(&archive).expect("open archive");
    let decoder = flate2::read::GzDecoder::new(file);
    let mut archive = Archive::new(decoder);
    let mut names = archive
        .entries()
        .expect("archive entries")
        .map(|entry| entry.expect("entry").path().expect("entry path").display().to_string())
        .collect::<Vec<_>>();
    names.sort();
    assert!(names.iter().any(|name| name.ends_with("/si")));
}

#[test]
fn build_installer_settings_helper_prints_expected_doc() {
    let dir = tempdir().expect("tempdir");
    let settings = dir.path().join("settings.toml");
    let output = release_bin()
        .args([
            "installer",
            "settings-helper",
            "--settings",
            settings.to_str().expect("settings"),
            "--default-browser",
            "safari",
            "--print",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(String::from_utf8_lossy(&output), "[codex.login]\ndefault_browser = \"safari\"\n");
}

#[test]
fn build_installer_settings_helper_rewrites_existing_login_block() {
    let dir = tempdir().expect("tempdir");
    let settings = dir.path().join("settings.toml");
    fs::write(&settings, "[codex.login]\ndefault_browser = \"chrome\"\nother = true\n")
        .expect("write settings");
    release_bin()
        .args([
            "installer",
            "settings-helper",
            "--settings",
            settings.to_str().expect("settings"),
            "--default-browser",
            "safari",
        ])
        .assert()
        .success();
    let rendered = fs::read_to_string(settings).expect("read settings");
    assert!(rendered.contains("default_browser = \"safari\""));
    assert!(rendered.contains("other = true"));
}
