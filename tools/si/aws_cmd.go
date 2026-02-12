package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/httpx"
	"si/tools/si/internal/integrationruntime"
	"si/tools/si/internal/netpolicy"
	"si/tools/si/internal/providers"
)

const awsUsageText = "usage: si aws <auth|context|doctor|iam|sts|s3|ec2|lambda|ecr|secrets|kms|dynamodb|ssm|cloudwatch|logs|bedrock|raw>"

type awsRuntimeContext struct {
	AccountAlias string
	Region       string
	BaseURL      string
	AccessKeyID  string
	SecretKey    string
	SessionToken string
	Source       string
	LogPath      string
}

type awsRuntimeContextInput struct {
	AccountFlag   string
	RegionFlag    string
	BaseURLFlag   string
	AccessKeyFlag string
	SecretKeyFlag string
	SessionFlag   string
}

type awsResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
}

type awsAPIErrorDetails struct {
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

func (e *awsAPIErrorDetails) Error() string {
	if e == nil {
		return "aws iam api error"
	}
	parts := make([]string, 0, 6)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+e.Code)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if strings.TrimSpace(e.RequestID) != "" {
		parts = append(parts, "request_id="+e.RequestID)
	}
	if len(parts) == 0 {
		return "aws iam api error"
	}
	return "aws iam api error: " + strings.Join(parts, ", ")
}

func cmdAWS(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, awsUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(awsUsageText)
	case "auth":
		cmdAWSAuth(rest)
	case "context":
		cmdAWSContext(rest)
	case "doctor":
		cmdAWSDoctor(rest)
	case "iam":
		cmdAWSIAM(rest)
	case "sts":
		cmdAWSSTS(rest)
	case "s3":
		cmdAWSS3(rest)
	case "ec2":
		cmdAWSEC2(rest)
	case "lambda":
		cmdAWSLambda(rest)
	case "ecr":
		cmdAWSECR(rest)
	case "secrets", "secretsmanager", "secret":
		cmdAWSSecrets(rest)
	case "kms":
		cmdAWSKMS(rest)
	case "dynamodb":
		cmdAWSDynamoDB(rest)
	case "ssm":
		cmdAWSSSM(rest)
	case "cloudwatch":
		cmdAWSCloudWatch(rest)
	case "logs", "cloudwatch-logs":
		cmdAWSLogs(rest)
	case "bedrock", "ai":
		cmdAWSBedrock(rest)
	case "raw":
		cmdAWSRaw(rest)
	default:
		printUnknown("aws", sub)
		printUsage(awsUsageText)
	}
}

func cmdAWSAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws auth status [--account <alias>] [--region <aws-region>] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "status":
		cmdAWSAuthStatus(args[1:])
	default:
		printUnknown("aws auth", sub)
	}
}

func cmdAWSAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("aws auth status", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	userName := fs.String("user-name", "", "iam username for GetUser")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws auth status [--account <alias>] [--region <aws-region>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	params := map[string]string{}
	if value := strings.TrimSpace(*userName); value != "" {
		params["UserName"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	resp, verifyErr := awsDoQuery(ctx, runtime, "GetUser", params)
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":        status,
		"account_alias": runtime.AccountAlias,
		"region":        runtime.Region,
		"base_url":      runtime.BaseURL,
		"source":        runtime.Source,
		"access_key":    previewAWSAccessKey(runtime.AccessKeyID),
	}
	if verifyErr == nil {
		payload["verify_status"] = resp.StatusCode
		payload["verify"] = resp.Data
	} else {
		payload["verify_error"] = verifyErr.Error()
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if verifyErr != nil {
			os.Exit(1)
		}
		return
	}
	if verifyErr != nil {
		fmt.Printf("%s %s\n", styleHeading("AWS auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatAWSContext(runtime))
		printAWSError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("AWS auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatAWSContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Access key:"), previewAWSAccessKey(runtime.AccessKeyID))
}

func cmdAWSContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAWSContextList(rest)
	case "current":
		cmdAWSContextCurrent(rest)
	case "use":
		cmdAWSContextUse(rest)
	default:
		printUnknown("aws context", sub)
	}
}

func cmdAWSContextList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("aws context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := awsAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.AWS.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":          alias,
			"name":           strings.TrimSpace(entry.Name),
			"default":        boolString(alias == strings.TrimSpace(settings.AWS.DefaultAccount)),
			"region":         firstNonEmpty(strings.TrimSpace(entry.Region), strings.TrimSpace(settings.AWS.DefaultRegion)),
			"access_key_env": strings.TrimSpace(entry.AccessKeyIDEnv),
		})
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fatal(err)
		}
		return
	}
	if len(rows) == 0 {
		infof("no aws accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("REGION"), 14),
		padRightANSI(styleHeading("ACCESS KEY ENV"), 32),
		styleHeading("NAME"),
	)
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["region"]), 14),
			padRightANSI(orDash(row["access_key_env"]), 32),
			orDash(row["name"]),
		)
	}
}

