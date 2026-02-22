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

type heliaError struct {
	Error string `json:"error"`
}

func newHeliaClient(baseURL string, token string, timeout time.Duration) (*heliaClient, error) {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("helia base url is required (set settings.helia.base_url or SI_HELIA_BASE_URL)")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid helia base url %q", baseURL)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("helia token is required (run `si helia auth login` or set SI_HELIA_TOKEN)")
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

func (c *heliaClient) whoAmI(ctx context.Context) (heliaWhoAmI, error) {
	var out heliaWhoAmI
	body, err := c.doJSON(ctx, http.MethodGet, "/v1/auth/whoami", nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err == nil && strings.TrimSpace(out.AccountSlug) != "" {
		return out, nil
	}
	var wrapped struct {
		AccountID   string   `json:"account_id"`
		AccountSlug string   `json:"account_slug"`
		TokenID     string   `json:"token_id"`
		Scopes      []string `json:"scopes"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return out, fmt.Errorf("parse whoami response: %w", err)
	}
	out = heliaWhoAmI(wrapped)
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
		return fmt.Errorf("helia: %s (status %d)", parsed.Error, res.StatusCode)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		trimmed = http.StatusText(res.StatusCode)
	}
	return fmt.Errorf("helia: %s (status %d)", trimmed, res.StatusCode)
}
