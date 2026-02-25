package main

import (
	"flag"
	"fmt"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultHistory(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("vault history", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	limit := fs.Int("limit", 20, "max revisions to display")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault history <KEY> [--scope <name>] [--limit <n>] [--json]")
		return
	}
	key := strings.TrimSpace(rest[0])
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
	}
	if *limit <= 0 {
		fatal(fmt.Errorf("invalid --limit %d", *limit))
	}

	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	target, err := vaultResolveTargetStatus(settings, scope)
	if err != nil {
		fatal(err)
	}
	revs, used, err := vaultSunKVListHistory(settings, target, key, *limit)
	if err != nil {
		fatal(err)
	}
	if !used {
		fatal(fmt.Errorf("sun vault key history unavailable: configure `si sun auth login` first"))
	}
	if *jsonOut {
		printJSON(map[string]any{
			"scope":     strings.TrimSpace(target.File),
			"source":    "sun-kv",
			"key":       key,
			"limit":     *limit,
			"revisions": revs,
		})
		return
	}

	fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
	fmt.Printf("source: sun-kv\n")
	fmt.Printf("key: %s\n", key)
	if len(revs) == 0 {
		fmt.Printf("history: none\n")
		return
	}
	for _, rev := range revs {
		operation := strings.TrimSpace(formatAny(rev.Metadata["operation"]))
		if operation == "" {
			operation = "set"
		}
		deleted := vaultSunKVMetaBool(rev.Metadata, "deleted")
		changedAt := strings.TrimSpace(formatAny(rev.Metadata["changed_at"]))
		if changedAt == "" {
			changedAt = strings.TrimSpace(rev.CreatedAt)
		}
		extra := ""
		if deleted {
			extra = " deleted=true"
		}
		if hash := strings.TrimSpace(formatAny(rev.Metadata["value_sha256"])); hash != "" {
			extra += " value_sha256=" + hash
		}
		fmt.Printf("  rev=%d changed_at=%s operation=%s%s\n", rev.Revision, changedAt, operation, extra)
	}
}