func cmdAWSContextCurrent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("aws context current", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws context current [--json]")
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"region":        runtime.Region,
		"base_url":      runtime.BaseURL,
		"source":        runtime.Source,
		"access_key":    previewAWSAccessKey(runtime.AccessKeyID),
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current aws context:"), formatAWSContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdAWSContextUse(args []string) {
	fs := flag.NewFlagSet("aws context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	region := fs.String("region", "", "default aws region")
	baseURL := fs.String("base-url", "", "api base url")
	accessKeyEnv := fs.String("access-key-env", "", "access key env-var reference")
	secretKeyEnv := fs.String("secret-key-env", "", "secret key env-var reference")
	sessionEnv := fs.String("session-token-env", "", "session token env-var reference")
	vaultPrefix := fs.String("vault-prefix", "", "account env prefix")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws context use [--account <alias>] [--region <aws-region>] [--base-url <url>] [--access-key-env <env>] [--secret-key-env <env>] [--session-token-env <env>] [--vault-prefix <prefix>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.AWS.DefaultAccount = value
	}
	if value := strings.TrimSpace(*region); value != "" {
		settings.AWS.DefaultRegion = value
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.AWS.APIBaseURL = value
	}
	targetAlias := strings.TrimSpace(settings.AWS.DefaultAccount)
	if value := strings.TrimSpace(*account); value != "" {
		targetAlias = value
	}
	if targetAlias == "" && (strings.TrimSpace(*accessKeyEnv) != "" || strings.TrimSpace(*secretKeyEnv) != "" || strings.TrimSpace(*sessionEnv) != "" || strings.TrimSpace(*vaultPrefix) != "") {
		targetAlias = "default"
		settings.AWS.DefaultAccount = targetAlias
	}
	if targetAlias != "" {
		if settings.AWS.Accounts == nil {
			settings.AWS.Accounts = map[string]AWSAccountEntry{}
		}
		entry := settings.AWS.Accounts[targetAlias]
		if value := strings.TrimSpace(*region); value != "" {
			entry.Region = value
		}
		if value := strings.TrimSpace(*accessKeyEnv); value != "" {
			entry.AccessKeyIDEnv = value
		}
		if value := strings.TrimSpace(*secretKeyEnv); value != "" {
			entry.SecretAccessKeyEnv = value
		}
		if value := strings.TrimSpace(*sessionEnv); value != "" {
			entry.SessionTokenEnv = value
		}
		if value := strings.TrimSpace(*vaultPrefix); value != "" {
			entry.VaultPrefix = value
		}
		settings.AWS.Accounts[targetAlias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	fmt.Printf("%s default_account=%s region=%s base=%s\n",
		styleHeading("Updated aws context:"),
		orDash(settings.AWS.DefaultAccount),
		orDash(settings.AWS.DefaultRegion),
		orDash(settings.AWS.APIBaseURL),
	)
}

func cmdAWSDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("aws doctor", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	publicProbe := fs.Bool("public", false, "run unauthenticated public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws doctor [--account <alias>] [--region <aws-region>] [--public] [--json]")
		return
	}
	if *publicProbe {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.AWSIAM, strings.TrimSpace(*flags.baseURL))
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("aws", result, *jsonOut)
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, verifyErr := awsDoQuery(ctx, runtime, "GetUser", nil)
	checks := []doctorCheck{
		{Name: "access-key", OK: strings.TrimSpace(runtime.AccessKeyID) != "", Detail: previewAWSAccessKey(runtime.AccessKeyID)},
		{Name: "region", OK: strings.TrimSpace(runtime.Region) != "", Detail: runtime.Region},
		{Name: "request", OK: verifyErr == nil, Detail: errorOrOK(verifyErr)},
	}
	ok := true
	for _, check := range checks {
		if !check.OK {
			ok = false
		}
	}
	payload := map[string]any{
		"ok":            ok,
		"provider":      "aws_iam",
		"base_url":      runtime.BaseURL,
		"account_alias": runtime.AccountAlias,
		"region":        runtime.Region,
		"checks":        checks,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if !ok {
			os.Exit(1)
		}
		return
	}
	if ok {
		fmt.Printf("%s %s\n", styleHeading("AWS doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("AWS doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatAWSContext(runtime))
	for _, check := range checks {
		icon := styleSuccess("OK")
		if !check.OK {
			icon = styleError("ERR")
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(icon, 4), padRightANSI(check.Name, 14), strings.TrimSpace(check.Detail))
	}
	if !ok {
		os.Exit(1)
	}
}

func cmdAWSIAM(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws iam <user|query>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "user", "users":
		cmdAWSIAMUser(rest)
	case "query":
		cmdAWSIAMQuery(rest)
	default:
		printUnknown("aws iam", sub)
	}
}

func cmdAWSIAMUser(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si aws iam user <create|attach-policy>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "create":
		cmdAWSIAMUserCreate(rest)
	case "attach-policy", "attach":
		cmdAWSIAMUserAttachPolicy(rest)
	default:
		printUnknown("aws iam user", sub)
	}
}

func cmdAWSIAMUserCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("aws iam user create", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	name := fs.String("name", "", "iam user name")
	path := fs.String("path", "", "iam user path")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*name) == "" {
		printUsage("usage: si aws iam user create --name <user> [--path /system/] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	params := map[string]string{"UserName": strings.TrimSpace(*name)}
	if value := strings.TrimSpace(*path); value != "" {
		params["Path"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoQuery(ctx, runtime, "CreateUser", params)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, false)
}

func cmdAWSIAMUserAttachPolicy(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("aws iam user attach-policy", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	user := fs.String("user", "", "iam user name")
	policyARN := fs.String("policy-arn", "", "policy arn")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*user) == "" || strings.TrimSpace(*policyARN) == "" {
		printUsage("usage: si aws iam user attach-policy --user <name> --policy-arn <arn> [--json]")
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	params := map[string]string{
		"UserName":  strings.TrimSpace(*user),
		"PolicyArn": strings.TrimSpace(*policyARN),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoQuery(ctx, runtime, "AttachUserPolicy", params)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, false)
}

func cmdAWSIAMQuery(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws iam query", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	action := fs.String("action", "", "iam action name")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*action) == "" {
		printUsage("usage: si aws iam query --action <Action> [--param key=value] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoQuery(ctx, runtime, strings.TrimSpace(*action), parseAWSParams(params))
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSRaw(args []string) {
	cmdAWSRawCompat(args)
}

func resolveRuntimeFromAWSFlags(flags awsCommonFlags) (awsRuntimeContext, error) {
	return resolveRuntimeFromAWSFlagsWithBase(flags, "")
}

func resolveRuntimeFromAWSFlagsWithBase(flags awsCommonFlags, defaultBaseURL string) (awsRuntimeContext, error) {
	base := strings.TrimSpace(valueOrEmpty(flags.baseURL))
	if base == "" {
		base = strings.TrimSpace(defaultBaseURL)
	}
	return resolveAWSRuntimeContext(awsRuntimeContextInput{
		AccountFlag:   strings.TrimSpace(valueOrEmpty(flags.account)),
		RegionFlag:    strings.TrimSpace(valueOrEmpty(flags.region)),
		BaseURLFlag:   base,
		AccessKeyFlag: strings.TrimSpace(valueOrEmpty(flags.accessKey)),
		SecretKeyFlag: strings.TrimSpace(valueOrEmpty(flags.secretKey)),
		SessionFlag:   strings.TrimSpace(valueOrEmpty(flags.sessionToken)),
	})
}

func resolveAWSRuntimeContext(input awsRuntimeContextInput) (awsRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveAWSAccountSelection(settings, input.AccountFlag)

	region := strings.TrimSpace(input.RegionFlag)
	if region == "" {
		region = strings.TrimSpace(account.Region)
	}
	if region == "" {
		region = strings.TrimSpace(settings.AWS.DefaultRegion)
	}
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_REGION"))
	}
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	if region == "" {
		region = "us-east-1"
	}

	spec := providers.Resolve(providers.AWSIAM)
	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.AWS.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("AWS_IAM_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(spec.BaseURL)
	}
	if baseURL == "" {
		baseURL = "https://iam.amazonaws.com"
	}

	accessKey, accessSource := resolveAWSAccessKey(alias, account, strings.TrimSpace(input.AccessKeyFlag))
	secretKey, secretSource := resolveAWSSecretKey(alias, account, strings.TrimSpace(input.SecretKeyFlag))
	sessionToken, sessionSource := resolveAWSSessionToken(alias, account, strings.TrimSpace(input.SessionFlag))
	if strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		prefix := awsAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "AWS_<ACCOUNT>_"
		}
		return awsRuntimeContext{}, fmt.Errorf("aws credentials not found (set --access-key/--secret-key, %sACCESS_KEY_ID, %sSECRET_ACCESS_KEY, or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY)", prefix, prefix)
	}

	source := strings.Join(nonEmpty(accessSource, secretSource, sessionSource), ",")
	return awsRuntimeContext{
		AccountAlias: strings.TrimSpace(alias),
		Region:       strings.TrimSpace(region),
		BaseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		AccessKeyID:  strings.TrimSpace(accessKey),
		SecretKey:    strings.TrimSpace(secretKey),
		SessionToken: strings.TrimSpace(sessionToken),
		Source:       source,
		LogPath:      resolveAWSLogPath(settings),
	}, nil
}

func resolveAWSAccountSelection(settings Settings, accountFlag string) (string, AWSAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.AWS.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("AWS_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := awsAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", AWSAccountEntry{}
	}
	if entry, ok := settings.AWS.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, AWSAccountEntry{}
}

func awsAccountAliases(settings Settings) []string {
	if len(settings.AWS.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.AWS.Accounts))
	for alias := range settings.AWS.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func awsAccountEnvPrefix(alias string, account AWSAccountEntry) string {
	if prefix := strings.TrimSpace(account.VaultPrefix); prefix != "" {
		if strings.HasSuffix(prefix, "_") {
			return strings.ToUpper(prefix)
		}
		return strings.ToUpper(prefix) + "_"
	}
	alias = slugUpper(alias)
	if alias == "" {
		return ""
	}
	return "AWS_" + alias + "_"
}

func resolveAWSEnv(alias string, account AWSAccountEntry, key string) string {
	prefix := awsAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveAWSAccessKey(alias string, account AWSAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--access-key"
	}
	if ref := strings.TrimSpace(account.AccessKeyIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveAWSEnv(alias, account, "ACCESS_KEY_ID")); value != "" {
		return value, "env:" + awsAccountEnvPrefix(alias, account) + "ACCESS_KEY_ID"
	}
	if value := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")); value != "" {
		return value, "env:AWS_ACCESS_KEY_ID"
	}
	return "", ""
}

func resolveAWSSecretKey(alias string, account AWSAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--secret-key"
	}
	if ref := strings.TrimSpace(account.SecretAccessKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveAWSEnv(alias, account, "SECRET_ACCESS_KEY")); value != "" {
		return value, "env:" + awsAccountEnvPrefix(alias, account) + "SECRET_ACCESS_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")); value != "" {
		return value, "env:AWS_SECRET_ACCESS_KEY"
	}
	return "", ""
}

func resolveAWSSessionToken(alias string, account AWSAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--session-token"
	}
	if ref := strings.TrimSpace(account.SessionTokenEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveAWSEnv(alias, account, "SESSION_TOKEN")); value != "" {
		return value, "env:" + awsAccountEnvPrefix(alias, account) + "SESSION_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN")); value != "" {
		return value, "env:AWS_SESSION_TOKEN"
	}
	return "", ""
}

func resolveAWSLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_AWS_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.AWS.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "aws-iam.log")
}

func formatAWSContext(runtime awsRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	return fmt.Sprintf("account=%s region=%s base=%s", account, runtime.Region, runtime.BaseURL)
}

func previewAWSAccessKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}

func awsSignIAMRequest(httpReq *http.Request, body string, runtime awsRuntimeContext) error {
	return awsSignRequest(httpReq, body, runtime, "iam")
}

func awsSignRequest(httpReq *http.Request, body string, runtime awsRuntimeContext, service string) error {
	accessKey := strings.TrimSpace(runtime.AccessKeyID)
	secretKey := strings.TrimSpace(runtime.SecretKey)
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("aws access key and secret key are required for signing")
	}
	service = strings.ToLower(strings.TrimSpace(service))
	if service == "" {
		service = "iam"
	}
	region := strings.TrimSpace(runtime.Region)
	if region == "" {
		region = "us-east-1"
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256.Sum256([]byte(body))
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	httpReq.Header.Set("X-Amz-Date", amzDate)
	httpReq.Header.Set("X-Amz-Content-Sha256", payloadHashHex)
	if token := strings.TrimSpace(runtime.SessionToken); token != "" {
		httpReq.Header.Set("X-Amz-Security-Token", token)
	}

	signedHeaderNames := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if strings.TrimSpace(runtime.SessionToken) != "" {
		signedHeaderNames = append(signedHeaderNames, "x-amz-security-token")
	}
	sort.Strings(signedHeaderNames)

	host := strings.TrimSpace(httpReq.URL.Host)
	if host == "" {
		return fmt.Errorf("request host is required for aws signing")
	}
	httpReq.Host = host
	canonicalHeaders := make([]string, 0, len(signedHeaderNames))
	for _, name := range signedHeaderNames {
		value := ""
		switch name {
		case "host":
			value = strings.ToLower(host)
		default:
			value = strings.TrimSpace(httpReq.Header.Get(name))
		}
		canonicalHeaders = append(canonicalHeaders, name+":"+awsCanonicalHeaderValue(value))
	}
	signedHeaders := strings.Join(signedHeaderNames, ";")
	canonicalURI := awsCanonicalURI(httpReq.URL)
	canonicalQuery := awsCanonicalQuery(httpReq.URL.RawQuery)
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(httpReq.Method)),
		canonicalURI,
		canonicalQuery,
		strings.Join(canonicalHeaders, "\n"),
		"",
		signedHeaders,
		payloadHashHex,
	}, "\n")
	canonicalRequestHash := sha256.Sum256([]byte(canonicalRequest))
	credentialScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hex.EncodeToString(canonicalRequestHash[:]),
	}, "\n")
	signingKey := awsSignatureKey(secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(awsHMACSHA256(signingKey, stringToSign))
	httpReq.Header.Set(
		"Authorization",
		"AWS4-HMAC-SHA256 Credential="+accessKey+"/"+credentialScope+
			", SignedHeaders="+signedHeaders+
			", Signature="+signature,
	)
	return nil
}

