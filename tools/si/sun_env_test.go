package main

import "testing"

func TestSunEnvReadsSunVariables(t *testing.T) {
	t.Setenv("SI_SUN_BASE_URL", "https://sun.example")
	t.Setenv("SI_SUN_TOKEN", "sun-token")
	t.Setenv("SI_SUN_LOGIN_URL", "https://aureuma.ai/sun/auth/cli/start")
	t.Setenv("SI_SUN_LOGIN_OPEN_CMD", "xdg-open {url}")
	t.Setenv("SI_SUN_TASKBOARD", "sun-board")
	t.Setenv("SI_SUN_TASKBOARD_AGENT", "sun-agent")
	t.Setenv("SI_SUN_TASKBOARD_LEASE_SECONDS", "77")
	t.Setenv("SI_SUN_MACHINE_ID", "sun-machine")
	t.Setenv("SI_SUN_OPERATOR_ID", "sun-operator")
	t.Setenv("SI_SUN_ALLOW_INSECURE_HTTP", "1")
	t.Setenv("SI_SUN_ORBIT_GATEWAY_REGISTRY", "team-reg")
	t.Setenv("SI_SUN_ORBIT_GATEWAY_SLOTS", "32")

	if got := envSunBaseURL(); got != "https://sun.example" {
		t.Fatalf("envSunBaseURL=%q", got)
	}
	if got := envSunToken(); got != "sun-token" {
		t.Fatalf("envSunToken=%q", got)
	}
	if got := envSunLoginURL(); got != "https://aureuma.ai/sun/auth/cli/start" {
		t.Fatalf("envSunLoginURL=%q", got)
	}
	if got := envSunLoginOpenCmd(); got != "xdg-open {url}" {
		t.Fatalf("envSunLoginOpenCmd=%q", got)
	}
	if got := envSunTaskboard(); got != "sun-board" {
		t.Fatalf("envSunTaskboard=%q", got)
	}
	if got := envSunTaskboardAgent(); got != "sun-agent" {
		t.Fatalf("envSunTaskboardAgent=%q", got)
	}
	if got := envSunTaskboardLeaseSeconds(); got != "77" {
		t.Fatalf("envSunTaskboardLeaseSeconds=%q", got)
	}
	if got := envSunMachineID(); got != "sun-machine" {
		t.Fatalf("envSunMachineID=%q", got)
	}
	if got := envSunOperatorID(); got != "sun-operator" {
		t.Fatalf("envSunOperatorID=%q", got)
	}
	if !envSunAllowInsecureHTTP() {
		t.Fatalf("expected insecure-http override true")
	}
	if got := envSunOrbitGatewayRegistry(); got != "team-reg" {
		t.Fatalf("envSunOrbitGatewayRegistry=%q", got)
	}
	if got := envSunOrbitGatewaySlots(); got != "32" {
		t.Fatalf("envSunOrbitGatewaySlots=%q", got)
	}
}

func TestSunEnvEmptyWhenUnset(t *testing.T) {
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_SUN_TOKEN", "")
	t.Setenv("SI_SUN_LOGIN_URL", "")
	t.Setenv("SI_SUN_LOGIN_OPEN_CMD", "")
	t.Setenv("SI_SUN_ORBIT_GATEWAY_REGISTRY", "")
	t.Setenv("SI_SUN_ORBIT_GATEWAY_SLOTS", "")
	t.Setenv("SI_SUN_ALLOW_INSECURE_HTTP", "")

	if got := envSunBaseURL(); got != "" {
		t.Fatalf("envSunBaseURL=%q", got)
	}
	if got := envSunToken(); got != "" {
		t.Fatalf("envSunToken=%q", got)
	}
	if got := envSunLoginURL(); got != "" {
		t.Fatalf("envSunLoginURL=%q", got)
	}
	if got := envSunLoginOpenCmd(); got != "" {
		t.Fatalf("envSunLoginOpenCmd=%q", got)
	}
	if got := envSunOrbitGatewayRegistry(); got != "" {
		t.Fatalf("envSunOrbitGatewayRegistry=%q", got)
	}
	if got := envSunOrbitGatewaySlots(); got != "" {
		t.Fatalf("envSunOrbitGatewaySlots=%q", got)
	}
	if envSunAllowInsecureHTTP() {
		t.Fatalf("expected insecure-http override false")
	}
}
