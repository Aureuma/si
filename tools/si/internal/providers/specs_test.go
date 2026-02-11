package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestResolveDefaults(t *testing.T) {
	ResetRuntimeCounters()
	spec := Resolve(GitHub)
	if spec.BaseURL == "" {
		t.Fatalf("expected github base url default")
	}
	if spec.UserAgent == "" {
		t.Fatalf("expected github user agent default")
	}
	if spec.RateLimitPerSecond <= 0 {
		t.Fatalf("expected github default rate")
	}
}

func TestAdmitRespectsRetryAfterFeedback(t *testing.T) {
	ResetRuntimeCounters()
	headers := http.Header{}
	headers.Set("Retry-After", "1")
	Feedback(SocialX, "core", http.StatusTooManyRequests, headers)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := Admit(ctx, SocialX, "core", http.MethodGet, "/2/users/me")
	if err == nil {
		t.Fatalf("expected admit to block during retry-after cooldown")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got: %v", err)
	}
}

func TestAdmitRespectsRemainingResetFeedback(t *testing.T) {
	ResetRuntimeCounters()
	headers := http.Header{}
	headers.Set("X-RateLimit-Remaining", "0")
	headers.Set("X-RateLimit-Reset", "1")
	Feedback(GitHub, "core", http.StatusForbidden, headers)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := Admit(ctx, GitHub, "core", http.MethodGet, "/rate_limit")
	if err == nil {
		t.Fatalf("expected admit to block during reset cooldown")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got: %v", err)
	}
}

func TestParseID(t *testing.T) {
	id, ok := ParseID("twitter")
	if !ok {
		t.Fatalf("expected parse success")
	}
	if id != SocialX {
		t.Fatalf("unexpected parsed id: %s", id)
	}
	playID, ok := ParseID("google-play")
	if !ok {
		t.Fatalf("expected parse success for google play")
	}
	if playID != GooglePlay {
		t.Fatalf("unexpected parsed id for google play: %s", playID)
	}
}

func TestCircuitBreakerOpensAfterRepeatedFailures(t *testing.T) {
	ResetRuntimeCounters()
	for i := 0; i < breakerFailureThreshold; i++ {
		Feedback(GitHub, "core", http.StatusServiceUnavailable, nil)
	}
	err := Admit(context.Background(), GitHub, "core", http.MethodGet, "/repos/acme/repo")
	if err == nil {
		t.Fatalf("expected breaker open error")
	}
}

func TestCircuitBreakerClosesOnSuccess(t *testing.T) {
	ResetRuntimeCounters()
	for i := 0; i < breakerFailureThreshold; i++ {
		Feedback(GitHub, "core", http.StatusServiceUnavailable, nil)
	}
	Feedback(GitHub, "core", http.StatusOK, nil)
	if err := Admit(context.Background(), GitHub, "core", http.MethodGet, "/repos/acme/repo"); err != nil {
		t.Fatalf("expected breaker closed after success, got: %v", err)
	}
	entries := BreakerSnapshot()
	if len(entries) == 0 {
		t.Fatalf("expected breaker snapshot entries")
	}
	if entries[0].State != "closed" {
		t.Fatalf("expected closed state in snapshot, got: %s", entries[0].State)
	}
}

func TestHealthSnapshotTracksLatencyAndStatus(t *testing.T) {
	ResetRuntimeCounters()
	FeedbackWithLatency(GitHub, "core", http.StatusOK, nil, 100*time.Millisecond)
	FeedbackWithLatency(GitHub, "core", http.StatusTooManyRequests, nil, 220*time.Millisecond)
	FeedbackWithLatency(GitHub, "core", http.StatusBadGateway, nil, 340*time.Millisecond)
	entries := HealthSnapshot(GitHub)
	if len(entries) != 1 {
		t.Fatalf("expected one health entry, got: %d", len(entries))
	}
	entry := entries[0]
	if entry.Requests != 3 {
		t.Fatalf("expected requests=3, got: %d", entry.Requests)
	}
	if entry.Success != 1 {
		t.Fatalf("expected success=1, got: %d", entry.Success)
	}
	if entry.TooManyRequests != 1 {
		t.Fatalf("expected 429=1, got: %d", entry.TooManyRequests)
	}
	if entry.ServerErrors5xx != 1 {
		t.Fatalf("expected 5xx=1, got: %d", entry.ServerErrors5xx)
	}
	if entry.P95LatencyMS < entry.P50LatencyMS {
		t.Fatalf("expected p95 >= p50, got p50=%d p95=%d", entry.P50LatencyMS, entry.P95LatencyMS)
	}
}

