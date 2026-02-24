package main

import (
	"os"
	"strings"
)

func envSunBaseURL() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_BASE_URL"))
}

func envSunToken() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_TOKEN"))
}

func envSunLoginURL() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_LOGIN_URL"))
}

func envSunLoginOpenCmd() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_LOGIN_OPEN_CMD"))
}

func envSunTaskboard() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_TASKBOARD"))
}

func envSunTaskboardAgent() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_TASKBOARD_AGENT"))
}

func envSunTaskboardLeaseSeconds() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_TASKBOARD_LEASE_SECONDS"))
}

func envSunMachineID() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_MACHINE_ID"))
}

func envSunOperatorID() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_OPERATOR_ID"))
}

func envSunAllowInsecureHTTP() bool {
	return isTruthyFlagValue(os.Getenv("SI_SUN_ALLOW_INSECURE_HTTP"))
}

func envSunPluginGatewayRegistry() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_PLUGIN_GATEWAY_REGISTRY"))
}

func envSunPluginGatewaySlots() string {
	return strings.TrimSpace(os.Getenv("SI_SUN_PLUGIN_GATEWAY_SLOTS"))
}
