package main

import "strings"

const githubUsageText = "usage: si github <auth|context|doctor|repo|branch|pr|issue|workflow|release|secret|raw|graphql>"

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
	case "doctor":
		cmdGithubDoctor(rest)
	case "repo":
		cmdGithubRepo(rest)
	case "branch":
		cmdGithubBranch(rest)
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
