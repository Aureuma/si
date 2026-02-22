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
	"strings"
	"time"
)

type heliaClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type heliaWhoAmI struct {
	AccountID   string   `json:"account_id"`
	AccountSlug string   `json:"account_slug"`
	TokenID     string   `json:"token_id"`
	Scopes      []string `json:"scopes"`
}

type heliaObjectMeta struct {
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

type heliaPutResult struct {
	Result struct {
		Object struct {
			LatestRevision int64 `json:"latest_revision"`
		} `json:"object"`
		Revision struct {
			Revision int64 `json:"revision"`
		} `json:"revision"`
	} `json:"result"`
}

type heliaTokenRecord struct {
	TokenID    string   `json:"token_id"`
	Label      string   `json:"label"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
	RevokedAt  string   `json:"revoked_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

type heliaIssuedToken struct {
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

type heliaAuditEvent struct {
	ID        int64                  `json:"id"`
	TokenID   string                 `json:"token_id,omitempty"`
	Action    string                 `json:"action"`
	Kind      string                 `json:"kind"`
	Name      string                 `json:"name"`
	Revision  int64                  `json:"revision,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	CreatedAt string                 `json:"created_at"`
}

type heliaError struct {
	Error string `json:"error"`
}

type heliaIntegrationRegistryResponse struct {
	Registry string          `json:"registry"`
	Index    json.RawMessage `json:"index"`
}

type heliaIntegrationShardResponse struct {
	Registry string          `json:"registry"`
	Shard    string          `json:"shard"`
	Payload  json.RawMessage `json:"payload"`
}

func newHeliaClient(baseURL string, token string, timeout time.Duration) (*heliaClient, error) {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("sun base url is required (set settings.sun.base_url/settings.helia.base_url, SI_SUN_BASE_URL, or SI_HELIA_BASE_URL)")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid sun base url %q", baseURL)
	}
	parsed, _ := url.Parse(baseURL)
	if parsed != nil && !heliaAllowsInsecureHTTP(parsed) {
		return nil, fmt.Errorf("sun base url must use https for non-local hosts (set SI_SUN_ALLOW_INSECURE_HTTP=1 to override)")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("sun token is required (run `si sun auth login` or set SI_SUN_TOKEN)")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &heliaClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

func heliaAllowsInsecureHTTP(u *url.URL) bool {
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

func (c *heliaClient) ready(ctx context.Context) error {
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
		return decodeHeliaError(res)
	}
	return nil
}

func (c *heliaClient) whoAmI(ctx context.Context) (heliaWhoAmI, error) {
	var out heliaWhoAmI
	body, err := c.doJSON(ctx, http.MethodGet, "/v1/auth/whoami", nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("parse whoami response: %w", err)
	}
	return out, nil
}

func (c *heliaClient) listObjects(ctx context.Context, kind string, name string, limit int) ([]heliaObjectMeta, error) {
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
		Items []heliaObjectMeta `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse list objects response: %w", err)
	}
	return parsed.Items, nil
}

func (c *heliaClient) putObject(ctx context.Context, kind string, name string, payload []byte, contentType string, metadata map[string]interface{}, expectedRevision *int64) (heliaPutResult, error) {
	var out heliaPutResult
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

func (c *heliaClient) getPayload(ctx context.Context, kind string, name string) ([]byte, error) {
	endpoint := c.baseURL + "/v1/objects/" + url.PathEscape(kind) + "/" + url.PathEscape(name) + "/payload"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, decodeHeliaError(res)
	}
	return io.ReadAll(res.Body)
}

func (c *heliaClient) listTokens(ctx context.Context, includeRevoked bool, limit int) ([]heliaTokenRecord, error) {
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
		Items []heliaTokenRecord `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse list tokens response: %w", err)
	}
	return parsed.Items, nil
}

func (c *heliaClient) createToken(ctx context.Context, label string, scopes []string, expiresInHours int) (heliaIssuedToken, error) {
	var out heliaIssuedToken
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

func (c *heliaClient) revokeToken(ctx context.Context, tokenID string) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/v1/tokens/"+url.PathEscape(strings.TrimSpace(tokenID))+"/revoke", map[string]interface{}{})
	return err
}

func (c *heliaClient) listAuditEvents(ctx context.Context, action string, kind string, name string, limit int) ([]heliaAuditEvent, error) {
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
		Items []heliaAuditEvent `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse audit response: %w", err)
	}
	return parsed.Items, nil
}

func (c *heliaClient) getIntegrationRegistryIndex(ctx context.Context, registry string) ([]byte, error) {
	body, err := c.doJSON(ctx, http.MethodGet, "/v1/integrations/registries/"+url.PathEscape(strings.TrimSpace(registry)), nil)
	if err != nil {
		return nil, err
	}
	var parsed heliaIntegrationRegistryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse integration registry response: %w", err)
	}
	if len(parsed.Index) == 0 {
		return nil, fmt.Errorf("integration registry response missing index payload")
	}
	return parsed.Index, nil
}

func (c *heliaClient) putIntegrationRegistryIndex(ctx context.Context, registry string, payload []byte, expectedRevision *int64) (heliaPutResult, error) {
	var out heliaPutResult
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

func (c *heliaClient) getIntegrationRegistryShard(ctx context.Context, registry string, shard string) ([]byte, error) {
	path := "/v1/integrations/registries/" + url.PathEscape(strings.TrimSpace(registry)) + "/shards/" + url.PathEscape(strings.TrimSpace(shard))
	body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var parsed heliaIntegrationShardResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse integration shard response: %w", err)
	}
	if len(parsed.Payload) == 0 {
		return nil, fmt.Errorf("integration shard response missing payload")
	}
	return parsed.Payload, nil
}

func (c *heliaClient) putIntegrationRegistryShard(ctx context.Context, registry string, shard string, payload []byte, expectedRevision *int64) (heliaPutResult, error) {
	var out heliaPutResult
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

func (c *heliaClient) doJSON(ctx context.Context, method string, path string, payload interface{}) ([]byte, error) {
	endpoint := c.baseURL + path
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, decodeHeliaError(res)
	}
	return io.ReadAll(res.Body)
}

func decodeHeliaError(res *http.Response) error {
	body, _ := io.ReadAll(res.Body)
	var parsed heliaError
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error) != "" {
		return fmt.Errorf("sun: %s (status %d)", parsed.Error, res.StatusCode)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		trimmed = http.StatusText(res.StatusCode)
	}
	return fmt.Errorf("sun: %s (status %d)", trimmed, res.StatusCode)
}