func awsCanonicalURI(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := strings.TrimSpace(u.EscapedPath())
	if path == "" {
		return "/"
	}
	return path
}

func awsCanonicalQuery(rawQuery string) string {
	rawQuery = strings.TrimSpace(rawQuery)
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		vals := values[key]
		if len(vals) == 0 {
			parts = append(parts, awsPercentEncode(key)+"=")
			continue
		}
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, awsPercentEncode(key)+"="+awsPercentEncode(value))
		}
	}
	return strings.Join(parts, "&")
}

func awsPercentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func awsCanonicalHeaderValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func awsSignatureKey(secretKey string, date string, region string, service string) []byte {
	kDate := awsHMACSHA256([]byte("AWS4"+secretKey), date)
	kRegion := awsHMACSHA256(kDate, region)
	kService := awsHMACSHA256(kRegion, service)
	return awsHMACSHA256(kService, "aws4_request")
}

func awsHMACSHA256(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func awsDoQuery(ctx context.Context, runtime awsRuntimeContext, action string, params map[string]string) (awsResponse, error) {
	action = strings.TrimSpace(action)
	if action == "" {
		return awsResponse{}, fmt.Errorf("action is required")
	}
	form := url.Values{}
	form.Set("Action", action)
	form.Set("Version", "2010-05-08")
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		form.Set(key, strings.TrimSpace(value))
	}
	body := form.Encode()
	endpoint := runtime.BaseURL
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "https://iam.amazonaws.com"
	}
	providerID := providers.AWSIAM

	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[awsResponse]{
		Provider:    providerID,
		Subject:     runtime.AccountAlias,
		Method:      http.MethodPost,
		RequestPath: "/",
		Endpoint:    endpoint,
		MaxRetries:  2,
		Client:      httpx.SharedClient(45 * time.Second),
		BuildRequest: func(callCtx context.Context, callMethod string, callEndpoint string) (*http.Request, error) {
			httpReq, reqErr := http.NewRequestWithContext(callCtx, callMethod, callEndpoint, strings.NewReader(body))
			if reqErr != nil {
				return nil, reqErr
			}
			spec := providers.Resolve(providerID)
			accept := strings.TrimSpace(spec.Accept)
			if accept == "" {
				accept = "application/xml"
			}
			httpReq.Header.Set("Accept", accept)
			userAgent := strings.TrimSpace(spec.UserAgent)
			if userAgent == "" {
				userAgent = "si-aws-iam/1.0"
			}
			httpReq.Header.Set("User-Agent", userAgent)
			httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
			if signErr := awsSignIAMRequest(httpReq, body, runtime); signErr != nil {
				return nil, signErr
			}
			return httpReq, nil
		},
		NormalizeResponse: normalizeAWSResponse,
		StatusCode: func(resp awsResponse) int {
			return resp.StatusCode
		},
		NormalizeHTTPError: normalizeAWSHTTPError,
		IsRetryableNetwork: func(method string, _ error) bool {
			return netpolicy.IsSafeMethod(method)
		},
		IsRetryableHTTP: func(method string, statusCode int, _ http.Header, _ string) bool {
			if !netpolicy.IsSafeMethod(method) {
				return false
			}
			switch statusCode {
			case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				return true
			}
			return statusCode >= 500
		},
		OnCacheHit: func(resp awsResponse) {
			awsLogEvent(runtime.LogPath, map[string]any{
				"event":       "cache_hit",
				"account":     runtime.AccountAlias,
				"region":      runtime.Region,
				"action":      action,
				"status_code": resp.StatusCode,
			})
		},
		OnResponse: func(_ int, resp awsResponse, _ http.Header, duration time.Duration) {
			awsLogEvent(runtime.LogPath, map[string]any{
				"event":       "response",
				"account":     runtime.AccountAlias,
				"region":      runtime.Region,
				"action":      action,
				"status_code": resp.StatusCode,
				"request_id":  resp.RequestID,
				"duration_ms": duration.Milliseconds(),
			})
		},
		OnError: func(_ int, callErr error, duration time.Duration) {
			awsLogEvent(runtime.LogPath, map[string]any{
				"event":       "error",
				"account":     runtime.AccountAlias,
				"region":      runtime.Region,
				"action":      action,
				"duration_ms": duration.Milliseconds(),
				"error":       redactAWSSensitive(callErr.Error()),
			})
		},
	})
}

