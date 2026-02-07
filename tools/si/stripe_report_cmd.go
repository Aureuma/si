package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/stripebridge"
)

func cmdStripeReport(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json": true,
	})
	fs := flag.NewFlagSet("stripe report", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	from := fs.String("from", "", "start timestamp (unix seconds or RFC3339)")
	to := fs.String("to", "", "end timestamp (unix seconds or RFC3339)")
	jsonOut := fs.Bool("json", false, "output json")
	limit := fs.Int("limit", 100, "sample limit for list-based reports")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si stripe report <revenue-summary|payment-intent-status|subscription-churn|balance-overview> [--from <time>] [--to <time>] [--json]")
		return
	}
	preset := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)

	windowFrom, err := parseReportTime(*from)
	if err != nil {
		fatal(err)
	}
	windowTo, err := parseReportTime(*to)
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	report, err := runStripeReport(ctx, client, preset, windowFrom, windowTo, *limit)
	if err != nil {
		printStripeError(err)
		return
	}
	report["preset"] = preset
	report["context"] = map[string]string{
		"account_alias": runtime.AccountAlias,
		"account_id":    runtime.AccountID,
		"environment":   string(runtime.Environment),
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Report:"), preset)
	printKeyValueMap(report)
}

func runStripeReport(ctx context.Context, client stripeBridgeClient, preset string, from *time.Time, to *time.Time, limit int) (map[string]any, error) {
	switch preset {
	case "revenue-summary":
		return reportRevenueSummary(ctx, client, from, to, limit)
	case "payment-intent-status":
		return reportPaymentIntentStatus(ctx, client, from, to, limit)
	case "subscription-churn":
		return reportSubscriptionChurn(ctx, client, from, to, limit)
	case "balance-overview":
		return reportBalanceOverview(ctx, client, limit)
	default:
		return nil, fmt.Errorf("unknown report preset %q", preset)
	}
}

func reportRevenueSummary(ctx context.Context, client stripeBridgeClient, from *time.Time, to *time.Time, limit int) (map[string]any, error) {
	params := map[string]string{"type": "charge"}
	addCreatedRange(params, from, to)
	items, err := client.ListAll(ctx, "/v1/balance_transactions", params, limit)
	if err != nil {
		return nil, err
	}
	var amount int64
	var fee int64
	currency := ""
	for _, item := range items {
		if val, ok := readIntLike(item["amount"]); ok {
			amount += val
		}
		if val, ok := readIntLike(item["fee"]); ok {
			fee += val
		}
		if currency == "" {
			if c, _ := item["currency"].(string); strings.TrimSpace(c) != "" {
				currency = strings.ToUpper(strings.TrimSpace(c))
			}
		}
	}
	return map[string]any{
		"transactions": len(items),
		"gross_amount": amount,
		"fees":         fee,
		"net_amount":   amount - fee,
		"currency":     orDash(currency),
	}, nil
}

func reportPaymentIntentStatus(ctx context.Context, client stripeBridgeClient, from *time.Time, to *time.Time, limit int) (map[string]any, error) {
	params := map[string]string{}
	addCreatedRange(params, from, to)
	items, err := client.ListAll(ctx, "/v1/payment_intents", params, limit)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, item := range items {
		status, _ := item["status"].(string)
		status = strings.TrimSpace(status)
		if status == "" {
			status = "unknown"
		}
		counts[status]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := map[string]any{}
	for _, key := range keys {
		ordered[key] = counts[key]
	}
	return map[string]any{
		"total":           len(items),
		"status_counts":   ordered,
		"sampled_records": limit,
	}, nil
}

func reportSubscriptionChurn(ctx context.Context, client stripeBridgeClient, from *time.Time, to *time.Time, limit int) (map[string]any, error) {
	params := map[string]string{}
	addCreatedRange(params, from, to)
	items, err := client.ListAll(ctx, "/v1/subscriptions", params, limit)
	if err != nil {
		return nil, err
	}
	total := len(items)
	canceled := 0
	active := 0
	pastDue := 0
	for _, item := range items {
		status, _ := item["status"].(string)
		switch strings.TrimSpace(status) {
		case "canceled":
			canceled++
		case "active", "trialing":
			active++
		case "past_due", "unpaid":
			pastDue++
		}
	}
	churnPct := 0.0
	if total > 0 {
		churnPct = (float64(canceled) / float64(total)) * 100
	}
	return map[string]any{
		"total":       total,
		"canceled":    canceled,
		"active":      active,
		"past_due":    pastDue,
		"churn_pct":   fmt.Sprintf("%.2f", churnPct),
		"sampled_max": limit,
	}, nil
}

func reportBalanceOverview(ctx context.Context, client stripeBridgeClient, limit int) (map[string]any, error) {
	balanceResp, err := client.Do(ctx, stripebridge.Request{Method: "GET", Path: "/v1/balance"})
	if err != nil {
		return nil, err
	}
	payouts, err := client.ListAll(ctx, "/v1/payouts", map[string]string{}, limit)
	if err != nil {
		return nil, err
	}
	pending := int64(0)
	available := int64(0)
	if balanceResp.Data != nil {
		pending = sumBalanceAmounts(balanceResp.Data["pending"])
		available = sumBalanceAmounts(balanceResp.Data["available"])
	}
	return map[string]any{
		"available_amount": available,
		"pending_amount":   pending,
		"recent_payouts":   len(payouts),
	}, nil
}

func addCreatedRange(params map[string]string, from *time.Time, to *time.Time) {
	if params == nil {
		return
	}
	if from != nil {
		params["created[gte]"] = strconv.FormatInt(from.UTC().Unix(), 10)
	}
	if to != nil {
		params["created[lte]"] = strconv.FormatInt(to.UTC().Unix(), 10)
	}
}

func parseReportTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		value := time.Unix(seconds, 0).UTC()
		return &value, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("invalid time %q (use unix seconds or RFC3339)", raw)
	}
	value := parsed.UTC()
	return &value, nil
}

func readIntLike(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		num, err := typed.Int64()
		return num, err == nil
	default:
		return 0, false
	}
}

func sumBalanceAmounts(value any) int64 {
	items, ok := value.([]any)
	if !ok {
		return 0
	}
	var total int64
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if amount, ok := readIntLike(record["amount"]); ok {
			total += amount
		}
	}
	return total
}
