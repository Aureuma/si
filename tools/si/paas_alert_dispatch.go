package main

import (
	"fmt"
	"sort"
	"strings"
)

func emitPaasOperationalAlert(command, severity, target, message, guidance string, fields map[string]string) string {
	resolvedSeverity := strings.ToLower(strings.TrimSpace(severity))
	if resolvedSeverity == "" {
		resolvedSeverity = "warning"
	}
	resolvedCommand := strings.TrimSpace(command)
	if resolvedCommand == "" {
		resolvedCommand = "alert operational"
	}
	resolvedMessage := strings.TrimSpace(message)
	if resolvedMessage == "" {
		resolvedMessage = "si paas operational alert"
	}
	callbackHints := buildPaasAlertCallbackHints(resolvedCommand, target, fields, resolvedSeverity)
	fields = copyPaasFields(fields)
	for key, value := range callbackHints {
		fields[key] = value
	}
	route, _, err := resolvePaasAlertRoute(resolvedSeverity)
	status := "sent"
	channel := route
	if err != nil {
		route = "telegram"
		channel = "telegram"
		status = "failed"
	}
	switch route {
	case "disabled":
		status = "suppressed"
	default:
		cfg, _, loadErr := loadPaasTelegramConfig(currentPaasContext())
		if loadErr != nil {
			status = "failed"
			fields = appendPaasAlertDispatchField(fields, "delivery_error", loadErr.Error())
		} else if sendErr := sendPaasTelegramMessage(cfg, appendPaasAlertCallbackHintsToMessage(
			formatPaasOperationalAlertMessage(resolvedSeverity, resolvedCommand, target, resolvedMessage),
			callbackHints,
		)); sendErr != nil {
			status = "failed"
			fields = appendPaasAlertDispatchField(fields, "delivery_error", sendErr.Error())
		}
	}
	fields = appendPaasAlertDispatchField(fields, "channel", channel)
	historyPath := recordPaasAlertEntry(paasAlertEntry{
		Command:  resolvedCommand,
		Severity: resolvedSeverity,
		Status:   status,
		Target:   strings.TrimSpace(target),
		Message:  resolvedMessage,
		Guidance: strings.TrimSpace(guidance),
		Fields:   fields,
	})
	return historyPath
}

func formatPaasOperationalAlertMessage(severity, command, target, message string) string {
	if strings.TrimSpace(target) == "" {
		return fmt.Sprintf("[%s] %s: %s", strings.ToUpper(strings.TrimSpace(severity)), strings.TrimSpace(command), strings.TrimSpace(message))
	}
	return fmt.Sprintf("[%s] %s (%s): %s", strings.ToUpper(strings.TrimSpace(severity)), strings.TrimSpace(command), strings.TrimSpace(target), strings.TrimSpace(message))
}

func appendPaasAlertDispatchField(fields map[string]string, key, value string) map[string]string {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return fields
	}
	out := copyPaasFields(fields)
	if out == nil {
		out = map[string]string{}
	}
	out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	return out
}

func buildPaasAlertCallbackHints(command, target string, fields map[string]string, severity string) map[string]string {
	hints := map[string]string{}
	targetName := strings.TrimSpace(target)
	if targetName == "" {
		targetName = strings.TrimSpace(fields["target"])
	}
	app := strings.TrimSpace(fields["app"])
	release := strings.TrimSpace(fields["release"])

	if targetName != "" {
		logsCmd := "si paas logs --target " + targetName + " --tail 200"
		if app != "" {
			logsCmd += " --app " + app
		}
		hints["callback_view_logs"] = logsCmd
	}
	if app != "" && targetName != "" {
		rollbackCmd := "si paas rollback --app " + app + " --target " + targetName + " --apply"
		if release != "" {
			rollbackCmd += " --to-release " + release
		}
		hints["callback_rollback"] = rollbackCmd
	}
	ackCmd := "si paas alert acknowledge"
	if targetName != "" {
		ackCmd += " --target " + targetName
	}
	if command != "" {
		ackCmd += " --command " + quoteSingle(command)
	}
	ackCmd += " --note " + quoteSingle("acknowledged "+strings.ToLower(strings.TrimSpace(severity))+" alert")
	hints["callback_acknowledge"] = ackCmd
	return hints
}

func appendPaasAlertCallbackHintsToMessage(message string, hints map[string]string) string {
	if len(hints) == 0 {
		return message
	}
	keys := make([]string, 0, len(hints))
	for key := range hints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(message))
	b.WriteString("\n\nActions:")
	for _, key := range keys {
		value := strings.TrimSpace(hints[key])
		if value == "" {
			continue
		}
		label := strings.TrimPrefix(key, "callback_")
		b.WriteString("\n- ")
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(value)
	}
	return b.String()
}