func normalizeAWSResponse(httpResp *http.Response, body string) awsResponse {
	resp := awsResponse{
		StatusCode: httpResp.StatusCode,
		Status:     strings.TrimSpace(httpResp.Status),
		RequestID:  firstAWSHeader(httpResp.Header, "x-amzn-RequestId", "x-amz-request-id"),
		Headers:    map[string]string{},
		Body:       strings.TrimSpace(body),
		Data:       map[string]any{},
	}
	for key, values := range httpResp.Header {
		if len(values) == 0 {
			continue
		}
		resp.Headers[key] = strings.Join(values, ",")
	}
	type xmlEnvelope struct {
		XMLName   xml.Name
		RequestID string `xml:"ResponseMetadata>RequestId"`
	}
	parsed := xmlEnvelope{}
	if err := xml.Unmarshal([]byte(resp.Body), &parsed); err == nil {
		if strings.TrimSpace(parsed.RequestID) != "" {
			resp.RequestID = strings.TrimSpace(parsed.RequestID)
		}
		if strings.TrimSpace(parsed.XMLName.Local) != "" {
			resp.Data["response"] = parsed.XMLName.Local
		}
		if strings.TrimSpace(parsed.RequestID) != "" {
			resp.Data["request_id"] = strings.TrimSpace(parsed.RequestID)
		}
	}
	if len(resp.Data) == 0 && strings.TrimSpace(resp.Body) != "" {
		var jsonPayload any
		if err := json.Unmarshal([]byte(resp.Body), &jsonPayload); err == nil {
			switch typed := jsonPayload.(type) {
			case map[string]any:
				resp.Data = typed
			case []any:
				resp.Data = map[string]any{"items": typed}
			}
		}
	}
	if len(resp.Data) == 0 {
		resp.Data = nil
	}
	return resp
}

