package main

import (
	"fmt"
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
		} else if sendErr := sendPaasTelegramMessage(cfg, formatPaasOperationalAlertMessage(resolvedSeverity, resolvedCommand, target, resolvedMessage)); sendErr != nil {
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
