package main

const cloudflareUsageText = "usage: si cloudflare <auth|context|doctor|status|smoke|zone|dns|email|tls|ssl|origin|cert|cache|waf|ruleset|firewall|ratelimit|workers|pages|r2|d1|kv|queue|access|token|tokens|tunnel|tunnels|lb|analytics|logs|report|raw|api>"

func cmdCloudflare(args []string) {
	delegated, err := runCloudflareCommand(args)
	requireRustCLIDelegation("cloudflare", delegated, err)
}
