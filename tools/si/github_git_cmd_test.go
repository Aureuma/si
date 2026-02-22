package main

import (
	"os"
	"os/exec"
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
	got, ok := normalizeGitHubRemoteURL("git@github.com:Aureuma/platform.git")
	if !ok {
		t.Fatalf("expected github ssh remote to normalize")
	}
	if got.URL != "https://github.com/Aureuma/platform.git" {
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
	input := strings.NewReader("protocol=https\nhost=github.com\npath=Aureuma/platform.git\n\n")
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
	if req.Path != "Aureuma/platform.git" {
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
		VaultFile: "/home/dev/.si/vault/prod.env",
		Account:   "core",
	})
	want := "!si vault run --file /home/dev/.si/vault/prod.env -- si github git credential --account core"
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

func TestBuildGitHubRemoteURLWithPAT(t *testing.T) {
	urlWithPAT, err := buildGitHubRemoteURLWithPAT("https://github.com/Aureuma/sun.git", "github_pat_example123")
	if err != nil {
		t.Fatalf("buildGitHubRemoteURLWithPAT: %v", err)
	}
	want := "https://github_pat_example123@github.com/Aureuma/sun.git"
	if urlWithPAT != want {
		t.Fatalf("unexpected url with pat:\nwant: %s\ngot:  %s", want, urlWithPAT)
	}
}

func TestBuildGitHubRemoteURLWithPATRejectsNonHTTPS(t *testing.T) {
	if _, err := buildGitHubRemoteURLWithPAT("ssh://github.com/Aureuma/sun.git", "token"); err == nil {
		t.Fatalf("expected non-https url to fail")
	}
}

func TestRedactGitRemotePATURL(t *testing.T) {
	raw := "https://github_pat_1234567890abcdef@github.com/Aureuma/sun.git"
	redacted := redactGitRemotePATURL(raw)
	if strings.Contains(redacted, "github_pat_1234567890abcdef") {
		t.Fatalf("expected PAT to be redacted, got: %s", redacted)
	}
	if !strings.Contains(redacted, "https://gith...cdef@github.com/Aureuma/sun.git") {
		t.Fatalf("unexpected redacted url: %s", redacted)
	}
}

func TestCountRemoteAuthFunctions(t *testing.T) {
	items := []githubGitRemoteAuthRepoChange{
		{Changed: true},
		{Changed: true, Error: "failed"},
		{Skipped: "not github"},
		{Error: "boom"},
	}
	if got := countRemoteAuthChanged(items); got != 1 {
		t.Fatalf("countRemoteAuthChanged=%d want=1", got)
	}
	if got := countRemoteAuthSkipped(items); got != 1 {
		t.Fatalf("countRemoteAuthSkipped=%d want=1", got)
	}
	if got := countRemoteAuthErrored(items); got != 2 {
		t.Fatalf("countRemoteAuthErrored=%d want=2", got)
	}
}

func TestEnsureGitBranchTrackingSetsConfig(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "test")
	runGit(t, repo, "remote", "add", "origin", "https://github.com/Aureuma/demo.git")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	status, err := ensureGitBranchTracking(repo, "origin", false)
	if err != nil {
		t.Fatalf("ensureGitBranchTracking: %v", err)
	}
	if status != "set" {
		t.Fatalf("tracking status=%q want=set", status)
	}

	branch, err := gitCurrentBranch(repo)
	if err != nil {
		t.Fatalf("gitCurrentBranch: %v", err)
	}
	if branch == "" {
		t.Fatalf("expected non-empty current branch")
	}

	remote := strings.TrimSpace(runGit(t, repo, "config", "--get", "branch."+branch+".remote"))
	if remote != "origin" {
		t.Fatalf("branch remote=%q want=origin", remote)
	}
	merge := strings.TrimSpace(runGit(t, repo, "config", "--get", "branch."+branch+".merge"))
	if merge != "refs/heads/"+branch {
		t.Fatalf("branch merge=%q want=%q", merge, "refs/heads/"+branch)
	}
}

func TestParseGitHubCloneSourceOwnerRepo(t *testing.T) {
	normalized, err := parseGitHubCloneSource("Aureuma/GitHubProj")
	if err != nil {
		t.Fatalf("parseGitHubCloneSource: %v", err)
	}
	if normalized.Owner != "Aureuma" || normalized.Repo != "GitHubProj" {
		t.Fatalf("unexpected owner/repo: %s/%s", normalized.Owner, normalized.Repo)
	}
	if normalized.URL != "https://github.com/Aureuma/GitHubProj.git" {
		t.Fatalf("unexpected canonical url: %s", normalized.URL)
	}
}

func TestParseGitHubCloneSourceURL(t *testing.T) {
	normalized, err := parseGitHubCloneSource("https://github.com/Aureuma/GitHubProj")
	if err != nil {
		t.Fatalf("parseGitHubCloneSource: %v", err)
	}
	if normalized.URL != "https://github.com/Aureuma/GitHubProj.git" {
		t.Fatalf("unexpected canonical url: %s", normalized.URL)
	}
}

func TestParseGitHubCloneSourceRejectsInvalid(t *testing.T) {
	if _, err := parseGitHubCloneSource("not-a-repo"); err == nil {
		t.Fatalf("expected invalid clone source to fail")
	}
}

func TestPlanGitCloneDestination(t *testing.T) {
	root := "/tmp/dev"
	if got := planGitCloneDestination(root, "GitHubProj", ""); got != filepath.Join(root, "GitHubProj") {
		t.Fatalf("unexpected default destination: %s", got)
	}
	if got := planGitCloneDestination(root, "GitHubProj", "custom/path"); got != filepath.Join(root, "custom/path") {
		t.Fatalf("unexpected relative destination: %s", got)
	}
	if got := planGitCloneDestination(root, "GitHubProj", "/tmp/elsewhere"); got != "/tmp/elsewhere" {
		t.Fatalf("unexpected absolute destination: %s", got)
	}
}

func TestEnsureCloneDestinationAvailable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "repo")
	if err := ensureCloneDestinationAvailable(target); err != nil {
		t.Fatalf("ensureCloneDestinationAvailable new path: %v", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := ensureCloneDestinationAvailable(target); err == nil {
		t.Fatalf("expected existing destination error")
	}
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