func normalizeAWSHTTPError(statusCode int, headers http.Header, body string) error {
	details := &awsAPIErrorDetails{
		StatusCode: statusCode,
		RequestID:  firstAWSHeader(headers, "x-amzn-RequestId", "x-amz-request-id"),
		RawBody:    strings.TrimSpace(body),
		Message:    "aws iam request failed",
	}
	type xmlErr struct {
		Code      string `xml:"Error>Code"`
		Message   string `xml:"Error>Message"`
		RequestID string `xml:"RequestId"`
	}
	parsed := xmlErr{}
	if err := xml.Unmarshal([]byte(strings.TrimSpace(body)), &parsed); err == nil {
		if strings.TrimSpace(parsed.Code) != "" {
			details.Code = strings.TrimSpace(parsed.Code)
		}
		if strings.TrimSpace(parsed.Message) != "" {
			details.Message = strings.TrimSpace(parsed.Message)
		}
		if strings.TrimSpace(parsed.RequestID) != "" {
			details.RequestID = strings.TrimSpace(parsed.RequestID)
		}
	}
	if details.Code == "" || details.Message == "aws iam request failed" {
		var jsonErr map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &jsonErr); err == nil {
			if value := strings.TrimSpace(stringifyWorkOSAny(jsonErr["__type"])); value != "" {
				details.Code = value
			}
			if value := strings.TrimSpace(stringifyWorkOSAny(jsonErr["code"])); value != "" && details.Code == "" {
				details.Code = value
			}
			if value := strings.TrimSpace(stringifyWorkOSAny(jsonErr["message"])); value != "" {
				details.Message = value
			}
			if value := strings.TrimSpace(stringifyWorkOSAny(jsonErr["Message"])); value != "" && strings.TrimSpace(details.Message) == "aws iam request failed" {
				details.Message = value
			}
		}
	}
	if strings.TrimSpace(details.Message) == "aws iam request failed" {
		details.Message = strings.TrimSpace(body)
		if details.Message == "" {
			details.Message = http.StatusText(statusCode)
		}
	}
	return details
}

