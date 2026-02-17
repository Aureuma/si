package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const paasUsageText = "usage: si paas [--context <name>] <target|app|deploy|rollback|logs|alert|secret|ai|context|doctor|agent|events> [args...]"

const defaultPaasContext = "default"

var paasCommandContext = defaultPaasContext

var paasActions = []subcommandAction{
	{Name: "target", Description: "manage VPS target inventory"},
	{Name: "app", Description: "manage app lifecycle metadata"},
	{Name: "deploy", Description: "deploy app releases"},
	{Name: "rollback", Description: "rollback app releases"},
	{Name: "logs", Description: "view app and service logs"},
	{Name: "alert", Description: "configure and test alerts"},
	{Name: "secret", Description: "manage app secrets via si vault"},
	{Name: "ai", Description: "run Codex-assisted operations"},
	{Name: "context", Description: "manage isolated paas contexts"},
	{Name: "doctor", Description: "run isolation and secret exposure checks"},
	{Name: "agent", Description: "manage long-running agents"},
	{Name: "events", Description: "query operational events"},
}

const (
	paasTargetUsageText   = "usage: si paas target <add|list|check|use|remove|bootstrap|ingress-baseline> [args...]"
	paasAppUsageText      = "usage: si paas app <init|list|status|remove|addon> [args...]"
	paasDeployUsageText   = "usage: si paas deploy [prune ...] [reconcile ...] [webhook ...] [bluegreen ...] [--app <slug>] [--target <id>] [--targets <id1,id2|all>] [--strategy <serial|rolling|canary|parallel>] [--max-parallel <n>] [--continue-on-error] [--release <id>] [--compose-file <path>] [--bundle-root <path>] [--apply] [--remote-dir <path>] [--apply-timeout <duration>] [--health-cmd <command>] [--health-timeout <duration>] [--rollback-on-failure[=true|false]] [--rollback-timeout <duration>] [--vault-file <path>] [--wait-timeout <duration>] [--allow-plaintext-secrets] [--allow-untrusted-vault] [--json]"
	paasRollbackUsageText = "usage: si paas rollback [--app <slug>] [--target <id>] [--targets <id1,id2|all>] [--to-release <id>] [--bundle-root <path>] [--strategy <serial|rolling|canary|parallel>] [--max-parallel <n>] [--continue-on-error] [--apply] [--remote-dir <path>] [--apply-timeout <duration>] [--health-cmd <command>] [--health-timeout <duration>] [--wait-timeout <duration>] [--vault-file <path>] [--allow-untrusted-vault] [--json]"
	paasLogsUsageText     = "usage: si paas logs [--app <slug>] [--target <id>] [--service <name>] [--tail <n>] [--follow] [--since <duration>] [--json]"
	paasAlertUsageText    = "usage: si paas alert <setup-telegram|test|history|acknowledge|policy|ingress-tls> [args...]"
	paasSecretUsageText   = "usage: si paas secret <set|get|unset|list|key> [args...]"
	paasAIUsageText       = "usage: si paas ai <plan|inspect|fix> [args...]"
	paasContextUsageText  = "usage: si paas context <create|init|list|use|show|remove|export|import> [args...]"
	paasDoctorUsageText   = "usage: si paas doctor [--json]"
	paasAgentUsageText    = "usage: si paas agent <enable|disable|status|logs|run-once|approve|deny> [args...]"
	paasEventsUsageText   = "usage: si paas events <list> [args...]"
)

func cmdPaas(args []string) {
	filtered, contextName, ok := parsePaasContextFlag(args)
	if !ok {
		return
	}
	if !isPaasDoctorInvocation(filtered) {
		if err := enforcePaasStateRootIsolationGuardrail(); err != nil {
			fatal(err)
		}
	}
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(filtered, isInteractiveTerminal(), selectPaasAction)
	if showUsage {
		printUsage(paasUsageText)
		return
	}
	if !ok {
		return
	}
	args = resolved
	previousContext := paasCommandContext
	paasCommandContext = contextName
	defer func() {
		paasCommandContext = previousContext
	}()
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasUsageText)
	case "target":
		cmdPaasTarget(rest)
	case "app":
		cmdPaasApp(rest)
	case "deploy":
		cmdPaasDeploy(rest)
	case "rollback":
		cmdPaasRollback(rest)
	case "logs":
		cmdPaasLogs(rest)
	case "alert":
		cmdPaasAlert(rest)
	case "secret":
		cmdPaasSecret(rest)
	case "ai":
		cmdPaasAI(rest)
	case "context":
		cmdPaasContext(rest)
	case "doctor":
		cmdPaasDoctor(rest)
	case "agent":
		cmdPaasAgent(rest)
	case "events":
		cmdPaasEvents(rest)
	default:
		printUnknown("paas", sub)
		printUsage(paasUsageText)
		os.Exit(1)
	}
}

