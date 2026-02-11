package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

func cmdAWSBedrock(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock <foundation-model|inference-profile|guardrail|runtime|job|agent|knowledge-base|agent-runtime> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "foundation-model", "foundation-models", "model", "models":
		cmdAWSBedrockFoundationModel(rest)
	case "inference-profile", "inference-profiles", "profile", "profiles":
		cmdAWSBedrockInferenceProfile(rest)
	case "guardrail", "guardrails":
		cmdAWSBedrockGuardrail(rest)
	case "runtime":
		cmdAWSBedrockRuntime(rest)
	case "job", "jobs", "batch":
		cmdAWSBedrockJob(rest)
	case "agent", "agents":
		cmdAWSBedrockAgent(rest)
	case "knowledge-base", "knowledge-bases", "knowledgebase", "knowledgebases", "kb":
		cmdAWSBedrockKnowledgeBase(rest)
	case "agent-runtime", "runtime-agent", "rag":
		cmdAWSBedrockAgentRuntime(rest)
	default:
		printUnknown("aws bedrock", sub)
	}
}

func cmdAWSBedrockFoundationModel(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock foundation-model <list|get> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockFoundationModelList(rest)
	case "get", "describe":
		cmdAWSBedrockFoundationModelGet(rest)
	default:
		printUnknown("aws bedrock foundation-model", sub)
	}
}

func cmdAWSBedrockFoundationModelList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock foundation-model list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	provider := fs.String("provider", "", "provider filter")
	outputModality := fs.String("output-modality", "", "output modality filter")
	inferenceType := fs.String("inference-type", "", "inference type filter")
	customizationType := fs.String("customization-type", "", "customization type filter")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock foundation-model list [--provider <name>] [--output-modality <text|image|embedding>] [--json]")
		return
	}
	params := map[string]string{}
	if value := strings.TrimSpace(*provider); value != "" {
		params["byProvider"] = value
	}
	if value := strings.TrimSpace(*outputModality); value != "" {
		params["byOutputModality"] = value
	}
	if value := strings.TrimSpace(*inferenceType); value != "" {
		params["byInferenceType"] = value
	}
	if value := strings.TrimSpace(*customizationType); value != "" {
		params["byCustomizationType"] = value
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/foundation-models", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockFoundationModelGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock foundation-model get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	modelID := fs.String("model-id", "", "foundation model id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*modelID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock foundation-model get <model-id> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/foundation-models/"+url.PathEscape(id), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockInferenceProfile(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock inference-profile <list|get> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockInferenceProfileList(rest)
	case "get", "describe":
		cmdAWSBedrockInferenceProfileGet(rest)
	default:
		printUnknown("aws bedrock inference-profile", sub)
	}
}

func cmdAWSBedrockInferenceProfileList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock inference-profile list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 25, "max profiles")
	typeEquals := fs.String("type", "", "inference profile type")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock inference-profile list [--limit N] [--type <SYSTEM_DEFINED|APPLICATION>] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	if value := strings.TrimSpace(*typeEquals); value != "" {
		params["typeEquals"] = value
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/inference-profiles", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockInferenceProfileGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock inference-profile get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	profileID := fs.String("id", "", "inference profile id or arn")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*profileID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock inference-profile get <id|arn> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/inference-profiles/"+url.PathEscape(id), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockGuardrail(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock guardrail <list|get> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockGuardrailList(rest)
	case "get", "describe":
		cmdAWSBedrockGuardrailGet(rest)
	default:
		printUnknown("aws bedrock guardrail", sub)
	}
}

