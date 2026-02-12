package main

import "strings"

const cloudflareUsageText = "usage: si cloudflare <auth|context|doctor|status|smoke|zone|dns|email|tls|ssl|origin|cert|cache|waf|ruleset|firewall|ratelimit|workers|pages|r2|d1|kv|queue|access|token|tokens|tunnel|tunnels|lb|analytics|logs|report|raw|api>"

func cmdCloudflare(args []string) {
	if len(args) == 0 {
		printUsage(cloudflareUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(cloudflareUsageText)
	case "auth":
		cmdCloudflareAuth(rest)
	case "context":
		cmdCloudflareContext(rest)
	case "doctor":
		cmdCloudflareDoctor(rest)
	case "status":
		cmdCloudflareStatus(rest)
	case "smoke":
		cmdCloudflareSmoke(rest)
	case "zone":
		cmdCloudflareZone(rest)
	case "dns":
		cmdCloudflareDNS(rest)
	case "email":
		cmdCloudflareEmail(rest)
	case "tls", "ssl", "origin-cert":
		cmdCloudflareTLS(rest)
	case "origin":
		cmdCloudflareTLS(append([]string{"origin-cert"}, rest...))
	case "cert", "certs", "certificate", "certificates":
		cmdCloudflareTLS(append([]string{"cert"}, rest...))
	case "cache":
		cmdCloudflareCache(rest)
	case "waf":
		cmdCloudflareWAF(rest)
	case "ruleset":
		cmdCloudflareRuleset(rest)
	case "firewall":
		cmdCloudflareFirewall(rest)
	case "ratelimit":
		cmdCloudflareRateLimit(rest)
	case "workers":
		cmdCloudflareWorkers(rest)
	case "pages":
		cmdCloudflarePages(rest)
	case "r2":
		cmdCloudflareR2(rest)
	case "d1":
		cmdCloudflareD1(rest)
	case "kv":
		cmdCloudflareKV(rest)
	case "queue", "queues":
		cmdCloudflareQueue(rest)
	case "access":
		cmdCloudflareAccess(rest)
	case "token", "tokens":
		cmdCloudflareToken(rest)
	case "tunnel":
		cmdCloudflareTunnel(rest)
	case "tunnels":
		cmdCloudflareTunnel(rest)
	case "lb", "loadbalancer", "load-balancer":
		cmdCloudflareLB(rest)
	case "analytics":
		cmdCloudflareAnalytics(rest)
	case "logs":
		cmdCloudflareLogs(rest)
	case "report":
		cmdCloudflareReport(rest)
	case "raw", "api":
		cmdCloudflareRaw(rest)
	default:
		printUnknown("cloudflare", cmd)
		printUsage(cloudflareUsageText)
	}
}
