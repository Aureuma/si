package main

import (
	"os"
	"strings"
	"testing"
)

func TestIntegrationRequestPathsUseAcquireAndFeedback(t *testing.T) {
	files := []string{
		"internal/cloudflarebridge/client.go",
		"internal/githubbridge/client.go",
		"internal/googleplacesbridge/client.go",
		"internal/stripebridge/client.go",
		"internal/youtubebridge/client.go",
		"social_contract.go",
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(raw)
		usesRuntime := strings.Contains(content, "integrationruntime.DoHTTP(")
		if !usesRuntime && !strings.Contains(content, "providers.Acquire(") {
			t.Fatalf("%s must call integrationruntime.DoHTTP or providers.Acquire for admission", path)
		}
		if !usesRuntime && !strings.Contains(content, "providers.FeedbackWithLatency(") {
			t.Fatalf("%s must call integrationruntime.DoHTTP or providers.FeedbackWithLatency for runtime feedback", path)
		}
		if strings.Contains(content, "providers.Admit(") {
			t.Fatalf("%s should not call providers.Admit directly", path)
		}
	}
}
