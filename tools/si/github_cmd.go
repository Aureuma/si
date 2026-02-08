package main

import "strings"

const githubUsageText = "usage: si github <auth|context|repo|pr|issue|workflow|release|secret|raw|graphql>"

func cmdGithub(args []string) {
	if len(args) == 0 {
		printUsage(githubUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(githubUsageText)
	case "auth":
		cmdGithubAuth(rest)
	case "context":
		cmdGithubContext(rest)
	case "repo":
		cmdGithubRepo(rest)
	case "pr":
		cmdGithubPR(rest)
	case "issue":
		cmdGithubIssue(rest)
	case "workflow":
		cmdGithubWorkflow(rest)
	case "release":
		cmdGithubRelease(rest)
	case "secret":
		cmdGithubSecret(rest)
	case "raw":
		cmdGithubRaw(rest)
	case "graphql":
		cmdGithubGraphQL(rest)
	default:
		printUnknown("github", cmd)
		printUsage(githubUsageText)
	}
}

func cmdGithubRepo(args []string) {
	printUsage("usage: si github repo <list|get|create|update|archive|delete> ...")
}

func cmdGithubPR(args []string) {
	printUsage("usage: si github pr <list|get|create|comment|merge> ...")
}

func cmdGithubIssue(args []string) {
	printUsage("usage: si github issue <list|get|create|comment|close|reopen> ...")
}

func cmdGithubWorkflow(args []string) {
	printUsage("usage: si github workflow <list|run|runs|run get|run cancel|run rerun|logs> ...")
}

func cmdGithubRelease(args []string) {
	printUsage("usage: si github release <list|get|create|upload|delete> ...")
}

func cmdGithubSecret(args []string) {
	printUsage("usage: si github secret <repo|env|org> ...")
}
