package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	awsSTSVersion = "2011-06-15"
	awsEC2Version = "2016-11-15"
	awsIAMVersion = "2010-05-08"
)

func cmdAWSSTS(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws sts <get-caller-identity|assume-role> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get-caller-identity", "identity", "whoami":
		cmdAWSSTSGetCallerIdentity(rest)
	case "assume-role", "assume":
		cmdAWSSTSAssumeRole(rest)
	default:
		printUnknown("aws sts", sub)
	}
}

func cmdAWSSTSGetCallerIdentity(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws sts get-caller-identity", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws sts get-caller-identity [--json]")
		return
	}
	runtime := mustAWSRuntimeForService(flags, "sts")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceQuery(ctx, runtime, "sts", awsSTSVersion, "GetCallerIdentity", nil)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSSTSAssumeRole(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws sts assume-role", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	roleARN := fs.String("role-arn", "", "role arn")
	sessionName := fs.String("session-name", "", "role session name")
	duration := fs.Int("duration-seconds", 3600, "session duration seconds")
	externalID := fs.String("external-id", "", "external id")
	sourceIdentity := fs.String("source-identity", "", "source identity")
	serial := fs.String("serial-number", "", "mfa serial number")
	tokenCode := fs.String("token-code", "", "mfa token code")
	policy := fs.String("policy", "", "inline session policy json")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*roleARN) == "" {
		printUsage("usage: si aws sts assume-role --role-arn <arn> [--session-name <name>] [--duration-seconds N] [--json]")
		return
	}
	session := strings.TrimSpace(*sessionName)
	if session == "" {
		session = fmt.Sprintf("si-%d", time.Now().UTC().Unix())
	}
	params := map[string]string{
		"RoleArn":         strings.TrimSpace(*roleARN),
		"RoleSessionName": session,
	}
	if *duration > 0 {
		params["DurationSeconds"] = strconv.Itoa(*duration)
	}
	if value := strings.TrimSpace(*externalID); value != "" {
		params["ExternalId"] = value
	}
	if value := strings.TrimSpace(*sourceIdentity); value != "" {
		params["SourceIdentity"] = value
	}
	if value := strings.TrimSpace(*serial); value != "" {
		params["SerialNumber"] = value
	}
	if value := strings.TrimSpace(*tokenCode); value != "" {
		params["TokenCode"] = value
	}
	if value := strings.TrimSpace(*policy); value != "" {
		params["Policy"] = value
	}
	runtime := mustAWSRuntimeForService(flags, "sts")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceQuery(ctx, runtime, "sts", awsSTSVersion, "AssumeRole", params)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSEC2(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ec2 <instance> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "instance", "instances":
		cmdAWSEC2Instance(rest)
	default:
		printUnknown("aws ec2", sub)
	}
}

func cmdAWSEC2Instance(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ec2 instance <list|start|stop|terminate> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "describe":
		cmdAWSEC2InstanceList(rest)
	case "start":
		cmdAWSEC2InstanceStartStopTerminate("StartInstances", rest)
	case "stop":
		cmdAWSEC2InstanceStartStopTerminate("StopInstances", rest)
	case "terminate", "delete", "remove", "rm":
		cmdAWSEC2InstanceStartStopTerminate("TerminateInstances", rest)
	default:
		printUnknown("aws ec2 instance", sub)
	}
}

func cmdAWSEC2InstanceList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws ec2 instance list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	ids := multiFlag{}
	fs.Var(&ids, "id", "instance id (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws ec2 instance list [--id i-123 --id i-456] [--json]")
		return
	}
	params := map[string]string{}
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		params[fmt.Sprintf("InstanceId.%d", i+1)] = id
	}
	runtime := mustAWSRuntimeForService(flags, "ec2")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceQuery(ctx, runtime, "ec2", awsEC2Version, "DescribeInstances", params)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSEC2InstanceStartStopTerminate(action string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws ec2 instance mutation", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	ids := multiFlag{}
	fs.Var(&ids, "id", "instance id (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws ec2 instance start|stop|terminate --id i-123 [--id i-456] [--force] [--json]")
		return
	}
	params := map[string]string{}
	clean := make([]string, 0, len(ids))
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		clean = append(clean, id)
		params[fmt.Sprintf("InstanceId.%d", i+1)] = id
	}
	if len(clean) == 0 {
		fatal(fmt.Errorf("at least one --id is required"))
	}
	if err := requireAWSConfirmation(strings.ToLower(action)+" instances "+strings.Join(clean, ","), *force); err != nil {
		fatal(err)
	}
	runtime := mustAWSRuntimeForService(flags, "ec2")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceQuery(ctx, runtime, "ec2", awsEC2Version, action, params)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws s3 <bucket|object> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "bucket", "buckets":
		cmdAWSS3Bucket(rest)
	case "object", "objects":
		cmdAWSS3Object(rest)
	default:
		printUnknown("aws s3", sub)
	}
}

