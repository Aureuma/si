package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type githubProjectRef struct {
	ProjectID    string
	Organization string
	Number       int
}

type githubProjectFieldDescriptor struct {
	Name       string
	Options    []githubProjectFieldOption
	Iterations []githubProjectIteration
}

type githubProjectFieldOption struct {
	ID   string
	Name string
}

type githubProjectIteration struct {
	ID        string
	Title     string
	StartDate string
}

func parseGitHubProjectRef(raw string) (githubProjectRef, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return githubProjectRef{}, fmt.Errorf("project reference is required")
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return githubProjectRef{}, fmt.Errorf("invalid project url: %w", err)
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) >= 4 && strings.EqualFold(segments[0], "orgs") && strings.EqualFold(segments[2], "projects") {
			number, err := parseGitHubNumber(segments[3], "project number")
			if err != nil {
				return githubProjectRef{}, err
			}
			return githubProjectRef{Organization: strings.TrimSpace(segments[1]), Number: number}, nil
		}
		return githubProjectRef{}, fmt.Errorf("unsupported project url format: %s", value)
	}
	if parsedNumber, err := parseGitHubNumber(value, "project number"); err == nil {
		return githubProjectRef{Number: parsedNumber}, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		org := strings.TrimSpace(parts[0])
		number, err := parseGitHubNumber(parts[1], "project number")
		if err == nil && org != "" {
			return githubProjectRef{Organization: org, Number: number}, nil
		}
	}
	return githubProjectRef{ProjectID: value}, nil
}

func githubProjectResolveSingleSelectOptionID(field githubProjectFieldDescriptor, optionName string) (string, error) {
	if len(field.Options) == 0 {
		return "", fmt.Errorf("field %q has no single-select options", field.Name)
	}
	target := strings.ToLower(strings.TrimSpace(optionName))
	if target == "" {
		return "", fmt.Errorf("single-select option name is required")
	}
	for _, option := range field.Options {
		if strings.ToLower(strings.TrimSpace(option.Name)) == target {
			if strings.TrimSpace(option.ID) == "" {
				break
			}
			return option.ID, nil
		}
	}
	return "", fmt.Errorf("single-select option %q not found on field %q", optionName, field.Name)
}

func githubProjectResolveIterationID(field githubProjectFieldDescriptor, iteration string) (string, error) {
	if len(field.Iterations) == 0 {
		return "", fmt.Errorf("field %q has no iterations", field.Name)
	}
	target := strings.TrimSpace(iteration)
	if target == "" {
		return "", fmt.Errorf("iteration name is required")
	}
	if target == "@current" {
		now := time.Now().UTC()
		var chosen githubProjectIteration
		var chosenStart time.Time
		for _, candidate := range field.Iterations {
			if candidate.StartDate == "" {
				continue
			}
			start, err := time.Parse("2006-01-02", candidate.StartDate)
			if err != nil {
				continue
			}
			if start.After(now) {
				continue
			}
			if chosen.ID == "" || start.After(chosenStart) {
				chosen = candidate
				chosenStart = start
			}
		}
		if chosen.ID == "" {
			return "", fmt.Errorf("unable to resolve @current iteration for field %q", field.Name)
		}
		return chosen.ID, nil
	}
	lower := strings.ToLower(target)
	for _, candidate := range field.Iterations {
		if strings.TrimSpace(candidate.ID) == target {
			return candidate.ID, nil
		}
		if strings.ToLower(strings.TrimSpace(candidate.Title)) == lower {
			return candidate.ID, nil
		}
		if strings.TrimSpace(candidate.StartDate) == target {
			return candidate.ID, nil
		}
	}
	return "", fmt.Errorf("iteration %q not found on field %q", iteration, field.Name)
}

func githubProjectParseOptionalBoolFlag(flagName, value string) (*bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%s must be true or false", flagName)
	}
	return &parsed, nil
}
