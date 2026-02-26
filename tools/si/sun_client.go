package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type sunClient struct {
	baseURL string
	token   string
	http    *http.Client
}

const (
	maxSunBearerTokenChars = 256
	sunHTTPMaxAttempts     = 4
)

var (
	sunHTTPRetryBaseDelay = 200 * time.Millisecond
	sunHTTPRetryMaxDelay  = 2 * time.Second
)

type sunWhoAmI struct {
	AccountID   string   `json:"account_id"`
	AccountSlug string   `json:"account_slug"`
	TokenID     string   `json:"token_id"`
	Scopes      []string `json:"scopes"`
}

type sunObjectMeta struct {
	Kind           string                 `json:"kind"`
	Name           string                 `json:"name"`
	LatestRevision int64                  `json:"latest_revision"`
	Checksum       string                 `json:"checksum"`
	ContentType    string                 `json:"content_type"`
	SizeBytes      int64                  `json:"size_bytes"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

type sunObjectRevision struct {
	Revision    int64                  `json:"revision"`
	Checksum    string                 `json:"checksum"`
	ContentType string                 `json:"content_type"`
	SizeBytes   int64                  `json:"size_bytes"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   string                 `json:"created_at"`
}

type sunPutResult struct {
	Result struct {
		Object struct {
			LatestRevision int64 `json:"latest_revision"`
		} `json:"object"`
		Revision struct {
			Revision int64 `json:"revision"`
		} `json:"revision"`
	} `json:"result"`
}

type sunTokenRecord struct {
	TokenID    string   `json:"token_id"`
	Label      string   `json:"label"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
	RevokedAt  string   `json:"revoked_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

type sunIssuedToken struct {
	Account struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"account"`
	Token     string   `json:"token"`
	TokenID   string   `json:"token_id"`
	Label     string   `json:"label"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	IssuedAt  string   `json:"issued_at"`
}

type sunAuditEvent struct {
	ID        int64                  `json:"id"`
	TokenID   string                 `json:"token_id,omitempty"`
	Action    string                 `json:"action"`
	Kind      string                 `json:"kind"`
	Name      string                 `json:"name"`
	Revision  int64                  `json:"revision,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	CreatedAt string                 `json:"created_at"`
}

type sunError struct {
	Error string `json:"error"`
}

type sunIntegrationRegistryResponse struct {
	Registry string          `json:"registry"`
	Index    json.RawMessage `json:"index"`
}

type sunIntegrationShardResponse struct {
	Registry string          `json:"registry"`
	Shard    string          `json:"shard"`
	Payload  json.RawMessage `json:"payload"`
}

type sunVaultPrivateKey struct {
	Repo              string   `json:"repo"`
	Env               string   `json:"env"`
	PublicKey         string   `json:"public_key"`
	PrivateKey        string   `json:"private_key"`
	BackupPrivateKeys []string `json:"backup_private_keys,omitempty"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
}

func newSunClient(baseURL string, token string, timeout time.Duration) (*sunClient, error) {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("sun base url is required (set settings.sun.base_url or SI_SUN_BASE_URL)")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid sun base url %q", baseURL)
	}
	parsed, _ := url.Parse(baseURL)
	if parsed != nil && !sunAllowsInsecureHTTP(parsed) {
		return nil, fmt.Errorf("sun base url must use https for non-local hosts (set SI_SUN_ALLOW_INSECURE_HTTP=1 to override)")
	}
	token = strings.TrimSpace(token)
	if err := validateSunBearerToken(token); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &sunClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

func validateSunBearerToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("sun token is required (run `si sun auth login` or set SI_SUN_TOKEN)")
	}
	if len(token) > maxSunBearerTokenChars {
		return fmt.Errorf("sun token is too long")
	}
	for _, ch := range token {
		if ch <= 0x20 || ch == 0x7f {
			return fmt.Errorf("sun token must not contain whitespace or control characters")
		}
	}
	return nil
}