func cmdAWSS3Bucket(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws s3 bucket <list|create|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSS3BucketList(rest)
	case "create":
		cmdAWSS3BucketCreate(rest)
	case "delete", "remove", "rm":
		cmdAWSS3BucketDelete(rest)
	default:
		printUnknown("aws s3 bucket", sub)
	}
}

func cmdAWSS3BucketList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws s3 bucket list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws s3 bucket list [--json]")
		return
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodGet, "/", nil, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3BucketCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws s3 bucket create", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	bucket := fs.String("bucket", "", "bucket name")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	name := strings.TrimSpace(*bucket)
	if name == "" && fs.NArg() == 1 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	if name == "" || fs.NArg() > 1 {
		printUsage("usage: si aws s3 bucket create <bucket> [--region <region>] [--json]")
		return
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	body := ""
	contentType := ""
	if strings.TrimSpace(runtime.Region) != "" && strings.TrimSpace(runtime.Region) != "us-east-1" {
		body = `<CreateBucketConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><LocationConstraint>` + runtime.Region + `</LocationConstraint></CreateBucketConfiguration>`
		contentType = "application/xml"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodPut, "/"+name, nil, nil, body, contentType)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3BucketDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws s3 bucket delete", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	bucket := fs.String("bucket", "", "bucket name")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	name := strings.TrimSpace(*bucket)
	if name == "" && fs.NArg() == 1 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	if name == "" || fs.NArg() > 1 {
		printUsage("usage: si aws s3 bucket delete <bucket> [--force] [--json]")
		return
	}
	if err := requireAWSConfirmation("delete s3 bucket "+name, *force); err != nil {
		fatal(err)
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodDelete, "/"+name, nil, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3Object(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws s3 object <list|get|put|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSS3ObjectList(rest)
	case "get":
		cmdAWSS3ObjectGet(rest)
	case "put", "set", "upload":
		cmdAWSS3ObjectPut(rest)
	case "delete", "remove", "rm":
		cmdAWSS3ObjectDelete(rest)
	default:
		printUnknown("aws s3 object", sub)
	}
}

func cmdAWSS3ObjectList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws s3 object list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	bucket := fs.String("bucket", "", "bucket name")
	prefix := fs.String("prefix", "", "object key prefix")
	maxKeys := fs.Int("max-keys", 100, "max keys")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*bucket) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws s3 object list --bucket <name> [--prefix <prefix>] [--max-keys N] [--json]")
		return
	}
	query := map[string]string{"list-type": "2"}
	if value := strings.TrimSpace(*prefix); value != "" {
		query["prefix"] = value
	}
	if *maxKeys > 0 {
		query["max-keys"] = strconv.Itoa(*maxKeys)
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodGet, "/"+strings.TrimSpace(*bucket), query, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3ObjectGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws s3 object get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	bucket := fs.String("bucket", "", "bucket name")
	key := fs.String("key", "", "object key")
	output := fs.String("output", "", "write body to file")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*bucket) == "" || strings.TrimSpace(*key) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws s3 object get --bucket <name> --key <key> [--output <path>] [--json]")
		return
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodGet, awsS3ObjectPath(strings.TrimSpace(*bucket), strings.TrimSpace(*key)), nil, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	if value := strings.TrimSpace(*output); value != "" {
		if err := os.WriteFile(value, []byte(resp.Body), 0o600); err != nil {
			fatal(err)
		}
		if *jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status_code": resp.StatusCode, "output": value})
			return
		}
		successf("wrote object to %s", value)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3ObjectPut(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws s3 object put", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	bucket := fs.String("bucket", "", "bucket name")
	key := fs.String("key", "", "object key")
	body := fs.String("body", "", "raw object body")
	file := fs.String("file", "", "read object body from file")
	contentType := fs.String("content-type", "application/octet-stream", "content type")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*bucket) == "" || strings.TrimSpace(*key) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws s3 object put --bucket <name> --key <key> [--file <path>|--body <text>] [--json]")
		return
	}
	payload := strings.TrimSpace(*body)
	if value := strings.TrimSpace(*file); value != "" {
		data, err := os.ReadFile(value)
		if err != nil {
			fatal(err)
		}
		payload = string(data)
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodPut, awsS3ObjectPath(strings.TrimSpace(*bucket), strings.TrimSpace(*key)), nil, nil, payload, strings.TrimSpace(*contentType))
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSS3ObjectDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws s3 object delete", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	bucket := fs.String("bucket", "", "bucket name")
	key := fs.String("key", "", "object key")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*bucket) == "" || strings.TrimSpace(*key) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws s3 object delete --bucket <name> --key <key> [--force] [--json]")
		return
	}
	if err := requireAWSConfirmation("delete s3 object s3://"+strings.TrimSpace(*bucket)+"/"+strings.TrimSpace(*key), *force); err != nil {
		fatal(err)
	}
	runtime := mustAWSRuntimeForService(flags, "s3")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "s3", http.MethodDelete, awsS3ObjectPath(strings.TrimSpace(*bucket), strings.TrimSpace(*key)), nil, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSLambda(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws lambda <function> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "function", "functions":
		cmdAWSLambdaFunction(rest)
	default:
		printUnknown("aws lambda", sub)
	}
}

