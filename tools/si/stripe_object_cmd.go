package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/stripebridge"
)

func cmdStripeObject(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si stripe object <list|get|create|update|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdStripeObjectList(args[1:])
	case "get":
		cmdStripeObjectGet(args[1:])
	case "create":
		cmdStripeObjectCreate(args[1:])
	case "update":
		cmdStripeObjectUpdate(args[1:])
	case "delete":
		cmdStripeObjectDelete(args[1:])
	default:
		printUnknown("stripe object", args[0])
	}
}

func cmdStripeObjectList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json": true,
		"raw":  true,
	})
	fs := flag.NewFlagSet("stripe object list", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	limit := fs.Int("limit", 100, "max objects to return (-1 for all)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si stripe object list <object> [--limit N] [--param key=value] [--account <alias>] [--env <live|sandbox>] [--json]")
		return
	}
	spec, err := stripebridge.ResolveObject(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := client.ListAll(ctx, spec.ListPath, parseStripeParams(params), *limit)
	if err != nil {
		printStripeError(err)
		return
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"object": spec.Name, "count": len(items), "data": items}); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		rawBody, _ := json.Marshal(items)
		fmt.Println(string(rawBody))
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Object list:"), spec.Name, len(items))
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		name := inferStripeObjectName(item)
		rows = append(rows, []string{orDash(id), orDash(name)})
	}
	printAlignedRows(rows, 2, "  ")
}

func cmdStripeObjectGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json": true,
		"raw":  true,
	})
	fs := flag.NewFlagSet("stripe object get", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si stripe object get <object> <id> [--param key=value] [--account <alias>] [--env <live|sandbox>] [--json]")
		return
	}
	spec, err := stripebridge.ResolveObject(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	id := strings.TrimSpace(fs.Arg(1))
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.ExecuteCRUD(ctx, spec, stripebridge.CRUDGet, id, parseStripeParams(params), "")
	if err != nil {
		printStripeError(err)
		return
	}
	printStripeResponse(resp, *jsonOut, *raw)
}

func cmdStripeObjectCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json": true,
		"raw":  true,
	})
	fs := flag.NewFlagSet("stripe object create", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for mutation safety")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si stripe object create <object> [--param key=value] [--idempotency-key <key>] [--account <alias>] [--env <live|sandbox>] [--json]")
		return
	}
	spec, err := stripebridge.ResolveObject(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.ExecuteCRUD(ctx, spec, stripebridge.CRUDCreate, "", parseStripeParams(params), strings.TrimSpace(*idempotencyKey))
	if err != nil {
		printStripeError(err)
		return
	}
	printStripeResponse(resp, *jsonOut, *raw)
}

func cmdStripeObjectUpdate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json": true,
		"raw":  true,
	})
	fs := flag.NewFlagSet("stripe object update", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for mutation safety")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si stripe object update <object> <id> [--param key=value] [--idempotency-key <key>] [--account <alias>] [--env <live|sandbox>] [--json]")
		return
	}
	spec, err := stripebridge.ResolveObject(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	id := strings.TrimSpace(fs.Arg(1))
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.ExecuteCRUD(ctx, spec, stripebridge.CRUDUpdate, id, parseStripeParams(params), strings.TrimSpace(*idempotencyKey))
	if err != nil {
		printStripeError(err)
		return
	}
	printStripeResponse(resp, *jsonOut, *raw)
}

func cmdStripeObjectDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json":  true,
		"raw":   true,
		"force": true,
	})
	fs := flag.NewFlagSet("stripe object delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for mutation safety")
	params := multiFlag{}
	fs.Var(&params, "param", "request parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si stripe object delete <object> <id> [--force] [--idempotency-key <key>] [--account <alias>] [--env <live|sandbox>] [--json]")
		return
	}
	spec, err := stripebridge.ResolveObject(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	id := strings.TrimSpace(fs.Arg(1))
	if err := requireStripeConfirmation("delete "+spec.Name+" "+id, *force); err != nil {
		fatal(err)
	}
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.ExecuteCRUD(ctx, spec, stripebridge.CRUDDelete, id, parseStripeParams(params), strings.TrimSpace(*idempotencyKey))
	if err != nil {
		printStripeError(err)
		return
	}
	printStripeResponse(resp, *jsonOut, *raw)
}

func cmdStripeRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json": true,
		"raw":  true,
	})
	fs := flag.NewFlagSet("stripe raw", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override stripe api key")
	method := fs.String("method", "GET", "http method")
	path := fs.String("path", "", "api path (for example /v1/products)")
	body := fs.String("body", "", "raw body payload")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for mutation safety")
	params := multiFlag{}
	fs.Var(&params, "param", "request parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si stripe raw --method <GET|POST|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]")
		return
	}
	runtime, client := mustStripeClient(*account, *env, *apiKey)
	printStripeContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, stripebridge.Request{
		Method:         strings.ToUpper(strings.TrimSpace(*method)),
		Path:           strings.TrimSpace(*path),
		Params:         parseStripeParams(params),
		RawBody:        strings.TrimSpace(*body),
		IdempotencyKey: strings.TrimSpace(*idempotencyKey),
	})
	if err != nil {
		printStripeError(err)
		return
	}
	printStripeResponse(resp, *jsonOut, *raw)
}

func mustStripeClient(account string, env string, apiKey string) (stripeRuntimeContext, stripeBridgeClient) {
	runtime, err := resolveStripeRuntimeContext(account, env, apiKey)
	if err != nil {
		fatal(err)
	}
	client, err := buildStripeClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func printStripeContextBanner(runtime stripeRuntimeContext) {
	fmt.Printf("%s %s\n", styleDim("stripe context:"), formatStripeContext(runtime))
}

func parseStripeParams(values []string) map[string]string {
	out := map[string]string{}
	for _, entry := range values {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(parts[1])
	}
	return out
}

func inferStripeObjectName(item map[string]any) string {
	candidates := []string{"name", "description", "email", "code", "nickname"}
	for _, key := range candidates {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	if value, ok := item["unit_amount"].(float64); ok {
		currency, _ := item["currency"].(string)
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', 0, 64) + " " + strings.ToUpper(currency))
	}
	return "-"
}

func stripeFlagsFirst(args []string, boolFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		flagName := strings.TrimLeft(arg, "-")
		if idx := strings.Index(flagName, "="); idx != -1 {
			continue
		}
		if boolFlags[flagName] {
			continue
		}
		if i+1 < len(args) {
			next := strings.TrimSpace(args[i+1])
			if next != "" && (!strings.HasPrefix(next, "-") || isSignedNumberToken(next)) {
				flags = append(flags, next)
				i++
			}
		}
	}
	return append(flags, positionals...)
}

func isSignedNumberToken(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	if !strings.HasPrefix(value, "-") {
		return false
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return true
	}
	return false
}
