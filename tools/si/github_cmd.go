package main

const githubUsageText = "usage: si github <auth|context|doctor|git|repo|project|branch|pr|issue|workflow|release|secret|raw|graphql>"

func cmdGithub(args []string) {
	delegated, err := runGitHubCommand(args)
	requireRustCLIDelegation("github", delegated, err)
}