func TestCapabilitiesSnapshot(t *testing.T) {
	snapshot := CapabilitiesSnapshot(Stripe, GitHub)
	if !snapshot[Stripe].SupportsIdempotency {
		t.Fatalf("expected stripe to support idempotency")
	}
	if !snapshot[GitHub].SupportsRaw {
		t.Fatalf("expected github to support raw")
	}
}

func TestAPIVersionPolicyStatusNoExpiry(t *testing.T) {
	warnings, errs := APIVersionPolicyStatus(time.Now().UTC())
	for _, warning := range warnings {
		t.Logf("version policy warning: %s", warning)
	}
	if len(errs) > 0 {
		t.Fatalf("api version policy errors: %v", errs)
	}
}

func TestAPIVersionPolicyCoverage(t *testing.T) {
	missing, invalid := APIVersionPolicyCoverage(DefaultIDs()...)
	if len(missing) > 0 {
		t.Fatalf("missing api version policies: %v", missing)
	}
	if len(invalid) > 0 {
		t.Fatalf("invalid api version policies: %v", invalid)
	}
}

func TestCacheStoreLookupAndInvalidate(t *testing.T) {
	ResetRuntimeCounters()
	headers := http.Header{}
	headers.Set("X-Test", "ok")

	CacheStore(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo", http.StatusOK, "200 OK", headers, `{"ok":true}`)
	code, status, gotHeaders, body, ok := CacheLookup(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo")
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if code != http.StatusOK || status != "200 OK" || body != `{"ok":true}` {
		t.Fatalf("unexpected cached response: code=%d status=%q body=%q", code, status, body)
	}
	if gotHeaders.Get("X-Test") != "ok" {
		t.Fatalf("expected cached headers")
	}

	CacheInvalidate(GitHub, "core")
	if _, _, _, _, ok := CacheLookup(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo"); ok {
		t.Fatalf("expected cache miss after invalidate")
	}
}

func TestCacheInvalidateIsSubjectScoped(t *testing.T) {
	ResetRuntimeCounters()
	headers := http.Header{}
	headers.Set("X-Test", "ok")

	CacheStore(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo", http.StatusOK, "200 OK", headers, `{"scope":"core"}`)
	CacheStore(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/other", http.StatusOK, "200 OK", headers, `{"scope":"core"}`)
	CacheStore(GitHub, "billing", http.MethodGet, "https://api.github.com/orgs/acme", http.StatusOK, "200 OK", headers, `{"scope":"billing"}`)

	CacheInvalidate(GitHub, "core")
	if _, _, _, _, ok := CacheLookup(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo"); ok {
		t.Fatalf("expected first core entry to be invalidated")
	}
	if _, _, _, _, ok := CacheLookup(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/other"); ok {
		t.Fatalf("expected second core entry to be invalidated")
	}
	if _, _, _, _, ok := CacheLookup(GitHub, "billing", http.MethodGet, "https://api.github.com/orgs/acme"); !ok {
		t.Fatalf("expected non-core subject cache entry to remain")
	}
}

func TestCacheStoreSkipsNoStoreAndUnsafe(t *testing.T) {
	ResetRuntimeCounters()
	noStoreHeaders := http.Header{}
	noStoreHeaders.Set("Cache-Control", "no-store")
	CacheStore(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo", http.StatusOK, "200 OK", noStoreHeaders, `{"ok":true}`)
	if _, _, _, _, ok := CacheLookup(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo"); ok {
		t.Fatalf("expected no-store response to bypass cache")
	}

	CacheStore(GitHub, "core", http.MethodPost, "https://api.github.com/repos/acme/repo", http.StatusOK, "200 OK", nil, `{"ok":true}`)
	if _, _, _, _, ok := CacheLookup(GitHub, "core", http.MethodPost, "https://api.github.com/repos/acme/repo"); ok {
		t.Fatalf("expected unsafe method to bypass cache")
	}
}

func TestCacheLookupExpiresEntry(t *testing.T) {
	ResetRuntimeCounters()
	oldTTL := providerCacheTTLs[GitHub]
	providerCacheTTLs[GitHub] = 10 * time.Millisecond
	defer func() {
		providerCacheTTLs[GitHub] = oldTTL
	}()

	CacheStore(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo", http.StatusOK, "200 OK", nil, `{"ok":true}`)
	time.Sleep(20 * time.Millisecond)
	if _, _, _, _, ok := CacheLookup(GitHub, "core", http.MethodGet, "https://api.github.com/repos/acme/repo"); ok {
		t.Fatalf("expected cache entry to expire")
	}
}

func TestAcquireRespectsProviderConcurrencyLimit(t *testing.T) {
	ResetRuntimeCounters()
	oldLimit := providerConcurrencyLimits[GitHub]
	providerConcurrencyLimits[GitHub] = 1
	defer func() {
		providerConcurrencyLimits[GitHub] = oldLimit
	}()

	release, err := Acquire(context.Background(), GitHub, "core", http.MethodGet, "/repos/acme/repo")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, GitHub, "core", http.MethodGet, "/repos/acme/repo"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected second acquire to block with deadline exceeded, got: %v", err)
	}
}

func TestAcquireCommandConcurrencyIsolation(t *testing.T) {
	ResetRuntimeCounters()
	oldProviderLimit := providerConcurrencyLimits[GitHub]
	oldCommandLimit := providerCommandConcurrencyLimits[GitHub]
	providerConcurrencyLimits[GitHub] = 4
	providerCommandConcurrencyLimits[GitHub] = 1
	defer func() {
		providerConcurrencyLimits[GitHub] = oldProviderLimit
		providerCommandConcurrencyLimits[GitHub] = oldCommandLimit
	}()

	release, err := Acquire(context.Background(), GitHub, "core", http.MethodGet, "/repos/acme/repo")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, GitHub, "core", http.MethodGet, "/repos/acme/other"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected same command key to block, got: %v", err)
	}

	otherRelease, err := Acquire(context.Background(), GitHub, "core", http.MethodGet, "/orgs/acme/repos")
	if err != nil {
		t.Fatalf("expected different command key to pass, got: %v", err)
	}
	otherRelease()
}

func TestGuardrailFeedbackWithoutAcquire(t *testing.T) {
	ResetRuntimeCounters()
	FeedbackWithLatency(GitHub, "core", http.StatusOK, nil, 25*time.Millisecond)
	entries := GuardrailSnapshot(GitHub)
	if len(entries) != 1 {
		t.Fatalf("expected one guardrail entry, got: %d", len(entries))
	}
	if entries[0].FeedbackWithoutAcquire != 1 {
		t.Fatalf("expected feedback_without_acquire=1, got: %d", entries[0].FeedbackWithoutAcquire)
	}
}

func TestGuardrailAcquireFeedbackReleaseClean(t *testing.T) {
	ResetRuntimeCounters()
	release, err := Acquire(context.Background(), GitHub, "core", http.MethodGet, "/repos/acme/repo")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	FeedbackWithLatency(GitHub, "core", http.StatusOK, nil, 25*time.Millisecond)
	release()

	entries := GuardrailSnapshot(GitHub)
	if len(entries) != 0 {
		t.Fatalf("expected no guardrail violations, got: %#v", entries)
	}
}