func cmdAWSLambdaFunction(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws lambda function <list|get|invoke|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSLambdaFunctionList(rest)
	case "get":
		cmdAWSLambdaFunctionGet(rest)
	case "invoke":
		cmdAWSLambdaFunctionInvoke(rest)
	case "delete", "remove", "rm":
		cmdAWSLambdaFunctionDelete(rest)
	default:
		printUnknown("aws lambda function", sub)
	}
}

func cmdAWSLambdaFunctionList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws lambda function list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 50, "max functions")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws lambda function list [--limit N] [--json]")
		return
	}
	query := map[string]string{}
	if *limit > 0 {
		query["MaxItems"] = strconv.Itoa(*limit)
	}
	runtime := mustAWSRuntimeForService(flags, "lambda")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "lambda", http.MethodGet, "/2015-03-31/functions/", query, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSLambdaFunctionGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws lambda function get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "function name or arn")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws lambda function get <name|arn> [--json]")
		return
	}
	runtime := mustAWSRuntimeForService(flags, "lambda")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "lambda", http.MethodGet, "/2015-03-31/functions/"+url.PathEscape(id), nil, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSLambdaFunctionInvoke(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws lambda function invoke", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "function name or arn")
	payload := fs.String("payload", "", "raw payload")
	payloadFile := fs.String("payload-file", "", "payload file path")
	invocationType := fs.String("invocation-type", "RequestResponse", "RequestResponse|Event|DryRun")
	qualifier := fs.String("qualifier", "", "version/alias qualifier")
	logType := fs.String("log-type", "", "Tail or None")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws lambda function invoke <name|arn> [--payload <json>] [--payload-file <path>] [--json]")
		return
	}
	body := strings.TrimSpace(*payload)
	if value := strings.TrimSpace(*payloadFile); value != "" {
		data, err := os.ReadFile(value)
		if err != nil {
			fatal(err)
		}
		body = string(data)
	}
	query := map[string]string{}
	if value := strings.TrimSpace(*qualifier); value != "" {
		query["Qualifier"] = value
	}
	headers := map[string]string{"X-Amz-Invocation-Type": strings.TrimSpace(*invocationType)}
	if value := strings.TrimSpace(*logType); value != "" {
		headers["X-Amz-Log-Type"] = value
	}
	runtime := mustAWSRuntimeForService(flags, "lambda")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "lambda", http.MethodPost, "/2015-03-31/functions/"+url.PathEscape(id)+"/invocations", query, headers, body, "application/json")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSLambdaFunctionDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws lambda function delete", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "function name or arn")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si aws lambda function delete <name|arn> [--force] [--json]")
		return
	}
	if err := requireAWSConfirmation("delete lambda function "+id, *force); err != nil {
		fatal(err)
	}
	runtime := mustAWSRuntimeForService(flags, "lambda")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoServiceREST(ctx, runtime, "lambda", http.MethodDelete, "/2015-03-31/functions/"+url.PathEscape(id), nil, nil, "", "")
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSECR(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ecr <repository|image> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "repository", "repositories", "repo":
		cmdAWSECRRepository(rest)
	case "image", "images":
		cmdAWSECRImage(rest)
	default:
		printUnknown("aws ecr", sub)
	}
}

func cmdAWSECRRepository(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ecr repository <list|create|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSECRRepositoryList(rest)
	case "create":
		cmdAWSECRRepositoryCreate(rest)
	case "delete", "remove", "rm":
		cmdAWSECRRepositoryDelete(rest)
	default:
		printUnknown("aws ecr repository", sub)
	}
}

func cmdAWSECRRepositoryList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws ecr repository list", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	limit := fs.Int("limit", 100, "max repositories")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws ecr repository list [--limit N] [--json]")
		return
	}
	payload := map[string]any{}
	if *limit > 0 {
		payload["maxResults"] = *limit
	}
	runAWSJSONTarget(flags, "ecr", "AmazonEC2ContainerRegistry_V20150921.DescribeRepositories", payload, *jsonOut, *raw)
}

func cmdAWSECRRepositoryCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws ecr repository create", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "repository name")
	scanOnPush := fs.Bool("scan-on-push", false, "enable image scan on push")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	repo := strings.TrimSpace(*name)
	if repo == "" && fs.NArg() == 1 {
		repo = strings.TrimSpace(fs.Arg(0))
	}
	if repo == "" || fs.NArg() > 1 {
		printUsage("usage: si aws ecr repository create <name> [--scan-on-push] [--json]")
		return
	}
	payload := map[string]any{"repositoryName": repo}
	if *scanOnPush {
		payload["imageScanningConfiguration"] = map[string]any{"scanOnPush": true}
	}
	runAWSJSONTarget(flags, "ecr", "AmazonEC2ContainerRegistry_V20150921.CreateRepository", payload, *jsonOut, *raw)
}

func cmdAWSECRRepositoryDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws ecr repository delete", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "repository name")
	deleteImages := fs.Bool("delete-images", false, "force delete with images")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	repo := strings.TrimSpace(*name)
	if repo == "" && fs.NArg() == 1 {
		repo = strings.TrimSpace(fs.Arg(0))
	}
	if repo == "" || fs.NArg() > 1 {
		printUsage("usage: si aws ecr repository delete <name> [--delete-images] [--force] [--json]")
		return
	}
	if err := requireAWSConfirmation("delete ecr repository "+repo, *force); err != nil {
		fatal(err)
	}
	payload := map[string]any{"repositoryName": repo, "force": *deleteImages}
	runAWSJSONTarget(flags, "ecr", "AmazonEC2ContainerRegistry_V20150921.DeleteRepository", payload, *jsonOut, *raw)
}

func cmdAWSECRImage(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ecr image <list> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws ecr image list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		repo := fs.String("repository", "", "repository name")
		limit := fs.Int("limit", 100, "max images")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		if strings.TrimSpace(*repo) == "" || fs.NArg() > 0 {
			printUsage("usage: si aws ecr image list --repository <name> [--limit N] [--json]")
			return
		}
		payload := map[string]any{"repositoryName": strings.TrimSpace(*repo)}
		if *limit > 0 {
			payload["maxResults"] = *limit
		}
		runAWSJSONTarget(flags, "ecr", "AmazonEC2ContainerRegistry_V20150921.ListImages", payload, *jsonOut, *raw)
	default:
		printUnknown("aws ecr image", sub)
	}
}

func cmdAWSSecrets(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws secrets <list|get|create|put|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws secrets list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		limit := fs.Int("limit", 100, "max secrets")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		payload := map[string]any{}
		if *limit > 0 {
			payload["MaxResults"] = *limit
		}
		runAWSJSONTarget(flags, "secretsmanager", "secretsmanager.ListSecrets", payload, *jsonOut, *raw)
	case "get":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws secrets get", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		id := fs.String("id", "", "secret id or arn")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		name := strings.TrimSpace(*id)
		if name == "" && fs.NArg() == 1 {
			name = strings.TrimSpace(fs.Arg(0))
		}
		if name == "" || fs.NArg() > 1 {
			printUsage("usage: si aws secrets get <secret_id> [--json]")
			return
		}
		runAWSJSONTarget(flags, "secretsmanager", "secretsmanager.GetSecretValue", map[string]any{"SecretId": name}, *jsonOut, *raw)
	case "create":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws secrets create", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		name := fs.String("name", "", "secret name")
		secret := fs.String("value", "", "secret string value")
		description := fs.String("description", "", "secret description")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		if strings.TrimSpace(*name) == "" {
			printUsage("usage: si aws secrets create --name <name> --value <text> [--description <text>] [--json]")
			return
		}
		payload := map[string]any{"Name": strings.TrimSpace(*name), "SecretString": *secret}
		if value := strings.TrimSpace(*description); value != "" {
			payload["Description"] = value
		}
		runAWSJSONTarget(flags, "secretsmanager", "secretsmanager.CreateSecret", payload, *jsonOut, *raw)
	case "put", "set", "update":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws secrets put", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		id := fs.String("id", "", "secret id or arn")
		secret := fs.String("value", "", "secret string value")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		name := strings.TrimSpace(*id)
		if name == "" && fs.NArg() == 1 {
			name = strings.TrimSpace(fs.Arg(0))
		}
		if name == "" || fs.NArg() > 1 {
			printUsage("usage: si aws secrets put <secret_id> --value <text> [--json]")
			return
		}
		runAWSJSONTarget(flags, "secretsmanager", "secretsmanager.PutSecretValue", map[string]any{"SecretId": name, "SecretString": *secret}, *jsonOut, *raw)
	case "delete", "remove", "rm":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true, "force": true})
		fs := flag.NewFlagSet("aws secrets delete", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		id := fs.String("id", "", "secret id or arn")
		force := fs.Bool("force", false, "skip confirmation prompt")
		forceNow := fs.Bool("force-delete", true, "force immediate delete")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		name := strings.TrimSpace(*id)
		if name == "" && fs.NArg() == 1 {
			name = strings.TrimSpace(fs.Arg(0))
		}
		if name == "" || fs.NArg() > 1 {
			printUsage("usage: si aws secrets delete <secret_id> [--force] [--json]")
			return
		}
		if err := requireAWSConfirmation("delete secret "+name, *force); err != nil {
			fatal(err)
		}
		payload := map[string]any{"SecretId": name, "ForceDeleteWithoutRecovery": *forceNow}
		runAWSJSONTarget(flags, "secretsmanager", "secretsmanager.DeleteSecret", payload, *jsonOut, *raw)
	default:
		printUnknown("aws secrets", sub)
	}
}

