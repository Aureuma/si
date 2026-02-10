package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/providers"
)

const providersUsageText = "usage: si providers <characteristics|health>"

func cmdProviders(args []string) {
	if len(args) == 0 {
		printUsage(providersUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(providersUsageText)
	case "characteristics", "chars", "status", "list":
		cmdProvidersCharacteristics(rest)
	case "health":
		cmdProvidersHealth(rest)
	default:
		printUnknown("providers", sub)
		printUsage(providersUsageText)
	}
}

func cmdProvidersCharacteristics(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("providers characteristics", flag.ExitOnError)
	providerID := fs.String("provider", "", "provider id (github, cloudflare, google_places, youtube, stripe, social_*, workos, aws_iam, gcp_serviceusage, oci_core)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si providers characteristics [--provider <id>] [--json]")
		return
	}
	ids := providers.DefaultIDs()
	if raw := strings.TrimSpace(*providerID); raw != "" {
		id, ok := providers.ParseID(raw)
		if !ok {
			valid := make([]string, 0, len(ids))
			for _, item := range ids {
				valid = append(valid, string(item))
			}
			fatal(fmt.Errorf("unknown provider id %q (valid: %s)", raw, strings.Join(valid, ", ")))
		}
		ids = []providers.ID{id}
	}
	specs := providers.SpecsSnapshot(ids...)
	caps := providers.CapabilitiesSnapshot(ids...)

	type row struct {
		Provider      string               `json:"provider"`
		BaseURL       string               `json:"base_url"`
		UploadBaseURL string               `json:"upload_base_url,omitempty"`
		APIVersion    string               `json:"api_version,omitempty"`
		AuthStyle     string               `json:"auth_style,omitempty"`
		RatePerSecond float64              `json:"rate_limit_per_second"`
		Burst         int                  `json:"rate_limit_burst"`
		PublicProbe   map[string]string    `json:"public_probe,omitempty"`
		Capabilities  providers.Capability `json:"capabilities"`
	}
	rows := make([]row, 0, len(ids))
	for _, id := range ids {
		spec := specs[id]
		entry := row{
			Provider:      string(id),
			BaseURL:       strings.TrimSpace(spec.BaseURL),
			UploadBaseURL: strings.TrimSpace(spec.UploadBaseURL),
			APIVersion:    strings.TrimSpace(spec.APIVersion),
			AuthStyle:     strings.TrimSpace(spec.AuthStyle),
			RatePerSecond: spec.RateLimitPerSecond,
			Burst:         spec.RateLimitBurst,
			Capabilities:  caps[id],
		}
		if method, path, ok := providers.PublicProbe(id); ok {
			entry.PublicProbe = map[string]string{"method": method, "path": path}
		}
		rows = append(rows, entry)
	}
	payload := map[string]any{
		"policy": map[string]any{
			"defaults":          "built_in_go",
			"admission":         "token_bucket",
			"adaptive_feedback": true,
		},
		"providers": rows,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Policy:"), "built-in defaults + runtime adaptive feedback")
	fmt.Printf("%s %s %s %s %s %s\n",
		padRightANSI(styleHeading("PROVIDER"), 18),
		padRightANSI(styleHeading("RATE"), 8),
		padRightANSI(styleHeading("BURST"), 6),
		padRightANSI(styleHeading("AUTH"), 8),
		padRightANSI(styleHeading("CAPS"), 8),
		styleHeading("PUBLIC PROBE"),
	)
	for _, entry := range rows {
		probe := "-"
		if entry.PublicProbe != nil {
			probe = entry.PublicProbe["method"] + " " + entry.PublicProbe["path"]
		}
		capsText := "-"
		if entry.Capabilities.SupportsPagination {
			capsText = "p"
		}
		if entry.Capabilities.SupportsBulk {
			capsText += "b"
		}
		if entry.Capabilities.SupportsIdempotency {
			capsText += "i"
		}
		if entry.Capabilities.SupportsRaw {
			capsText += "r"
		}
		fmt.Printf("%s %s %s %s %s %s\n",
			padRightANSI(entry.Provider, 18),
			padRightANSI(formatRate(entry.RatePerSecond), 8),
			padRightANSI(strconv.Itoa(entry.Burst), 6),
			padRightANSI(orDash(entry.AuthStyle), 8),
			padRightANSI(capsText, 8),
			probe,
		)
	}
}

func formatRate(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func cmdProvidersHealth(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("providers health", flag.ExitOnError)
	providerID := fs.String("provider", "", "provider id (github, cloudflare, google_places, youtube, stripe, social_*, workos, aws_iam, gcp_serviceusage, oci_core)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si providers health [--provider <id>] [--json]")
		return
	}
	ids := providers.DefaultIDs()
	if raw := strings.TrimSpace(*providerID); raw != "" {
		id, ok := providers.ParseID(raw)
		if !ok {
			valid := make([]string, 0, len(ids))
			for _, item := range ids {
				valid = append(valid, string(item))
			}
			fatal(fmt.Errorf("unknown provider id %q (valid: %s)", raw, strings.Join(valid, ", ")))
		}
		ids = []providers.ID{id}
	}
	entries := providers.HealthSnapshot(ids...)
	guardrails := providers.GuardrailSnapshot(ids...)
	versionWarnings, versionErrors := providers.APIVersionPolicyStatus(time.Now().UTC())
	policyMissing, policyInvalid := providers.APIVersionPolicyCoverage(ids...)
	for _, missing := range policyMissing {
		versionErrors = append(versionErrors, fmt.Sprintf("%s version policy missing", missing))
	}
	for _, invalid := range policyInvalid {
		versionErrors = append(versionErrors, fmt.Sprintf("%s version policy invalid", invalid))
	}
	sort.Strings(versionErrors)
	if *jsonOut {
		payload := map[string]any{
			"entries":          entries,
			"guardrails":       guardrails,
			"version_warnings": versionWarnings,
			"version_errors":   versionErrors,
			"version_missing":  policyMissing,
			"version_invalid":  policyInvalid,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if len(entries) == 0 {
		infof("no integration traffic observed yet")
		for _, entry := range guardrails {
			fmt.Printf("%s provider=%s subject=%s feedback_without_acquire=%d release_without_acquire=%d\n",
				styleHeading("Guardrail warning:"),
				entry.Provider,
				orDash(entry.Subject),
				entry.FeedbackWithoutAcquire,
				entry.ReleaseWithoutAcquire,
			)
		}
		for _, warning := range versionWarnings {
			fmt.Printf("%s %s\n", styleHeading("Version warning:"), warning)
		}
		for _, errText := range versionErrors {
			fmt.Printf("%s %s\n", styleHeading("Version error:"), styleError(errText))
		}
		return
	}
	fmt.Printf("%s %s %s %s %s %s %s %s %s\n",
		padRightANSI(styleHeading("PROVIDER"), 16),
		padRightANSI(styleHeading("SUBJECT"), 14),
		padRightANSI(styleHeading("REQ"), 6),
		padRightANSI(styleHeading("2XX"), 6),
		padRightANSI(styleHeading("429"), 6),
		padRightANSI(styleHeading("5XX"), 6),
		padRightANSI(styleHeading("P50"), 6),
		padRightANSI(styleHeading("P95"), 6),
		styleHeading("CIRCUIT"),
	)
	for _, entry := range entries {
		circuit := entry.CircuitState
		if strings.TrimSpace(circuit) == "" {
			circuit = "closed"
		}
		if circuit == "open" && !entry.OpenUntil.IsZero() {
			circuit = fmt.Sprintf("open until %s", entry.OpenUntil.Format(time.RFC3339))
		}
		fmt.Printf("%s %s %s %s %s %s %s %s %s\n",
			padRightANSI(string(entry.Provider), 16),
			padRightANSI(orDash(entry.Subject), 14),
			padRightANSI(strconv.FormatInt(entry.Requests, 10), 6),
			padRightANSI(strconv.FormatInt(entry.Success, 10), 6),
			padRightANSI(strconv.FormatInt(entry.TooManyRequests, 10), 6),
			padRightANSI(strconv.FormatInt(entry.ServerErrors5xx, 10), 6),
			padRightANSI(strconv.FormatInt(entry.P50LatencyMS, 10), 6),
			padRightANSI(strconv.FormatInt(entry.P95LatencyMS, 10), 6),
			circuit,
		)
	}
	for _, entry := range guardrails {
		fmt.Printf("%s provider=%s subject=%s feedback_without_acquire=%d release_without_acquire=%d\n",
			styleHeading("Guardrail warning:"),
			entry.Provider,
			orDash(entry.Subject),
			entry.FeedbackWithoutAcquire,
			entry.ReleaseWithoutAcquire,
		)
	}
	for _, warning := range versionWarnings {
		fmt.Printf("%s %s\n", styleHeading("Version warning:"), warning)
	}
	for _, errText := range versionErrors {
		fmt.Printf("%s %s\n", styleHeading("Version error:"), styleError(errText))
	}
}
