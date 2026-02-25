package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type sunObjectStore struct {
	mu       sync.Mutex
	payloads map[string][]byte
	revs     map[string]int64
	metadata map[string]map[string]any
	history  map[string][]sunObjectRevision
	created  map[string]string
	updated  map[string]string
	putCalls int
}

func newSunObjectStore() *sunObjectStore {
	return &sunObjectStore{
		payloads: map[string][]byte{},
		revs:     map[string]int64{},
		metadata: map[string]map[string]any{},
		history:  map[string][]sunObjectRevision{},
		created:  map[string]string{},
		updated:  map[string]string{},
	}
}

func (s *sunObjectStore) key(kind string, name string) string {
	return kind + "/" + name
}

func (s *sunObjectStore) get(kind string, name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.payloads[s.key(kind, name)]
	if !ok {
		return nil, false
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, true
}

func (s *sunObjectStore) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putCalls
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *sunObjectStore) list(kind string, name string, limit int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	trimmedKind := strings.TrimSpace(kind)
	trimmedName := strings.TrimSpace(name)
	items := make([]map[string]any, 0, len(s.payloads))
	for key, payload := range s.payloads {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		itemKind := parts[0]
		itemName := parts[1]
		if trimmedKind != "" && !strings.EqualFold(itemKind, trimmedKind) {
			continue
		}
		if trimmedName != "" && !strings.EqualFold(itemName, trimmedName) {
			continue
		}
		created := s.created[key]
		updated := s.updated[key]
		if strings.TrimSpace(created) == "" {
			created = "2026-01-01T00:00:00Z"
		}
		if strings.TrimSpace(updated) == "" {
			updated = created
		}
		items = append(items, map[string]any{
			"kind":            itemKind,
			"name":            itemName,
			"latest_revision": s.revs[key],
			"checksum":        sunPayloadSHA256Hex(payload),
			"content_type":    "application/json",
			"size_bytes":      len(payload),
			"metadata":        cloneAnyMap(s.metadata[key]),
			"created_at":      created,
			"updated_at":      updated,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.Compare(formatAny(items[i]["name"]), formatAny(items[j]["name"])) < 0
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *sunObjectStore) revisions(kind string, name string, limit int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	key := s.key(kind, name)
	rows := s.history[key]
	if len(rows) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for idx := len(rows) - 1; idx >= 0; idx-- {
		row := rows[idx]
		out = append(out, map[string]any{
			"revision":     row.Revision,
			"checksum":     row.Checksum,
			"content_type": row.ContentType,
			"size_bytes":   row.SizeBytes,
			"metadata":     cloneAnyMap(row.Metadata),
			"created_at":   row.CreatedAt,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func newSunTestServer(t *testing.T, account string, token string) (*httptest.Server, *sunObjectStore) {
	t.Helper()
	store := newSunObjectStore()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/whoami" && r.Method == http.MethodGet {
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+token {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account_id":   "acc-1",
				"account_slug": account,
				"token_id":     "token-1",
				"scopes":       []string{"objects:read", "objects:write"},
			})
			return
		}
		if r.URL.Path == "/v1/objects" && r.Method == http.MethodGet {
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+token {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			kind := strings.TrimSpace(r.URL.Query().Get("kind"))
			name := strings.TrimSpace(r.URL.Query().Get("name"))
			limit := 200
			if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
				if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": store.list(kind, name, limit)})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/objects/") {
			http.NotFound(w, r)
			return
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/objects/"), "/")
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		kind := parts[0]
		name := parts[1]
		switch {
		case r.Method == http.MethodPut && len(parts) == 2:
			var req struct {
				PayloadBase64    string         `json:"payload_base64"`
				Metadata         map[string]any `json:"metadata"`
				ExpectedRevision *int64         `json:"expected_revision"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.PayloadBase64))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			store.mu.Lock()
			key := store.key(kind, name)
			currentRev := store.revs[key]
			if req.ExpectedRevision != nil && *req.ExpectedRevision != currentRev {
				store.mu.Unlock()
				http.Error(w, `{"error":"revision mismatch"}`, http.StatusConflict)
				return
			}
			store.putCalls++
			if _, ok := store.payloads[key]; !ok {
				store.created[key] = "2026-01-01T00:00:00Z"
			}
			store.payloads[key] = payload
			store.metadata[key] = cloneAnyMap(req.Metadata)
			store.revs[key]++
			rev := store.revs[key]
			store.updated[key] = "2026-01-02T00:00:00Z"
			store.history[key] = append(store.history[key], sunObjectRevision{
				Revision:    rev,
				Checksum:    sunPayloadSHA256Hex(payload),
				ContentType: "application/json",
				SizeBytes:   int64(len(payload)),
				Metadata:    cloneAnyMap(req.Metadata),
				CreatedAt:   "2026-01-02T00:00:00Z",
			})
			store.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": rev},
					"revision": map[string]any{"revision": rev},
				},
			})
			return
		case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "payload":
			payload, ok := store.get(kind, name)
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(payload)
			return
		case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "revisions":
			limit := 50
			if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
				if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": store.revisions(kind, name, limit)})
			return
		default:
			http.NotFound(w, r)
			return
		}
	})
	return httptest.NewServer(handler), store
}

func appendCodexProfileToSettings(t *testing.T, home string, id string, name string, email string) {
	t.Helper()
	settingsPath := filepath.Join(home, ".si", "settings.toml")
	f, err := os.OpenFile(settingsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n[codex.profiles]\nactive = %q\n[codex.profiles.entries.%s]\nname = %q\nemail = %q\n", id, id, name, email)
	if err != nil {
		t.Fatalf("append settings: %v", err)
	}
}

func setupSunAuthState(t *testing.T, serverURL string, account string, token string) (string, map[string]string) {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}
	stdout, stderr, err := runSICommand(t, env, "sun", "auth", "login", "--url", serverURL, "--token", token, "--account", account, "--auto-sync")
	if err != nil {
		t.Fatalf("sun auth login failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	return home, env
}

func TestSunE2E_ProfilePushPullRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, store := newSunTestServer(t, "acme", "token-123")
	defer server.Close()

	home, env := setupSunAuthState(t, server.URL, "acme", "token-123")
	authPath := filepath.Join(home, ".si", "codex", "profiles", "demo", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	wantAuth := `{"tokens":{"access_token":"demo-access","refresh_token":"demo-refresh"}}`
	if err := os.WriteFile(authPath, []byte(wantAuth), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	appendCodexProfileToSettings(t, home, "demo", "Demo User", "demo@example.com")

	stdout, stderr, err := runSICommand(t, env, "sun", "profile", "push", "--profile", "demo")
	if err != nil {
		t.Fatalf("profile push failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	if err := os.Remove(authPath); err != nil {
		t.Fatalf("remove auth path: %v", err)
	}
	stdout, stderr, err = runSICommand(t, env, "sun", "profile", "pull", "--profile", "demo")
	if err != nil {
		t.Fatalf("profile pull failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	gotAuth, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("read pulled auth file: %v", err)
	}
	if strings.TrimSpace(string(gotAuth)) != wantAuth {
		t.Fatalf("auth payload mismatch\ngot:  %s\nwant: %s", strings.TrimSpace(string(gotAuth)), wantAuth)
	}
	if payload, ok := store.get(sunCodexProfileBundleKind, "demo"); !ok || len(payload) == 0 {
		t.Fatalf("expected server to persist profile bundle payload")
	}
}

func TestSunE2E_TaskboardClaimAndLocking(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-taskboard")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-taskboard")

	stdout, stderr, err := runSICommand(t, env, "sun", "taskboard", "add", "--name", "shared", "--title", "low priority", "--prompt", "handle low", "--priority", "P3", "--json")
	if err != nil {
		t.Fatalf("taskboard add low failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var lowTask sunTaskboardTask
	if err := json.Unmarshal([]byte(stdout), &lowTask); err != nil {
		t.Fatalf("decode low task json: %v output=%q", err, stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "add", "--name", "shared", "--title", "high priority", "--prompt", "handle high", "--priority", "P1", "--json")
	if err != nil {
		t.Fatalf("taskboard add high failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var highTask sunTaskboardTask
	if err := json.Unmarshal([]byte(stdout), &highTask); err != nil {
		t.Fatalf("decode high task json: %v output=%q", err, stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "claim", "--name", "shared", "--agent", "agent-a", "--json")
	if err != nil {
		t.Fatalf("taskboard claim #1 failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var firstClaim sunTaskboardClaimResult
	if err := json.Unmarshal([]byte(stdout), &firstClaim); err != nil {
		t.Fatalf("decode first claim json: %v output=%q", err, stdout)
	}
	if firstClaim.Task.ID != highTask.ID {
		t.Fatalf("expected first claim to pick high priority task %q, got %q", highTask.ID, firstClaim.Task.ID)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "claim", "--name", "shared", "--agent", "agent-b", "--json")
	if err != nil {
		t.Fatalf("taskboard claim #2 failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var secondClaim sunTaskboardClaimResult
	if err := json.Unmarshal([]byte(stdout), &secondClaim); err != nil {
		t.Fatalf("decode second claim json: %v output=%q", err, stdout)
	}
	if secondClaim.Task.ID != lowTask.ID {
		t.Fatalf("expected second claim to pick low task %q, got %q", lowTask.ID, secondClaim.Task.ID)
	}

	_, _, err = runSICommand(t, env, "sun", "taskboard", "release", "--name", "shared", "--id", firstClaim.Task.ID, "--agent", "agent-b")
	if err == nil {
		t.Fatalf("expected release by non-owner agent to fail")
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "release", "--name", "shared", "--id", firstClaim.Task.ID, "--agent", "agent-a", "--json")
	if err != nil {
		t.Fatalf("taskboard release by owner failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "claim", "--name", "shared", "--id", firstClaim.Task.ID, "--agent", "agent-b", "--json")
	if err != nil {
		t.Fatalf("taskboard claim released task failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var reclaimed sunTaskboardClaimResult
	if err := json.Unmarshal([]byte(stdout), &reclaimed); err != nil {
		t.Fatalf("decode reclaimed json: %v output=%q", err, stdout)
	}
	if reclaimed.Task.ID != firstClaim.Task.ID {
		t.Fatalf("expected to reclaim task %q, got %q", firstClaim.Task.ID, reclaimed.Task.ID)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "done", "--name", "shared", "--id", secondClaim.Task.ID, "--agent", "agent-b", "--result", "completed", "--json")
	if err != nil {
		t.Fatalf("taskboard done failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var doneTask sunTaskboardTask
	if err := json.Unmarshal([]byte(stdout), &doneTask); err != nil {
		t.Fatalf("decode done task json: %v output=%q", err, stdout)
	}
	if doneTask.Status != sunTaskStatusDone {
		t.Fatalf("expected done status, got %q", doneTask.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "taskboard", "show", "--name", "shared", "--json")
	if err != nil {
		t.Fatalf("taskboard show failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var board sunTaskboard
	if err := json.Unmarshal([]byte(stdout), &board); err != nil {
		t.Fatalf("decode board json: %v output=%q", err, stdout)
	}
	if len(board.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(board.Tasks))
	}
}

func TestSunE2E_MachineRunServeWithACL(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-machine")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-machine")

	stdout, stderr, err := runSICommand(t, env, "sun", "machine", "register",
		"--machine", "controller-a",
		"--operator", "op:controller@local",
		"--can-control-others",
		"--can-be-controlled=false",
		"--json",
	)
	if err != nil {
		t.Fatalf("controller register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "register",
		"--machine", "worker-a",
		"--operator", "op:worker@remote",
		"--allow-operators", "op:controller@local",
		"--can-be-controlled",
		"--json",
	)
	if err != nil {
		t.Fatalf("worker register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	_, _, err = runSICommand(t, env, "sun", "machine", "run",
		"--machine", "worker-a",
		"--source-machine", "controller-a",
		"--operator", "op:rogue@local",
		"--json",
		"--", "version",
	)
	if err == nil {
		t.Fatalf("expected unauthorized operator dispatch to fail")
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "run",
		"--machine", "worker-a",
		"--source-machine", "controller-a",
		"--operator", "op:controller@local",
		"--json",
		"--", "version",
	)
	if err != nil {
		t.Fatalf("remote run dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var queued sunMachineJob
	if err := json.Unmarshal([]byte(stdout), &queued); err != nil {
		t.Fatalf("decode queued job payload: %v output=%q", err, stdout)
	}
	if strings.TrimSpace(queued.Status) != sunMachineJobStatusQueued {
		t.Fatalf("expected queued status, got %q", queued.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "serve",
		"--machine", "worker-a",
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

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "jobs",
		"--machine", "worker-a",
		"--status", "succeeded",
		"--json",
	)
	if err != nil {
		t.Fatalf("jobs list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var jobs []sunMachineJob
	if err := json.Unmarshal([]byte(stdout), &jobs); err != nil {
		t.Fatalf("decode jobs payload: %v output=%q", err, stdout)
	}
	if len(jobs) == 0 {
		t.Fatalf("expected at least one succeeded job")
	}
	if strings.TrimSpace(jobs[0].Status) != sunMachineJobStatusSucceeded {
		t.Fatalf("expected succeeded job, got %q", jobs[0].Status)
	}
}

func TestSunE2E_MachineRunRemoteLoginAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-machine-remote")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-machine-remote")

	stdout, stderr, err := runSICommand(t, env, "sun", "machine", "register",
		"--machine", "controller-b",
		"--operator", "op:controller@local",
		"--can-control-others",
		"--can-be-controlled=false",
		"--json",
	)
	if err != nil {
		t.Fatalf("controller register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "register",
		"--machine", "worker-b",
		"--operator", "op:worker@remote",
		"--allow-operators", "op:controller@local",
		"--can-be-controlled",
		"--json",
	)
	if err != nil {
		t.Fatalf("worker register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "run",
		"--machine", "worker-b",
		"--source-machine", "controller-b",
		"--operator", "op:controller@local",
		"--json",
		"--", "login", "--help",
	)
	if err != nil {
		t.Fatalf("remote login --help dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var loginJob sunMachineJob
	if err := json.Unmarshal([]byte(stdout), &loginJob); err != nil {
		t.Fatalf("decode login queued job: %v output=%q", err, stdout)
	}
	if loginJob.Status != sunMachineJobStatusQueued {
		t.Fatalf("expected queued login job status, got %q", loginJob.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "serve",
		"--machine", "worker-b",
		"--operator", "op:worker@remote",
		"--once",
		"--json",
	)
	if err != nil {
		t.Fatalf("serve once (login job) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "run",
		"--machine", "worker-b",
		"--source-machine", "controller-b",
		"--operator", "op:controller@local",
		"--json",
		"--", "list", "--json",
	)
	if err != nil {
		t.Fatalf("remote list --json dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var listJob sunMachineJob
	if err := json.Unmarshal([]byte(stdout), &listJob); err != nil {
		t.Fatalf("decode list queued job: %v output=%q", err, stdout)
	}
	if listJob.Status != sunMachineJobStatusQueued {
		t.Fatalf("expected queued list job status, got %q", listJob.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "serve",
		"--machine", "worker-b",
		"--operator", "op:worker@remote",
		"--once",
		"--json",
	)
	if err != nil {
		t.Fatalf("serve once (list job) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "machine", "jobs",
		"--machine", "worker-b",
		"--status", "succeeded",
		"--json",
	)
	if err != nil {
		t.Fatalf("jobs list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var jobs []sunMachineJob
	if err := json.Unmarshal([]byte(stdout), &jobs); err != nil {
		t.Fatalf("decode jobs payload: %v output=%q", err, stdout)
	}
	if len(jobs) < 2 {
		t.Fatalf("expected at least two succeeded jobs, got %d", len(jobs))
	}
	hasLoginHelp := false
	hasListJSON := false
	for _, job := range jobs {
		if len(job.Command) == 2 && job.Command[0] == "login" && job.Command[1] == "--help" {
			hasLoginHelp = true
		}
		if len(job.Command) == 2 && job.Command[0] == "list" && job.Command[1] == "--json" {
			hasListJSON = true
		}
	}
	if !hasLoginHelp {
		t.Fatalf("expected succeeded remote login --help job")
	}
	if !hasListJSON {
		t.Fatalf("expected succeeded remote list --json job")
	}
}

func TestMaybeSunAutoSyncProfileUploadsCredentials(t *testing.T) {
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