func sunAllowsInsecureHTTP(u *url.URL) bool {
	if u == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(u.Scheme), "https") {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(u.Scheme), "http") {
		return false
	}
	if envSunAllowInsecureHTTP() {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func (c *sunClient) ready(ctx context.Context) error {
	endpoint := c.baseURL + "/v1/readyz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return decodeSunError(res)
	}
	return nil
}

func (c *sunClient) whoAmI(ctx context.Context) (sunWhoAmI, error) {
	var out sunWhoAmI
	body, err := c.doJSON(ctx, http.MethodGet, "/v1/auth/whoami", nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse whoami response: %w", err)
	}
	return out, nil
}

func (c *sunClient) listObjects(ctx context.Context, kind string, name string, limit int) ([]sunObjectMeta, error) {
	params := url.Values{}
	if strings.TrimSpace(kind) != "" {
		params.Set("kind", strings.TrimSpace(kind))
	}
	if strings.TrimSpace(name) != "" {
		params.Set("name", strings.TrimSpace(name))
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/v1/objects"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Items []sunObjectMeta `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse list objects response: %w", err)
	}
	return parsed.Items, nil
}

func (c *sunClient) putObject(ctx context.Context, kind string, name string, payload []byte, contentType string, metadata map[string]interface{}, expectedRevision *int64) (sunPutResult, error) {
	var out sunPutResult
	request := map[string]interface{}{
		"content_type":   strings.TrimSpace(contentType),
		"payload_base64": base64.StdEncoding.EncodeToString(payload),
	}
	if len(metadata) > 0 {
		request["metadata"] = metadata
	}
	if expectedRevision != nil {
		request["expected_revision"] = *expectedRevision
	}
	body, err := c.doJSON(ctx, http.MethodPut, "/v1/objects/"+url.PathEscape(kind)+"/"+url.PathEscape(name), request)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse put object response: %w", err)
	}
	return out, nil
}

func (c *sunClient) getPayload(ctx context.Context, kind string, name string) ([]byte, error) {
	endpoint := c.baseURL + "/v1/objects/" + url.PathEscape(kind) + "/" + url.PathEscape(name) + "/payload"

	var lastErr error
	for attempt := 1; attempt <= sunHTTPMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		res, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt < sunHTTPMaxAttempts {
				if sleepErr := sunSleepWithContext(ctx, sunRetryDelay(attempt, "")); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			retryAfter := strings.TrimSpace(res.Header.Get("Retry-After"))
			if attempt < sunHTTPMaxAttempts && sunShouldRetryStatus(res.StatusCode) {
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
				if sleepErr := sunSleepWithContext(ctx, sunRetryDelay(attempt, retryAfter)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			err := decodeSunError(res)
			_ = res.Body.Close()
			return nil, err
		}
		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < sunHTTPMaxAttempts {
				if sleepErr := sunSleepWithContext(ctx, sunRetryDelay(attempt, "")); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, readErr
		}
		return body, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("sun payload request failed")
	}
	return nil, lastErr
}

func (c *sunClient) listRevisions(ctx context.Context, kind string, name string, limit int) ([]sunObjectRevision, error) {
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/v1/objects/" + url.PathEscape(strings.TrimSpace(kind)) + "/" + url.PathEscape(strings.TrimSpace(name)) + "/revisions"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Items []sunObjectRevision `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse list revisions response: %w", err)
	}
	return parsed.Items, nil
}

