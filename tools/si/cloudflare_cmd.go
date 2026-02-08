package main

import "strings"

const cloudflareUsageText = "usage: si cloudflare <auth|context|doctor|zone|dns|tls|cache|waf|ruleset|firewall|ratelimit|workers|pages|r2|d1|kv|queue|access|tunnel|lb|analytics|logs|report|raw>"

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
	case "zone":
		cmdCloudflareZone(rest)
	case "dns":
		cmdCloudflareDNS(rest)
	case "tls", "cert", "origin-cert":
		cmdCloudflareTLS(rest)
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
	case "tunnel":
		cmdCloudflareTunnel(rest)
	case "lb", "loadbalancer", "load-balancer":
		cmdCloudflareLB(rest)
	case "analytics":
		cmdCloudflareAnalytics(rest)
	case "logs":
		cmdCloudflareLogs(rest)
	case "report":
		cmdCloudflareReport(rest)
	case "raw":
		cmdCloudflareRaw(rest)
	default:
		printUnknown("cloudflare", cmd)
		printUsage(cloudflareUsageText)
	}
}