func cmdAWSBedrockGuardrailList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock guardrail list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 25, "max guardrails")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock guardrail list [--limit N] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/guardrails", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockGuardrailGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock guardrail get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	guardrailID := fs.String("id", "", "guardrail id")
	version := fs.String("version", "", "guardrail version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*guardrailID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock guardrail get <guardrail-id> [--version <version>] [--json]")
		return
	}
	params := map[string]string{}
	if value := strings.TrimSpace(*version); value != "" {
		params["guardrailVersion"] = value
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/guardrails/"+url.PathEscape(id), params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockRuntime(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock runtime <invoke|converse|count-tokens> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "invoke":
		cmdAWSBedrockRuntimeInvoke(rest)
	case "converse":
		cmdAWSBedrockRuntimeConverse(rest)
	case "count-tokens", "count", "tokens":
		cmdAWSBedrockRuntimeCountTokens(rest)
	default:
		printUnknown("aws bedrock runtime", sub)
	}
}

func cmdAWSBedrockRuntimeInvoke(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock runtime invoke", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	modelID := fs.String("model-id", "", "model id or arn")
	prompt := fs.String("prompt", "", "simple text prompt")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	accept := fs.String("accept", "application/json", "accept header")
	contentType := fs.String("content-type", "application/json", "request content type")
	trace := fs.String("trace", "", "trace mode (ENABLED|ENABLED_FULL|DISABLED)")
	guardrailID := fs.String("guardrail-id", "", "guardrail id")
	guardrailVersion := fs.String("guardrail-version", "", "guardrail version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*modelID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock runtime invoke --model-id <id> [--prompt <text>|--body <json>|--body-file <path>] [--json]")
		return
	}
	var fallback any
	if value := strings.TrimSpace(*prompt); value != "" {
		fallback = map[string]any{"inputText": value}
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), fallback)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		fatal(fmt.Errorf("provide --prompt, --body, or --body-file"))
	}
	headers := map[string]string{"Accept": strings.TrimSpace(*accept)}
	if value := strings.TrimSpace(*trace); value != "" {
		headers["X-Amzn-Bedrock-Trace"] = value
	}
	if value := strings.TrimSpace(*guardrailID); value != "" {
		headers["X-Amzn-Bedrock-GuardrailIdentifier"] = value
	}
	if value := strings.TrimSpace(*guardrailVersion); value != "" {
		headers["X-Amzn-Bedrock-GuardrailVersion"] = value
	}
	runAWSBedrockREST(flags, "bedrock-runtime", http.MethodPost, "/model/"+url.PathEscape(id)+"/invoke", nil, headers, payload, strings.TrimSpace(*contentType), *jsonOut, *raw)
}

func cmdAWSBedrockRuntimeConverse(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock runtime converse", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	modelID := fs.String("model-id", "", "model id or arn")
	prompt := fs.String("prompt", "", "simple user prompt")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	trace := fs.String("trace", "", "trace mode (ENABLED|ENABLED_FULL|DISABLED)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*modelID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock runtime converse --model-id <id> [--prompt <text>|--body <json>|--body-file <path>] [--json]")
		return
	}
	var fallback any
	if value := strings.TrimSpace(*prompt); value != "" {
		fallback = map[string]any{
			"messages": []map[string]any{{
				"role": "user",
				"content": []map[string]any{{
					"text": value,
				}},
			}},
		}
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), fallback)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		fatal(fmt.Errorf("provide --prompt, --body, or --body-file"))
	}
	headers := map[string]string{}
	if value := strings.TrimSpace(*trace); value != "" {
		headers["X-Amzn-Bedrock-Trace"] = value
	}
	runAWSBedrockREST(flags, "bedrock-runtime", http.MethodPost, "/model/"+url.PathEscape(id)+"/converse", nil, headers, payload, "application/json", *jsonOut, *raw)
}

func cmdAWSBedrockRuntimeCountTokens(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock runtime count-tokens", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	modelID := fs.String("model-id", "", "model id or arn")
	prompt := fs.String("prompt", "", "simple text prompt")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*modelID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock runtime count-tokens --model-id <id> [--prompt <text>|--body <json>|--body-file <path>] [--json]")
		return
	}
	var fallback any
	if value := strings.TrimSpace(*prompt); value != "" {
		fallback = map[string]any{"inputText": value}
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), fallback)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		fatal(fmt.Errorf("provide --prompt, --body, or --body-file"))
	}
	runAWSBedrockREST(flags, "bedrock-runtime", http.MethodPost, "/model/"+url.PathEscape(id)+"/count-tokens", nil, nil, payload, "application/json", *jsonOut, *raw)
}

