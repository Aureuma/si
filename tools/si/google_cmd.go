package main

const googleUsageText = "usage: si google <places|play|youtube|youtube-data>"

func cmdGoogle(args []string) {
	delegated, err := runGoogleCommand(args)
	requireRustCLIDelegation("google", delegated, err)
}