func selectPaasAction() (string, bool) {
	return selectSubcommandAction("PaaS commands:", paasActions)
}

type paasScaffoldEnvelope struct {
	OK      bool              `json:"ok"`
	Command string            `json:"command"`
	Context string            `json:"context"`
	Mode    string            `json:"mode"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func printPaasScaffold(command string, fields map[string]string, jsonOut bool) {
	fields = redactPaasSensitiveFields(fields)
	if jsonOut {
		envelope := paasScaffoldEnvelope{
			OK:      true,
			Command: command,
			Context: currentPaasContext(),
			Mode:    "scaffold",
			Fields:  fields,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(envelope); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent(command, "succeeded", "scaffold", fields, nil)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("si paas:"), command)
	fmt.Printf("  context=%s\n", currentPaasContext())
	if len(fields) == 0 {
		return
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %s=%s\n", key, fields[key])
	}
	_ = recordPaasAuditEvent(command, "succeeded", "scaffold", fields, nil)
}

func parsePaasJSONFlag(args []string) ([]string, bool) {
	jsonOut := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		value := strings.TrimSpace(arg)
		switch {
		case value == "--json":
			jsonOut = true
		case strings.HasPrefix(value, "--json="):
			tail := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "--json=")))
			switch tail {
			case "", "true", "1", "yes", "on":
				jsonOut = true
			case "false", "0", "no", "off":
				// Explicitly disabled.
			default:
				filtered = append(filtered, arg)
			}
		default:
			filtered = append(filtered, arg)
		}
	}
	return filtered, jsonOut
}

func requirePaasValue(value, flagName, usageText string) bool {
	if strings.TrimSpace(value) != "" {
		return true
	}
	fmt.Fprintf(os.Stderr, "missing required --%s\n", flagName)
	printUsage(usageText)
	return false
}

func parseCSV(value string) []string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeTargets(single, multi string) []string {
	if strings.TrimSpace(multi) != "" {
		if strings.EqualFold(strings.TrimSpace(multi), "all") {
			return []string{"all"}
		}
		return parseCSV(multi)
	}
	if strings.TrimSpace(single) == "" {
		return nil
	}
	return []string{strings.TrimSpace(single)}
}

func formatTargets(targets []string) string {
	if len(targets) == 0 {
		return ""
	}
	return strings.Join(targets, ",")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func intString(v int) string {
	return strconv.Itoa(v)
}

func currentPaasContext() string {
	value := strings.TrimSpace(paasCommandContext)
	if value == "" {
		return defaultPaasContext
	}
	return value
}

func parsePaasContextFlag(args []string) ([]string, string, bool) {
	contextName := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		value := strings.TrimSpace(args[i])
		switch {
		case value == "--context":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "missing value for --context")
				printUsage(paasUsageText)
				return nil, "", false
			}
			next := strings.TrimSpace(args[i+1])
			if next == "" {
				fmt.Fprintln(os.Stderr, "missing value for --context")
				printUsage(paasUsageText)
				return nil, "", false
			}
			contextName = next
			i++
		case strings.HasPrefix(value, "--context="):
			assigned := strings.TrimSpace(strings.TrimPrefix(value, "--context="))
			if assigned == "" {
				fmt.Fprintln(os.Stderr, "missing value for --context")
				printUsage(paasUsageText)
				return nil, "", false
			}
			contextName = assigned
		default:
			filtered = append(filtered, args[i])
		}
	}
	if strings.TrimSpace(contextName) == "" {
		// Respect persisted context selection only when state root is explicitly scoped.
		if strings.TrimSpace(os.Getenv(paasStateRootEnvKey)) != "" {
			if selected, err := loadPaasSelectedContext(); err == nil {
				contextName = strings.TrimSpace(selected)
			}
		}
	}
	if strings.TrimSpace(contextName) == "" {
		contextName = defaultPaasContext
	}
	return filtered, contextName, true
}

func isPaasDoctorInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	return sub == "doctor"
}