func cmdAWSBedrockJob(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock job <create|get|list|stop> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "create":
		cmdAWSBedrockJobCreate(rest)
	case "get", "describe":
		cmdAWSBedrockJobGet(rest)
	case "list":
		cmdAWSBedrockJobList(rest)
	case "stop", "cancel":
		cmdAWSBedrockJobStop(rest)
	default:
		printUnknown("aws bedrock job", sub)
	}
}

func cmdAWSBedrockJobCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock job create", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "job name")
	roleARN := fs.String("role-arn", "", "iam role arn")
	modelID := fs.String("model-id", "", "model id or arn")
	inputS3URI := fs.String("input-s3-uri", "", "input s3 uri")
	outputS3URI := fs.String("output-s3-uri", "", "output s3 uri")
	timeoutHours := fs.Int("timeout-hours", 0, "timeout duration in hours")
	clientRequestToken := fs.String("client-request-token", "", "idempotency token")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	tags := multiFlag{}
	fs.Var(&tags, "tag", "job tag key=value (repeatable)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock job create --name <job> --role-arn <arn> --model-id <id> --input-s3-uri s3://... --output-s3-uri s3://... [--json]")
		return
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*name) == "" || strings.TrimSpace(*roleARN) == "" || strings.TrimSpace(*modelID) == "" || strings.TrimSpace(*inputS3URI) == "" || strings.TrimSpace(*outputS3URI) == "" {
			fatal(fmt.Errorf("provide --body/--body-file or required flags: --name --role-arn --model-id --input-s3-uri --output-s3-uri"))
		}
		request := map[string]any{
			"jobName": strings.TrimSpace(*name),
			"roleArn": strings.TrimSpace(*roleARN),
			"modelId": strings.TrimSpace(*modelID),
			"inputDataConfig": map[string]any{
				"s3InputDataConfig": map[string]any{
					"s3Uri": strings.TrimSpace(*inputS3URI),
				},
			},
			"outputDataConfig": map[string]any{
				"s3OutputDataConfig": map[string]any{
					"s3Uri": strings.TrimSpace(*outputS3URI),
				},
			},
		}
		if *timeoutHours > 0 {
			request["timeoutDurationInHours"] = *timeoutHours
		}
		if value := strings.TrimSpace(*clientRequestToken); value != "" {
			request["clientRequestToken"] = value
		}
		if parsedTags := parseAWSParams(tags); len(parsedTags) > 0 {
			orderedKeys := make([]string, 0, len(parsedTags))
			for key := range parsedTags {
				orderedKeys = append(orderedKeys, key)
			}
			sort.Strings(orderedKeys)
			tagItems := make([]map[string]string, 0, len(orderedKeys))
			for _, key := range orderedKeys {
				tagItems = append(tagItems, map[string]string{"key": key, "value": parsedTags[key]})
			}
			request["tags"] = tagItems
		}
		encoded, encodeErr := json.Marshal(request)
		if encodeErr != nil {
			fatal(encodeErr)
		}
		payload = string(encoded)
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodPost, "/model-invocation-job", nil, nil, payload, "application/json", *jsonOut, *raw)
}

func cmdAWSBedrockJobGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock job get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jobID := fs.String("id", "", "job identifier")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*jobID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock job get <job-id> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/model-invocation-job/"+url.PathEscape(id), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockJobList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock job list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 25, "max jobs")
	status := fs.String("status", "", "status filter")
	sortBy := fs.String("sort-by", "", "sortBy field")
	sortOrder := fs.String("sort-order", "", "Ascending or Descending")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock job list [--status <Submitted|InProgress|Completed|Failed|Stopping|Stopped>] [--limit N] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	if value := strings.TrimSpace(*status); value != "" {
		params["statusEquals"] = value
	}
	if value := strings.TrimSpace(*sortBy); value != "" {
		params["sortBy"] = value
	}
	if value := strings.TrimSpace(*sortOrder); value != "" {
		params["sortOrder"] = value
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodGet, "/model-invocation-jobs", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockJobStop(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws bedrock job stop", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jobID := fs.String("id", "", "job identifier")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*jobID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock job stop <job-id> [--force] [--json]")
		return
	}
	if err := requireAWSConfirmation("stop bedrock model invocation job "+id, *force); err != nil {
		fatal(err)
	}
	runAWSBedrockREST(flags, "bedrock", http.MethodPost, "/model-invocation-job/"+url.PathEscape(id)+"/stop", nil, nil, "{}", "application/json", *jsonOut, *raw)
}

func cmdAWSBedrockAgent(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock agent <list|get|alias> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockAgentList(rest)
	case "get", "describe":
		cmdAWSBedrockAgentGet(rest)
	case "alias", "aliases":
		cmdAWSBedrockAgentAlias(rest)
	default:
		printUnknown("aws bedrock agent", sub)
	}
}

func cmdAWSBedrockAgentList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 25, "max agents")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock agent list [--limit N] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/agents", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockAgentGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	agentID := fs.String("id", "", "agent id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*agentID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock agent get <agent-id> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/agents/"+url.PathEscape(id), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockAgentAlias(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock agent alias <list|get> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockAgentAliasList(rest)
	case "get", "describe":
		cmdAWSBedrockAgentAliasGet(rest)
	default:
		printUnknown("aws bedrock agent alias", sub)
	}
}

func cmdAWSBedrockAgentAliasList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent alias list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	agentID := fs.String("agent-id", "", "agent id")
	limit := fs.Int("limit", 25, "max aliases")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*agentID)
	if id == "" {
		printUsage("usage: si aws bedrock agent alias list --agent-id <id> [--limit N] [--json]")
		return
	}
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock agent alias list --agent-id <id> [--limit N] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/agents/"+url.PathEscape(id)+"/agentAliases", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockAgentAliasGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent alias get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	agentID := fs.String("agent-id", "", "agent id")
	aliasID := fs.String("alias-id", "", "agent alias id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*agentID) == "" || strings.TrimSpace(*aliasID) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws bedrock agent alias get --agent-id <id> --alias-id <id> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/agents/"+url.PathEscape(strings.TrimSpace(*agentID))+"/agentAliases/"+url.PathEscape(strings.TrimSpace(*aliasID)), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockKnowledgeBase(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock knowledge-base <list|get|data-source> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockKnowledgeBaseList(rest)
	case "get", "describe":
		cmdAWSBedrockKnowledgeBaseGet(rest)
	case "data-source", "datasource", "datasources":
		cmdAWSBedrockKnowledgeBaseDataSource(rest)
	default:
		printUnknown("aws bedrock knowledge-base", sub)
	}
}

func cmdAWSBedrockKnowledgeBaseList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock knowledge-base list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 25, "max knowledge bases")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock knowledge-base list [--limit N] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/knowledgebases", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockKnowledgeBaseGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock knowledge-base get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	kbID := fs.String("id", "", "knowledge base id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*kbID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws bedrock knowledge-base get <knowledge-base-id> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/knowledgebases/"+url.PathEscape(id), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockKnowledgeBaseDataSource(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock knowledge-base data-source <list|get> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSBedrockKnowledgeBaseDataSourceList(rest)
	case "get", "describe":
		cmdAWSBedrockKnowledgeBaseDataSourceGet(rest)
	default:
		printUnknown("aws bedrock knowledge-base data-source", sub)
	}
}

