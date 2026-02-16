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
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, providersUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
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
	providerID := fs.String("provider", "", "provider id (github, cloudflare, google_places, google_play, apple_appstore, youtube, stripe, social_*, workos, aws_iam, gcp_serviceusage, oci_core)")
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
	headers := []string{
		styleHeading("PROVIDER"),
		styleHeading("RATE"),
		styleHeading("BURST"),
		styleHeading("AUTH"),
		styleHeading("CAPS"),
		styleHeading("PUBLIC PROBE"),
	}
	tableRows := make([][]string, 0, len(rows))
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
		tableRows = append(tableRows, []string{
			entry.Provider,
			formatRate(entry.RatePerSecond),
			strconv.Itoa(entry.Burst),
			orDash(entry.AuthStyle),
			capsText,
			probe,
		})
	}
	printAlignedTable(headers, tableRows, 2)
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
	providerID := fs.String("provider", "", "provider id (github, cloudflare, google_places, google_play, apple_appstore, youtube, stripe, social_*, workos, aws_iam, gcp_serviceusage, oci_core)")
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
	headers := []string{
		styleHeading("PROVIDER"),
		styleHeading("SUBJECT"),
		styleHeading("REQ"),
		styleHeading("2XX"),
		styleHeading("429"),
		styleHeading("5XX"),
		styleHeading("P50"),
		styleHeading("P95"),
		styleHeading("CIRCUIT"),
	}
	tableRows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		circuit := entry.CircuitState
		if strings.TrimSpace(circuit) == "" {
			circuit = "closed"
		}
		if circuit == "open" && !entry.OpenUntil.IsZero() {
			circuit = fmt.Sprintf("open until %s", formatDateWithGitHubRelativeNow(entry.OpenUntil))
		}
		tableRows = append(tableRows, []string{
			string(entry.Provider),
			orDash(entry.Subject),
			strconv.FormatInt(entry.Requests, 10),
			strconv.FormatInt(entry.Success, 10),
			strconv.FormatInt(entry.TooManyRequests, 10),
			strconv.FormatInt(entry.ServerErrors5xx, 10),
			strconv.FormatInt(entry.P50LatencyMS, 10),
			strconv.FormatInt(entry.P95LatencyMS, 10),
			circuit,
		})
	}
	printAlignedTable(headers, tableRows, 2)
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
