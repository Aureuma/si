package providers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ID string

const (
	Cloudflare      ID = "cloudflare"
	GitHub          ID = "github"
	GooglePlaces    ID = "google_places"
	GooglePlay      ID = "google_play"
	YouTube         ID = "youtube"
	Stripe          ID = "stripe"
	SocialFacebook  ID = "social_facebook"
	SocialInstagram ID = "social_instagram"
	SocialX         ID = "social_x"
	SocialLinkedIn  ID = "social_linkedin"
	SocialReddit    ID = "social_reddit"
	WorkOS          ID = "workos"
	AWSIAM          ID = "aws_iam"
	GCPServiceUsage ID = "gcp_serviceusage"
	OpenAI          ID = "openai"
	OCICore         ID = "oci_core"
)

type Spec struct {
	BaseURL       string
	UploadBaseURL string
	APIVersion    string
	UserAgent     string
	Accept        string
	AuthStyle     string

	RequestIDHeaders []string
	DefaultHeaders   map[string]string

	RateLimitPerSecond float64
	RateLimitBurst     int

	PublicProbePath   string
	PublicProbeMethod string
}

type Capability struct {
	SupportsPagination  bool `json:"supports_pagination"`
	SupportsBulk        bool `json:"supports_bulk"`
	SupportsIdempotency bool `json:"supports_idempotency"`
	SupportsRaw         bool `json:"supports_raw"`
}

type APIVersionPolicy struct {
	Provider     ID
	Version      string
	ExemptReason string
	ReviewBy     time.Time
}

