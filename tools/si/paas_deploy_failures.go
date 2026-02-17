package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	paasFailureInvalidArgument      = "PAAS_INVALID_ARGUMENT"
	paasFailurePlaintextSecrets     = "PAAS_PRECHECK_PLAINTEXT_SECRETS"
	paasFailureVaultTrust           = "PAAS_PRECHECK_VAULT_TRUST"
	paasFailureBundleCreate         = "PAAS_BUNDLE_CREATE_FAILED"
	paasFailureTargetResolution     = "PAAS_TARGET_RESOLUTION_FAILED"
	paasFailureRemoteUpload         = "PAAS_REMOTE_UPLOAD_FAILED"
	paasFailureRemoteApply          = "PAAS_REMOTE_APPLY_FAILED"
	paasFailureHealthCheck          = "PAAS_HEALTH_CHECK_FAILED"
	paasFailureRollbackResolve      = "PAAS_ROLLBACK_RESOLVE_FAILED"
	paasFailureRollbackApply        = "PAAS_ROLLBACK_APPLY_FAILED"
	paasFailureRollbackBundle       = "PAAS_ROLLBACK_BUNDLE_NOT_FOUND"
	paasFailureWebhookAuth          = "PAAS_WEBHOOK_AUTH_FAILED"
	paasFailureWebhookPayload       = "PAAS_WEBHOOK_PAYLOAD_INVALID"
	paasFailureWebhookMapping       = "PAAS_WEBHOOK_MAPPING_NOT_FOUND"
	paasFailureUnknown              = "PAAS_UNKNOWN_FAILURE"
	defaultPaasFailureRemediation   = "inspect stderr details and retry after correcting inputs/target state"
	defaultPaasFailureOperationMode = "live"
)

type paasOperationFailure struct {
	Code        string
	Stage       string
	Target      string
	Remediation string
	Err         error
}

func (f *paasOperationFailure) Error() string {
	if f == nil || f.Err == nil {
		return ""
	}
	return strings.TrimSpace(f.Err.Error())
}

func (f *paasOperationFailure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Err
}

func newPaasOperationFailure(code, stage, target, remediation string, err error) error {
	return &paasOperationFailure{
		Code:        strings.TrimSpace(code),
		Stage:       strings.TrimSpace(stage),
		Target:      strings.TrimSpace(target),
		Remediation: strings.TrimSpace(remediation),
		Err:         err,
	}
}

func asPaasOperationFailure(err error) paasOperationFailure {
	var opErr *paasOperationFailure
	if errors.As(err, &opErr) && opErr != nil {
		code := strings.TrimSpace(opErr.Code)
		if code == "" {
			code = paasFailureUnknown
		}
		remediation := strings.TrimSpace(opErr.Remediation)
		if remediation == "" {
			remediation = defaultPaasFailureRemediation
		}
		return paasOperationFailure{
			Code:        code,
			Stage:       strings.TrimSpace(opErr.Stage),
			Target:      strings.TrimSpace(opErr.Target),
			Remediation: remediation,
			Err:         opErr.Err,
		}
	}
	message := strings.TrimSpace(errString(err))
	if message == "" {
		message = "unknown failure"
	}
	return paasOperationFailure{
		Code:        paasFailureUnknown,
		Remediation: defaultPaasFailureRemediation,
		Err:         fmt.Errorf("%s", message),
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func failPaasCommand(command string, jsonOut bool, err error, fields map[string]string) {
	f := asPaasOperationFailure(err)
	fields = redactPaasSensitiveFields(fields)
	_ = recordPaasAuditEvent(strings.TrimSpace(command), "failed", defaultPaasFailureOperationMode, fields, err)
	message := errString(f.Err)
	if jsonOut {
		payload := map[string]any{
			"ok":      false,
			"command": strings.TrimSpace(command),
			"context": currentPaasContext(),
			"mode":    defaultPaasFailureOperationMode,
			"error": map[string]any{
				"code":        f.Code,
				"stage":       f.Stage,
				"target":      f.Target,
				"message":     message,
				"remediation": f.Remediation,
			},
		}
		if len(fields) > 0 {
			payload["fields"] = fields
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encodeErr := enc.Encode(payload); encodeErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, styleError(encodeErr.Error()))
		}
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("[%s] %s", f.Code, message)))
	if strings.TrimSpace(f.Remediation) != "" {
		_, _ = fmt.Fprintln(os.Stderr, styleDim("hint: "+f.Remediation))
	}
	os.Exit(1)
}
