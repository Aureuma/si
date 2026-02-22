package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubProject(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github project <list|get|fields|items|item-add|item-set|item-clear|item-archive|item-unarchive|item-delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubProjectList(args[1:])
	case "get":
		cmdGithubProjectGet(args[1:])
	case "fields":
		cmdGithubProjectFields(args[1:])
	case "items":
		cmdGithubProjectItems(args[1:])
	case "item-add":
		cmdGithubProjectItemAdd(args[1:])
	case "item-set":
		cmdGithubProjectItemSet(args[1:])
	case "item-clear":
		cmdGithubProjectItemClear(args[1:])
	case "item-archive":
		cmdGithubProjectItemArchive(args[1:])
	case "item-unarchive":
		cmdGithubProjectItemUnarchive(args[1:])
	case "item-delete":
		cmdGithubProjectItemDelete(args[1:])
	default:
		printUnknown("github project", args[0])
	}
}

type githubProjectAuthInputs struct {
	account        string
	owner          string
	baseURL        string
	authMode       string
	token          string
	appID          int64
	appKey         string
	installationID int64
}

func addGithubProjectAuthFlags(fs *flag.FlagSet) *githubProjectAuthInputs {
	opts := &githubProjectAuthInputs{}
	fs.StringVar(&opts.account, "account", "", "account alias")
	fs.StringVar(&opts.owner, "owner", "", "owner/org")
	fs.StringVar(&opts.baseURL, "base-url", "", "github api base url")
	fs.StringVar(&opts.authMode, "auth-mode", "", "auth mode (app|oauth)")
	fs.StringVar(&opts.token, "token", "", "override oauth access token")
	fs.Int64Var(&opts.appID, "app-id", 0, "override app id")
	fs.StringVar(&opts.appKey, "app-key", "", "override app private key pem")
	fs.Int64Var(&opts.installationID, "installation-id", 0, "override installation id")
	return opts
}

func (o *githubProjectAuthInputs) runtimeClient() (githubRuntimeContext, githubBridgeClient) {
	return mustGithubClient(o.account, o.owner, o.baseURL, githubAuthOverrides{AuthMode: o.authMode, AccessToken: o.token, AppID: o.appID, AppKey: o.appKey, InstallationID: o.installationID})
}

