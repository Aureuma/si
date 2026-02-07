package stripebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	stripe "github.com/stripe/stripe-go/v83"
)

type mockRawRequester struct {
	lastMethod  string
	lastPath    string
	lastContent string
	response    *stripe.APIResponse
	err         error
}

func (m *mockRawRequester) RawRequest(method string, path string, content string, params *stripe.RawParams) (*stripe.APIResponse, error) {
	m.lastMethod = method
	m.lastPath = path
	m.lastContent = content
	return m.response, m.err
}

func TestClientDoGETBuildsQuery(t *testing.T) {
	mock := &mockRawRequester{
		response: &stripe.APIResponse{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			RawJSON:    []byte(`{"object":"list","data":[],"has_more":false}`),
		},
	}
	client, err := newClientWithRaw(ClientConfig{APIKey: "sk_test_123"}, mock)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Do(context.Background(), Request{
		Method: "GET",
		Path:   "/v1/products",
		Params: map[string]string{"limit": "3", "active": "true"},
	})
	if err != nil {
		t.Fatalf("client do: %v", err)
	}
	if mock.lastMethod != "GET" {
		t.Fatalf("unexpected method: %s", mock.lastMethod)
	}
	if mock.lastContent != "" {
		t.Fatalf("expected empty GET content, got %q", mock.lastContent)
	}
	if mock.lastPath == "/v1/products" {
		t.Fatalf("expected query string in path")
	}
}

func TestBuildRequestContentForPostForm(t *testing.T) {
	content, path, err := buildRequestContent("/v1/products", "POST", map[string]string{"name": "Demo"}, "")
	if err != nil {
		t.Fatalf("build content: %v", err)
	}
	if path != "/v1/products" {
		t.Fatalf("unexpected path %q", path)
	}
	if content == "" || !strings.Contains(content, "name=Demo") {
		t.Fatalf("expected encoded form content, got %q", content)
	}
}

type mockEventLogger struct {
	events []map[string]any
}

func (m *mockEventLogger) Log(event map[string]any) {
	copyEvent := map[string]any{}
	for key, value := range event {
		copyEvent[key] = value
	}
	m.events = append(m.events, copyEvent)
}

func TestClientDoWritesLogEvents(t *testing.T) {
	mock := &mockRawRequester{
		response: &stripe.APIResponse{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			RawJSON:    []byte(`{"id":"prod_1"}`),
		},
	}
	logger := &mockEventLogger{}
	client, err := newClientWithRaw(ClientConfig{
		APIKey:     "sk_test_123",
		Logger:     logger,
		LogContext: map[string]string{"environment": "sandbox"},
	}, mock)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Do(context.Background(), Request{
		Method: "GET",
		Path:   "/v1/products/prod_1",
	})
	if err != nil {
		t.Fatalf("client do: %v", err)
	}
	if len(logger.events) < 2 {
		t.Fatalf("expected at least request+response events, got %d", len(logger.events))
	}
	if logger.events[0]["event"] != "request" {
		t.Fatalf("unexpected first event: %+v", logger.events[0])
	}
}

func TestJSONLLoggerWritesFile(t *testing.T) {
	path := t.TempDir() + "/stripe.log"
	logger := NewJSONLLogger(path)
	logger.Log(map[string]any{"event": "request", "message": "ok"})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one line, got %d", len(lines))
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if decoded["event"] != "request" {
		t.Fatalf("unexpected log event: %+v", decoded)
	}
	if _, ok := decoded["ts"]; !ok {
		t.Fatalf("expected ts field in log")
	}
}