func cmdAWSBedrockKnowledgeBaseDataSourceList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock knowledge-base data-source list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	kbID := fs.String("knowledge-base-id", "", "knowledge base id")
	limit := fs.Int("limit", 25, "max data sources")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*kbID) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws bedrock knowledge-base data-source list --knowledge-base-id <id> [--limit N] [--json]")
		return
	}
	params := map[string]string{}
	if *limit > 0 {
		params["maxResults"] = fmt.Sprintf("%d", *limit)
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/knowledgebases/"+url.PathEscape(strings.TrimSpace(*kbID))+"/datasources", params, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockKnowledgeBaseDataSourceGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock knowledge-base data-source get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	kbID := fs.String("knowledge-base-id", "", "knowledge base id")
	dsID := fs.String("id", "", "data source id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*kbID) == "" || strings.TrimSpace(*dsID) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws bedrock knowledge-base data-source get --knowledge-base-id <id> --id <datasource-id> [--json]")
		return
	}
	runAWSBedrockREST(flags, "bedrock-agent", http.MethodGet, "/knowledgebases/"+url.PathEscape(strings.TrimSpace(*kbID))+"/datasources/"+url.PathEscape(strings.TrimSpace(*dsID)), nil, nil, "", "", *jsonOut, *raw)
}

func cmdAWSBedrockAgentRuntime(args []string) {
	if len(args) == 0 {
		printUsage("usage: si aws bedrock agent-runtime <invoke-agent|retrieve|retrieve-and-generate> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "invoke-agent", "invoke":
		cmdAWSBedrockAgentRuntimeInvokeAgent(rest)
	case "retrieve":
		cmdAWSBedrockAgentRuntimeRetrieve(rest)
	case "retrieve-and-generate", "rag":
		cmdAWSBedrockAgentRuntimeRetrieveAndGenerate(rest)
	default:
		printUnknown("aws bedrock agent-runtime", sub)
	}
}

func cmdAWSBedrockAgentRuntimeInvokeAgent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent-runtime invoke-agent", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	agentID := fs.String("agent-id", "", "agent id")
	agentAliasID := fs.String("agent-alias-id", "", "agent alias id")
	sessionID := fs.String("session-id", "", "session id")
	inputText := fs.String("input-text", "", "input text")
	enableTrace := fs.Bool("enable-trace", false, "enable trace")
	sessionState := fs.String("session-state", "", "session state json")
	sessionStateFile := fs.String("session-state-file", "", "session state json file")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*agentID) == "" || strings.TrimSpace(*agentAliasID) == "" || strings.TrimSpace(*sessionID) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws bedrock agent-runtime invoke-agent --agent-id <id> --agent-alias-id <id> --session-id <id> [--input-text <text>] [--json]")
		return
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		request := map[string]any{}
		if value := strings.TrimSpace(*inputText); value != "" {
			request["inputText"] = value
		}
		if *enableTrace {
			request["enableTrace"] = true
		}
		if stateRaw, stateErr := awsResolveJSONBody(strings.TrimSpace(*sessionState), strings.TrimSpace(*sessionStateFile), nil); stateErr != nil {
			fatal(stateErr)
		} else if strings.TrimSpace(stateRaw) != "" {
			var sessionStateValue any
			if err := json.Unmarshal([]byte(stateRaw), &sessionStateValue); err != nil {
				fatal(fmt.Errorf("invalid --session-state json: %w", err))
			}
			request["sessionState"] = sessionStateValue
		}
		encoded, encodeErr := json.Marshal(request)
		if encodeErr != nil {
			fatal(encodeErr)
		}
		payload = string(encoded)
	}
	runAWSBedrockREST(flags, "bedrock-agent-runtime", http.MethodPost, "/agents/"+url.PathEscape(strings.TrimSpace(*agentID))+"/agentAliases/"+url.PathEscape(strings.TrimSpace(*agentAliasID))+"/sessions/"+url.PathEscape(strings.TrimSpace(*sessionID))+"/text", nil, nil, payload, "application/json", *jsonOut, *raw)
}