func cmdAWSKMS(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws kms <key|encrypt|decrypt> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "key", "keys":
		cmdAWSKMSKey(rest)
	case "encrypt":
		cmdAWSKMSEncrypt(rest)
	case "decrypt":
		cmdAWSKMSDecrypt(rest)
	default:
		printUnknown("aws kms", sub)
	}
}

func cmdAWSKMSKey(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws kms key <list|describe>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws kms key list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		limit := fs.Int("limit", 100, "max keys")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		payload := map[string]any{}
		if *limit > 0 {
			payload["Limit"] = *limit
		}
		runAWSJSONTarget(flags, "kms", "TrentService.ListKeys", payload, *jsonOut, *raw)
	case "describe":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws kms key describe", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		keyID := fs.String("key-id", "", "kms key id or arn")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		id := strings.TrimSpace(*keyID)
		if id == "" && fs.NArg() == 1 {
			id = strings.TrimSpace(fs.Arg(0))
		}
		if id == "" || fs.NArg() > 1 {
			printUsage("usage: si aws kms key describe <key_id|arn> [--json]")
			return
		}
		runAWSJSONTarget(flags, "kms", "TrentService.DescribeKey", map[string]any{"KeyId": id}, *jsonOut, *raw)
	default:
		printUnknown("aws kms key", sub)
	}
}

func cmdAWSKMSEncrypt(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws kms encrypt", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	keyID := fs.String("key-id", "", "kms key id or arn")
	plaintext := fs.String("plaintext", "", "plaintext value")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*keyID) == "" || strings.TrimSpace(*plaintext) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws kms encrypt --key-id <id|arn> --plaintext <text> [--json]")
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(*plaintext))
	runAWSJSONTarget(flags, "kms", "TrentService.Encrypt", map[string]any{"KeyId": strings.TrimSpace(*keyID), "Plaintext": encoded}, *jsonOut, *raw)
}

func cmdAWSKMSDecrypt(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws kms decrypt", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	cipher := fs.String("ciphertext", "", "ciphertext blob (base64)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*cipher) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws kms decrypt --ciphertext <base64> [--json]")
		return
	}
	runAWSJSONTarget(flags, "kms", "TrentService.Decrypt", map[string]any{"CiphertextBlob": strings.TrimSpace(*cipher)}, *jsonOut, *raw)
}

func cmdAWSDynamoDB(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws dynamodb <table|item> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "table", "tables":
		cmdAWSDynamoTable(rest)
	case "item", "items":
		cmdAWSDynamoItem(rest)
	default:
		printUnknown("aws dynamodb", sub)
	}
}

func cmdAWSDynamoTable(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws dynamodb table <list|describe>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws dynamodb table list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		limit := fs.Int("limit", 100, "max tables")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		payload := map[string]any{}
		if *limit > 0 {
			payload["Limit"] = *limit
		}
		runAWSJSONTarget(flags, "dynamodb", "DynamoDB_20120810.ListTables", payload, *jsonOut, *raw)
	case "describe":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws dynamodb table describe", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		name := fs.String("name", "", "table name")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		table := strings.TrimSpace(*name)
		if table == "" && fs.NArg() == 1 {
			table = strings.TrimSpace(fs.Arg(0))
		}
		if table == "" || fs.NArg() > 1 {
			printUsage("usage: si aws dynamodb table describe <name> [--json]")
			return
		}
		runAWSJSONTarget(flags, "dynamodb", "DynamoDB_20120810.DescribeTable", map[string]any{"TableName": table}, *jsonOut, *raw)
	default:
		printUnknown("aws dynamodb table", sub)
	}
}

