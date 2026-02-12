package main

import (
	"slices"
	"strings"
	"testing"
)

func TestSocialLinkedInOrganizationNoArgsShowsUsage(t *testing.T) {
	out := captureStdout(t, func() {
		cmdSocialLinkedInOrganization(nil)
	})
	if !strings.Contains(out, socialLinkedInOrganizationUsageText) {
		t.Fatalf("expected usage output, got %q", out)
	}
}

func TestSocialLinkedInOrganizationActions(t *testing.T) {
	if len(socialLinkedInOrganizationActions) == 0 {
		t.Fatalf("social linkedin organization actions should not be empty")
	}
	names := make([]string, 0, len(socialLinkedInOrganizationActions))
	for _, action := range socialLinkedInOrganizationActions {
		names = append(names, action.Name)
	}
	if !slices.Contains(names, "get") {
		t.Fatalf("missing get action: %v", names)
	}
}
