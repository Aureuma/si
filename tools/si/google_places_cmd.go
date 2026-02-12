package main

import "strings"

const googlePlacesUsageText = "usage: si google places <auth|context|doctor|session|autocomplete|search-text|search-nearby|details|photo|types|raw|report>"

func cmdGooglePlaces(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, googlePlacesUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(googlePlacesUsageText)
	case "auth":
		cmdGooglePlacesAuth(rest)
	case "context":
		cmdGooglePlacesContext(rest)
	case "doctor":
		cmdGooglePlacesDoctor(rest)
	case "session":
		cmdGooglePlacesSession(rest)
	case "autocomplete":
		cmdGooglePlacesAutocomplete(rest)
	case "search-text", "text-search", "searchtext":
		cmdGooglePlacesSearchText(rest)
	case "search-nearby", "nearby-search", "searchnearby":
		cmdGooglePlacesSearchNearby(rest)
	case "details", "detail":
		cmdGooglePlacesDetails(rest)
	case "photo", "photos":
		cmdGooglePlacesPhoto(rest)
	case "types", "type":
		cmdGooglePlacesTypes(rest)
	case "raw":
		cmdGooglePlacesRaw(rest)
	case "report":
		cmdGooglePlacesReport(rest)
	default:
		printUnknown("google places", cmd)
		printUsage(googlePlacesUsageText)
	}
}
