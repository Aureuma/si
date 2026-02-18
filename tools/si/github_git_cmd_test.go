package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeGitHubRemoteURLHTTPSWithPAT(t *testing.T) {
	got, ok := normalizeGitHubRemoteURL("https://github_pat_abc123@github.com/Aureuma/si.git")
	if !ok {
		t.Fatalf("expected github remote to normalize")
	}
	if got.URL != "https://github.com/Aureuma/si.git" {
		t.Fatalf("unexpected normalized url: %s", got.URL)
	}
	if got.Owner != "Aureuma" || got.Repo != "si" {
		t.Fatalf("unexpected owner/repo: %s/%s", got.Owner, got.Repo)
	}
}

func TestNormalizeGitHubRemoteURLSSH(t *testing.T) {
	got, ok := normalizeGitHubRemoteURL("git@github.com:Aureuma/viva.git")
	if !ok {
		t.Fatalf("expected github ssh remote to normalize")
	}
	if got.URL != "https://github.com/Aureuma/viva.git" {
		t.Fatalf("unexpected normalized url: %s", got.URL)
	}
}

func TestNormalizeGitHubRemoteURLRejectsNonGitHub(t *testing.T) {
	if _, ok := normalizeGitHubRemoteURL("https://gitlab.com/acme/repo.git"); ok {
		t.Fatalf("expected non-github remote to be rejected")
	}
}

func TestGitOwnerRepoFromCredentialPath(t *testing.T) {
	owner, repo := gitOwnerRepoFromCredentialPath("/Aureuma/si.git")
	if owner != "Aureuma" || repo != "si" {
		t.Fatalf("unexpected owner/repo: %s/%s", owner, repo)
	}
}

func TestReadGitCredentialRequestFromURL(t *testing.T) {
	input := strings.NewReader("url=https://github.com/Aureuma/si.git\n\n")
	req, err := readGitCredentialRequest(input)
	if err != nil {
		t.Fatalf("read credential request: %v", err)
	}
	if req.Host != "github.com" {
		t.Fatalf("unexpected host: %s", req.Host)
	}
	if req.Path != "/Aureuma/si.git" {
		t.Fatalf("unexpected path: %s", req.Path)
	}
}

func TestReadGitCredentialRequestFromFields(t *testing.T) {
	input := strings.NewReader("protocol=https\nhost=github.com\npath=Aureuma/viva.git\n\n")
	req, err := readGitCredentialRequest(input)
	if err != nil {
		t.Fatalf("read credential request: %v", err)
	}
	if req.Protocol != "https" {
		t.Fatalf("unexpected protocol: %s", req.Protocol)
	}
	if req.Host != "github.com" {
		t.Fatalf("unexpected host: %s", req.Host)
	}
	if req.Path != "Aureuma/viva.git" {
		t.Fatalf("unexpected path: %s", req.Path)
	}
}

func TestReadGitCredentialRequestMissingHost(t *testing.T) {
	input := strings.NewReader("protocol=https\npath=Aureuma/si.git\n\n")
	_, err := readGitCredentialRequest(input)
	if err == nil {
		t.Fatalf("expected missing-host error")
	}
	if !strings.Contains(err.Error(), "missing host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildGitHubCredentialHelperCommandUsesVault(t *testing.T) {
	cmd := buildGitHubCredentialHelperCommand(githubGitHelperOptions{
		UseVault: true,
		Account:  "core",
		Owner:    "Aureuma",
	})
	want := "!si vault run -- si github git credential --account core --owner Aureuma"
	if cmd != want {
		t.Fatalf("unexpected helper command:\nwant: %s\ngot:  %s", want, cmd)
	}
}

func TestBuildGitHubCredentialHelperCommandUsesVaultFile(t *testing.T) {
	cmd := buildGitHubCredentialHelperCommand(githubGitHelperOptions{
		UseVault:  true,
		VaultFile: "/home/shawn/Development/viva/.env.prod",
		Account:   "core",
	})
	want := "!si vault run --file /home/shawn/Development/viva/.env.prod -- si github git credential --account core"
	if cmd != want {
		t.Fatalf("unexpected helper command:\nwant: %s\ngot:  %s", want, cmd)
	}
}

func TestBuildGitHubCredentialHelperCommandUsesVaultIdentity(t *testing.T) {
	cmd := buildGitHubCredentialHelperCommand(githubGitHelperOptions{
		UseVault:      true,
		VaultIdentity: "/home/si/.si/vault/keys/age.key",
		Account:       "core",
	})
	want := "!SI_VAULT_IDENTITY_FILE=/home/si/.si/vault/keys/age.key si vault run -- si github git credential --account core"
	if cmd != want {
		t.Fatalf("unexpected helper command:\nwant: %s\ngot:  %s", want, cmd)
	}
}

func TestBuildGitHubCredentialHelperCommandNoVault(t *testing.T) {
	cmd := buildGitHubCredentialHelperCommand(githubGitHelperOptions{
		UseVault: false,
		Account:  "core",
		Owner:    "Aureuma",
	})
	want := "!si github git credential --account core --owner Aureuma"
	if cmd != want {
		t.Fatalf("unexpected helper command:\nwant: %s\ngot:  %s", want, cmd)
	}
}

func TestBuildGitHubCredentialHelperCommandQuotesValue(t *testing.T) {
	cmd := buildGitHubCredentialHelperCommand(githubGitHelperOptions{
		UseVault: false,
		Owner:    "Aureuma Team",
	})
	want := "!si github git credential --owner 'Aureuma Team'"
	if cmd != want {
		t.Fatalf("unexpected helper command:\nwant: %s\ngot:  %s", want, cmd)
	}
}

func TestIsGitCredentialHostAllowed(t *testing.T) {
	if !isGitCredentialHostAllowed("github.com", "https://api.github.com") {
		t.Fatalf("expected github.com to be allowed")
	}
	if isGitCredentialHostAllowed("gitlab.com", "https://api.github.com") {
		t.Fatalf("expected gitlab.com to be rejected")
	}
}

func TestGitOwnerRepoFromCredentialPathWithQuery(t *testing.T) {
	owner, repo := gitOwnerRepoFromCredentialPath("/Aureuma/si.git?service=git-upload-pack")
	if owner != "Aureuma" || repo != "si" {
		t.Fatalf("unexpected owner/repo: %s/%s", owner, repo)
	}
}

func TestNormalizeGitHubRemoteURLHTTPSNoSuffix(t *testing.T) {
	got, ok := normalizeGitHubRemoteURL("https://github.com/Aureuma/si")
	if !ok {
		t.Fatalf("expected github remote to normalize")
	}
	if got.URL != "https://github.com/Aureuma/si.git" {
		t.Fatalf("unexpected normalized url: %s", got.URL)
	}
}

func TestListGitRepos(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "a")
	repoB := filepath.Join(root, "b")
	nonRepo := filepath.Join(root, "not-repo")
	for _, dir := range []string{repoA, repoB, nonRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repoA, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoB, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git b: %v", err)
	}

	repos, err := listGitRepos(root)
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d (%v)", len(repos), repos)
	}
	if repos[0] != repoA || repos[1] != repoB {
		t.Fatalf("unexpected repos: %v", repos)
	}
}