func cmdAWSBedrockAgentRuntimeRetrieve(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent-runtime retrieve", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	knowledgeBaseID := fs.String("knowledge-base-id", "", "knowledge base id")
	query := fs.String("query", "", "query text")
	results := fs.Int("results", 0, "number of results")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*knowledgeBaseID) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws bedrock agent-runtime retrieve --knowledge-base-id <id> [--query <text>] [--results N] [--json]")
		return
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*query) == "" {
			fatal(fmt.Errorf("provide --query or --body/--body-file"))
		}
		request := map[string]any{
			"retrievalQuery": map[string]any{"text": strings.TrimSpace(*query)},
		}
		if *results > 0 {
			request["retrievalConfiguration"] = map[string]any{
				"vectorSearchConfiguration": map[string]any{
					"numberOfResults": *results,
				},
			}
		}
		encoded, encodeErr := json.Marshal(request)
		if encodeErr != nil {
			fatal(encodeErr)
		}
		payload = string(encoded)
	}
	runAWSBedrockREST(flags, "bedrock-agent-runtime", http.MethodPost, "/knowledgebases/"+url.PathEscape(strings.TrimSpace(*knowledgeBaseID))+"/retrieve", nil, nil, payload, "application/json", *jsonOut, *raw)
}

func cmdAWSBedrockAgentRuntimeRetrieveAndGenerate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws bedrock agent-runtime retrieve-and-generate", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	knowledgeBaseID := fs.String("knowledge-base-id", "", "knowledge base id")
	query := fs.String("query", "", "query text")
	modelARN := fs.String("model-arn", "", "model arn")
	results := fs.Int("results", 0, "number of results")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws bedrock agent-runtime retrieve-and-generate --knowledge-base-id <id> --query <text> [--model-arn <arn>] [--results N] [--json]")
		return
	}
	payload, err := awsResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*knowledgeBaseID) == "" || strings.TrimSpace(*query) == "" {
			fatal(fmt.Errorf("provide --knowledge-base-id and --query, or provide --body/--body-file"))
		}
		kbConfig := map[string]any{"knowledgeBaseId": strings.TrimSpace(*knowledgeBaseID)}
		if value := strings.TrimSpace(*modelARN); value != "" {
			kbConfig["modelArn"] = value
		}
		if *results > 0 {
			kbConfig["retrievalConfiguration"] = map[string]any{
				"vectorSearchConfiguration": map[string]any{
					"numberOfResults": *results,
				},
			}
		}
		request := map[string]any{
			"input": map[string]any{"text": strings.TrimSpace(*query)},
			"retrieveAndGenerateConfiguration": map[string]any{
				"type":                       "KNOWLEDGE_BASE",
				"knowledgeBaseConfiguration": kbConfig,
			},
		}
		encoded, encodeErr := json.Marshal(request)
		if encodeErr != nil {
			fatal(encodeErr)
		}
		payload = string(encoded)
	}
	runAWSBedrockREST(flags, "bedrock-agent-runtime", http.MethodPost, "/retrieveAndGenerate", nil, nil, payload, "application/json", *jsonOut, *raw)
}

func runAWSBedrockREST(flags awsCommonFlags, service string, method string, reqPath string, params map[string]string, headers map[string]string, body string, contentType string, jsonOut bool, raw bool) {
	runtime := mustAWSRuntimeForService(flags, service)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, service, method, reqPath, params, headers, body, contentType)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, jsonOut, raw)
}

func awsResolveJSONBody(body string, bodyFile string, fallback any) (string, error) {
	body = strings.TrimSpace(body)
	bodyFile = strings.TrimSpace(bodyFile)
	if body != "" && bodyFile != "" {
		return "", fmt.Errorf("--body and --body-file cannot both be set")
	}
	if bodyFile != "" {
		raw, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if body != "" {
		return body, nil
	}
	if fallback == nil {
		return "", nil
	}
	raw, err := json.Marshal(fallback)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
