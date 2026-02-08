package apibridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type memLogger struct {
	events []map[string]any
}

func (m *memLogger) Log(event map[string]any) {
	m.events = append(m.events, event)
}

func TestClient_Do_JSONBodyAndRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct == "" {
			t.Fatalf("missing content-type")
		}
		// Fail first request to trigger retry.
		if atomic.LoadInt32(&calls) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("X-Request-Id", "req-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	logger := &memLogger{}
	c, err := NewClient(Config{
		BaseURL:    srv.URL,
		UserAgent:  "ua",
		MaxRetries: 1,
		Logger:     logger,
		RequestIDFromHeaders: func(h http.Header) string {
			return h.Get("X-Request-Id")
		},
		RetryDecider: func(ctx context.Context, attempt int, req Request, resp *http.Response, body []byte, callErr error) RetryDecision {
			_ = ctx
			_ = req
			_ = body
			if callErr != nil {
				return RetryDecision{}
			}
			if resp != nil && resp.StatusCode == http.StatusTooManyRequests && attempt == 1 {
				return RetryDecision{Retry: true, Wait: 0}
			}
			return RetryDecision{}
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "/v1/x",
		JSONBody: map[string]any{"a": "b"},
		Headers:  map[string]string{"Accept": "application/json"},
		LogFields: map[string]any{
			"component": "test",
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if resp.RequestID != "req-123" {
		t.Fatalf("request_id=%q", resp.RequestID)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls=%d", atomic.LoadInt32(&calls))
	}
	if len(logger.events) == 0 {
		t.Fatalf("expected log events")
	}
}

func TestClient_Do_PrepareCalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Fatalf("missing auth header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.Do(context.Background(), Request{
		Method: http.MethodGet,
		Path:   "/ok",
		Prepare: func(ctx context.Context, attempt int, httpReq *http.Request) error {
			_ = ctx
			_ = attempt
			httpReq.Header.Set("Authorization", "Bearer t")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestClient_Do_NetworkErrorNoRetryOnUnsafeMethod(t *testing.T) {
	c, err := NewClient(Config{
		BaseURL:    "http://127.0.0.1:1", // closed
		MaxRetries: 3,
		RetryDecider: func(ctx context.Context, attempt int, req Request, resp *http.Response, body []byte, callErr error) RetryDecision {
			_ = ctx
			_ = resp
			_ = body
			if callErr == nil {
				return RetryDecision{}
			}
			// Only retry safe methods.
			if IsSafeMethod(req.Method) {
				return RetryDecision{Retry: true, Wait: 0}
			}
			return RetryDecision{}
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/x"})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestDefaultRetryAfterDelay_ParsesSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "2")
	d, ok := RetryAfterDelay(h)
	if !ok || d != 2*time.Second {
		t.Fatalf("got %v ok=%v", d, ok)
	}
}

func TestDefaultRetryDecider_RetriesSafe429(t *testing.T) {
	dec := DefaultRetryDecider(context.Background(), 1, Request{Method: http.MethodGet}, &http.Response{StatusCode: 429, Header: http.Header{}}, nil, nil)
	if !dec.Retry {
		t.Fatalf("expected retry")
	}
}

func TestDefaultRetryDecider_DoesNotRetryUnsafe(t *testing.T) {
	dec := DefaultRetryDecider(context.Background(), 1, Request{Method: http.MethodPost}, &http.Response{StatusCode: 500, Header: http.Header{}}, nil, nil)
	if dec.Retry {
		t.Fatalf("did not expect retry")
	}
}

func TestClient_NewClient_ValidatesBaseURL(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestClient_Do_BuildRequestJSONMarshalError(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.Do(context.Background(), Request{
		Method:   http.MethodPost,
		Path:     "/x",
		JSONBody: func() {}, // not marshalable
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}
