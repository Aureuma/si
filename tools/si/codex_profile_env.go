package main

import (
	"os"
	"strings"
)

func resolveContainerProfileEnv(profile *codexProfile) (string, string) {
	profileID := ""
	profileName := ""
	if profile != nil {
		profileID = strings.TrimSpace(profile.ID)
		profileName = strings.TrimSpace(profile.Name)
	}
	if profileID == "" {
		profileID = strings.TrimSpace(os.Getenv("SI_CODEX_PROFILE_ID"))
	}
	if profileName == "" {
		profileName = strings.TrimSpace(os.Getenv("SI_CODEX_PROFILE_NAME"))
	}
	return profileID, profileName
}

func appendContainerProfileEnv(env []string, profile *codexProfile) []string {
	profileID, profileName := resolveContainerProfileEnv(profile)
	return appendContainerProfileEnvValues(env, profileID, profileName)
}

func appendContainerProfileEnvValues(env []string, profileID string, profileName string) []string {
	profileID = strings.TrimSpace(profileID)
	profileName = strings.TrimSpace(profileName)
	if profileID != "" {
		env = append(env, "SI_CODEX_PROFILE_ID="+profileID)
	}
	if profileName != "" {
		env = append(env, "SI_CODEX_PROFILE_NAME="+profileName)
	}
	return env
}
