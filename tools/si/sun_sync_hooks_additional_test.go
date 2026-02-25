package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaybeSunAutoSyncProfileUploadsCredentialsAdditional(t *testing.T) {
	server, store := newSunTestServer(t, "acme", "token-autosync")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.AutoSync = true
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-autosync"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	profile := codexProfile{ID: "demo-sync", Name: "Demo Sync", Email: "demo-sync@example.com"}
	dir, err := ensureCodexProfileDir(profile)
	if err != nil {
		t.Fatalf("ensure profile dir: %v", err)
	}
	authPath := filepath.Join(dir, "auth.json")
	authJSON := `{"tokens":{"access_token":"access-demo","refresh_token":"refresh-demo"}}`
	if err := os.WriteFile(authPath, []byte(authJSON), 0o600); err != nil {
		t.Fatalf("write auth cache: %v", err)
	}

	maybeSunAutoSyncProfile("test_auto_sync", profile)

	payload, ok := store.get(sunCodexProfileBundleKind, profile.ID)
	if !ok || len(payload) == 0 {
		t.Fatalf("expected cloud profile payload to be uploaded")
	}
	var bundle sunCodexProfileBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		t.Fatalf("decode uploaded profile bundle: %v", err)
	}
	if bundle.ID != profile.ID {
		t.Fatalf("bundle id mismatch: got %q want %q", bundle.ID, profile.ID)
	}
	if strings.TrimSpace(string(bundle.AuthJSON)) != authJSON {
		t.Fatalf("bundle auth payload mismatch")
	}
}

func TestSICommandSupportsLoginHelpAndVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI smoke in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}
	stdout, stderr, err := runSICommand(t, env, "login", "--help")
	if err != nil {
		t.Fatalf("login --help failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "version")
	if err != nil {
		t.Fatalf("version failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("expected non-empty version output")
	}
}

func TestSunE2E_MachineRunWaitFailsOnRemoteCommandError(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-machine-wait")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-machine-wait")

	stdout, stderr, err := runSICommand(t, env, "sun", "machine", "register",
		"--machine", "controller-wait",
		"--operator", "op:controller@local",
		"--can-control-others",
		"--can-be-controlled=false",
		"--json",
	)
	if err != nil {
		t.Fatalf("controller register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "register",
		"--machine", "worker-wait",
		"--operator", "op:worker@remote",
		"--allow-operators", "op:controller@local",
		"--can-be-controlled",
		"--json",
	)
	if err != nil {
		t.Fatalf("worker register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	type cmdResult struct {
		stdout string
		stderr string
		err    error
	}
	waitResultCh := make(chan cmdResult, 1)
	go func() {
		out, errOut, runErr := runSICommand(t, env, "sun", "machine", "run",
			"--machine", "worker-wait",
			"--source-machine", "controller-wait",
			"--operator", "op:controller@local",
			"--wait",
			"--wait-timeout-seconds", "30",
			"--poll-seconds", "1",
			"--json",
			"--", "not-a-real-si-command",
		)
		waitResultCh <- cmdResult{stdout: out, stderr: errOut, err: runErr}
	}()

	time.Sleep(600 * time.Millisecond)
	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "serve",
		"--machine", "worker-wait",
		"--operator", "op:worker@remote",
		"--once",
		"--json",
	)
	if err != nil {
		t.Fatalf("machine serve once failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var serveSummary sunMachineServeSummary
	if err := json.Unmarshal([]byte(stdout), &serveSummary); err != nil {
		t.Fatalf("decode serve summary payload: %v output=%q", err, stdout)
	}
	if serveSummary.Processed != 1 {
		t.Fatalf("expected serve to process one job, got %d", serveSummary.Processed)
	}

	waitResult := <-waitResultCh
	if waitResult.err == nil {
		t.Fatalf("expected machine run --wait to fail for remote command error\nstdout=%s\nstderr=%s", waitResult.stdout, waitResult.stderr)
	}
	if !strings.Contains(strings.ToLower(waitResult.stderr), "finished with status failed") {
		t.Fatalf("expected wait stderr to mention failed status, got: %s", waitResult.stderr)
	}
	var job sunMachineJob
	if err := json.Unmarshal([]byte(waitResult.stdout), &job); err != nil {
		t.Fatalf("decode wait job payload: %v output=%q stderr=%q", err, waitResult.stdout, waitResult.stderr)
	}
	if job.Status != sunMachineJobStatusFailed {
		t.Fatalf("expected failed status from --wait json output, got %q", job.Status)
	}
	if job.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for failed remote command")
	}
}
