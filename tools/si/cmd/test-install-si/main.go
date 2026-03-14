package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const usageText = `Usage: ./tools/test-install-si.sh

Runs installer smoke tests against tools/install-si.sh, including
settings helper regression tests.`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	help, err := parseCLI(args)
	if help {
		_, _ = fmt.Fprintln(stdout, usageText)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		_, _ = fmt.Fprintln(stderr, "Run ./tools/test-install-si.sh --help for usage.")
		return 1
	}
	root, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "resolve repo root: %v\n", err)
		return 1
	}
	installer := filepath.Join(root, "tools", "install-si.sh")
	settingsHelperTest := filepath.Join(root, "tools", "test-install-si-settings.sh")

	if _, err := exec.LookPath("git"); err != nil {
		_, _ = fmt.Fprintln(stderr, "FAIL: git is required to run installer tests")
		return 1
	}
	if _, err := os.Stat(installer); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: installer not found at %s\n", installer)
		return 1
	}
	if fi, err := os.Stat(settingsHelperTest); err != nil || fi.Mode().Perm()&0o111 == 0 {
		_, _ = fmt.Fprintf(stderr, "FAIL: installer settings helper test not found at %s\n", settingsHelperTest)
		return 1
	}

	tmp, err := os.MkdirTemp("", "si-test-install-*")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	note := func(s string) { _, _ = fmt.Fprintf(stderr, "==> %s\n", s) }
	fail := func(msg string) int {
		_, _ = fmt.Fprintf(stderr, "FAIL: %s\n", msg)
		return 1
	}

	note("syntax check")
	if err := runCmd(root, nil, "bash", "-n", installer); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: syntax check: %v\n", err)
		return 1
	}

	note("installer settings helper tests")
	if err := runCmd(root, nil, settingsHelperTest); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: installer settings helper tests: %v\n", err)
		return 1
	}

	note("help output")
	if err := runCmd(root, nil, installer, "--help"); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: help output: %v\n", err)
		return 1
	}

	note("dry-run: linux/amd64 install-dir with spaces")
	if err := os.MkdirAll(filepath.Join(tmp, "bin dir"), 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runCmd(root, nil, installer, "--dry-run", "--source-dir", root, "--install-dir", filepath.Join(tmp, "bin dir"), "--force"); err != nil {
		return fail("dry-run install-dir with spaces")
	}

	note("dry-run: darwin/arm64 go download URL computation")
	if err := runCmd(root, nil, installer, "--dry-run", "--source-dir", root, "--os", "darwin", "--arch", "arm64", "--go-mode", "auto", "--force"); err != nil {
		return fail("dry-run darwin/arm64")
	}

	note("dry-run: no-path-hint flag")
	if err := runCmd(root, nil, installer, "--dry-run", "--no-path-hint", "--source-dir", root, "--force"); err != nil {
		return fail("dry-run no-path-hint")
	}

	note("dry-run: --yes accepted")
	if err := runCmd(root, nil, installer, "--dry-run", "--yes", "--source-dir", root, "--force"); err != nil {
		return fail("dry-run --yes")
	}

	note("dry-run: backend local accepted")
	if err := runCmd(root, nil, installer, "--dry-run", "--backend", "local", "--source-dir", root, "--force"); err != nil {
		return fail("dry-run backend local")
	}

	note("edge: invalid backend rejected")
	if err := runExpectFail(root, nil, installer, "--dry-run", "--backend", "bad-backend", "--source-dir", root, "--force"); err != nil {
		return fail(err.Error())
	}

	note("edge: install-dir and install-path are mutually exclusive")
	if err := runExpectFail(root, nil, installer, "--dry-run", "--source-dir", root, "--install-dir", filepath.Join(tmp, "x"), "--install-path", filepath.Join(tmp, "y", "si"), "--force"); err != nil {
		return fail(err.Error())
	}

	note("edge: invalid source-dir rejected")
	if err := runExpectFail(root, nil, installer, "--dry-run", "--source-dir", filepath.Join(tmp, "missing-source"), "--force"); err != nil {
		return fail(err.Error())
	}

	note("edge: non-si source-dir rejected")
	notSI := filepath.Join(tmp, "not-si")
	if err := os.MkdirAll(notSI, 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runExpectFail(root, nil, installer, "--dry-run", "--source-dir", notSI, "--force"); err != nil {
		return fail(err.Error())
	}

	note("e2e: install from local checkout into temp bin")
	installDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runCmd(root, nil, installer, "--source-dir", root, "--install-dir", installDir, "--force", "--quiet"); err != nil {
		return fail("install from local checkout")
	}
	installed := filepath.Join(installDir, "si")
	if !isExecutable(installed) {
		return fail(fmt.Sprintf("expected installed binary at %s", installed))
	}
	if err := runCmd(root, nil, installed, "version"); err != nil {
		return fail("run installed si version")
	}
	if err := runCmd(root, nil, installed, "--help"); err != nil {
		return fail("run installed si --help")
	}

	note("e2e: install-path override (with spaces)")
	installPath := filepath.Join(tmp, "bin custom", "si")
	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runCmd(root, nil, installer, "--source-dir", root, "--install-path", installPath, "--force", "--quiet"); err != nil {
		return fail("install-path override")
	}
	if !isExecutable(installPath) {
		return fail(fmt.Sprintf("expected installed binary at %s", installPath))
	}
	if err := runCmd(root, nil, installPath, "version"); err != nil {
		return fail("run install-path binary")
	}

	note("e2e: install with explicit tmp-dir (path with spaces)")
	tmpDir := filepath.Join(tmp, "tmp build")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runCmd(root, nil, installer, "--source-dir", root, "--tmp-dir", tmpDir, "--install-dir", installDir, "--force", "--quiet"); err != nil {
		return fail("install with explicit tmp-dir")
	}
	if err := runCmd(root, nil, installed, "version"); err != nil {
		return fail("run binary after tmp-dir install")
	}

	note("e2e: non-quiet install works")
	nonquietDir := filepath.Join(tmp, "bin-nonquiet")
	if err := os.MkdirAll(nonquietDir, 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runCmd(root, nil, installer, "--source-dir", root, "--install-dir", nonquietDir, "--force"); err != nil {
		return fail("non-quiet install")
	}
	if err := runCmd(root, nil, filepath.Join(nonquietDir, "si"), "version"); err != nil {
		return fail("run non-quiet binary")
	}

	note("e2e: idempotent reinstall over existing binary")
	if err := runCmd(root, nil, installer, "--source-dir", root, "--install-dir", installDir, "--force", "--quiet"); err != nil {
		return fail("idempotent reinstall")
	}
	if err := runCmd(root, nil, installed, "version"); err != nil {
		return fail("run binary after reinstall")
	}

	note("edge: reinstall without --force fails when binary exists")
	if err := runExpectFail(root, nil, installer, "--source-dir", root, "--install-dir", installDir, "--quiet"); err != nil {
		return fail(err.Error())
	}

	note("e2e: uninstall")
	if err := runCmd(root, nil, installer, "--install-dir", installDir, "--uninstall", "--quiet"); err != nil {
		return fail("uninstall")
	}
	if _, err := os.Stat(installed); err == nil {
		return fail(fmt.Sprintf("expected %s to be removed", installed))
	}

	note("edge: uninstall when not installed succeeds")
	if err := runCmd(root, nil, installer, "--install-dir", installDir, "--uninstall", "--quiet"); err != nil {
		return fail("uninstall when missing")
	}

	note("edge: unwritable install dir")
	if os.Getuid() == 0 {
		note("skip unwritable-dir assertion as root (root bypasses directory permission bits)")
	} else {
		ro := filepath.Join(tmp, "ro")
		if err := os.MkdirAll(ro, 0o755); err != nil {
			return fail(err.Error())
		}
		if err := os.Chmod(ro, 0o000); err != nil {
			return fail(err.Error())
		}
		err := runExpectFail(root, nil, installer, "--source-dir", root, "--install-dir", ro, "--force", "--quiet")
		_ = os.Chmod(ro, 0o755)
		if err != nil {
			return fail(err.Error())
		}
	}

	note("edge: go-mode system fails when go is unavailable")
	fakeGoPath := filepath.Join(tmp, "fake-go-path")
	if err := os.MkdirAll(fakeGoPath, 0o755); err != nil {
		return fail(err.Error())
	}
	fakeGo := filepath.Join(fakeGoPath, "go")
	if err := os.WriteFile(fakeGo, []byte("#!/usr/bin/env bash\nexit 127\n"), 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runExpectFail(root, map[string]string{"PATH": fakeGoPath + ":/usr/bin:/bin"}, installer, "--dry-run", "--source-dir", root, "--go-mode", "system", "--force", "--quiet"); err != nil {
		return fail(err.Error())
	}

	note("edge: --os/--arch overrides are rejected without --dry-run")
	if err := runExpectFail(root, nil, installer, "--source-dir", root, "--os", "darwin", "--arch", "arm64", "--force", "--quiet"); err != nil {
		return fail(err.Error())
	}

	note("edge: clone+checkout by commit sha (local repo-url)")
	shaBytes, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return fail(fmt.Sprintf("resolve git sha: %v", err))
	}
	sha := strings.TrimSpace(string(shaBytes))
	cloneInstallDir := filepath.Join(tmp, "bin-clone")
	if err := os.MkdirAll(cloneInstallDir, 0o755); err != nil {
		return fail(err.Error())
	}
	if err := runCmd(root, nil, installer, "--repo-url", root, "--ref", sha, "--install-dir", cloneInstallDir, "--force", "--quiet"); err != nil {
		return fail("clone+checkout by commit sha")
	}
	cloneBinary := filepath.Join(cloneInstallDir, "si")
	if !isExecutable(cloneBinary) {
		return fail(fmt.Sprintf("expected installed binary at %s", cloneBinary))
	}
	if err := runCmd(root, nil, cloneBinary, "version"); err != nil {
		return fail("run clone binary")
	}

	note("ok")
	return 0
}

func parseCLI(args []string) (bool, error) {
	fs := flag.NewFlagSet("test-install-si", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	if *help {
		return true, nil
	}
	if fs.NArg() > 0 {
		return false, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return false, nil
}

func runCmd(dir string, env map[string]string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if len(env) > 0 {
		merged := append([]string{}, os.Environ()...)
		for k, v := range env {
			merged = append(merged, k+"="+v)
		}
		cmd.Env = merged
	}
	return cmd.Run()
}

func runExpectFail(dir string, env map[string]string, name string, args ...string) error {
	err := runCmd(dir, env, name, args...)
	if err == nil {
		return fmt.Errorf("expected command to fail: %s %s", name, strings.Join(args, " "))
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return nil
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode().Perm()&0o111 != 0
}