func (c *sunClient) getVaultPrivateKey(ctx context.Context, repo string, env string) (sunVaultPrivateKey, error) {
	var out sunVaultPrivateKey
	path := "/v1/vault/private-keys/" + url.PathEscape(strings.TrimSpace(repo)) + "/" + url.PathEscape(strings.TrimSpace(env))
	body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return out, err
	}
	var parsed struct {
		Vault sunVaultPrivateKey `json:"vault"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return out, fmt.Errorf("parse vault private key response: %w", err)
	}
	return parsed.Vault, nil
}

func (c *sunClient) putVaultPrivateKey(ctx context.Context, vault sunVaultPrivateKey, expectedRevision *int64) (sunVaultPrivateKey, error) {
	var out sunVaultPrivateKey
	request := map[string]interface{}{
		"public_key":  strings.TrimSpace(vault.PublicKey),
		"private_key": strings.TrimSpace(vault.PrivateKey),
	}
	if len(vault.BackupPrivateKeys) > 0 {
		request["backup_private_keys"] = vault.BackupPrivateKeys
	}
	if expectedRevision != nil {
		request["expected_revision"] = *expectedRevision
	}
	path := "/v1/vault/private-keys/" + url.PathEscape(strings.TrimSpace(vault.Repo)) + "/" + url.PathEscape(strings.TrimSpace(vault.Env))
	body, err := c.doJSON(ctx, http.MethodPut, path, request)
	if err != nil {
		return out, err
	}
	var parsed struct {
		Vault sunVaultPrivateKey `json:"vault"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return out, fmt.Errorf("parse vault private key put response: %w", err)
	}
	return parsed.Vault, nil
}

func (c *sunClient) listTokens(ctx context.Context, includeRevoked bool, limit int) ([]sunTokenRecord, error) {
	params := url.Values{}
	params.Set("include_revoked", boolString(includeRevoked))
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	body, err := c.doJSON(ctx, http.MethodGet, "/v1/tokens?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Items []sunTokenRecord `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse list tokens response: %w", err)
	}
	return parsed.Items, nil
}

func (c *sunClient) createToken(ctx context.Context, label string, scopes []string, expiresInHours int) (sunIssuedToken, error) {
	var out sunIssuedToken
	request := map[string]interface{}{
		"label":  strings.TrimSpace(label),
		"scopes": scopes,
	}
	if expiresInHours > 0 {
		request["expires_in_hours"] = expiresInHours
	}
	body, err := c.doJSON(ctx, http.MethodPost, "/v1/tokens", request)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse create token response: %w", err)
	}
	return out, nil
}

func (c *sunClient) revokeToken(ctx context.Context, tokenID string) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/v1/tokens/"+url.PathEscape(strings.TrimSpace(tokenID))+"/revoke", map[string]interface{}{})
	return err
}

func (c *sunClient) listAuditEvents(ctx context.Context, action string, kind string, name string, limit int) ([]sunAuditEvent, error) {
	params := url.Values{}
	if strings.TrimSpace(action) != "" {
		params.Set("action", strings.TrimSpace(action))
	}
	if strings.TrimSpace(kind) != "" {
		params.Set("kind", strings.TrimSpace(kind))
	}
	if strings.TrimSpace(name) != "" {
		params.Set("name", strings.TrimSpace(name))
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/v1/audit"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Items []sunAuditEvent `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse audit response: %w", err)
	}
	return parsed.Items, nil
}

func (c *sunClient) getIntegrationRegistryIndex(ctx context.Context, registry string) ([]byte, error) {
	body, err := c.doJSON(ctx, http.MethodGet, "/v1/integrations/registries/"+url.PathEscape(strings.TrimSpace(registry)), nil)
	if err != nil {
		return nil, err
	}
	var parsed sunIntegrationRegistryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse integration registry response: %w", err)
	}
	if len(parsed.Index) == 0 {
		return nil, fmt.Errorf("integration registry response missing index payload")
	}
	return parsed.Index, nil
}

func (c *sunClient) putIntegrationRegistryIndex(ctx context.Context, registry string, payload []byte, expectedRevision *int64) (sunPutResult, error) {
	var out sunPutResult
	request := map[string]interface{}{
		"payload": json.RawMessage(payload),
	}
	if expectedRevision != nil {
		request["expected_revision"] = *expectedRevision
	}
	body, err := c.doJSON(ctx, http.MethodPut, "/v1/integrations/registries/"+url.PathEscape(strings.TrimSpace(registry)), request)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse put integration registry response: %w", err)
	}
	return out, nil
}

