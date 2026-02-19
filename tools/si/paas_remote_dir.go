package main

import (
	"fmt"
	"path"
	"strings"
)

const defaultPaasReleaseRemoteDir = "/opt/si/paas/releases"

func normalizePaasRemoteDir(remoteDir string) string {
	resolved := strings.TrimSpace(remoteDir)
	if resolved == "" {
		resolved = defaultPaasReleaseRemoteDir
	}
	resolved = strings.TrimSpace(path.Clean(resolved))
	if resolved == "." || resolved == "" {
		return defaultPaasReleaseRemoteDir
	}
	return resolved
}

func resolvePaasRemoteDir(remoteDir string) (string, error) {
	resolved := normalizePaasRemoteDir(remoteDir)
	if !path.IsAbs(resolved) {
		return "", fmt.Errorf("remote-dir must be an absolute path, got %q", strings.TrimSpace(remoteDir))
	}
	return resolved, nil
}

func resolvePaasRemoteDirForApp(remoteDir, app string) (string, error) {
	resolvedInput := strings.TrimSpace(remoteDir)
	if resolvedInput != "" {
		return resolvePaasRemoteDir(resolvedInput)
	}
	resolvedApp := strings.TrimSpace(app)
	if resolvedApp == "" {
		return resolvePaasRemoteDir("")
	}
	_, historyRemoteDir, err := resolvePaasCurrentReleaseInfo(resolvedApp)
	if err != nil {
		return "", fmt.Errorf("resolve remote dir from deploy history for %q: %w", resolvedApp, err)
	}
	return resolvePaasRemoteDir(historyRemoteDir)
}
