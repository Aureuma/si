package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	paasTargetIngressBaselineUsageText = "usage: si paas target ingress-baseline --target <id> --domain <fqdn> --acme-email <email> [--lb-mode <dns|l4>] [--output-dir <path>] [--json]"
	paasIngressProviderTraefik         = "traefik"
)

func cmdPaasTargetIngressBaseline(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target ingress-baseline", flag.ExitOnError)
	targetName := fs.String("target", "", "target id")
	domain := fs.String("domain", "", "base ingress domain (for example apps.example.com)")
	acmeEmail := fs.String("acme-email", "", "acme certificate email")
	lbMode := fs.String("lb-mode", "dns", "dns or l4")
	outputDir := fs.String("output-dir", "", "override output directory for generated artifacts")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasTargetIngressBaselineUsageText)
		return
	}
	if !requirePaasValue(*targetName, "target", paasTargetIngressBaselineUsageText) {
		return
	}
	if !requirePaasValue(*domain, "domain", paasTargetIngressBaselineUsageText) {
		return
	}
	if !requirePaasValue(*acmeEmail, "acme-email", paasTargetIngressBaselineUsageText) {
		return
	}
	normalizedLBMode := normalizeIngressLBMode(*lbMode)
	if normalizedLBMode != "dns" && normalizedLBMode != "l4" {
		fatal(fmt.Errorf("invalid --lb-mode %q (expected dns or l4)", strings.TrimSpace(*lbMode)))
	}

	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		fatal(err)
	}
	idx := findPaasTarget(store, *targetName)
	if idx == -1 {
		fatal(fmt.Errorf("target %q not found", strings.TrimSpace(*targetName)))
	}
	target := store.Targets[idx]

	renderDir := strings.TrimSpace(*outputDir)
	if renderDir == "" {
		contextDir, err := resolvePaasContextDir(currentPaasContext())
		if err != nil {
			fatal(err)
		}
		renderDir = filepath.Join(contextDir, "targets", target.Name, "ingress", paasIngressProviderTraefik)
	}
	if err := renderTraefikIngressBaseline(renderDir, target, strings.ToLower(strings.TrimSpace(*domain)), strings.TrimSpace(*acmeEmail), normalizedLBMode); err != nil {
		fatal(err)
	}

	target.IngressProvider = paasIngressProviderTraefik
	target.IngressDomain = strings.ToLower(strings.TrimSpace(*domain))
	target.IngressACMEEmail = strings.TrimSpace(*acmeEmail)
	target.IngressLBMode = normalizedLBMode
	target.UpdatedAt = utcNowRFC3339()
	store.Targets[idx] = target
	if err := savePaasTargetStore(currentPaasContext(), store); err != nil {
		fatal(err)
	}

	if jsonOut {
		payload := map[string]any{
			"ok":               true,
			"command":          "target ingress-baseline",
			"context":          currentPaasContext(),
			"mode":             "live",
			"target":           target.Name,
			"provider":         target.IngressProvider,
			"domain":           target.IngressDomain,
			"lb_mode":          target.IngressLBMode,
			"acme_email":       target.IngressACMEEmail,
			"artifacts_dir":    renderDir,
			"dashboard_domain": "traefik." + target.IngressDomain,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s target ingress-baseline\n", styleHeading("si paas:"))
	fmt.Printf("  context=%s\n", currentPaasContext())
	fmt.Printf("  target=%s\n", target.Name)
	fmt.Printf("  provider=%s\n", target.IngressProvider)
	fmt.Printf("  domain=%s\n", target.IngressDomain)
	fmt.Printf("  lb_mode=%s\n", target.IngressLBMode)
	fmt.Printf("  artifacts_dir=%s\n", renderDir)
	fmt.Printf("  dashboard_domain=%s\n", "traefik."+target.IngressDomain)
}

func renderTraefikIngressBaseline(dir string, target paasTarget, domain string, acmeEmail string, lbMode string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	composePath := filepath.Join(dir, "docker-compose.traefik.yaml")
	staticPath := filepath.Join(dir, "traefik.yaml")
	dynamicPath := filepath.Join(dir, "dynamic.yaml")
	readmePath := filepath.Join(dir, "README.md")
	acmePath := filepath.Join(dir, "acme.json")

	compose := fmt.Sprintf(`services:
  traefik:
    image: traefik:v3.1
    container_name: si-traefik-%s
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./traefik.yaml:/etc/traefik/traefik.yaml:ro
      - ./dynamic.yaml:/etc/traefik/dynamic.yaml:ro
      - ./acme.json:/var/lib/traefik/acme.json
      - /var/run/docker.sock:/var/run/docker.sock:ro
`, target.Name)

	static := fmt.Sprintf(`api:
  dashboard: true

entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"

providers:
  file:
    filename: /etc/traefik/dynamic.yaml
    watch: true

certificatesResolvers:
  le:
    acme:
      email: %s
      storage: /var/lib/traefik/acme.json
      httpChallenge:
        entryPoint: web
`, acmeEmail)

	dynamic := fmt.Sprintf(`http:
  routers:
    traefik-dashboard:
      rule: Host("traefik.%s")
      service: api@internal
      entryPoints:
        - websecure
      tls:
        certResolver: le
`, domain)

	readme := fmt.Sprintf(`# Traefik Ingress Baseline

Target: %s
Provider: traefik (MVP locked)
Base domain: %s
LB mode: %s

## Generated Files

- docker-compose.traefik.yaml
- traefik.yaml
- dynamic.yaml
- acme.json (must be chmod 600)

## DNS/LB Model

1. DNS mode (lb-mode=dns):
- Point app domains (for example app.%s) directly to %s.

2. L4 mode (lb-mode=l4):
- Place an L4 TCP load balancer in front of this node and forward ports 80/443.
- Keep host rules in Traefik; LB remains transport-only.

## Bootstrap Commands

1. chmod 600 acme.json
2. docker compose -f docker-compose.traefik.yaml up -d
3. Verify dashboard at https://traefik.%s
`, target.Name, domain, lbMode, domain, target.Host, domain)

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(staticPath, []byte(static), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(dynamicPath, []byte(dynamic), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(acmePath); os.IsNotExist(err) {
		if err := os.WriteFile(acmePath, []byte{}, 0o600); err != nil {
			return err
		}
	}
	return nil
}
