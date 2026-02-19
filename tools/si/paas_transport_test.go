package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizePaasAuthMethod(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: paasAuthMethodKey},
		{in: "KEY", want: paasAuthMethodKey},
		{in: "password", want: paasAuthMethodPassword},
		{in: "LoCaL", want: paasAuthMethodLocal},
		{in: "custom", want: "custom"},
	}
	for _, tc := range tests {
		got := normalizePaasAuthMethod(tc.in)
		if got != tc.want {
			t.Fatalf("normalizePaasAuthMethod(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestIsValidPaasAuthMethod(t *testing.T) {
	valid := []string{"key", "password", "local", "KEY", " Local "}
	for _, method := range valid {
		if !isValidPaasAuthMethod(method) {
			t.Fatalf("expected method %q to be valid", method)
		}
	}
	if isValidPaasAuthMethod("token") {
		t.Fatalf("unexpected valid method for token")
	}
}

func TestIsPaasLocalTarget(t *testing.T) {
	if !isPaasLocalTarget(paasTarget{Host: "example.com", AuthMethod: "local"}) {
		t.Fatalf("expected auth-method local to force local target behavior")
	}
	if !isPaasLocalTarget(paasTarget{Host: "localhost", AuthMethod: "key"}) {
		t.Fatalf("expected localhost host to be treated as local target")
	}
	if isPaasLocalTarget(paasTarget{Host: "example.com", AuthMethod: "key"}) {
		t.Fatalf("unexpected local detection for remote key target")
	}
}

func TestRunPaasSSHCommandLocal(t *testing.T) {
	target := paasTarget{
		Name:       "local",
		Host:       "127.0.0.1",
		User:       "root",
		Port:       22,
		AuthMethod: "local",
	}
	out, err := runPaasSSHCommand(context.Background(), target, "printf si-local-ok")
	if err != nil {
		t.Fatalf("runPaasSSHCommand local failed: %v", err)
	}
	if strings.TrimSpace(out) != "si-local-ok" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunPaasSCPUploadLocal(t *testing.T) {
	srcDir := t.TempDir()
	remoteDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "compose.yaml")
	content := []byte("services:\n  api:\n    image: example\n")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}
	target := paasTarget{
		Name:       "local",
		Host:       "localhost",
		User:       "root",
		Port:       22,
		AuthMethod: "local",
	}
	if err := runPaasSCPUpload(context.Background(), target, srcPath, remoteDir); err != nil {
		t.Fatalf("runPaasSCPUpload local failed: %v", err)
	}
	destPath := filepath.Join(remoteDir, filepath.Base(srcPath))
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("copied file content mismatch: got=%q want=%q", string(got), string(content))
	}
}

func TestPaasTargetAddAcceptsLocalAuthMethod(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	out := captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "local-node", "--host", "localhost", "--user", "root", "--auth-method", "local", "--json"})
	})
	env := parsePaasEnvelope(t, out)
	if env.Command != "target add" {
		t.Fatalf("unexpected command %q in envelope", env.Command)
	}
	if env.Fields["auth_method"] != "local" {
		t.Fatalf("expected auth_method=local, got %#v", env.Fields)
	}
	store, err := loadPaasTargetStore(defaultPaasContext)
	if err != nil {
		t.Fatalf("load target store: %v", err)
	}
	if len(store.Targets) != 1 || store.Targets[0].AuthMethod != "local" {
		t.Fatalf("expected one local target in store, got %#v", store.Targets)
	}
}
