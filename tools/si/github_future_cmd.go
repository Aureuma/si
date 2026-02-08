package main

func cmdGithubWorkflow(args []string) {
	printUsage("usage: si github workflow <list|run|runs|run get|run cancel|run rerun|logs> ...")
}

func cmdGithubRelease(args []string) {
	printUsage("usage: si github release <list|get|create|upload|delete> ...")
}

func cmdGithubSecret(args []string) {
	printUsage("usage: si github secret <repo|env|org> ...")
}