func cmdAWSDynamoItem(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws dynamodb item <get|put|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdAWSDynamoItemGet(rest)
	case "put":
		cmdAWSDynamoItemPut(rest)
	case "delete", "remove", "rm":
		cmdAWSDynamoItemDelete(rest)
	default:
		printUnknown("aws dynamodb item", sub)
	}
}

func cmdAWSDynamoItemGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws dynamodb item get", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	table := fs.String("table", "", "table name")
	keyJSON := fs.String("key-json", "", "key object json")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*table) == "" || strings.TrimSpace(*keyJSON) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws dynamodb item get --table <name> --key-json '{...}' [--json]")
		return
	}
	payload := map[string]any{"TableName": strings.TrimSpace(*table)}
	var key any
	if err := json.Unmarshal([]byte(strings.TrimSpace(*keyJSON)), &key); err != nil {
		fatal(fmt.Errorf("invalid --key-json: %w", err))
	}
	payload["Key"] = key
	runAWSJSONTarget(flags, "dynamodb", "DynamoDB_20120810.GetItem", payload, *jsonOut, *raw)
}

func cmdAWSDynamoItemPut(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws dynamodb item put", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	table := fs.String("table", "", "table name")
	itemJSON := fs.String("item-json", "", "item object json")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*table) == "" || strings.TrimSpace(*itemJSON) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws dynamodb item put --table <name> --item-json '{...}' [--json]")
		return
	}
	payload := map[string]any{"TableName": strings.TrimSpace(*table)}
	var item any
	if err := json.Unmarshal([]byte(strings.TrimSpace(*itemJSON)), &item); err != nil {
		fatal(fmt.Errorf("invalid --item-json: %w", err))
	}
	payload["Item"] = item
	runAWSJSONTarget(flags, "dynamodb", "DynamoDB_20120810.PutItem", payload, *jsonOut, *raw)
}

func cmdAWSDynamoItemDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("aws dynamodb item delete", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	table := fs.String("table", "", "table name")
	keyJSON := fs.String("key-json", "", "key object json")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*table) == "" || strings.TrimSpace(*keyJSON) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws dynamodb item delete --table <name> --key-json '{...}' [--force] [--json]")
		return
	}
	if err := requireAWSConfirmation("delete dynamodb item from table "+strings.TrimSpace(*table), *force); err != nil {
		fatal(err)
	}
	payload := map[string]any{"TableName": strings.TrimSpace(*table)}
	var key any
	if err := json.Unmarshal([]byte(strings.TrimSpace(*keyJSON)), &key); err != nil {
		fatal(fmt.Errorf("invalid --key-json: %w", err))
	}
	payload["Key"] = key
	runAWSJSONTarget(flags, "dynamodb", "DynamoDB_20120810.DeleteItem", payload, *jsonOut, *raw)
}

func cmdAWSSSM(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ssm <parameter> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "parameter", "parameters":
		cmdAWSSSMParameter(rest)
	default:
		printUnknown("aws ssm", sub)
	}
}

func cmdAWSSSMParameter(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws ssm parameter <list|get|put|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws ssm parameter list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		limit := fs.Int("limit", 50, "max params")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		payload := map[string]any{"MaxResults": *limit}
		runAWSJSONTarget(flags, "ssm", "AmazonSSM.DescribeParameters", payload, *jsonOut, *raw)
	case "get":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws ssm parameter get", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		name := fs.String("name", "", "parameter name")
		decrypt := fs.Bool("decrypt", true, "with decryption")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		param := strings.TrimSpace(*name)
		if param == "" && fs.NArg() == 1 {
			param = strings.TrimSpace(fs.Arg(0))
		}
		if param == "" || fs.NArg() > 1 {
			printUsage("usage: si aws ssm parameter get <name> [--decrypt] [--json]")
			return
		}
		runAWSJSONTarget(flags, "ssm", "AmazonSSM.GetParameter", map[string]any{"Name": param, "WithDecryption": *decrypt}, *jsonOut, *raw)
	case "put", "set":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws ssm parameter put", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		name := fs.String("name", "", "parameter name")
		value := fs.String("value", "", "parameter value")
		typ := fs.String("type", "SecureString", "String|StringList|SecureString")
		overwrite := fs.Bool("overwrite", true, "overwrite existing parameter")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		if strings.TrimSpace(*name) == "" || fs.NArg() > 0 {
			printUsage("usage: si aws ssm parameter put --name <name> --value <text> [--type SecureString] [--overwrite] [--json]")
			return
		}
		payload := map[string]any{"Name": strings.TrimSpace(*name), "Value": *value, "Type": strings.TrimSpace(*typ), "Overwrite": *overwrite}
		runAWSJSONTarget(flags, "ssm", "AmazonSSM.PutParameter", payload, *jsonOut, *raw)
	case "delete", "remove", "rm":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true, "force": true})
		fs := flag.NewFlagSet("aws ssm parameter delete", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		name := fs.String("name", "", "parameter name")
		force := fs.Bool("force", false, "skip confirmation prompt")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		param := strings.TrimSpace(*name)
		if param == "" && fs.NArg() == 1 {
			param = strings.TrimSpace(fs.Arg(0))
		}
		if param == "" || fs.NArg() > 1 {
			printUsage("usage: si aws ssm parameter delete <name> [--force] [--json]")
			return
		}
		if err := requireAWSConfirmation("delete ssm parameter "+param, *force); err != nil {
			fatal(err)
		}
		runAWSJSONTarget(flags, "ssm", "AmazonSSM.DeleteParameter", map[string]any{"Name": param}, *jsonOut, *raw)
	default:
		printUnknown("aws ssm parameter", sub)
	}
}

