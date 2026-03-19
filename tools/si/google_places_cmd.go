package main

const googlePlacesUsageText = "usage: si google places <auth|context|doctor|session|autocomplete|search-text|search-nearby|details|photo|types|raw|report>"

func cmdGooglePlaces(args []string) {
	delegated, err := runGooglePlacesCommand(args)
	requireRustCLIDelegation("google places", delegated, err)
}
