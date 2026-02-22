package main

import (
	"os"
	"strings"
)

func envSunBaseURL() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_BASE_URL"), os.Getenv("SI_HELIA_BASE_URL")))
}

func envSunToken() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_TOKEN"), os.Getenv("SI_HELIA_TOKEN")))
}

func envSunTaskboard() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_TASKBOARD"), os.Getenv("SI_HELIA_TASKBOARD")))
}

func envSunTaskboardAgent() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_TASKBOARD_AGENT"), os.Getenv("SI_HELIA_TASKBOARD_AGENT")))
}

func envSunTaskboardLeaseSeconds() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_TASKBOARD_LEASE_SECONDS"), os.Getenv("SI_HELIA_TASKBOARD_LEASE_SECONDS")))
}

func envSunMachineID() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_MACHINE_ID"), os.Getenv("SI_HELIA_MACHINE_ID")))
}

func envSunOperatorID() string {
	return strings.TrimSpace(firstNonEmpty(os.Getenv("SI_SUN_OPERATOR_ID"), os.Getenv("SI_HELIA_OPERATOR_ID")))
}

func envSunAllowInsecureHTTP() bool {
	return isTruthyFlagValue(firstNonEmpty(os.Getenv("SI_SUN_ALLOW_INSECURE_HTTP"), os.Getenv("SI_HELIA_ALLOW_INSECURE_HTTP")))
}