func (c *sunClient) getIntegrationRegistryShard(ctx context.Context, registry string, shard string) ([]byte, error) {
	path := "/v1/integrations/registries/" + url.PathEscape(strings.TrimSpace(registry)) + "/shards/" + url.PathEscape(strings.TrimSpace(shard))
	body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var parsed sunIntegrationShardResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse integration shard response: %w", err)
	}
	if len(parsed.Payload) == 0 {
		return nil, fmt.Errorf("integration shard response missing payload")
	}
	return parsed.Payload, nil
}

func (c *sunClient) putIntegrationRegistryShard(ctx context.Context, registry string, shard string, payload []byte, expectedRevision *int64) (sunPutResult, error) {
	var out sunPutResult
	request := map[string]interface{}{
		"payload": json.RawMessage(payload),
	}
	if expectedRevision != nil {
		request["expected_revision"] = *expectedRevision
	}
	path := "/v1/integrations/registries/" + url.PathEscape(strings.TrimSpace(registry)) + "/shards/" + url.PathEscape(strings.TrimSpace(shard))
	body, err := c.doJSON(ctx, http.MethodPut, path, request)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse put integration shard response: %w", err)
	}
	return out, nil
}

func (c *sunClient) doJSON(ctx context.Context, method string, path string, payload interface{}) ([]byte, error) {
	endpoint := c.baseURL + path
	var encoded []byte
	if payload != nil {
		var err error
		encoded, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 1; attempt <= sunHTTPMaxAttempts; attempt++ {
		var body io.Reader
		if encoded != nil {
			body = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		if encoded != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt < sunHTTPMaxAttempts {
				if sleepErr := sunSleepWithContext(ctx, sunRetryDelay(attempt, "")); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, err
		}

		if res.StatusCode < 200 || res.StatusCode >= 300 {
			retryAfter := strings.TrimSpace(res.Header.Get("Retry-After"))
			if attempt < sunHTTPMaxAttempts && sunShouldRetryStatus(res.StatusCode) {
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
				if sleepErr := sunSleepWithContext(ctx, sunRetryDelay(attempt, retryAfter)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			err := decodeSunError(res)
			_ = res.Body.Close()
			return nil, err
		}

		responseBody, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < sunHTTPMaxAttempts {
				if sleepErr := sunSleepWithContext(ctx, sunRetryDelay(attempt, "")); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, readErr
		}
		return responseBody, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("sun request failed")
	}
	return nil, lastErr
}

func decodeSunError(res *http.Response) error {
	body, _ := io.ReadAll(res.Body)
	var parsed sunError
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error) != "" {
		return fmt.Errorf("sun: %s (status %d)", parsed.Error, res.StatusCode)
	}
	trimmed := strings.TrimSpace(string(body))
	if res.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(trimmed), "error code: 1010") {
		return fmt.Errorf("sun: access denied by cloudflare (error 1010); check firewall/bot rules for this client IP and user-agent")
	}
	if trimmed == "" {
		trimmed = http.StatusText(res.StatusCode)
	}
	return fmt.Errorf("sun: %s (status %d)", trimmed, res.StatusCode)
}

func sunShouldRetryStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusTooEarly:
		return true
	default:
		return status >= 500 && status <= 599
	}
}

func sunRetryDelay(attempt int, retryAfter string) time.Duration {
	retryAfter = strings.TrimSpace(retryAfter)
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
			delay := time.Duration(seconds) * time.Second
			if delay > sunHTTPRetryMaxDelay {
				delay = sunHTTPRetryMaxDelay
			}
			return delay
		}
		if retryAt, err := http.ParseTime(retryAfter); err == nil {
			delay := time.Until(retryAt)
			if delay < 0 {
				delay = 0
			}
			if delay > sunHTTPRetryMaxDelay {
				delay = sunHTTPRetryMaxDelay
			}
			return delay
		}
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := sunHTTPRetryBaseDelay * time.Duration(1<<(attempt-1))
	if delay > sunHTTPRetryMaxDelay {
		delay = sunHTTPRetryMaxDelay
	}
	return delay
}

func sunSleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
