package main

import (
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

func TestIsGitCredentialHostAllowed(t *testing.T) {
	if !isGitCredentialHostAllowed("github.com", "https://api.github.com") {
		t.Fatalf("expected github.com to be allowed")
	}
	if isGitCredentialHostAllowed("gitlab.com", "https://api.github.com") {
		t.Fatalf("expected gitlab.com to be rejected")
	}
}
