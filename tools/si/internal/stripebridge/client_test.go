package stripebridge

import (
	"context"
	"net/http"
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
