package main

import "strings"

type githubWorkflowRunStatus struct {
	ID         int
	Name       string
	Status     string
	Conclusion string
	HTMLURL    string
	HeadBranch string
	Event      string
}

func githubWorkflowRunStatusFromData(data map[string]any) githubWorkflowRunStatus {
	return githubWorkflowRunStatus{
		ID:         githubWorkflowInt(data["id"]),
		Name:       strings.TrimSpace(stringifyGitHubAny(data["name"])),
		Status:     strings.TrimSpace(stringifyGitHubAny(data["status"])),
		Conclusion: strings.TrimSpace(stringifyGitHubAny(data["conclusion"])),
		HTMLURL:    strings.TrimSpace(stringifyGitHubAny(data["html_url"])),
		HeadBranch: strings.TrimSpace(stringifyGitHubAny(data["head_branch"])),
		Event:      strings.TrimSpace(stringifyGitHubAny(data["event"])),
	}
}

func githubWorkflowInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func githubWorkflowRunIsFailureConclusion(conclusion string) bool {
	switch strings.ToLower(strings.TrimSpace(conclusion)) {
	case "success", "skipped", "neutral":
		return false
	default:
		return true
	}
}