var defaultSpecs = map[ID]Spec{
	Cloudflare: {
		BaseURL:            "https://api.cloudflare.com/client/v4",
		APIVersion:         "v4",
		UserAgent:          "si-cloudflare/1.0",
		Accept:             "application/json",
		RequestIDHeaders:   []string{"CF-Ray", "X-Request-ID"},
		RateLimitPerSecond: 4.0,
		RateLimitBurst:     8,
		PublicProbePath:    "/ips",
		PublicProbeMethod:  "GET",
	},
	GitHub: {
		BaseURL:            "https://api.github.com",
		APIVersion:         "2022-11-28",
		UserAgent:          "si-github/1.0",
		Accept:             "application/vnd.github+json",
		RequestIDHeaders:   []string{"X-GitHub-Request-Id"},
		DefaultHeaders:     map[string]string{"X-GitHub-Api-Version": "2022-11-28"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/zen",
		PublicProbeMethod:  "GET",
	},
	GooglePlaces: {
		BaseURL:            "https://places.googleapis.com",
		APIVersion:         "v1",
		UserAgent:          "si-google-places/1.0",
		Accept:             "application/json",
		RequestIDHeaders:   []string{"X-Request-Id", "X-Google-Request-Id"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/$discovery/rest?version=v1",
		PublicProbeMethod:  "GET",
	},
	GooglePlay: {
		BaseURL:            "https://androidpublisher.googleapis.com",
		UploadBaseURL:      "https://androidpublisher.googleapis.com",
		APIVersion:         "v3",
		UserAgent:          "si-google-play/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"X-Google-Request-Id", "X-Request-Id"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/$discovery/rest?version=v3",
		PublicProbeMethod:  "GET",
	},
	YouTube: {
		BaseURL:            "https://www.googleapis.com",
		UploadBaseURL:      "https://www.googleapis.com/upload",
		APIVersion:         "v3",
		UserAgent:          "si-youtube/1.0",
		Accept:             "application/json",
		RequestIDHeaders:   []string{"X-Google-Request-Id", "X-Request-Id"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/discovery/v1/apis/youtube/v3/rest",
		PublicProbeMethod:  "GET",
	},
	Stripe: {
		BaseURL:            "https://api.stripe.com",
		APIVersion:         "account-default",
		UserAgent:          "si-stripe/1.0",
		Accept:             "application/json",
		RequestIDHeaders:   []string{"Request-Id"},
		RateLimitPerSecond: 8.0,
		RateLimitBurst:     16,
		PublicProbePath:    "/v1/charges",
		PublicProbeMethod:  "GET",
	},
	SocialFacebook: {
		BaseURL:            "https://graph.facebook.com",
		APIVersion:         "v22.0",
		UserAgent:          "si-social-facebook/1.0",
		Accept:             "application/json",
		AuthStyle:          "query",
		RequestIDHeaders:   []string{"x-fb-trace-id", "X-Request-ID"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/platform",
		PublicProbeMethod:  "GET",
	},
	SocialInstagram: {
		BaseURL:            "https://graph.facebook.com",
		APIVersion:         "v22.0",
		UserAgent:          "si-social-instagram/1.0",
		Accept:             "application/json",
		AuthStyle:          "query",
		RequestIDHeaders:   []string{"x-fb-trace-id", "X-Request-ID"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/oauth/access_token",
		PublicProbeMethod:  "GET",
	},
	SocialX: {
		BaseURL:            "https://api.twitter.com",
		APIVersion:         "2",
		UserAgent:          "si-social-x/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"x-request-id"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/2/openapi.json",
		PublicProbeMethod:  "GET",
	},
	SocialLinkedIn: {
		BaseURL:            "https://api.linkedin.com",
		APIVersion:         "v2",
		UserAgent:          "si-social-linkedin/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"x-li-request-id"},
		DefaultHeaders:     map[string]string{"X-Restli-Protocol-Version": "2.0.0"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/v2/me",
		PublicProbeMethod:  "GET",
	},
	SocialReddit: {
		BaseURL:            "https://oauth.reddit.com",
		UserAgent:          "si-social-reddit/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"x-request-id"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/api/v1/scopes",
		PublicProbeMethod:  "GET",
	},
	WorkOS: {
		BaseURL:            "https://api.workos.com",
		APIVersion:         "v1",
		UserAgent:          "si-workos/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"X-Request-ID", "X-Request-Id"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/organizations?limit=1",
		PublicProbeMethod:  "GET",
	},
	AWSIAM: {
		BaseURL:            "https://iam.amazonaws.com",
		APIVersion:         "2010-05-08",
		UserAgent:          "si-aws-iam/1.0",
		Accept:             "application/xml",
		AuthStyle:          "sigv4",
		RequestIDHeaders:   []string{"x-amzn-RequestId", "x-amz-request-id"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/",
		PublicProbeMethod:  "GET",
	},
	GCPServiceUsage: {
		BaseURL:            "https://serviceusage.googleapis.com",
		APIVersion:         "v1",
		UserAgent:          "si-gcp-serviceusage/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"X-Request-Id", "X-Google-Request-Id"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/v1/services?filter=state:ENABLED&pageSize=1",
		PublicProbeMethod:  "GET",
	},
	OpenAI: {
		BaseURL:            "https://api.openai.com",
		APIVersion:         "v1",
		UserAgent:          "si-openai/1.0",
		Accept:             "application/json",
		AuthStyle:          "bearer",
		RequestIDHeaders:   []string{"x-request-id"},
		RateLimitPerSecond: 2.0,
		RateLimitBurst:     4,
		PublicProbePath:    "/v1/models?limit=1",
		PublicProbeMethod:  "GET",
	},
	OCICore: {
		BaseURL:            "https://iaas.us-ashburn-1.oraclecloud.com",
		APIVersion:         "20160918",
		UserAgent:          "si-oci-core/1.0",
		Accept:             "application/json",
		AuthStyle:          "signature",
		RequestIDHeaders:   []string{"opc-request-id"},
		RateLimitPerSecond: 1.0,
		RateLimitBurst:     2,
		PublicProbePath:    "/20160918/instances",
		PublicProbeMethod:  "GET",
	},
}

var defaultCapabilities = map[ID]Capability{
	Cloudflare: {
		SupportsPagination:  true,
		SupportsBulk:        true,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	GitHub: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	GooglePlaces: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	GooglePlay: {
		SupportsPagination:  true,
		SupportsBulk:        true,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	YouTube: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	Stripe: {
		SupportsPagination:  true,
		SupportsBulk:        true,
		SupportsIdempotency: true,
		SupportsRaw:         true,
	},
	SocialFacebook: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	SocialInstagram: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	SocialX: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	SocialLinkedIn: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	SocialReddit: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	WorkOS: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	AWSIAM: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	GCPServiceUsage: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
	OpenAI: {
		SupportsPagination:  true,
		SupportsBulk:        true,
		SupportsIdempotency: true,
		SupportsRaw:         true,
	},
	OCICore: {
		SupportsPagination:  true,
		SupportsBulk:        false,
		SupportsIdempotency: false,
		SupportsRaw:         true,
	},
}

var apiVersionPolicies = []APIVersionPolicy{
	{Provider: Cloudflare, Version: "v4", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: GitHub, Version: "2022-11-28", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: GooglePlaces, Version: "v1", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: GooglePlay, Version: "v3", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: YouTube, Version: "v3", ReviewBy: time.Date(2027, time.January, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: Stripe, Version: "account-default", ExemptReason: "Stripe API version is pinned by account/workspace unless explicitly overridden.", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: SocialFacebook, Version: "v22.0", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: SocialInstagram, Version: "v22.0", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: SocialX, Version: "2", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: SocialLinkedIn, Version: "v2", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: SocialReddit, ExemptReason: "Reddit OAuth API is unversioned; pinning occurs by endpoint contracts.", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: WorkOS, Version: "v1", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: AWSIAM, Version: "2010-05-08", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: GCPServiceUsage, Version: "v1", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: OpenAI, Version: "v1", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
	{Provider: OCICore, Version: "20160918", ReviewBy: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)},
}

var providerCacheTTLs = map[ID]time.Duration{
	Cloudflare:      3 * time.Second,
	GitHub:          3 * time.Second,
	GooglePlaces:    3 * time.Second,
	GooglePlay:      3 * time.Second,
	YouTube:         3 * time.Second,
	SocialFacebook:  3 * time.Second,
	SocialInstagram: 3 * time.Second,
	SocialX:         3 * time.Second,
	SocialLinkedIn:  3 * time.Second,
	SocialReddit:    3 * time.Second,
	WorkOS:          3 * time.Second,
	AWSIAM:          3 * time.Second,
	GCPServiceUsage: 3 * time.Second,
	OpenAI:          3 * time.Second,
	OCICore:         3 * time.Second,
}

var providerConcurrencyLimits = map[ID]int{
	Cloudflare:      4,
	GitHub:          2,
	GooglePlaces:    3,
	GooglePlay:      2,
	YouTube:         2,
	Stripe:          8,
	SocialFacebook:  2,
	SocialInstagram: 2,
	SocialX:         2,
	SocialLinkedIn:  2,
	SocialReddit:    2,
	WorkOS:          2,
	AWSIAM:          2,
	GCPServiceUsage: 2,
	OpenAI:          2,
	OCICore:         2,
}

var providerCommandConcurrencyLimits = map[ID]int{
	Cloudflare:      2,
	GitHub:          1,
	GooglePlaces:    2,
	GooglePlay:      1,
	YouTube:         1,
	Stripe:          2,
	SocialFacebook:  1,
	SocialInstagram: 1,
	SocialX:         1,
	SocialLinkedIn:  1,
	SocialReddit:    1,
	WorkOS:          1,
	AWSIAM:          1,
	GCPServiceUsage: 1,
	OpenAI:          1,
	OCICore:         1,
}

var (
	limiterMu sync.Mutex
	limiters  = map[string]*adaptiveLimiter{}

	concurrencyMu      sync.Mutex
	providerSemaphores = map[string]chan struct{}{}
	commandSemaphores  = map[string]chan struct{}{}

	adaptiveMu    sync.Mutex
	cooldownUntil = map[string]time.Time{}

	breakerMu sync.Mutex
	breakers  = map[string]breakerState{}

	metricsMu sync.Mutex
	metrics   = map[string]*trafficMetrics{}

	cacheMu            sync.RWMutex
	responseCache      = map[string]cachedResponse{}
	cacheKeysBySubject = map[string]map[string]struct{}{}

	guardrailMu            sync.Mutex
	inFlightRequests       = map[string]int64{}
	feedbackWithoutAcquire = map[string]int64{}
	releaseWithoutAcquire  = map[string]int64{}
)

const (
	breakerFailureThreshold = 5
	breakerOpenWindow       = 30 * time.Second
)

type breakerState struct {
	State               string
	ConsecutiveFailures int
	OpenUntil           time.Time
}

type BreakerSnapshotEntry struct {
	Provider            ID        `json:"provider"`
	Subject             string    `json:"subject"`
	State               string    `json:"state"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	OpenUntil           time.Time `json:"open_until,omitempty"`
}

type HealthSnapshotEntry struct {
	Provider            ID        `json:"provider"`
	Subject             string    `json:"subject"`
	Requests            int64     `json:"requests"`
	Success             int64     `json:"success"`
	TooManyRequests     int64     `json:"too_many_requests"`
	ClientErrors4xx     int64     `json:"client_errors_4xx"`
	ServerErrors5xx     int64     `json:"server_errors_5xx"`
	LastStatusCode      int       `json:"last_status_code,omitempty"`
	LastSeen            time.Time `json:"last_seen,omitempty"`
	AvgLatencyMS        int64     `json:"avg_latency_ms,omitempty"`
	P50LatencyMS        int64     `json:"p50_latency_ms,omitempty"`
	P95LatencyMS        int64     `json:"p95_latency_ms,omitempty"`
	CircuitState        string    `json:"circuit_state"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	OpenUntil           time.Time `json:"open_until,omitempty"`
	CooldownUntil       time.Time `json:"cooldown_until,omitempty"`
}

type GuardrailSnapshotEntry struct {
	Provider               ID     `json:"provider"`
	Subject                string `json:"subject"`
	InFlight               int64  `json:"in_flight"`
	FeedbackWithoutAcquire int64  `json:"feedback_without_acquire"`
	ReleaseWithoutAcquire  int64  `json:"release_without_acquire"`
}

type trafficMetrics struct {
	Requests        int64
	Success         int64
	TooManyRequests int64
	ClientErrors4xx int64
	ServerErrors5xx int64
	LastStatusCode  int
	LastSeen        time.Time
	LatencyTotalMS  int64
	LatencySamples  []int64
}

type cachedResponse struct {
	StatusCode int
	Status     string
	Headers    http.Header
	Body       string
	ExpiresAt  time.Time
}

func Resolve(id ID) Spec {
	base, ok := defaultSpecs[id]
	if !ok {
		return Spec{}
	}
	return base
}

func DefaultIDs() []ID {
	keys := make([]ID, 0, len(defaultSpecs))
	for id := range defaultSpecs {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return string(keys[i]) < string(keys[j]) })
	return keys
}

func Acquire(ctx context.Context, id ID, subject string, method string, path string) (func(), error) {
	subject = normalizeSubject(subject)
	key := stateKey(id, subject)
	if err := allowThroughBreaker(key); err != nil {
		return nil, err
	}
	if err := waitAdaptiveCooldown(ctx, key); err != nil {
		return nil, err
	}
	releaseFns := make([]func(), 0, 3)
	providerSem := providerSemaphoreForKey(key, providerConcurrencyLimit(id))
	if err := acquireSemaphore(ctx, providerSem); err != nil {
		return nil, err
	}
	if providerSem != nil {
		releaseFns = append(releaseFns, func() { releaseSemaphore(providerSem) })
	}
	commandSem := commandSemaphoreForKey(commandStateKey(id, subject, method, path), commandConcurrencyLimit(id, method))
	if err := acquireSemaphore(ctx, commandSem); err != nil {
		releaseAll(releaseFns)
		return nil, err
	}
	if commandSem != nil {
		releaseFns = append(releaseFns, func() { releaseSemaphore(commandSem) })
	}
	spec := Resolve(id)
	if spec.RateLimitPerSecond <= 0 {
		return buildRelease(releaseFns), nil
	}
	limiter := limiterForKey(key, spec)
	if limiter == nil {
		return buildRelease(releaseFns), nil
	}
	if err := limiter.Wait(ctx); err != nil {
		releaseAll(releaseFns)
		return nil, err
	}
	markAcquire(key)
	releaseFns = append(releaseFns, func() { markRelease(key) })
	return buildRelease(releaseFns), nil
}

func Admit(ctx context.Context, id ID, subject string, method string, path string) error {
	release, err := Acquire(ctx, id, subject, method, path)
	if err != nil {
		return err
	}
	release()
	return nil
}

func Feedback(id ID, subject string, statusCode int, headers http.Header) {
	FeedbackWithLatency(id, subject, statusCode, headers, 0)
}

func FeedbackWithLatency(id ID, subject string, statusCode int, headers http.Header, latency time.Duration) {
	subject = normalizeSubject(subject)
	key := stateKey(id, subject)
	markFeedback(key)
	now := time.Now().UTC()
	updateBreakerFromStatus(key, statusCode, headers, now)
	updateMetrics(key, statusCode, latency, now)

	if statusCode >= 200 && statusCode < 400 {
		clearExpiredCooldown(key, now)
	}

	candidate := time.Time{}
	if retryUntil, ok := parseRetryAfter(headers, now); ok {
		candidate = maxTime(candidate, retryUntil)
	}
	if remaining, hasRemaining := parseHeaderInt(headers,
		"X-RateLimit-Remaining",
		"RateLimit-Remaining",
		"X-Rate-Limit-Remaining",
	); hasRemaining {
		if remaining <= 1 {
			if resetAt, ok := parseRateLimitReset(headers, now); ok {
				candidate = maxTime(candidate, resetAt)
			}
		}
	}
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable {
		if resetAt, ok := parseRateLimitReset(headers, now); ok {
			candidate = maxTime(candidate, resetAt)
		}
	}
	if candidate.IsZero() {
		switch statusCode {
		case http.StatusTooManyRequests:
			candidate = now.Add(2 * time.Second)
		case http.StatusServiceUnavailable:
			candidate = now.Add(1 * time.Second)
		}
	}
	if !candidate.IsZero() && candidate.After(now) {
		setCooldownMax(key, candidate)
	}
	adaptLimiterRate(id, key, statusCode, headers, now)
}

func ResetRuntimeCounters() {
	limiterMu.Lock()
	limiters = map[string]*adaptiveLimiter{}
	limiterMu.Unlock()

	concurrencyMu.Lock()
	providerSemaphores = map[string]chan struct{}{}
	commandSemaphores = map[string]chan struct{}{}
	concurrencyMu.Unlock()

	adaptiveMu.Lock()
	cooldownUntil = map[string]time.Time{}
	adaptiveMu.Unlock()

	breakerMu.Lock()
	breakers = map[string]breakerState{}
	breakerMu.Unlock()

	metricsMu.Lock()
	metrics = map[string]*trafficMetrics{}
	metricsMu.Unlock()

	cacheMu.Lock()
	responseCache = map[string]cachedResponse{}
	cacheKeysBySubject = map[string]map[string]struct{}{}
	cacheMu.Unlock()

	guardrailMu.Lock()
	inFlightRequests = map[string]int64{}
	feedbackWithoutAcquire = map[string]int64{}
	releaseWithoutAcquire = map[string]int64{}
	guardrailMu.Unlock()
}

func normalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "default"
	}
	return subject
}

func stateKey(id ID, subject string) string {
	return fmt.Sprintf("%s|%s", id, normalizeSubject(subject))
}

func commandStateKey(id ID, subject string, method string, path string) string {
	return fmt.Sprintf("%s|%s|%s|%s", id, normalizeSubject(subject), normalizeMethod(method), normalizeCommandPath(path))
}

func normalizeMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return http.MethodGet
	}
	return method
}

func normalizeCommandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	first := strings.TrimSpace(segments[0])
	if first == "" {
		return "/"
	}
	if isVersionSegment(first) && len(segments) > 1 {
		first = strings.TrimSpace(segments[1])
	}
	if first == "" {
		return "/"
	}
	return "/" + first
}

func isVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for i := 1; i < len(segment); i++ {
		ch := segment[i]
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	return true
}

func providerConcurrencyLimit(id ID) int {
	limit := providerConcurrencyLimits[id]
	if limit <= 0 {
		return 0
	}
	return limit
}

func commandConcurrencyLimit(id ID, method string) int {
	limit := providerCommandConcurrencyLimits[id]
	if limit <= 0 {
		return 0
	}
	if !isCacheableMethod(method) && limit > 1 {
		return 1
	}
	return limit
}

func providerSemaphoreForKey(key string, limit int) chan struct{} {
	return semaphoreForKey(&providerSemaphores, key, limit)
}

func commandSemaphoreForKey(key string, limit int) chan struct{} {
	return semaphoreForKey(&commandSemaphores, key, limit)
}

func semaphoreForKey(store *map[string]chan struct{}, key string, limit int) chan struct{} {
	if limit <= 0 {
		return nil
	}
	concurrencyMu.Lock()
	defer concurrencyMu.Unlock()
	if existing, ok := (*store)[key]; ok && cap(existing) == limit {
		return existing
	}
	sem := make(chan struct{}, limit)
	(*store)[key] = sem
	return sem
}

func acquireSemaphore(ctx context.Context, sem chan struct{}) error {
	if sem == nil {
		return nil
	}
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseSemaphore(sem chan struct{}) {
	if sem == nil {
		return
	}
	select {
	case <-sem:
	default:
	}
}

func buildRelease(releaseFns []func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseAll(releaseFns)
		})
	}
}

func releaseAll(releaseFns []func()) {
	for i := len(releaseFns) - 1; i >= 0; i-- {
		releaseFns[i]()
	}
}

func markAcquire(key string) {
	guardrailMu.Lock()
	inFlightRequests[key]++
	guardrailMu.Unlock()
}

func markRelease(key string) {
	guardrailMu.Lock()
	defer guardrailMu.Unlock()
	inFlight := inFlightRequests[key]
	if inFlight <= 0 {
		releaseWithoutAcquire[key]++
		return
	}
	inFlight--
	if inFlight == 0 {
		delete(inFlightRequests, key)
		return
	}
	inFlightRequests[key] = inFlight
}

func markFeedback(key string) {
	guardrailMu.Lock()
	defer guardrailMu.Unlock()
	if inFlightRequests[key] <= 0 {
		feedbackWithoutAcquire[key]++
	}
}

func waitAdaptiveCooldown(ctx context.Context, key string) error {
	for {
		adaptiveMu.Lock()
		until := cooldownUntil[key]
		adaptiveMu.Unlock()
		if until.IsZero() || !until.After(time.Now().UTC()) {
			return nil
		}
		wait := time.Until(until)
		if wait <= 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func clearExpiredCooldown(key string, now time.Time) {
	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()
	until, ok := cooldownUntil[key]
	if !ok {
		return
	}
	if !until.After(now) {
		delete(cooldownUntil, key)
	}
}

func setCooldownMax(key string, until time.Time) {
	adaptiveMu.Lock()
	defer adaptiveMu.Unlock()
	current := cooldownUntil[key]
	if current.After(until) {
		return
	}
	cooldownUntil[key] = until
}

func allowThroughBreaker(key string) error {
	now := time.Now().UTC()
	breakerMu.Lock()
	defer breakerMu.Unlock()
	state := breakers[key]
	if state.State != "open" {
		return nil
	}
	if !state.OpenUntil.After(now) {
		state.State = "closed"
		state.OpenUntil = time.Time{}
		state.ConsecutiveFailures = 0
		breakers[key] = state
		return nil
	}
	return fmt.Errorf("provider circuit breaker open for %s until %s", key, state.OpenUntil.Format(time.RFC3339))
}

func updateBreakerFromStatus(key string, statusCode int, headers http.Header, now time.Time) {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	state := breakers[key]
	if state.State == "" {
		state.State = "closed"
	}
	switch {
	case statusCode >= 200 && statusCode < 400:
		state.State = "closed"
		state.ConsecutiveFailures = 0
		state.OpenUntil = time.Time{}
	case statusCode == http.StatusTooManyRequests || statusCode >= 500:
		state.ConsecutiveFailures++
		if state.ConsecutiveFailures >= breakerFailureThreshold {
			state.State = "open"
			candidate := now.Add(breakerOpenWindow)
			if retryUntil, ok := parseRetryAfter(headers, now); ok && retryUntil.After(candidate) {
				candidate = retryUntil
			}
			state.OpenUntil = candidate
		}
	}
	breakers[key] = state
}

func updateMetrics(key string, statusCode int, latency time.Duration, now time.Time) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	entry := metrics[key]
	if entry == nil {
		entry = &trafficMetrics{}
		metrics[key] = entry
	}
	entry.Requests++
	entry.LastStatusCode = statusCode
	entry.LastSeen = now
	switch {
	case statusCode >= 200 && statusCode < 400:
		entry.Success++
	case statusCode == http.StatusTooManyRequests:
		entry.TooManyRequests++
	case statusCode >= 500:
		entry.ServerErrors5xx++
	case statusCode >= 400:
		entry.ClientErrors4xx++
	}
	if latency > 0 {
		ms := latency.Milliseconds()
		if ms < 0 {
			ms = 0
		}
		entry.LatencyTotalMS += ms
		entry.LatencySamples = append(entry.LatencySamples, ms)
		const maxSamples = 256
		if len(entry.LatencySamples) > maxSamples {
			entry.LatencySamples = append([]int64{}, entry.LatencySamples[len(entry.LatencySamples)-maxSamples:]...)
		}
	}
}

func limiterForKey(key string, spec Spec) *adaptiveLimiter {
	limiterMu.Lock()
	defer limiterMu.Unlock()
	if existing, ok := limiters[key]; ok {
		return existing
	}
	if spec.RateLimitPerSecond <= 0 {
		return nil
	}
	burst := spec.RateLimitBurst
	if burst <= 0 {
		burst = 1
	}
	limiter := newAdaptiveLimiter(spec.RateLimitPerSecond, burst)
	limiters[key] = limiter
	return limiter
}

func adaptLimiterRate(id ID, key string, statusCode int, headers http.Header, now time.Time) {
	spec := Resolve(id)
	limiter := limiterForKey(key, spec)
	if limiter == nil {
		return
	}
	current := float64(limiter.Limit())
	if current <= 0 {
		current = spec.RateLimitPerSecond
	}
	if statusCode == http.StatusTooManyRequests {
		next := current / 2
		if next < 0.2 {
			next = 0.2
		}
		limiter.SetLimitAt(now, next)
		return
	}
	remaining, okRemaining := parseHeaderInt(headers,
		"X-RateLimit-Remaining",
		"RateLimit-Remaining",
		"X-Rate-Limit-Remaining",
	)
	if !okRemaining || remaining < 0 {
		return
	}
	resetAt, okReset := parseRateLimitReset(headers, now)
	if !okReset || !resetAt.After(now) {
		return
	}
	seconds := resetAt.Sub(now).Seconds()
	if seconds <= 0 {
		return
	}
	target := float64(remaining) / seconds
	if target <= 0 {
		return
	}
	min := 0.2
	max := spec.RateLimitPerSecond * 4
	if max <= 0 {
		max = 8.0
	}
	if target < min {
		target = min
	}
	if target > max {
		target = max
	}
	limiter.SetLimitAt(now, target)
}

type adaptiveLimiter struct {
	mu    sync.Mutex
	rate  float64
	burst float64
	last  time.Time
	token float64
}

func newAdaptiveLimiter(ratePerSecond float64, burst int) *adaptiveLimiter {
	if ratePerSecond <= 0 {
		ratePerSecond = 0.2
	}
	if burst <= 0 {
		burst = 1
	}
	now := time.Now().UTC()
	return &adaptiveLimiter{
		rate:  ratePerSecond,
		burst: float64(burst),
		last:  now,
		token: float64(burst),
	}
}

func (l *adaptiveLimiter) Limit() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

func (l *adaptiveLimiter) SetLimitAt(now time.Time, next float64) {
	if next <= 0 {
		next = 0.2
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked(now)
	l.rate = next
	if l.token > l.burst {
		l.token = l.burst
	}
}

func (l *adaptiveLimiter) Wait(ctx context.Context) error {
	for {
		delay := l.reserveDelay()
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *adaptiveLimiter) reserveDelay() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now().UTC()
	l.refillLocked(now)
	if l.token >= 1 {
		l.token -= 1
		return 0
	}
	if l.rate <= 0 {
		l.rate = 0.2
	}
	required := 1 - l.token
	seconds := required / l.rate
	if seconds <= 0 {
		seconds = 0.001
	}
	wait := time.Duration(seconds * float64(time.Second))
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	return wait
}

func (l *adaptiveLimiter) refillLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if l.last.IsZero() {
		l.last = now
		return
	}
	if now.Before(l.last) {
		now = l.last
	}
	elapsed := now.Sub(l.last).Seconds()
	if elapsed > 0 {
		l.token += elapsed * l.rate
		if l.token > l.burst {
			l.token = l.burst
		}
		l.last = now
	}
}

func parseRetryAfter(headers http.Header, now time.Time) (time.Time, bool) {
	raw := firstHeader(headers, "Retry-After")
	if raw == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if seconds <= 0 {
			return now, true
		}
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	if when, err := http.ParseTime(raw); err == nil {
		return when.UTC(), true
	}
	return time.Time{}, false
}

func parseRateLimitReset(headers http.Header, now time.Time) (time.Time, bool) {
	raw := firstHeader(headers,
		"X-RateLimit-Reset",
		"RateLimit-Reset",
		"X-Rate-Limit-Reset",
	)
	if raw == "" {
		return time.Time{}, false
	}
	if when, err := http.ParseTime(raw); err == nil {
		return when.UTC(), true
	}
	if iv, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if iv > now.Unix()+60 {
			return time.Unix(iv, 0).UTC(), true
		}
		if iv >= 0 {
			return now.Add(time.Duration(iv) * time.Second), true
		}
	}
	if fv, err := strconv.ParseFloat(raw, 64); err == nil {
		if fv > float64(now.Unix()+60) {
			return time.Unix(int64(fv), 0).UTC(), true
		}
		if fv >= 0 {
			return now.Add(time.Duration(fv * float64(time.Second))), true
		}
	}
	return time.Time{}, false
}

func parseHeaderInt(headers http.Header, keys ...string) (int64, bool) {
	raw := firstHeader(headers, keys...)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func firstHeader(headers http.Header, keys ...string) string {
	if headers == nil {
		return ""
	}
	for _, key := range keys {
		value := strings.TrimSpace(headers.Get(strings.TrimSpace(key)))
		if value != "" {
			return value
		}
	}
	return ""
}

func maxTime(a time.Time, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.After(a) {
		return b
	}
	return a
}

func normalizeID(raw string) ID {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "cloudflare":
		return Cloudflare
	case "github":
		return GitHub
	case "google_places", "googleplaces":
		return GooglePlaces
	case "google_play", "googleplay", "play":
		return GooglePlay
	case "youtube":
		return YouTube
	case "stripe":
		return Stripe
	case "social_facebook", "facebook":
		return SocialFacebook
	case "social_instagram", "instagram":
		return SocialInstagram
	case "social_x", "x", "twitter":
		return SocialX
	case "social_linkedin", "linkedin":
		return SocialLinkedIn
	case "social_reddit", "reddit":
		return SocialReddit
	case "workos":
		return WorkOS
	case "aws", "aws_iam", "iam":
		return AWSIAM
	case "gcp", "gcp_serviceusage", "serviceusage":
		return GCPServiceUsage
	case "openai":
		return OpenAI
	case "oci", "oracle", "oci_core":
		return OCICore
	default:
		return ""
	}
}

func ParseID(raw string) (ID, bool) {
	id := normalizeID(raw)
	if id == "" {
		return "", false
	}
	return id, true
}

func PublicProbe(id ID) (method string, path string, ok bool) {
	spec := Resolve(id)
	method = strings.ToUpper(strings.TrimSpace(spec.PublicProbeMethod))
	path = strings.TrimSpace(spec.PublicProbePath)
	if method == "" {
		method = "GET"
	}
	if path == "" {
		return "", "", false
	}
	return method, path, true
}

func MustResolve(id ID) Spec {
	spec := Resolve(id)
	if strings.TrimSpace(spec.BaseURL) == "" {
		panic(fmt.Sprintf("provider spec missing for %s", id))
	}
	return spec
}

func SpecsSnapshot(ids ...ID) map[ID]Spec {
	if len(ids) == 0 {
		ids = DefaultIDs()
	}
	out := make(map[ID]Spec, len(ids))
	for _, id := range ids {
		out[id] = Resolve(id)
	}
	return out
}

func Capabilities(id ID) Capability {
	return defaultCapabilities[id]
}

func CapabilitiesSnapshot(ids ...ID) map[ID]Capability {
	if len(ids) == 0 {
		ids = DefaultIDs()
	}
	out := make(map[ID]Capability, len(ids))
	for _, id := range ids {
		out[id] = Capabilities(id)
	}
	return out
}

func APIVersionPolicies() []APIVersionPolicy {
	out := make([]APIVersionPolicy, 0, len(apiVersionPolicies))
	for _, policy := range apiVersionPolicies {
		out = append(out, policy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

func APIVersionPolicyStatus(now time.Time) (warnings []string, errorsOut []string) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policies := APIVersionPolicies()
	for _, policy := range policies {
		exemptReason := strings.TrimSpace(policy.ExemptReason)
		expected := strings.TrimSpace(policy.Version)
		if exemptReason == "" && expected == "" {
			errorsOut = append(errorsOut, fmt.Sprintf("%s version policy missing version/exempt_reason", policy.Provider))
			continue
		}
		spec := Resolve(policy.Provider)
		active := strings.TrimSpace(spec.APIVersion)
		if expected != "" && active != expected {
			warnings = append(warnings, fmt.Sprintf("%s version changed: expected=%s active=%s", policy.Provider, expected, firstNonEmpty(active, "-")))
		}
		if policy.ReviewBy.IsZero() {
			continue
		}
		if now.After(policy.ReviewBy) {
			errorsOut = append(errorsOut, fmt.Sprintf("%s version %s review expired on %s", policy.Provider, expected, policy.ReviewBy.Format("2006-01-02")))
			continue
		}
		if now.AddDate(0, 0, 30).After(policy.ReviewBy) {
			warnings = append(warnings, fmt.Sprintf("%s version %s review due by %s", policy.Provider, expected, policy.ReviewBy.Format("2006-01-02")))
		}
	}
	sort.Strings(warnings)
	sort.Strings(errorsOut)
	return warnings, errorsOut
}

func CacheLookup(id ID, subject string, method string, endpoint string) (statusCode int, status string, headers http.Header, body string, ok bool) {
	if !isCacheableMethod(method) {
		return 0, "", nil, "", false
	}
	ttl := cacheTTLFor(id)
	if ttl <= 0 {
		return 0, "", nil, "", false
	}
	key := cacheKey(id, subject, method, endpoint)
	now := time.Now().UTC()
	cacheMu.RLock()
	entry, exists := responseCache[key]
	if !exists {
		cacheMu.RUnlock()
		return 0, "", nil, "", false
	}
	expired := !entry.ExpiresAt.After(now)
	cacheMu.RUnlock()
	if expired {
		cacheMu.Lock()
		if latest, stillExists := responseCache[key]; stillExists && !latest.ExpiresAt.After(now) {
			delete(responseCache, key)
			unindexCachedKey(cacheSubjectKey(id, subject), key)
		}
		cacheMu.Unlock()
		return 0, "", nil, "", false
	}
	return entry.StatusCode, entry.Status, cloneHeader(entry.Headers), entry.Body, true
}

func CacheStore(id ID, subject string, method string, endpoint string, statusCode int, status string, headers http.Header, body string) {
	if !isCacheableMethod(method) {
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		return
	}
	if strings.Contains(strings.ToLower(firstHeader(headers, "Cache-Control")), "no-store") {
		return
	}
	ttl := cacheTTLFor(id)
	if ttl <= 0 {
		return
	}
	key := cacheKey(id, subject, method, endpoint)
	subjectKey := cacheSubjectKey(id, subject)
	cacheMu.Lock()
	responseCache[key] = cachedResponse{
		StatusCode: statusCode,
		Status:     strings.TrimSpace(status),
		Headers:    cloneHeader(headers),
		Body:       body,
		ExpiresAt:  time.Now().UTC().Add(ttl),
	}
	indexCachedKey(subjectKey, key)
	cacheMu.Unlock()
}

func CacheInvalidate(id ID, subject string) {
	subjectKey := cacheSubjectKey(id, subject)
	cacheMu.Lock()
	keys := cacheKeysBySubject[subjectKey]
	if len(keys) == 0 {
		cacheMu.Unlock()
		return
	}
	for key := range keys {
		delete(responseCache, key)
	}
	delete(cacheKeysBySubject, subjectKey)
	cacheMu.Unlock()
}

func cacheTTLFor(id ID) time.Duration {
	return providerCacheTTLs[id]
}

func cacheKey(id ID, subject string, method string, endpoint string) string {
	return fmt.Sprintf("%s|%s|%s|%s", id, normalizeSubject(subject), strings.ToUpper(strings.TrimSpace(method)), strings.TrimSpace(endpoint))
}

func cacheSubjectKey(id ID, subject string) string {
	return fmt.Sprintf("%s|%s", id, normalizeSubject(subject))
}

func indexCachedKey(subjectKey, key string) {
	if strings.TrimSpace(subjectKey) == "" || strings.TrimSpace(key) == "" {
		return
	}
	keys, ok := cacheKeysBySubject[subjectKey]
	if !ok {
		keys = map[string]struct{}{}
		cacheKeysBySubject[subjectKey] = keys
	}
	keys[key] = struct{}{}
}

func unindexCachedKey(subjectKey, key string) {
	if strings.TrimSpace(subjectKey) == "" || strings.TrimSpace(key) == "" {
		return
	}
	keys, ok := cacheKeysBySubject[subjectKey]
	if !ok {
		return
	}
	delete(keys, key)
	if len(keys) == 0 {
		delete(cacheKeysBySubject, subjectKey)
	}
}

func isCacheableMethod(method string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	return method == http.MethodGet || method == http.MethodHead
}

func cloneHeader(in http.Header) http.Header {
	if len(in) == 0 {
		return nil
	}
	out := http.Header{}
	for key, values := range in {
		copied := make([]string, 0, len(values))
		copied = append(copied, values...)
		out[key] = copied
	}
	return out
}

func BreakerSnapshot() []BreakerSnapshotEntry {
	breakerMu.Lock()
	defer breakerMu.Unlock()
	out := make([]BreakerSnapshotEntry, 0, len(breakers))
	for key, state := range breakers {
		provider, subject := parseStateKey(key)
		out = append(out, BreakerSnapshotEntry{
			Provider:            provider,
			Subject:             subject,
			State:               firstNonEmpty(strings.TrimSpace(state.State), "closed"),
			ConsecutiveFailures: state.ConsecutiveFailures,
			OpenUntil:           state.OpenUntil,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func HealthSnapshot(ids ...ID) []HealthSnapshotEntry {
	if len(ids) == 0 {
		ids = DefaultIDs()
	}
	allowed := map[ID]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	breakerMu.Lock()
	breakerCopy := map[string]breakerState{}
	for key, value := range breakers {
		breakerCopy[key] = value
	}
	breakerMu.Unlock()

	adaptiveMu.Lock()
	cooldownCopy := map[string]time.Time{}
	for key, value := range cooldownUntil {
		cooldownCopy[key] = value
	}
	adaptiveMu.Unlock()

	metricsMu.Lock()
	metricsCopy := map[string]trafficMetrics{}
	for key, value := range metrics {
		if value == nil {
			continue
		}
		copyEntry := *value
		copyEntry.LatencySamples = append([]int64{}, value.LatencySamples...)
		metricsCopy[key] = copyEntry
	}
	metricsMu.Unlock()

	keys := map[string]bool{}
	for key := range breakerCopy {
		keys[key] = true
	}
	for key := range cooldownCopy {
		keys[key] = true
	}
	for key := range metricsCopy {
		keys[key] = true
	}
	out := make([]HealthSnapshotEntry, 0, len(keys))
	for key := range keys {
		provider, subject := parseStateKey(key)
		if provider == "" || !allowed[provider] {
			continue
		}
		metric := metricsCopy[key]
		breaker := breakerCopy[key]
		p50, p95 := latencyQuantiles(metric.LatencySamples)
		avg := int64(0)
		if len(metric.LatencySamples) > 0 {
			avg = metric.LatencyTotalMS / int64(len(metric.LatencySamples))
		}
		out = append(out, HealthSnapshotEntry{
			Provider:            provider,
			Subject:             subject,
			Requests:            metric.Requests,
			Success:             metric.Success,
			TooManyRequests:     metric.TooManyRequests,
			ClientErrors4xx:     metric.ClientErrors4xx,
			ServerErrors5xx:     metric.ServerErrors5xx,
			LastStatusCode:      metric.LastStatusCode,
			LastSeen:            metric.LastSeen,
			AvgLatencyMS:        avg,
			P50LatencyMS:        p50,
			P95LatencyMS:        p95,
			CircuitState:        firstNonEmpty(strings.TrimSpace(breaker.State), "closed"),
			ConsecutiveFailures: breaker.ConsecutiveFailures,
			OpenUntil:           breaker.OpenUntil,
			CooldownUntil:       cooldownCopy[key],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func GuardrailSnapshot(ids ...ID) []GuardrailSnapshotEntry {
	if len(ids) == 0 {
		ids = DefaultIDs()
	}
	allowed := map[ID]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	guardrailMu.Lock()
	inFlightCopy := map[string]int64{}
	for key, value := range inFlightRequests {
		inFlightCopy[key] = value
	}
	feedbackCopy := map[string]int64{}
	for key, value := range feedbackWithoutAcquire {
		feedbackCopy[key] = value
	}
	releaseCopy := map[string]int64{}
	for key, value := range releaseWithoutAcquire {
		releaseCopy[key] = value
	}
	guardrailMu.Unlock()

	keys := map[string]bool{}
	for key := range inFlightCopy {
		keys[key] = true
	}
	for key := range feedbackCopy {
		keys[key] = true
	}
	for key := range releaseCopy {
		keys[key] = true
	}
	out := make([]GuardrailSnapshotEntry, 0, len(keys))
	for key := range keys {
		provider, subject := parseStateKey(key)
		if provider == "" || !allowed[provider] {
			continue
		}
		entry := GuardrailSnapshotEntry{
			Provider:               provider,
			Subject:                subject,
			InFlight:               inFlightCopy[key],
			FeedbackWithoutAcquire: feedbackCopy[key],
			ReleaseWithoutAcquire:  releaseCopy[key],
		}
		if entry.InFlight == 0 && entry.FeedbackWithoutAcquire == 0 && entry.ReleaseWithoutAcquire == 0 {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func Wait(ctx context.Context, id ID, subject string, method string, path string) error {
	return Admit(ctx, id, subject, method, path)
}

func APIVersionPolicyCoverage(ids ...ID) (missing []ID, invalid []string) {
	if len(ids) == 0 {
		ids = DefaultIDs()
	}
	policyByID := map[ID]APIVersionPolicy{}
	for _, policy := range APIVersionPolicies() {
		policyByID[policy.Provider] = policy
	}
	for _, id := range ids {
		policy, ok := policyByID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if strings.TrimSpace(policy.Version) == "" && strings.TrimSpace(policy.ExemptReason) == "" {
			invalid = append(invalid, string(id))
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	sort.Strings(invalid)
	return missing, invalid
}

func parseStateKey(key string) (ID, string) {
	raw := strings.TrimSpace(key)
	if raw == "" {
		return "", ""
	}
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) == 1 {
		id, _ := ParseID(parts[0])
		return id, "default"
	}
	id, _ := ParseID(parts[0])
	subject := normalizeSubject(parts[1])
	return id, subject
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func latencyQuantiles(values []int64) (p50 int64, p95 int64) {
	if len(values) == 0 {
		return 0, 0
	}
	sorted := append([]int64{}, values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 = percentile(sorted, 50)
	p95 = percentile(sorted, 95)
	return p50, p95
}

func percentile(sorted []int64, q int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 100 {
		return sorted[len(sorted)-1]
	}
	index := int((float64(len(sorted)-1) * float64(q)) / 100.0)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