func firstAWSHeader(headers http.Header, keys ...string) string {
	if headers == nil {
		return ""
	}
	for _, key := range keys {
		value := strings.TrimSpace(headers.Get(strings.TrimSpace(key)))
		if value != "" {
			return value
		}
	}
	return ""
}

func parseAWSParams(values []string) map[string]string {
	out := map[string]string{}
	for _, entry := range values {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(parts[1])
	}
	return out
}

func printAWSResponse(resp awsResponse, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	if raw {
		fmt.Println(resp.Body)
		return
	}
	fmt.Printf("%s %d %s\n", styleHeading("Status:"), resp.StatusCode, orDash(resp.Status))
	if resp.RequestID != "" {
		fmt.Printf("%s %s\n", styleHeading("Request ID:"), resp.RequestID)
	}
	if len(resp.Data) > 0 {
		pretty, err := json.MarshalIndent(resp.Data, "", "  ")
		if err == nil {
			fmt.Println(string(pretty))
			return
		}
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(resp.Body)
	}
}

func printAWSError(err error) {
	if err == nil {
		return
	}
	var details *awsAPIErrorDetails
	if errors.As(err, &details) {
		fmt.Printf("%s %s\n", styleHeading("AWS error:"), styleError(details.Error()))
		if details.RequestID != "" {
			fmt.Printf("%s %s\n", styleHeading("Request ID:"), details.RequestID)
		}
		if details.RawBody != "" {
			fmt.Printf("%s %s\n", styleHeading("Body:"), truncateString(details.RawBody, 600))
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("AWS error:"), styleError(err.Error()))
}

func awsLogEvent(path string, event map[string]any) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if event == nil {
		event = map[string]any{}
	}
	if _, ok := event["ts"]; !ok {
		event["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for key, value := range event {
		if asString, ok := value.(string); ok {
			event[key] = redactAWSSensitive(asString)
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(raw)
	_ = file.Close()
}

func redactAWSSensitive(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacements := []struct {
		needle string
		repl   string
	}{
		{"AWS4-HMAC-SHA256 Credential=", "AWS4-HMAC-SHA256 Credential=***"},
		{"access_key_id=", "access_key_id=***"},
		{"secret_access_key=", "secret_access_key=***"},
		{"session_token=", "session_token=***"},
	}
	masked := value
	for _, item := range replacements {
		if strings.Contains(strings.ToLower(masked), strings.ToLower(item.needle)) {
			masked = maskAfterToken(masked, item.needle, item.repl)
		}
	}
	return masked
}

type awsCommonFlags struct {
	account      *string
	region       *string
	baseURL      *string
	accessKey    *string
	secretKey    *string
	sessionToken *string
}

func bindAWSCommonFlags(fs *flag.FlagSet) awsCommonFlags {
	return awsCommonFlags{
		account:      fs.String("account", "", "account alias"),
		region:       fs.String("region", "", "aws region"),
		baseURL:      fs.String("base-url", "", "api base url"),
		accessKey:    fs.String("access-key", "", "override aws access key id"),
		secretKey:    fs.String("secret-key", "", "override aws secret access key"),
		sessionToken: fs.String("session-token", "", "override aws session token"),
	}
}

func ensureURLWithSlash(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.TrimSpace(u.Path) == "" {
		u.Path = "/"
	}
	return strings.TrimSpace(u.String())
}

func awsDoSignedRequest(ctx context.Context, runtime awsRuntimeContext, method string, endpoint string, body string, contentType string, service string, headers map[string]string) (awsResponse, error) {
	providerID := providers.AWSIAM
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = runtime.BaseURL
	}
	endpoint = ensureURLWithSlash(endpoint)
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}

	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[awsResponse]{
		Provider:    providerID,
		Subject:     runtime.AccountAlias,
		Method:      method,
		RequestPath: "/",
		Endpoint:    endpoint,
		MaxRetries:  2,
		Client:      httpx.SharedClient(45 * time.Second),
		BuildRequest: func(callCtx context.Context, callMethod string, callEndpoint string) (*http.Request, error) {
			httpReq, reqErr := http.NewRequestWithContext(callCtx, callMethod, callEndpoint, bytes.NewBufferString(body))
			if reqErr != nil {
				return nil, reqErr
			}
			spec := providers.Resolve(providerID)
			httpReq.Header.Set("Accept", firstNonEmpty(spec.Accept, "application/json"))
			httpReq.Header.Set("User-Agent", firstNonEmpty(spec.UserAgent, "si-aws-iam/1.0"))
			if body != "" {
				httpReq.Header.Set("Content-Type", strings.TrimSpace(contentType))
			}
			for key, value := range headers {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				httpReq.Header.Set(key, strings.TrimSpace(value))
			}
			if signErr := awsSignRequest(httpReq, body, runtime, service); signErr != nil {
				return nil, signErr
			}
			return httpReq, nil
		},
		NormalizeResponse:  normalizeAWSResponse,
		StatusCode:         func(resp awsResponse) int { return resp.StatusCode },
		NormalizeHTTPError: normalizeAWSHTTPError,
		IsRetryableNetwork: func(method string, _ error) bool { return netpolicy.IsSafeMethod(method) },
		IsRetryableHTTP: func(method string, statusCode int, _ http.Header, _ string) bool {
			if !netpolicy.IsSafeMethod(method) {
				return false
			}
			return statusCode == http.StatusTooManyRequests || statusCode >= 500
		},
	})
}

