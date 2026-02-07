package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"si/tools/si/internal/stripebridge"
)

func cmdStripeSync(args []string) {
	if len(args) < 2 {
		printUsage("usage: si stripe sync live-to-sandbox <plan|apply> [flags]")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(args[0]))
	action := strings.ToLower(strings.TrimSpace(args[1]))
	rest := args[2:]
	if mode != "live-to-sandbox" {
		fatal(fmt.Errorf("unsupported sync mode %q (expected live-to-sandbox)", mode))
	}
	switch action {
	case "plan":
		cmdStripeSyncPlan(rest)
	case "apply":
		cmdStripeSyncApply(rest)
	default:
		printUnknown("stripe sync", action)
	}
}

func cmdStripeSyncPlan(args []string) {
	fs := flag.NewFlagSet("stripe sync plan", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	liveKey := fs.String("live-api-key", "", "override live api key")
	sandboxKey := fs.String("sandbox-api-key", "", "override sandbox api key")
	only := multiFlag{}
	fs.Var(&only, "only", "sync family filter (repeatable or comma-separated)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe sync live-to-sandbox plan [--account <alias>] [--only <family>] [--json]")
		return
	}
	plan, err := buildSyncPlan(*account, *liveKey, *sandboxKey, only)
	if err != nil {
		printStripeError(err)
		return
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(plan); err != nil {
			fatal(err)
		}
		return
	}
	printSyncPlan(plan)
}

func cmdStripeSyncApply(args []string) {
	fs := flag.NewFlagSet("stripe sync apply", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	liveKey := fs.String("live-api-key", "", "override live api key")
	sandboxKey := fs.String("sandbox-api-key", "", "override sandbox api key")
	only := multiFlag{}
	fs.Var(&only, "only", "sync family filter (repeatable or comma-separated)")
	dryRun := fs.Bool("dry-run", false, "plan changes without applying")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe sync live-to-sandbox apply [--account <alias>] [--only <family>] [--dry-run] [--force] [--json]")
		return
	}
	plan, liveRuntime, sandboxRuntime, liveClient, sandboxClient, err := buildSyncPlanWithClients(*account, *liveKey, *sandboxKey, only)
	if err != nil {
		printStripeError(err)
		return
	}
	fmt.Printf("%s live=%s sandbox=%s\n", styleDim("sync context:"), formatStripeContext(liveRuntime), formatStripeContext(sandboxRuntime))
	if err := requireStripeConfirmation("apply live-to-sandbox sync", *force || *dryRun); err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if *dryRun {
		result, err := stripebridge.ApplyLiveToSandboxPlan(ctx, sandboxClient, plan, stripebridge.ApplyOptions{
			DryRun:         true,
			IdempotencyKey: strings.TrimSpace(*idempotencyKey),
		})
		if err != nil {
			printStripeError(err)
			return
		}
		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(result)
			return
		}
		successf("dry-run complete: skipped=%d", result.Skipped)
		return
	}
	_ = liveClient
	result, err := stripebridge.ApplyLiveToSandboxPlan(ctx, sandboxClient, plan, stripebridge.ApplyOptions{
		DryRun:         false,
		IdempotencyKey: strings.TrimSpace(*idempotencyKey),
	})
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	}
	if err != nil {
		printStripeError(err)
		return
	}
	successf("sync apply complete: applied=%d skipped=%d", result.Applied, result.Skipped)
}

func buildSyncPlan(account string, liveKey string, sandboxKey string, familiesRaw []string) (stripebridge.SyncPlan, error) {
	plan, _, _, _, _, err := buildSyncPlanWithClients(account, liveKey, sandboxKey, familiesRaw)
	return plan, err
}

func buildSyncPlanWithClients(account string, liveKey string, sandboxKey string, familiesRaw []string) (stripebridge.SyncPlan, stripeRuntimeContext, stripeRuntimeContext, *stripebridge.Client, *stripebridge.Client, error) {
	families, err := stripebridge.ParseSyncFamilies(familiesRaw)
	if err != nil {
		return stripebridge.SyncPlan{}, stripeRuntimeContext{}, stripeRuntimeContext{}, nil, nil, err
	}
	liveRuntime, err := resolveStripeRuntimeContext(account, "live", liveKey)
	if err != nil {
		return stripebridge.SyncPlan{}, stripeRuntimeContext{}, stripeRuntimeContext{}, nil, nil, err
	}
	sandboxRuntime, err := resolveStripeRuntimeContext(account, "sandbox", sandboxKey)
	if err != nil {
		return stripebridge.SyncPlan{}, stripeRuntimeContext{}, stripeRuntimeContext{}, nil, nil, err
	}
	liveClient, err := buildStripeClient(liveRuntime)
	if err != nil {
		return stripebridge.SyncPlan{}, stripeRuntimeContext{}, stripeRuntimeContext{}, nil, nil, err
	}
	sandboxClient, err := buildStripeClient(sandboxRuntime)
	if err != nil {
		return stripebridge.SyncPlan{}, stripeRuntimeContext{}, stripeRuntimeContext{}, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	plan, err := stripebridge.BuildLiveToSandboxPlan(ctx, liveClient, sandboxClient, families)
	if err != nil {
		return stripebridge.SyncPlan{}, stripeRuntimeContext{}, stripeRuntimeContext{}, nil, nil, err
	}
	return plan, liveRuntime, sandboxRuntime, liveClient, sandboxClient, nil
}

func printSyncPlan(plan stripebridge.SyncPlan) {
	fmt.Printf("%s %s\n", styleHeading("Sync plan generated:"), plan.GeneratedAt.Format(time.RFC3339))
	fmt.Printf("%s %d actions\n", styleHeading("Total actions:"), len(plan.Actions))
	for _, action := range plan.Actions {
		fmt.Printf("  %s %-8s live=%s sandbox=%s %s\n",
			padRightANSI(string(action.Family), 16),
			string(action.Action),
			orDash(action.LiveID),
			orDash(action.SandboxID),
			orDash(action.Reason),
		)
	}
}
