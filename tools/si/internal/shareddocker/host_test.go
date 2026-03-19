package docker

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSocketExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if socketExists(filePath) {
		t.Fatalf("expected regular file to not be detected as socket")
	}

	socketPath := filepath.Join(dir, "test.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	if !socketExists(socketPath) {
		t.Fatalf("expected unix socket to be detected")
	}
}

func TestDetectColimaHostNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin behavior")
	}
	if host, ok := detectColimaHost(); ok || host != "" {
		t.Fatalf("expected no host on %s, got %q (ok=%v)", runtime.GOOS, host, ok)
	}
}

func TestDetectColimaHostDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only behavior")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	socketPath := filepath.Join(home, ".colima", "default", "docker.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	host, ok := detectColimaHost()
	if !ok {
		t.Fatalf("expected colima host to be detected")
	}
	if host != "unix://"+socketPath {
		t.Fatalf("expected host %q, got %q", "unix://"+socketPath, host)
	}
}

func TestColimaProfileFromDockerContext(t *testing.T) {
	tests := []struct {
		contextName string
		wantProfile string
		wantOK      bool
	}{
		{contextName: "colima", wantProfile: "default", wantOK: true},
		{contextName: "colima-work", wantProfile: "work", wantOK: true},
		{contextName: "desktop-linux", wantProfile: "", wantOK: false},
		{contextName: "colima-", wantProfile: "", wantOK: false},
	}
	for _, tc := range tests {
		got, ok := colimaProfileFromDockerContext(tc.contextName)
		if ok != tc.wantOK || got != tc.wantProfile {
			t.Fatalf("context %q expected (%q,%v), got (%q,%v)", tc.contextName, tc.wantProfile, tc.wantOK, got, ok)
		}
	}
}

func TestColimaProfileCandidatesUsesEnvAndContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COLIMA_PROFILE", "work")
	t.Setenv("COLIMA_INSTANCE", "")
	configPath := filepath.Join(home, ".docker", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload, err := json.Marshal(map[string]string{"currentContext": "colima-team"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(configPath, payload, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := colimaProfileCandidates(home)
	if len(got) < 3 {
		t.Fatalf("expected at least env/context/default profiles, got %v", got)
	}
	if got[0] != "work" {
		t.Fatalf("expected env profile first, got %v", got)
	}
	foundContext := false
	foundDefault := false
	for _, value := range got {
		if value == "team" {
			foundContext = true
		}
		if value == "default" {
			foundDefault = true
		}
	}
	if !foundContext || !foundDefault {
		t.Fatalf("expected context+default profiles in %v", got)
	}
}

func TestDetectColimaHostForProfiles(t *testing.T) {
	base := t.TempDir()
	socketPath := filepath.Join(base, "work", "docker.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	got, ok := detectColimaHostForProfiles(base, []string{"default", "work"})
	if !ok {
		t.Fatalf("expected host to be detected")
	}
	want := "unix://" + socketPath
	if got != want {
		t.Fatalf("expected host %q, got %q", want, got)
	}
}

func TestAutoDockerHostSkipsWhenContextExplicit(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "colima")
	host, ok := AutoDockerHost()
	if ok || host != "" {
		t.Fatalf("expected AutoDockerHost to skip when DOCKER_CONTEXT is set, got host=%q ok=%v", host, ok)
	}
}