func awsURLWithParams(base string, path string, params map[string]string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("base url is required")
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		u, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		if !u.IsAbs() {
			return "", fmt.Errorf("base url must be absolute: %q", base)
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		ref := &url.URL{Path: path}
		u = u.ResolveReference(ref)
		q := u.Query()
		for key, value := range params {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			q.Set(key, strings.TrimSpace(value))
		}
		u.RawQuery = q.Encode()
		return strings.TrimSpace(u.String()), nil
	}
	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		q.Set(key, strings.TrimSpace(value))
	}
	u.RawQuery = q.Encode()
	return strings.TrimSpace(u.String()), nil
}

func cmdAWSRawSigned(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("aws raw signed", flag.ExitOnError)
	flags := bindAWSCommonFlags(fs)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "/", "api path")
	body := fs.String("body", "", "raw request body")
	contentType := fs.String("content-type", "application/json", "request content type")
	service := fs.String("service", "iam", "sigv4 service name (for example iam|sts|ec2|s3)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si aws raw --method <GET|POST> --path </> [--param key=value] [--body raw] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromAWSFlags(flags)
	if err != nil {
		fatal(err)
	}
	endpoint, err := awsURLWithParams(runtime.BaseURL, strings.TrimSpace(*path), parseAWSParams(params))
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := awsDoSignedRequest(ctx, runtime, strings.ToUpper(strings.TrimSpace(*method)), endpoint, strings.TrimSpace(*body), strings.TrimSpace(*contentType), strings.TrimSpace(*service), nil)
	if err != nil {
		printAWSError(err)
		return
	}
	printAWSResponse(resp, *jsonOut, *raw)
}

func cmdAWSRawCompat(args []string) {
	if len(args) > 0 {
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		if sub == "signed" {
			cmdAWSRawSigned(args[1:])
			return
		}
	}
	cmdAWSIAMQuery(args)
}
