package main

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

type githubReleaseMeta struct {
	ID        int
	UploadURL string
}

func resolveReleaseMeta(ctx context.Context, client githubBridgeClient, owner string, repo string, ref string) (githubReleaseMeta, error) {
	meta := githubReleaseMeta{}
	if id, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil && id > 0 {
		meta.ID = id
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", owner, repo, "releases", strconv.Itoa(id)), Owner: owner, Repo: repo})
		if err != nil {
			return githubReleaseMeta{}, err
		}
		if value, ok := resp.Data["upload_url"].(string); ok {
			meta.UploadURL = strings.TrimSpace(value)
		}
		return meta, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", owner, repo, "releases", "tags", strings.TrimSpace(ref)), Owner: owner, Repo: repo})
	if err != nil {
		return githubReleaseMeta{}, err
	}
	if resp.Data == nil {
		return githubReleaseMeta{}, fmt.Errorf("release response missing data")
	}
	rawID, ok := resp.Data["id"]
	if !ok {
		return githubReleaseMeta{}, fmt.Errorf("release response missing id")
	}
	switch typed := rawID.(type) {
	case float64:
		meta.ID = int(typed)
	case int:
		meta.ID = typed
	case int64:
		meta.ID = int(typed)
	default:
		return githubReleaseMeta{}, fmt.Errorf("invalid release id type")
	}
	if value, ok := resp.Data["upload_url"].(string); ok {
		meta.UploadURL = strings.TrimSpace(value)
	}
	return meta, nil
}

func expandReleaseUploadURL(raw string, query map[string]string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("release upload url missing from github response")
	}
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("release upload url invalid")
	}
	if len(query) == 0 {
		return raw, nil
	}
	parts := make([]string, 0, len(query))
	for key, value := range query {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		parts = append(parts, key+"="+url.QueryEscape(strings.TrimSpace(value)))
	}
	if len(parts) == 0 {
		return raw, nil
	}
	if strings.Contains(raw, "?") {
		return raw + "&" + strings.Join(parts, "&"), nil
	}
	return raw + "?" + strings.Join(parts, "&"), nil
}
