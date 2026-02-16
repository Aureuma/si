package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"si/tools/si/internal/httpx"
	"si/tools/si/internal/providers"
)

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type publicDoctorResult struct {
	Provider string      `json:"provider"`
	BaseURL  string      `json:"base_url"`
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Check    doctorCheck `json:"check"`
}

func runPublicProviderDoctor(ctx context.Context, id providers.ID, baseURLOverride string) (publicDoctorResult, error) {
	spec := providers.Resolve(id)
	method, path, ok := providers.PublicProbe(id)
	if !ok {
		return publicDoctorResult{}, fmt.Errorf("public probe is not configured for %s", id)
	}
	baseURL := strings.TrimSpace(baseURLOverride)
	if baseURL == "" {
		baseURL = strings.TrimSpace(spec.BaseURL)
	}
	if baseURL == "" {
		return publicDoctorResult{}, fmt.Errorf("base url is not configured for %s", id)
	}
	endpoint, err := resolveProviderProbeURL(baseURL, path)
	if err != nil {
		return publicDoctorResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return publicDoctorResult{}, err
	}
	if value := strings.TrimSpace(spec.UserAgent); value != "" {
		req.Header.Set("User-Agent", value)
	}
	if value := strings.TrimSpace(spec.Accept); value != "" {
		req.Header.Set("Accept", value)
	}
	for key, value := range spec.DefaultHeaders {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, strings.TrimSpace(value))
	}
	resp, err := httpx.SharedClient(20 * time.Second).Do(req)
	if err != nil {
		return publicDoctorResult{
			Provider: string(id),
			BaseURL:  baseURL,
			Method:   method,
			Path:     path,
			Check: doctorCheck{
				Name:   "public.probe",
				OK:     false,
				Detail: err.Error(),
			},
		}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 220))
	snippet := strings.Join(strings.Fields(strings.TrimSpace(string(body))), " ")
	detail := fmt.Sprintf("status=%d", resp.StatusCode)
	if snippet != "" {
		detail += " body=" + snippet
	}
	checkOK := resp.StatusCode < 500
	return publicDoctorResult{
		Provider: string(id),
		BaseURL:  baseURL,
		Method:   method,
		Path:     path,
		Check: doctorCheck{
			Name:   "public.probe",
			OK:     checkOK,
			Detail: detail,
		},
	}, nil
}

func printPublicDoctorResult(label string, result publicDoctorResult, jsonOut bool) {
	ok := result.Check.OK
	payload := map[string]any{
		"ok":       ok,
		"provider": result.Provider,
		"base_url": result.BaseURL,
		"method":   result.Method,
		"path":     result.Path,
		"checks":   []doctorCheck{result.Check},
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if !ok {
			os.Exit(1)
		}
		return
	}
	if ok {
		fmt.Printf("%s %s\n", styleHeading(label+" doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading(label+" doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s %s %s\n", styleHeading("Public probe:"), result.Method, result.Path, styleDim("(unauthenticated)"))
	fmt.Printf("%s %s\n", styleHeading("Base URL:"), result.BaseURL)
	icon := styleSuccess("OK")
	if !result.Check.OK {
		icon = styleError("ERR")
	}
	printAlignedRows([][]string{{icon, result.Check.Name, strings.TrimSpace(result.Check.Detail)}}, 2, "  ")
	if !ok {
		os.Exit(1)
	}
}

func resolveProviderProbeURL(baseURL string, path string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	path = strings.TrimSpace(path)
	if baseURL == "" {
		return "", fmt.Errorf("base url is required")
	}
	if path == "" {
		return "", fmt.Errorf("probe path is required")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base url %q: %w", baseURL, err)
	}
	if !base.IsAbs() {
		return "", fmt.Errorf("base url must be absolute: %q", baseURL)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid probe path %q: %w", path, err)
	}
	if !strings.HasPrefix(path, "/") && !ref.IsAbs() {
		ref.Path = "/" + strings.TrimSpace(ref.Path)
	}
	return strings.TrimSpace(base.ResolveReference(ref).String()), nil
}