func cmdAWSCloudWatch(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws cloudwatch <metric> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "metric", "metrics":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws cloudwatch metric list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		namespace := fs.String("namespace", "", "metric namespace")
		name := fs.String("name", "", "metric name")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		params := map[string]string{}
		if value := strings.TrimSpace(*namespace); value != "" {
			params["Namespace"] = value
		}
		if value := strings.TrimSpace(*name); value != "" {
			params["MetricName"] = value
		}
		runtime := mustAWSRuntimeForService(flags, "monitoring")
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		resp, err := awsDoServiceQuery(ctx, runtime, "monitoring", "2010-08-01", "ListMetrics", params)
		if err != nil {
			printAWSError(err)
			return
		}
		printAWSResponse(resp, *jsonOut, *raw)
	default:
		printUnknown("aws cloudwatch", sub)
	}
}

func cmdAWSLogs(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws logs <group|stream|events> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "group", "groups":
		cmdAWSLogsGroup(rest)
	case "stream", "streams":
		cmdAWSLogsStream(rest)
	case "events", "filter":
		cmdAWSLogsEvents(rest)
	default:
		printUnknown("aws logs", sub)
	}
}

func cmdAWSLogsGroup(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws logs group <list>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws logs group list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		limit := fs.Int("limit", 50, "max groups")
		prefix := fs.String("prefix", "", "log group name prefix")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		payload := map[string]any{"limit": *limit}
		if value := strings.TrimSpace(*prefix); value != "" {
			payload["logGroupNamePrefix"] = value
		}
		runAWSJSONTarget(flags, "logs", "Logs_20140328.DescribeLogGroups", payload, *jsonOut, *raw)
	default:
		printUnknown("aws logs group", sub)
	}
}

func cmdAWSLogsStream(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws logs stream <list>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("aws logs stream list", flag.ExitOnError)
		flags := bindAWSCommonFlags(fs)
		group := fs.String("group", "", "log group name")
		limit := fs.Int("limit", 50, "max streams")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		_ = fs.Parse(args)
		if strings.TrimSpace(*group) == "" {
			printUsage("usage: si aws logs stream list --group <name> [--limit N] [--json]")
			return
		}
		payload := map[string]any{"logGroupName": strings.TrimSpace(*group), "limit": *limit}
		runAWSJSONTarget(flags, "logs", "Logs_20140328.DescribeLogStreams", payload, *jsonOut, *raw)
	default:
		printUnknown("aws logs stream", sub)
	}
}

func cmdAWSLogsEvents(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws logs events", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	group := fs.String("group", "", "log group name")
	stream := fs.String("stream", "", "log stream name")
	filterPattern := fs.String("filter", "", "filter pattern")
	start := fs.Int64("start", 0, "start time epoch millis")
	end := fs.Int64("end", 0, "end time epoch millis")
	limit := fs.Int("limit", 50, "max events")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*group) == "" || fs.NArg() > 0 {
		printUsage("usage: si aws logs events --group <name> [--stream <name>] [--filter <pattern>] [--start <ms>] [--end <ms>] [--limit N] [--json]")
		return
	}
	payload := map[string]any{"logGroupName": strings.TrimSpace(*group), "limit": *limit}
	if value := strings.TrimSpace(*stream); value != "" {
		payload["logStreamNames"] = []string{value}
	}
	if value := strings.TrimSpace(*filterPattern); value != "" {
		payload["filterPattern"] = value
	}
	if *start > 0 {
		payload["startTime"] = *start
	}
	if *end > 0 {
		payload["endTime"] = *end
	}
	runAWSJSONTarget(flags, "logs", "Logs_20140328.FilterLogEvents", payload, *jsonOut, *raw)
}