func cmdGithubProjectList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project list", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	limit := fs.Int("limit", 30, "max projects to list")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si github project list [org] [--owner <org>] [--limit <n>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	selectedOrg := strings.TrimSpace(auth.owner)
	if fs.NArg() == 1 {
		selectedOrg = strings.TrimSpace(fs.Arg(0))
	}
	if selectedOrg == "" {
		selectedOrg = strings.TrimSpace(runtime.Owner)
	}
	if selectedOrg == "" {
		fatal(fmt.Errorf("organization owner is required (use positional org, --owner, or context owner)"))
	}
	if *limit <= 0 {
		fatal(fmt.Errorf("--limit must be greater than 0"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($org:String!,$first:Int!){
  organization(login:$org) {
    projectsV2(first:$first, orderBy:{field:UPDATED_AT,direction:DESC}) {
      nodes {
        id
        number
        title
        shortDescription
        public
        closed
        url
        updatedAt
      }
    }
  }
}
`, map[string]any{"org": selectedOrg, "first": *limit})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	organization := githubProjectMap(data["organization"])
	projectsConn := githubProjectMap(organization["projectsV2"])
	projects := githubProjectNodeList(projectsConn["nodes"])
	if *jsonOut {
		payload := map[string]any{"organization": selectedOrg, "count": len(projects), "projects": projects}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Project list:"), selectedOrg, len(projects))
	for _, project := range projects {
		fmt.Printf("  %s\n", summarizeGitHubProject(project))
	}
}

func cmdGithubProjectGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project get", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github project get <project-id|org/number|url|number> [--owner <org>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($id:ID!){
  node(id:$id) {
    ... on ProjectV2 {
      id
      number
      title
      shortDescription
      readme
      public
      closed
      url
      updatedAt
      items(first:1) { totalCount }
      fields(first:1) { totalCount }
    }
  }
}
`, map[string]any{"id": resolved.ProjectID})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	project := githubProjectMap(data["node"])
	if len(project) == 0 {
		fatal(fmt.Errorf("project not found"))
	}
	if *jsonOut {
		payload := map[string]any{"project": project}
		if resolved.Organization != "" {
			payload["organization"] = resolved.Organization
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	printGitHubKeyValueMap(project)
}

func cmdGithubProjectFields(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project fields", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	limit := fs.Int("limit", 100, "max fields to return")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github project fields <project-id|org/number|url|number> [--owner <org>] [--limit <n>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	if *limit <= 0 {
		fatal(fmt.Errorf("--limit must be greater than 0"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($id:ID!,$first:Int!){
  node(id:$id) {
    ... on ProjectV2 {
      id
      fields(first:$first) {
        nodes {
          ... on ProjectV2Field {
            id
            name
            dataType
          }
          ... on ProjectV2SingleSelectField {
            id
            name
            dataType
            options {
              id
              name
            }
          }
          ... on ProjectV2IterationField {
            id
            name
            dataType
            configuration {
              iterations {
                id
                title
                startDate
                duration
              }
            }
          }
        }
      }
    }
  }
}
`, map[string]any{"id": resolved.ProjectID, "first": *limit})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	project := githubProjectMap(data["node"])
	fieldsConn := githubProjectMap(project["fields"])
	fields := githubProjectNodeList(fieldsConn["nodes"])
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "count": len(fields), "fields": fields}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Project fields:"), resolved.ProjectID, len(fields))
	for _, field := range fields {
		fmt.Printf("  %s\n", summarizeGitHubProjectField(field))
	}
}

func cmdGithubProjectItems(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project items", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	limit := fs.Int("limit", 50, "max items to return")
	includeArchived := fs.Bool("include-archived", false, "include archived items")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github project items <project-id|org/number|url|number> [--owner <org>] [--limit <n>] [--include-archived] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	if *limit <= 0 {
		fatal(fmt.Errorf("--limit must be greater than 0"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($id:ID!,$first:Int!,$includeArchived:Boolean!){
  node(id:$id) {
    ... on ProjectV2 {
      id
      items(first:$first, includeArchived:$includeArchived) {
        nodes {
          id
          isArchived
          type
          content {
            __typename
            ... on DraftIssue {
              title
              body
            }
            ... on Issue {
              id
              number
              title
              state
              url
              repository {
                name
                owner {
                  login
                }
              }
            }
            ... on PullRequest {
              id
              number
              title
              state
              url
              repository {
                name
                owner {
                  login
                }
              }
            }
          }
          fieldValues(first:20) {
            nodes {
              ... on ProjectV2ItemFieldTextValue {
                text
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldNumberValue {
                number
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldDateValue {
                date
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldSingleSelectValue {
                name
                optionId
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
              ... on ProjectV2ItemFieldIterationValue {
                title
                iterationId
                startDate
                field {
                  ... on ProjectV2FieldCommon {
                    id
                    name
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
`, map[string]any{"id": resolved.ProjectID, "first": *limit, "includeArchived": *includeArchived})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	project := githubProjectMap(data["node"])
	itemsConn := githubProjectMap(project["items"])
	items := githubProjectNodeList(itemsConn["nodes"])
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "count": len(items), "items": items}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Project items:"), resolved.ProjectID, len(items))
	for _, item := range items {
		fmt.Printf("  %s\n", summarizeGitHubProjectItem(item))
	}
}

func cmdGithubProjectItemAdd(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project item-add", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	contentID := fs.String("content-id", "", "content node ID (issue or pull request)")
	repo := fs.String("repo", "", "repository owner/repo (required with --issue)")
	issueNumber := fs.Int("issue", 0, "issue number (requires --repo)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github project item-add <project-id|org/number|url|number> (--content-id <node-id> | --repo <owner/repo|repo> --issue <n>) [--owner <org>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	selectedContentID := strings.TrimSpace(*contentID)
	if selectedContentID == "" {
		if strings.TrimSpace(*repo) == "" || *issueNumber <= 0 {
			fatal(fmt.Errorf("either --content-id or --repo + --issue is required"))
		}
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	if selectedContentID == "" {
		repoOwner, repoName, err := parseGitHubOwnerRepo(strings.TrimSpace(*repo), runtime.Owner)
		if err != nil {
			fatal(err)
		}
		resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($owner:String!,$repo:String!,$number:Int!){
  repository(owner:$owner, name:$repo) {
    issue(number:$number) {
      id
      number
      title
      url
    }
  }
}
`, map[string]any{"owner": repoOwner, "repo": repoName, "number": *issueNumber})
		if err != nil {
			githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
			return
		}
		repository := githubProjectMap(data["repository"])
		issue := githubProjectMap(repository["issue"])
		selectedContentID = strings.TrimSpace(githubProjectString(issue["id"]))
		if selectedContentID == "" {
			fatal(fmt.Errorf("issue not found: %s#%d", repoOwner+"/"+repoName, *issueNumber))
		}
	}
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
mutation($projectId:ID!,$contentId:ID!){
  addProjectV2ItemById(input:{projectId:$projectId, contentId:$contentId}) {
    item {
      id
      type
    }
  }
}
`, map[string]any{"projectId": resolved.ProjectID, "contentId": selectedContentID})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	result := githubProjectMap(data["addProjectV2ItemById"])
	item := githubProjectMap(result["item"])
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "content_id": selectedContentID, "item": item}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Added project item:"), orDash(githubProjectString(item["id"])))
}

func cmdGithubProjectItemSet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project item-set", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	fieldID := fs.String("field-id", "", "field node ID")
	fieldName := fs.String("field", "", "field name (resolved from project fields)")
	textValue := fs.String("text", "", "set text field value")
	numberRaw := fs.String("number", "", "set number field value")
	dateValue := fs.String("date", "", "set date field value (YYYY-MM-DD)")
	singleSelectOptionID := fs.String("single-select-option-id", "", "single select option ID")
	singleSelectName := fs.String("single-select", "", "single select option name")
	iterationID := fs.String("iteration-id", "", "iteration ID")
	iterationName := fs.String("iteration", "", "iteration title/start-date or @current")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github project item-set <project-id|org/number|url|number> <item-id> [--field-id <id>|--field <name>] (--text <v>|--number <n>|--date <yyyy-mm-dd>|--single-select-option-id <id>|--single-select <name>|--iteration-id <id>|--iteration <name|@current>) [--owner <org>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	itemID := strings.TrimSpace(fs.Arg(1))
	if itemID == "" {
		fatal(fmt.Errorf("item id is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}

	selectedFieldID := strings.TrimSpace(*fieldID)
	selectedFieldName := strings.TrimSpace(*fieldName)
	selectedSingleSelectOptionID := strings.TrimSpace(*singleSelectOptionID)
	selectedSingleSelectName := strings.TrimSpace(*singleSelectName)
	selectedIterationID := strings.TrimSpace(*iterationID)
	selectedIterationName := strings.TrimSpace(*iterationName)

	needsFieldLookup := selectedFieldID == "" || (selectedSingleSelectOptionID == "" && selectedSingleSelectName != "") || (selectedIterationID == "" && selectedIterationName != "")
	var fieldDescriptors []githubProjectFieldDescriptor
	if needsFieldLookup {
		fieldDescriptors, err = githubProjectLoadFieldDescriptors(ctx, client, runtime, resolved.ProjectID)
		if err != nil {
			fatal(err)
		}
	}
	selectedFieldDescriptor := githubProjectFieldDescriptor{}
	if selectedFieldID == "" {
		if selectedFieldName == "" {
			fatal(fmt.Errorf("either --field-id or --field is required"))
		}
		descriptor, ok := githubProjectFindFieldDescriptor(fieldDescriptors, selectedFieldName)
		if !ok {
			fatal(fmt.Errorf("project field not found: %s", selectedFieldName))
		}
		selectedFieldDescriptor = descriptor
		selectedFieldID = descriptor.ID
	} else if len(fieldDescriptors) > 0 {
		if descriptor, ok := githubProjectFindFieldDescriptorByID(fieldDescriptors, selectedFieldID); ok {
			selectedFieldDescriptor = descriptor
		}
	}

	value := map[string]any{}
	valueCount := 0
	if strings.TrimSpace(*textValue) != "" {
		value["text"] = strings.TrimSpace(*textValue)
		valueCount++
	}
	if strings.TrimSpace(*numberRaw) != "" {
		numberValue, err := strconv.ParseFloat(strings.TrimSpace(*numberRaw), 64)
		if err != nil {
			fatal(fmt.Errorf("invalid --number value: %w", err))
		}
		value["number"] = numberValue
		valueCount++
	}
	if strings.TrimSpace(*dateValue) != "" {
		date := strings.TrimSpace(*dateValue)
		if _, err := time.Parse("2006-01-02", date); err != nil {
			fatal(fmt.Errorf("invalid --date value %q (expected YYYY-MM-DD)", date))
		}
		value["date"] = date
		valueCount++
	}
	if selectedSingleSelectOptionID == "" && selectedSingleSelectName != "" {
		optionID, err := githubProjectResolveSingleSelectOptionID(selectedFieldDescriptor, selectedSingleSelectName)
		if err != nil {
			fatal(err)
		}
		selectedSingleSelectOptionID = optionID
	}
	if selectedSingleSelectOptionID != "" {
		value["singleSelectOptionId"] = selectedSingleSelectOptionID
		valueCount++
	}
	if selectedIterationID == "" && selectedIterationName != "" {
		iterationResolvedID, err := githubProjectResolveIterationID(selectedFieldDescriptor, selectedIterationName)
		if err != nil {
			fatal(err)
		}
		selectedIterationID = iterationResolvedID
	}
	if selectedIterationID != "" {
		value["iterationId"] = selectedIterationID
		valueCount++
	}
	if valueCount != 1 {
		fatal(fmt.Errorf("exactly one field value must be provided"))
	}

	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
mutation($projectId:ID!,$itemId:ID!,$fieldId:ID!,$value:ProjectV2FieldValue!){
  updateProjectV2ItemFieldValue(input:{projectId:$projectId, itemId:$itemId, fieldId:$fieldId, value:$value}) {
    projectV2Item {
      id
    }
  }
}
`, map[string]any{"projectId": resolved.ProjectID, "itemId": itemID, "fieldId": selectedFieldID, "value": value})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	result := githubProjectMap(data["updateProjectV2ItemFieldValue"])
	projectItem := githubProjectMap(result["projectV2Item"])
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "item_id": itemID, "field_id": selectedFieldID, "value": value, "project_item": projectItem}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Updated project item:"), orDash(githubProjectString(projectItem["id"])))
}

func cmdGithubProjectItemClear(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project item-clear", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	fieldID := fs.String("field-id", "", "field node ID")
	fieldName := fs.String("field", "", "field name (resolved from project fields)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github project item-clear <project-id|org/number|url|number> <item-id> [--field-id <id>|--field <name>] [--owner <org>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	itemID := strings.TrimSpace(fs.Arg(1))
	if itemID == "" {
		fatal(fmt.Errorf("item id is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	selectedFieldID := strings.TrimSpace(*fieldID)
	selectedFieldName := strings.TrimSpace(*fieldName)
	if selectedFieldID == "" {
		if selectedFieldName == "" {
			fatal(fmt.Errorf("either --field-id or --field is required"))
		}
		fieldDescriptors, err := githubProjectLoadFieldDescriptors(ctx, client, runtime, resolved.ProjectID)
		if err != nil {
			fatal(err)
		}
		descriptor, ok := githubProjectFindFieldDescriptor(fieldDescriptors, selectedFieldName)
		if !ok {
			fatal(fmt.Errorf("project field not found: %s", selectedFieldName))
		}
		selectedFieldID = descriptor.ID
	}
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
mutation($projectId:ID!,$itemId:ID!,$fieldId:ID!){
  clearProjectV2ItemFieldValue(input:{projectId:$projectId, itemId:$itemId, fieldId:$fieldId}) {
    projectV2Item {
      id
    }
  }
}
`, map[string]any{"projectId": resolved.ProjectID, "itemId": itemID, "fieldId": selectedFieldID})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	result := githubProjectMap(data["clearProjectV2ItemFieldValue"])
	projectItem := githubProjectMap(result["projectV2Item"])
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "item_id": itemID, "field_id": selectedFieldID, "project_item": projectItem}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Cleared project item field:"), orDash(githubProjectString(projectItem["id"])))
}

func cmdGithubProjectItemArchive(args []string) {
	githubProjectItemArchiveMutation(args, "archive")
}

func cmdGithubProjectItemUnarchive(args []string) {
	githubProjectItemArchiveMutation(args, "unarchive")
}

func githubProjectItemArchiveMutation(args []string, action string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project item "+action, flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github project item-" + action + " <project-id|org/number|url|number> <item-id> [--owner <org>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	itemID := strings.TrimSpace(fs.Arg(1))
	if itemID == "" {
		fatal(fmt.Errorf("item id is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	mutationName := map[string]string{"archive": "archiveProjectV2Item", "unarchive": "unarchiveProjectV2Item"}[action]
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, fmt.Sprintf(`
mutation($projectId:ID!,$itemId:ID!){
  %s(input:{projectId:$projectId, itemId:$itemId}) {
    item {
      id
      isArchived
    }
  }
}
`, mutationName), map[string]any{"projectId": resolved.ProjectID, "itemId": itemID})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	result := githubProjectMap(data[mutationName])
	item := githubProjectMap(result["item"])
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "item_id": itemID, "item": item}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	verb := map[string]string{"archive": "Archived", "unarchive": "Unarchived"}[action]
	if verb == "" {
		verb = "Updated"
	}
	fmt.Printf("%s %s\n", styleHeading(verb+" project item:"), orDash(githubProjectString(item["id"])))
}

func cmdGithubProjectItemDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github project item-delete", flag.ExitOnError)
	auth := addGithubProjectAuthFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github project item-delete <project-id|org/number|url|number> <item-id> [--owner <org>] [--json]")
		return
	}
	runtime, client := auth.runtimeClient()
	ref, err := parseGitHubProjectRef(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	itemID := strings.TrimSpace(fs.Arg(1))
	if itemID == "" {
		fatal(fmt.Errorf("item id is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resolved, err := resolveGitHubProjectRef(ctx, client, runtime, ref, strings.TrimSpace(auth.owner))
	if err != nil {
		fatal(err)
	}
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
mutation($projectId:ID!,$itemId:ID!){
  deleteProjectV2Item(input:{projectId:$projectId, itemId:$itemId}) {
    deletedItemId
  }
}
`, map[string]any{"projectId": resolved.ProjectID, "itemId": itemID})
	if err != nil {
		githubProjectHandleGraphQLError(err, resp, *jsonOut, *raw)
		return
	}
	result := githubProjectMap(data["deleteProjectV2Item"])
	deletedID := strings.TrimSpace(githubProjectString(result["deletedItemId"]))
	if *jsonOut {
		payload := map[string]any{"project_id": resolved.ProjectID, "item_id": itemID, "deleted_item_id": deletedID}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		printGithubResponse(resp, false, true)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Deleted project item:"), orDash(deletedID))
}

type githubProjectRef struct {
	ProjectID    string
	Organization string
	Number       int
}

type githubProjectResolvedRef struct {
	ProjectID    string
	Organization string
	Number       int
}

func parseGitHubProjectRef(raw string) (githubProjectRef, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return githubProjectRef{}, fmt.Errorf("project reference is required")
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return githubProjectRef{}, fmt.Errorf("invalid project url: %w", err)
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) >= 4 && strings.EqualFold(segments[0], "orgs") && strings.EqualFold(segments[2], "projects") {
			number, err := parseGitHubNumber(segments[3], "project number")
			if err != nil {
				return githubProjectRef{}, err
			}
			return githubProjectRef{Organization: strings.TrimSpace(segments[1]), Number: number}, nil
		}
		return githubProjectRef{}, fmt.Errorf("unsupported project url format: %s", value)
	}
	if parsedNumber, err := parseGitHubNumber(value, "project number"); err == nil {
		return githubProjectRef{Number: parsedNumber}, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		org := strings.TrimSpace(parts[0])
		number, err := parseGitHubNumber(parts[1], "project number")
		if err == nil && org != "" {
			return githubProjectRef{Organization: org, Number: number}, nil
		}
	}
	return githubProjectRef{ProjectID: value}, nil
}

func resolveGitHubProjectRef(ctx context.Context, client githubBridgeClient, runtime githubRuntimeContext, ref githubProjectRef, ownerOverride string) (githubProjectResolvedRef, error) {
	if strings.TrimSpace(ref.ProjectID) != "" {
		return githubProjectResolvedRef{ProjectID: strings.TrimSpace(ref.ProjectID)}, nil
	}
	organization := strings.TrimSpace(ref.Organization)
	if organization == "" {
		organization = strings.TrimSpace(ownerOverride)
	}
	if organization == "" {
		organization = strings.TrimSpace(runtime.Owner)
	}
	if organization == "" {
		return githubProjectResolvedRef{}, fmt.Errorf("organization is required to resolve project number %d", ref.Number)
	}
	if ref.Number <= 0 {
		return githubProjectResolvedRef{}, fmt.Errorf("project number is required")
	}
	_, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($org:String!,$number:Int!){
  organization(login:$org) {
    projectV2(number:$number) {
      id
      number
      title
      url
      closed
      public
    }
  }
}
`, map[string]any{"org": organization, "number": ref.Number})
	if err != nil {
		return githubProjectResolvedRef{}, err
	}
	org := githubProjectMap(data["organization"])
	project := githubProjectMap(org["projectV2"])
	projectID := strings.TrimSpace(githubProjectString(project["id"]))
	if projectID == "" {
		return githubProjectResolvedRef{}, fmt.Errorf("project not found: %s/%d", organization, ref.Number)
	}
	return githubProjectResolvedRef{ProjectID: projectID, Organization: organization, Number: ref.Number}, nil
}

func githubProjectGraphQL(ctx context.Context, client githubBridgeClient, runtime githubRuntimeContext, query string, variables map[string]any) (githubbridge.Response, map[string]any, error) {
	payload := map[string]any{"query": strings.TrimSpace(query)}
	if len(variables) > 0 {
		payload["variables"] = variables
	}
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: "/graphql", JSONBody: payload, Owner: runtime.Owner})
	if err != nil {
		return resp, nil, err
	}
	if len(resp.Data) == 0 {
		return resp, nil, fmt.Errorf("graphql response missing body")
	}
	if errs, ok := resp.Data["errors"].([]any); ok && len(errs) > 0 {
		messages := githubProjectGraphQLErrorMessages(resp)
		if len(messages) == 0 {
			return resp, nil, fmt.Errorf("graphql returned errors")
		}
		return resp, nil, fmt.Errorf("graphql returned errors: %s", strings.Join(messages, "; "))
	}
	data := githubProjectMap(resp.Data["data"])
	if len(data) == 0 {
		return resp, nil, fmt.Errorf("graphql response missing data")
	}
	return resp, data, nil
}

func githubProjectHandleGraphQLError(err error, resp githubbridge.Response, jsonOut bool, raw bool) {
	if err == nil {
		return
	}
	if jsonOut {
		printGithubResponse(resp, true, raw)
		fatal(err)
	}
	printGithubError(err)
}

func githubProjectGraphQLErrorMessages(resp githubbridge.Response) []string {
	if len(resp.Data) == 0 {
		return nil
	}
	rawErrors, ok := resp.Data["errors"].([]any)
	if !ok || len(rawErrors) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawErrors))
	for _, raw := range rawErrors {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		message := strings.TrimSpace(githubProjectString(item["message"]))
		if message == "" {
			continue
		}
		out = append(out, message)
	}
	return out
}

type githubProjectFieldDescriptor struct {
	ID         string
	Name       string
	DataType   string
	Options    []githubProjectFieldOption
	Iterations []githubProjectIteration
}

type githubProjectFieldOption struct {
	ID   string
	Name string
}

type githubProjectIteration struct {
	ID        string
	Title     string
	StartDate string
	Duration  int
}

func githubProjectLoadFieldDescriptors(ctx context.Context, client githubBridgeClient, runtime githubRuntimeContext, projectID string) ([]githubProjectFieldDescriptor, error) {
	resp, data, err := githubProjectGraphQL(ctx, client, runtime, `
query($id:ID!){
  node(id:$id) {
    ... on ProjectV2 {
      fields(first:100) {
        nodes {
          ... on ProjectV2Field {
            id
            name
            dataType
          }
          ... on ProjectV2SingleSelectField {
            id
            name
            dataType
            options {
              id
              name
            }
          }
          ... on ProjectV2IterationField {
            id
            name
            dataType
            configuration {
              iterations {
                id
                title
                startDate
                duration
              }
            }
          }
        }
      }
    }
  }
}
`, map[string]any{"id": strings.TrimSpace(projectID)})
	if err != nil {
		if len(resp.Data) > 0 {
			return nil, fmt.Errorf("load project fields failed: %w", err)
		}
		return nil, err
	}
	project := githubProjectMap(data["node"])
	fieldsConn := githubProjectMap(project["fields"])
	fieldNodes := githubProjectNodeList(fieldsConn["nodes"])
	out := make([]githubProjectFieldDescriptor, 0, len(fieldNodes))
	for _, field := range fieldNodes {
		descriptor := githubProjectFieldDescriptor{
			ID:       strings.TrimSpace(githubProjectString(field["id"])),
			Name:     strings.TrimSpace(githubProjectString(field["name"])),
			DataType: strings.TrimSpace(githubProjectString(field["dataType"])),
		}
		for _, option := range githubProjectNodeList(field["options"]) {
			descriptor.Options = append(descriptor.Options, githubProjectFieldOption{
				ID:   strings.TrimSpace(githubProjectString(option["id"])),
				Name: strings.TrimSpace(githubProjectString(option["name"])),
			})
		}
		configuration := githubProjectMap(field["configuration"])
		for _, iteration := range githubProjectNodeList(configuration["iterations"]) {
			descriptor.Iterations = append(descriptor.Iterations, githubProjectIteration{
				ID:        strings.TrimSpace(githubProjectString(iteration["id"])),
				Title:     strings.TrimSpace(githubProjectString(iteration["title"])),
				StartDate: strings.TrimSpace(githubProjectString(iteration["startDate"])),
				Duration:  githubProjectInt(iteration["duration"]),
			})
		}
		if descriptor.ID != "" {
			out = append(out, descriptor)
		}
	}
	return out, nil
}

func githubProjectFindFieldDescriptor(fields []githubProjectFieldDescriptor, name string) (githubProjectFieldDescriptor, bool) {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return githubProjectFieldDescriptor{}, false
	}
	for _, field := range fields {
		if strings.ToLower(strings.TrimSpace(field.Name)) == target {
			return field, true
		}
	}
	return githubProjectFieldDescriptor{}, false
}

func githubProjectFindFieldDescriptorByID(fields []githubProjectFieldDescriptor, fieldID string) (githubProjectFieldDescriptor, bool) {
	target := strings.TrimSpace(fieldID)
	if target == "" {
		return githubProjectFieldDescriptor{}, false
	}
	for _, field := range fields {
		if strings.TrimSpace(field.ID) == target {
			return field, true
		}
	}
	return githubProjectFieldDescriptor{}, false
}

func githubProjectResolveSingleSelectOptionID(field githubProjectFieldDescriptor, optionName string) (string, error) {
	if len(field.Options) == 0 {
		return "", fmt.Errorf("field %q has no single-select options", field.Name)
	}
	target := strings.ToLower(strings.TrimSpace(optionName))
	if target == "" {
		return "", fmt.Errorf("single-select option name is required")
	}
	for _, option := range field.Options {
		if strings.ToLower(strings.TrimSpace(option.Name)) == target {
			if strings.TrimSpace(option.ID) == "" {
				break
			}
			return option.ID, nil
		}
	}
	return "", fmt.Errorf("single-select option %q not found on field %q", optionName, field.Name)
}

func githubProjectResolveIterationID(field githubProjectFieldDescriptor, iteration string) (string, error) {
	if len(field.Iterations) == 0 {
		return "", fmt.Errorf("field %q has no iterations", field.Name)
	}
	target := strings.TrimSpace(iteration)
	if target == "" {
		return "", fmt.Errorf("iteration name is required")
	}
	if target == "@current" {
		now := time.Now().UTC()
		var chosen githubProjectIteration
		var chosenStart time.Time
		for _, candidate := range field.Iterations {
			if candidate.StartDate == "" {
				continue
			}
			start, err := time.Parse("2006-01-02", candidate.StartDate)
			if err != nil {
				continue
			}
			if start.After(now) {
				continue
			}
			if chosen.ID == "" || start.After(chosenStart) {
				chosen = candidate
				chosenStart = start
			}
		}
		if chosen.ID == "" {
			return "", fmt.Errorf("unable to resolve @current iteration for field %q", field.Name)
		}
		return chosen.ID, nil
	}
	lower := strings.ToLower(target)
	for _, candidate := range field.Iterations {
		if strings.TrimSpace(candidate.ID) == target {
			return candidate.ID, nil
		}
		if strings.ToLower(strings.TrimSpace(candidate.Title)) == lower {
			return candidate.ID, nil
		}
		if strings.TrimSpace(candidate.StartDate) == target {
			return candidate.ID, nil
		}
	}
	return "", fmt.Errorf("iteration %q not found on field %q", iteration, field.Name)
}

func summarizeGitHubProject(project map[string]any) string {
	number := githubProjectInt(project["number"])
	title := strings.TrimSpace(githubProjectString(project["title"]))
	if title == "" {
		title = "(untitled)"
	}
	projectID := strings.TrimSpace(githubProjectString(project["id"]))
	publicText := "private"
	if value, ok := project["public"].(bool); ok && value {
		publicText = "public"
	}
	closedText := "open"
	if value, ok := project["closed"].(bool); ok && value {
		closedText = "closed"
	}
	urlText := strings.TrimSpace(githubProjectString(project["url"]))
	if urlText != "" {
		return fmt.Sprintf("#%d %s [%s, %s] %s (%s)", number, title, publicText, closedText, projectID, urlText)
	}
	return fmt.Sprintf("#%d %s [%s, %s] %s", number, title, publicText, closedText, projectID)
}

func summarizeGitHubProjectField(field map[string]any) string {
	name := strings.TrimSpace(githubProjectString(field["name"]))
	if name == "" {
		name = "(unnamed)"
	}
	fieldID := strings.TrimSpace(githubProjectString(field["id"]))
	dataType := strings.TrimSpace(githubProjectString(field["dataType"]))
	if dataType == "" {
		dataType = "unknown"
	}
	optionCount := len(githubProjectNodeList(field["options"]))
	iterationCount := len(githubProjectNodeList(githubProjectMap(field["configuration"])["iterations"]))
	if optionCount > 0 {
		return fmt.Sprintf("%s (%s) id=%s options=%d", name, dataType, fieldID, optionCount)
	}
	if iterationCount > 0 {
		return fmt.Sprintf("%s (%s) id=%s iterations=%d", name, dataType, fieldID, iterationCount)
	}
	return fmt.Sprintf("%s (%s) id=%s", name, dataType, fieldID)
}

func summarizeGitHubProjectItem(item map[string]any) string {
	itemID := strings.TrimSpace(githubProjectString(item["id"]))
	if itemID == "" {
		itemID = "(unknown)"
	}
	content := githubProjectMap(item["content"])
	typeName := strings.TrimSpace(githubProjectString(content["__typename"]))
	if typeName == "" {
		typeName = strings.TrimSpace(githubProjectString(item["type"]))
	}
	title := strings.TrimSpace(githubProjectString(content["title"]))
	if title == "" {
		title = "(untitled)"
	}
	number := githubProjectInt(content["number"])
	state := strings.TrimSpace(strings.ToLower(githubProjectString(content["state"])))
	stateLabel := ""
	if state != "" {
		stateLabel = " state=" + state
	}
	archivedLabel := ""
	if archived, _ := item["isArchived"].(bool); archived {
		archivedLabel = " archived"
	}
	fieldValues := githubProjectNodeList(githubProjectMap(item["fieldValues"])["nodes"])
	status := githubProjectProjectItemFieldValue(fieldValues, "status")
	statusLabel := ""
	if status != "" {
		statusLabel = " status=" + status
	}
	if number > 0 {
		return fmt.Sprintf("%s %s#%d %s%s%s%s", itemID, typeName, number, title, stateLabel, statusLabel, archivedLabel)
	}
	return fmt.Sprintf("%s %s %s%s%s%s", itemID, typeName, title, stateLabel, statusLabel, archivedLabel)
}

func githubProjectProjectItemFieldValue(nodes []map[string]any, fieldName string) string {
	target := strings.ToLower(strings.TrimSpace(fieldName))
	if target == "" {
		return ""
	}
	for _, node := range nodes {
		field := githubProjectMap(node["field"])
		name := strings.ToLower(strings.TrimSpace(githubProjectString(field["name"])))
		if name != target {
			continue
		}
		for _, key := range []string{"name", "title", "text", "date"} {
			if value := strings.TrimSpace(githubProjectString(node[key])); value != "" {
				return value
			}
		}
		if raw := node["number"]; raw != nil {
			return strings.TrimSpace(githubProjectString(raw))
		}
	}
	return ""
}

func githubProjectMap(value any) map[string]any {
	mapped, _ := value.(map[string]any)
	if mapped == nil {
		return map[string]any{}
	}
	return mapped
}

func githubProjectNodeList(value any) []map[string]any {
	rawList, ok := value.([]any)
	if !ok {
		if typedList, ok := value.([]map[string]any); ok {
			return typedList
		}
		return nil
	}
	out := make([]map[string]any, 0, len(rawList))
	for _, item := range rawList {
		mapped, ok := item.(map[string]any)
		if !ok || mapped == nil {
			continue
		}
		out = append(out, mapped)
	}
	return out
}

func githubProjectString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(raw)
	}
}

func githubProjectInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		v, err := typed.Int64()
		if err == nil {
			return int(v)
		}
	}
	return 0
}