func runAWSJSONTarget(flags awsCommonFlags, service string, target string, payload map[string]any, jsonOut bool, raw bool) {
	runtime := mustAWSRuntimeForService(flags, service)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := awsDoServiceJSON(ctx, runtime, service, target, payload)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, jsonOut, raw)
}

func mustAWSRuntimeForService(flags awsCommonFlags, service string) awsRuntimeContext {
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(valueOrEmpty(flags.baseURL)) == "" {
		runtime.BaseURL = awsServiceDefaultEndpoint(service, runtime.Region)
	}
	return runtime
}

func awsServiceDefaultEndpoint(service string, region string) string {
	service = strings.ToLower(strings.TrimSpace(service))
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-east-1"
	}
	switch service {
	case "iam":
		return "https://iam.amazonaws.com"
	case "sts":
		if region == "us-east-1" {
			return "https://sts.amazonaws.com"
		}
		return "https://sts." + region + ".amazonaws.com"
	case "s3":
		if region == "us-east-1" {
			return "https://s3.amazonaws.com"
		}
		return "https://s3." + region + ".amazonaws.com"
	case "ec2":
		return "https://ec2." + region + ".amazonaws.com"
	case "lambda":
		return "https://lambda." + region + ".amazonaws.com"
	case "ecr":
		return "https://api.ecr." + region + ".amazonaws.com"
	case "secretsmanager":
		return "https://secretsmanager." + region + ".amazonaws.com"
	case "kms":
		return "https://kms." + region + ".amazonaws.com"
	case "dynamodb":
		return "https://dynamodb." + region + ".amazonaws.com"
	case "ssm":
		return "https://ssm." + region + ".amazonaws.com"
	case "monitoring":
		return "https://monitoring." + region + ".amazonaws.com"
	case "logs":
		return "https://logs." + region + ".amazonaws.com"
	default:
		return "https://" + service + "." + region + ".amazonaws.com"
	}
}

func awsDoServiceQuery(ctx context.Context, runtime awsRuntimeContext, service string, version string, action string, params map[string]string) (awsResponse, error) {
	action = strings.TrimSpace(action)
	if action == "" {
		return awsResponse{}, fmt.Errorf("action is required")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = awsIAMVersion
	}
	form := url.Values{}
	form.Set("Action", action)
	form.Set("Version", version)
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		form.Set(key, strings.TrimSpace(value))
	}
	body := form.Encode()
	endpoint := ensureURLWithSlash(strings.TrimSpace(runtime.BaseURL))
	return awsDoSignedRequest(ctx, runtime, http.MethodPost, endpoint, body, "application/x-www-form-urlencoded; charset=utf-8", service, nil)
}

func awsDoServiceJSON(ctx context.Context, runtime awsRuntimeContext, service string, target string, payload any) (awsResponse, error) {
	rawBody := "{}"
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return awsResponse{}, err
		}
		rawBody = string(encoded)
	}
	headers := map[string]string{}
	if strings.TrimSpace(target) != "" {
		headers["X-Amz-Target"] = strings.TrimSpace(target)
	}
	endpoint := ensureURLWithSlash(strings.TrimSpace(runtime.BaseURL))
	return awsDoSignedRequest(ctx, runtime, http.MethodPost, endpoint, rawBody, "application/x-amz-json-1.1", service, headers)
}

func awsDoServiceREST(ctx context.Context, runtime awsRuntimeContext, service string, method string, reqPath string, params map[string]string, headers map[string]string, body string, contentType string) (awsResponse, error) {
	endpoint, err := awsURLWithParams(runtime.BaseURL, reqPath, params)
	if err != nil {
		return awsResponse{}, err
	}
	return awsDoSignedRequest(ctx, runtime, strings.ToUpper(strings.TrimSpace(method)), endpoint, body, contentType, service, headers)
}

func awsS3ObjectPath(bucket string, key string) string {
	bucket = strings.TrimSpace(bucket)
	key = strings.TrimSpace(key)
	parts := strings.Split(key, "/")
	for idx, item := range parts {
		parts[idx] = url.PathEscape(item)
	}
	return path.Join("/", bucket, strings.Join(parts, "/"))
}

func requireAWSConfirmation(action string, force bool) error {
	if force {
		return nil
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "continue"
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("refusing to %s without confirmation in non-interactive mode; use --force", action)
	}
	fmt.Printf("%s ", styleWarn(fmt.Sprintf("Confirm %s? type `yes` to continue (Esc to cancel):", action)))
	line, err := promptLine(os.Stdin)
	if err != nil {
		return err
	}
	if isEscCancelInput(line) {
		return fmt.Errorf("operation canceled")
	}
	if strings.EqualFold(strings.TrimSpace(line), "yes") {
		return nil
	}
	return fmt.Errorf("operation canceled")
}
