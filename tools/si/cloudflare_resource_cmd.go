package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"si/tools/si/internal/cloudflarebridge"
)

type cloudflareScope string

const (
	cloudflareScopeGlobal  cloudflareScope = "global"
	cloudflareScopeAccount cloudflareScope = "account"
	cloudflareScopeZone    cloudflareScope = "zone"
)

type cloudflareResourceSpec struct {
	Name         string
	Scope        cloudflareScope
	ListPath     string
	ResourcePath string
	CreateMethod string
	UpdateMethod string
	DeleteMethod string
}

func (s cloudflareResourceSpec) createMethod() string {
	method := strings.ToUpper(strings.TrimSpace(s.CreateMethod))
	if method == "" {
		return http.MethodPost
	}
	return method
}

func (s cloudflareResourceSpec) updateMethod() string {
	method := strings.ToUpper(strings.TrimSpace(s.UpdateMethod))
	if method == "" {
		return http.MethodPatch
	}
	return method
}

func (s cloudflareResourceSpec) deleteMethod() string {
	method := strings.ToUpper(strings.TrimSpace(s.DeleteMethod))
	if method == "" {
		return http.MethodDelete
	}
	return method
}

func cmdCloudflareZone(args []string) {
	spec := cloudflareResourceSpec{Name: "zone", Scope: cloudflareScopeGlobal, ListPath: "/zones", ResourcePath: "/zones/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare zone <list|get|create|update|delete> ...")
}

func cmdCloudflareDNS(args []string) {
	spec := cloudflareResourceSpec{Name: "dns", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/dns_records", ResourcePath: "/zones/{zone_id}/dns_records/{id}"}
	if len(args) > 0 {
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		if sub == "import" || sub == "export" {
			cmdCloudflareDNSIO(sub, args[1:])
			return
		}
	}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare dns <list|get|create|update|delete|import|export> ...")
}

func cmdCloudflareEmail(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare email <rule|address|settings> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "rule", "rules", "route", "routes":
		spec := cloudflareResourceSpec{
			Name:         "email rule",
			Scope:        cloudflareScopeZone,
			ListPath:     "/zones/{zone_id}/email/routing/rules",
			ResourcePath: "/zones/{zone_id}/email/routing/rules/{id}",
		}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare email rule <list|get|create|update|delete> ...")
	case "address", "addresses", "destination", "destinations":
		spec := cloudflareResourceSpec{
			Name:         "email address",
			Scope:        cloudflareScopeAccount,
			ListPath:     "/accounts/{account_id}/email/routing/addresses",
			ResourcePath: "/accounts/{account_id}/email/routing/addresses/{id}",
		}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare email address <list|get|create|update|delete> ...")
	case "setting", "settings":
		cmdCloudflareEmailSettings(rest)
	default:
		printUnknown("cloudflare email", sub)
		printUsage("usage: si cloudflare email <rule|address|settings> ...")
	}
}

func cmdCloudflareStatus(args []string) {
	cmdCloudflareAuthStatus(args)
}

func cmdCloudflareToken(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare token <list|get|create|update|delete|verify|permission-groups> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "verify":
		cmdCloudflareTokenVerify(rest)
		return
	case "permission-groups", "permissions", "permission-group":
		cmdCloudflareScopedRead(rest, "cloudflare token permission-groups", "/user/tokens/permission_groups")
		return
	}
	spec := cloudflareResourceSpec{Name: "token", Scope: cloudflareScopeGlobal, ListPath: "/user/tokens", ResourcePath: "/user/tokens/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare token <list|get|create|update|delete|verify|permission-groups> ...")
}

func cmdCloudflareWAF(args []string) {
	spec := cloudflareResourceSpec{Name: "waf", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/firewall/waf/packages", ResourcePath: "/zones/{zone_id}/firewall/waf/packages/{id}", CreateMethod: "", UpdateMethod: http.MethodPatch}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare waf <list|get|update> ...")
}

func cmdCloudflareRuleset(args []string) {
	spec := cloudflareResourceSpec{Name: "ruleset", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/rulesets", ResourcePath: "/zones/{zone_id}/rulesets/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare ruleset <list|get|create|update|delete> ...")
}

func cmdCloudflareFirewall(args []string) {
	spec := cloudflareResourceSpec{Name: "firewall", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/firewall/rules", ResourcePath: "/zones/{zone_id}/firewall/rules/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare firewall <list|get|create|update|delete> ...")
}

func cmdCloudflareRateLimit(args []string) {
	spec := cloudflareResourceSpec{Name: "ratelimit", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/rate_limits", ResourcePath: "/zones/{zone_id}/rate_limits/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare ratelimit <list|get|create|update|delete> ...")
}

func cmdCloudflareWorkers(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare workers <script|route|secret> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "script", "scripts":
		spec := cloudflareResourceSpec{Name: "workers script", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/workers/scripts", ResourcePath: "/accounts/{account_id}/workers/scripts/{id}", UpdateMethod: http.MethodPut}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare workers script <list|get|create|update|delete> ...")
	case "route", "routes":
		spec := cloudflareResourceSpec{Name: "workers route", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/workers/routes", ResourcePath: "/zones/{zone_id}/workers/routes/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare workers route <list|get|create|update|delete> ...")
	case "secret", "secrets":
		cmdCloudflareWorkersSecret(rest)
	default:
		printUnknown("cloudflare workers", sub)
		printUsage("usage: si cloudflare workers <script|route|secret> ...")
	}
}

func cmdCloudflarePages(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare pages <project|deploy|domain> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "project", "projects":
		spec := cloudflareResourceSpec{Name: "pages project", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/pages/projects", ResourcePath: "/accounts/{account_id}/pages/projects/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare pages project <list|get|create|update|delete> ...")
	case "deploy", "deployment", "deployments":
		cmdCloudflarePagesDeploy(rest)
	case "domain", "domains":
		cmdCloudflarePagesDomain(rest)
	default:
		printUnknown("cloudflare pages", sub)
		printUsage("usage: si cloudflare pages <project|deploy|domain> ...")
	}
}

func cmdCloudflareR2(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare r2 <bucket|object> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "bucket", "buckets":
		spec := cloudflareResourceSpec{Name: "r2 bucket", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/r2/buckets", ResourcePath: "/accounts/{account_id}/r2/buckets/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare r2 bucket <list|get|create|update|delete> ...")
	case "object", "objects":
		cmdCloudflareR2Object(rest)
	default:
		printUnknown("cloudflare r2", sub)
		printUsage("usage: si cloudflare r2 <bucket|object> ...")
	}
}

func cmdCloudflareD1(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare d1 <db|query|migration> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "db", "database", "databases":
		spec := cloudflareResourceSpec{Name: "d1 db", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/d1/database", ResourcePath: "/accounts/{account_id}/d1/database/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare d1 db <list|get|create|update|delete> ...")
	case "query":
		cmdCloudflareD1Query(rest)
	case "migration", "migrations":
		cmdCloudflareD1Migration(rest)
	default:
		printUnknown("cloudflare d1", sub)
		printUsage("usage: si cloudflare d1 <db|query|migration> ...")
	}
}

func cmdCloudflareKV(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare kv <namespace|key> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "namespace", "namespaces":
		spec := cloudflareResourceSpec{Name: "kv namespace", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/storage/kv/namespaces", ResourcePath: "/accounts/{account_id}/storage/kv/namespaces/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare kv namespace <list|get|create|update|delete> ...")
	case "key", "keys":
		cmdCloudflareKVKey(rest)
	default:
		printUnknown("cloudflare kv", sub)
		printUsage("usage: si cloudflare kv <namespace|key> ...")
	}
}

func cmdCloudflareQueue(args []string) {
	spec := cloudflareResourceSpec{Name: "queue", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/queues", ResourcePath: "/accounts/{account_id}/queues/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare queue <list|get|create|update|delete> ...")
}

func cmdCloudflareAccess(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare access <app|policy> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "app", "apps":
		spec := cloudflareResourceSpec{Name: "access app", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/access/apps", ResourcePath: "/accounts/{account_id}/access/apps/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare access app <list|get|create|update|delete> ...")
	case "policy", "policies":
		spec := cloudflareResourceSpec{Name: "access policy", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/access/policies", ResourcePath: "/accounts/{account_id}/access/policies/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare access policy <list|get|create|update|delete> ...")
	default:
		printUnknown("cloudflare access", sub)
		printUsage("usage: si cloudflare access <app|policy> ...")
	}
}

func cmdCloudflareTunnel(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare tunnel <list|get|create|update|delete|token> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	if sub == "token" || sub == "issue" {
		cmdCloudflareTunnelToken(args[1:])
		return
	}
	spec := cloudflareResourceSpec{Name: "tunnel", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/cfd_tunnel", ResourcePath: "/accounts/{account_id}/cfd_tunnel/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare tunnel <list|get|create|update|delete|token> ...")
}

func cmdCloudflareLB(args []string) {
	if len(args) > 0 {
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		if sub == "pool" || sub == "pools" {
			spec := cloudflareResourceSpec{Name: "lb pool", Scope: cloudflareScopeAccount, ListPath: "/accounts/{account_id}/load_balancers/pools", ResourcePath: "/accounts/{account_id}/load_balancers/pools/{id}"}
			cmdCloudflareResourceFamily(args[1:], spec, "usage: si cloudflare lb pool <list|get|create|update|delete> ...")
			return
		}
	}
	spec := cloudflareResourceSpec{Name: "load balancer", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/load_balancers", ResourcePath: "/zones/{zone_id}/load_balancers/{id}"}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare lb <list|get|create|update|delete|pool> ...")
}

func cmdCloudflareTLS(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare tls <get|set|cert|origin-cert> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdCloudflareTLSGetSet("get", rest)
	case "set":
		cmdCloudflareTLSGetSet("set", rest)
	case "cert", "certs", "certificate", "certificates":
		spec := cloudflareResourceSpec{Name: "certificate", Scope: cloudflareScopeZone, ListPath: "/zones/{zone_id}/custom_certificates", ResourcePath: "/zones/{zone_id}/custom_certificates/{id}"}
		cmdCloudflareResourceFamily(rest, spec, "usage: si cloudflare tls cert <list|get|create|update|delete> ...")
	case "origin-cert", "origin", "originca":
		cmdCloudflareOriginCert(rest)
	default:
		printUnknown("cloudflare tls", sub)
		printUsage("usage: si cloudflare tls <get|set|cert|origin-cert> ...")
	}
}

func cmdCloudflareCache(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare cache <purge|settings> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "purge":
		cmdCloudflareCachePurge(rest)
	case "settings":
		cmdCloudflareCacheSettings(rest)
	default:
		printUnknown("cloudflare cache", sub)
		printUsage("usage: si cloudflare cache <purge|settings> ...")
	}
}

func cmdCloudflareResourceFamily(args []string, spec cloudflareResourceSpec, usage string) {
	if len(args) == 0 {
		printUsage(usage)
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	switch op {
	case "list":
		cmdCloudflareResourceList(spec, args[1:], usage)
	case "get":
		cmdCloudflareResourceGet(spec, args[1:], usage)
	case "create":
		cmdCloudflareResourceCreate(spec, args[1:], usage)
	case "update":
		cmdCloudflareResourceUpdate(spec, args[1:], usage)
	case "delete", "remove", "rm":
		cmdCloudflareResourceDelete(spec, args[1:], usage)
	default:
		printUnknown("cloudflare "+spec.Name, op)
		printUsage(usage)
	}
}

func cmdCloudflareResourceList(spec cloudflareResourceSpec, args []string, usage string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet(spec.Name+" list", args)
	maxPages := fs.Int("max-pages", 10, "max pages to fetch")
	limit := fs.Int("limit", 100, "max items to return (-1 for all)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(usage)
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath(spec.ListPath, runtime, "")
	if err != nil {
		fatal(err)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := client.ListAll(ctx, cloudflarebridge.Request{Method: http.MethodGet, Path: path, Params: parseCloudflareParams(params)}, *maxPages)
	if err != nil {
		printCloudflareError(err)
		return
	}
	if *limit >= 0 && len(items) > *limit {
		items = items[:*limit]
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"object": spec.Name, "count": len(items), "data": items}); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		rawBody, _ := json.Marshal(items)
		fmt.Println(string(rawBody))
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Object list:"), spec.Name, len(items))
	for _, item := range items {
		fmt.Printf("  %s\n", summarizeCloudflareItem(item))
	}
}

func cmdCloudflareResourceGet(spec cloudflareResourceSpec, args []string, usage string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet(spec.Name+" get", args)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage(usage)
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath(spec.ResourcePath, runtime, id)
	if err != nil {
		fatal(err)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: http.MethodGet, Path: path, Params: parseCloudflareParams(params)})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareResourceCreate(spec cloudflareResourceSpec, args []string, usage string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet(spec.Name+" create", args)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	body := fs.String("body", "", "raw request body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(usage)
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath(spec.ListPath, runtime, "")
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Method: spec.createMethod(), Path: path}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		request.JSONBody = parseCloudflareBodyParams(params)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareResourceUpdate(spec cloudflareResourceSpec, args []string, usage string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet(spec.Name+" update", args)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	body := fs.String("body", "", "raw request body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage(usage)
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath(spec.ResourcePath, runtime, id)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Method: spec.updateMethod(), Path: path}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		request.JSONBody = parseCloudflareBodyParams(params)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareResourceDelete(spec cloudflareResourceSpec, args []string, usage string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet(spec.Name+" delete", args)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	params := multiFlag{}
	fs.Var(&params, "param", "request parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage(usage)
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	if err := requireCloudflareConfirmation("delete "+spec.Name+" "+id, *force); err != nil {
		fatal(err)
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath(spec.ResourcePath, runtime, id)
	if err != nil {
		fatal(err)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: spec.deleteMethod(), Path: path, Params: parseCloudflareParams(params)})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

var (
	commonAccount   *string
	commonEnv       *string
	commonZone      *string
	commonZoneID    *string
	commonToken     *string
	commonBaseURL   *string
	commonAccountID *string
)

func cloudflareCommonFlagSet(name string, args []string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	commonAccount = fs.String("account", "", "account alias")
	commonEnv = fs.String("env", "", "environment (prod|staging|dev)")
	commonZone = fs.String("zone", "", "zone name")
	commonZoneID = fs.String("zone-id", "", "zone id")
	commonToken = fs.String("api-token", "", "override cloudflare api token")
	commonBaseURL = fs.String("base-url", "", "cloudflare api base url")
	commonAccountID = fs.String("account-id", "", "cloudflare account id")
	return fs
}

func cloudflareResolvePath(template string, runtime cloudflareRuntimeContext, id string) (string, error) {
	path := strings.TrimSpace(template)
	if path == "" {
		return "", fmt.Errorf("path template is required")
	}
	replace := func(needle string, value string, hint string) error {
		if strings.Contains(path, needle) {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s is required for this command (%s)", hint, needle)
			}
			path = strings.ReplaceAll(path, needle, strings.TrimSpace(value))
		}
		return nil
	}
	if err := replace("{account_id}", runtime.AccountID, "account id (use --account-id or configure context)"); err != nil {
		return "", err
	}
	if err := replace("{zone_id}", runtime.ZoneID, "zone id (use --zone-id or configure context)"); err != nil {
		return "", err
	}
	if err := replace("{zone_name}", runtime.ZoneName, "zone name (use --zone or configure context)"); err != nil {
		return "", err
	}
	if err := replace("{id}", id, "resource id"); err != nil {
		return "", err
	}
	if strings.Contains(path, "{") {
		return "", fmt.Errorf("unresolved path template %q", path)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path, nil
}

func cmdCloudflareDNSIO(mode string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare dns "+mode, args)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	body := fs.String("body", "", "request body for import")
	params := multiFlag{}
	fs.Var(&params, "param", "query/body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare dns import|export [--zone-id <zone>] [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path := "/zones/{zone_id}/dns_records/export"
	method := http.MethodGet
	request := cloudflarebridge.Request{Method: method}
	if mode == "import" {
		if err := requireCloudflareConfirmation("import dns records into zone "+runtime.ZoneID, *force); err != nil {
			fatal(err)
		}
		path = "/zones/{zone_id}/dns_records/import"
		method = http.MethodPost
		request.Method = method
		if strings.TrimSpace(*body) != "" {
			request.RawBody = strings.TrimSpace(*body)
		}
	}
	resolvedPath, err := cloudflareResolvePath(path, runtime, "")
	if err != nil {
		fatal(err)
	}
	request.Path = resolvedPath
	request.Params = parseCloudflareParams(params)
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareWorkersSecret(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare workers secret <set|delete> --script <name> --name <secret> [--text <value>] [--force]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare workers secret "+op, args)
	script := fs.String("script", "", "workers script name")
	name := fs.String("name", "", "secret name")
	text := fs.String("text", "", "secret value")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*script) == "" || strings.TrimSpace(*name) == "" {
		printUsage("usage: si cloudflare workers secret <set|delete> --script <name> --name <secret> [--text <value>] [--force]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	basePath, err := cloudflareResolvePath("/accounts/{account_id}/workers/scripts/{id}/secrets", runtime, *script)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Path: basePath}
	switch op {
	case "set", "create", "update":
		if strings.TrimSpace(*text) == "" {
			fatal(fmt.Errorf("--text is required for secret set"))
		}
		request.Method = http.MethodPut
		request.JSONBody = map[string]any{"name": strings.TrimSpace(*name), "text": *text, "type": "secret_text"}
	case "delete", "remove", "rm":
		if err := requireCloudflareConfirmation("delete workers secret "+strings.TrimSpace(*name), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodDelete
		request.Path = basePath + "/" + strings.TrimSpace(*name)
	default:
		printUnknown("cloudflare workers secret", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflarePagesDeploy(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare pages deploy <list|trigger|rollback> --project <name> [--deployment <id>]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare pages deploy "+op, args)
	project := fs.String("project", "", "pages project name")
	deployment := fs.String("deployment", "", "deployment id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	body := fs.String("body", "", "raw request body for trigger")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*project) == "" {
		printUsage("usage: si cloudflare pages deploy <list|trigger|rollback> --project <name> [--deployment <id>]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	basePath, err := cloudflareResolvePath("/accounts/{account_id}/pages/projects/{id}/deployments", runtime, *project)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Path: basePath, Method: http.MethodGet}
	switch op {
	case "list":
		request.Method = http.MethodGet
	case "trigger", "create":
		request.Method = http.MethodPost
		if strings.TrimSpace(*body) != "" {
			request.RawBody = strings.TrimSpace(*body)
		} else {
			request.JSONBody = map[string]any{}
		}
	case "rollback":
		if strings.TrimSpace(*deployment) == "" {
			fatal(fmt.Errorf("--deployment is required for rollback"))
		}
		if err := requireCloudflareConfirmation("rollback pages deployment "+strings.TrimSpace(*deployment), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodPost
		request.Path = basePath + "/" + strings.TrimSpace(*deployment) + "/rollback"
	default:
		printUnknown("cloudflare pages deploy", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflarePagesDomain(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare pages domain <list|get|create|delete> --project <name> [--domain <fqdn>]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare pages domain "+op, args)
	project := fs.String("project", "", "pages project name")
	domain := fs.String("domain", "", "custom domain fqdn")
	body := fs.String("body", "", "raw request body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query/body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*project) == "" {
		printUsage("usage: si cloudflare pages domain <list|get|create|delete> --project <name> [--domain <fqdn>] [--param key=value] [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	base, err := cloudflareResolvePath("/accounts/{account_id}/pages/projects/{id}/domains", runtime, *project)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{}
	switch op {
	case "list":
		request.Method = http.MethodGet
		request.Path = base
		request.Params = parseCloudflareParams(params)
	case "get":
		if strings.TrimSpace(*domain) == "" {
			fatal(fmt.Errorf("--domain is required for get"))
		}
		request.Method = http.MethodGet
		request.Path = base + "/" + strings.TrimSpace(*domain)
		request.Params = parseCloudflareParams(params)
	case "create":
		request.Method = http.MethodPost
		request.Path = base
		if strings.TrimSpace(*body) != "" {
			request.RawBody = strings.TrimSpace(*body)
		} else {
			payload := parseCloudflareBodyParams(params)
			if strings.TrimSpace(*domain) != "" {
				payload["name"] = strings.TrimSpace(*domain)
			}
			nameValue, _ := payload["name"].(string)
			if strings.TrimSpace(nameValue) == "" {
				fatal(fmt.Errorf("--domain or --param name=<fqdn> is required for create"))
			}
			request.JSONBody = payload
		}
	case "delete", "remove", "rm":
		if strings.TrimSpace(*domain) == "" {
			fatal(fmt.Errorf("--domain is required for delete"))
		}
		if err := requireCloudflareConfirmation("delete pages domain "+strings.TrimSpace(*domain), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodDelete
		request.Path = base + "/" + strings.TrimSpace(*domain)
	default:
		printUnknown("cloudflare pages domain", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareR2Object(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare r2 object <list|get|put|delete> --bucket <name> [--key <key>] [--body <text>]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare r2 object "+op, args)
	bucket := fs.String("bucket", "", "r2 bucket name")
	key := fs.String("key", "", "object key")
	body := fs.String("body", "", "object body for put")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*bucket) == "" {
		printUsage("usage: si cloudflare r2 object <list|get|put|delete> --bucket <name> [--key <key>] [--body <text>]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	base, err := cloudflareResolvePath("/accounts/{account_id}/r2/buckets/{id}", runtime, *bucket)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Params: parseCloudflareParams(params)}
	switch op {
	case "list":
		request.Method = http.MethodGet
		request.Path = base + "/objects"
	case "get":
		if strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("--key is required for get"))
		}
		request.Method = http.MethodGet
		request.Path = base + "/objects/" + strings.TrimSpace(*key)
	case "put", "create":
		if strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("--key is required for put"))
		}
		request.Method = http.MethodPut
		request.Path = base + "/objects/" + strings.TrimSpace(*key)
		request.RawBody = strings.TrimSpace(*body)
	case "delete", "remove", "rm":
		if strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("--key is required for delete"))
		}
		if err := requireCloudflareConfirmation("delete r2 object "+strings.TrimSpace(*key), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodDelete
		request.Path = base + "/objects/" + strings.TrimSpace(*key)
	default:
		printUnknown("cloudflare r2 object", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareD1Query(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet("cloudflare d1 query", args)
	db := fs.String("db", "", "d1 database id")
	sql := fs.String("sql", "", "sql statement")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*db) == "" || strings.TrimSpace(*sql) == "" {
		printUsage("usage: si cloudflare d1 query --db <id> --sql <statement> [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath("/accounts/{account_id}/d1/database/{id}/query", runtime, *db)
	if err != nil {
		fatal(err)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: http.MethodPost, Path: path, JSONBody: map[string]any{"sql": strings.TrimSpace(*sql)}})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareD1Migration(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare d1 migration <list|apply> --db <id> [--body <json>] [--force]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare d1 migration "+op, args)
	db := fs.String("db", "", "d1 database id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	body := fs.String("body", "", "migration payload json")
	force := fs.Bool("force", false, "skip confirmation prompt")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*db) == "" {
		printUsage("usage: si cloudflare d1 migration <list|apply> --db <id> [--body <json>] [--force]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath("/accounts/{account_id}/d1/database/{id}/migrations", runtime, *db)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Path: path}
	switch op {
	case "list":
		request.Method = http.MethodGet
	case "apply":
		if err := requireCloudflareConfirmation("apply d1 migrations on "+strings.TrimSpace(*db), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodPost
		if strings.TrimSpace(*body) != "" {
			request.RawBody = strings.TrimSpace(*body)
		} else {
			request.JSONBody = map[string]any{}
		}
	default:
		printUnknown("cloudflare d1 migration", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareKVKey(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare kv key <list|get|put|delete|bulk> --namespace <id> [--key <key>] [--value <text>] [--body <json>]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare kv key "+op, args)
	namespace := fs.String("namespace", "", "kv namespace id")
	key := fs.String("key", "", "kv key")
	value := fs.String("value", "", "kv value")
	body := fs.String("body", "", "raw request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*namespace) == "" {
		printUsage("usage: si cloudflare kv key <list|get|put|delete|bulk> --namespace <id> [--key <key>] [--value <text>] [--body <json>]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	base, err := cloudflareResolvePath("/accounts/{account_id}/storage/kv/namespaces/{id}", runtime, *namespace)
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Params: parseCloudflareParams(params)}
	switch op {
	case "list":
		request.Method = http.MethodGet
		request.Path = base + "/keys"
	case "get":
		if strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("--key is required for get"))
		}
		request.Method = http.MethodGet
		request.Path = base + "/values/" + strings.TrimSpace(*key)
	case "put", "set", "create", "update":
		if strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("--key is required for put"))
		}
		request.Method = http.MethodPut
		request.Path = base + "/values/" + strings.TrimSpace(*key)
		if strings.TrimSpace(*body) != "" {
			request.RawBody = strings.TrimSpace(*body)
		} else {
			request.RawBody = strings.TrimSpace(*value)
		}
	case "bulk":
		request.Method = http.MethodPut
		request.Path = base + "/bulk"
		request.RawBody = strings.TrimSpace(*body)
		if request.RawBody == "" {
			fatal(fmt.Errorf("--body is required for bulk"))
		}
	case "delete", "remove", "rm":
		if strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("--key is required for delete"))
		}
		if err := requireCloudflareConfirmation("delete kv key "+strings.TrimSpace(*key), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodDelete
		request.Path = base + "/values/" + strings.TrimSpace(*key)
	default:
		printUnknown("cloudflare kv key", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareTunnelToken(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet("cloudflare tunnel token", args)
	tunnel := fs.String("tunnel", "", "tunnel id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*tunnel) == "" {
		printUsage("usage: si cloudflare tunnel token --tunnel <id> [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath("/accounts/{account_id}/cfd_tunnel/{id}/token", runtime, *tunnel)
	if err != nil {
		fatal(err)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: http.MethodGet, Path: path})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareTokenVerify(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet("cloudflare token verify", args)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare token verify [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: http.MethodGet, Path: "/user/tokens/verify"})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareTLSGetSet(mode string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet("cloudflare tls "+mode, args)
	setting := fs.String("setting", "ssl", "zone setting name")
	value := fs.String("value", "", "value for set")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare tls get|set --setting <name> [--value <value>] [--zone-id <zone>] [--json]")
		return
	}
	if mode == "set" && strings.TrimSpace(*value) == "" {
		fatal(fmt.Errorf("--value is required for tls set"))
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath("/zones/{zone_id}/settings/"+strings.TrimSpace(*setting), runtime, "")
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Path: path, Method: http.MethodGet}
	if mode == "set" {
		request.Method = http.MethodPatch
		request.JSONBody = map[string]any{"value": strings.TrimSpace(*value)}
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareEmailSettings(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare email settings <get|enable|disable> [--zone-id <zone>] [--json]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare email settings "+op, args)
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query/body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare email settings <get|enable|disable> [--zone-id <zone>] [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	request := cloudflarebridge.Request{Params: parseCloudflareParams(params)}
	switch op {
	case "get":
		path, err := cloudflareResolvePath("/zones/{zone_id}/email/routing", runtime, "")
		if err != nil {
			fatal(err)
		}
		request.Method = http.MethodGet
		request.Path = path
	case "enable":
		if err := requireCloudflareConfirmation("enable email routing for zone "+runtime.ZoneID, *force); err != nil {
			fatal(err)
		}
		path, err := cloudflareResolvePath("/zones/{zone_id}/email/routing/enable", runtime, "")
		if err != nil {
			fatal(err)
		}
		request.Method = http.MethodPost
		request.Path = path
	case "disable":
		if err := requireCloudflareConfirmation("disable email routing for zone "+runtime.ZoneID, *force); err != nil {
			fatal(err)
		}
		path, err := cloudflareResolvePath("/zones/{zone_id}/email/routing/disable", runtime, "")
		if err != nil {
			fatal(err)
		}
		request.Method = http.MethodPost
		request.Path = path
	default:
		printUnknown("cloudflare email settings", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareOriginCert(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare tls origin-cert <list|create|revoke> ...")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare tls origin-cert "+op, args)
	id := fs.String("id", "", "origin cert id")
	body := fs.String("body", "", "raw request body for create")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query/body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare tls origin-cert <list|create|revoke> [--id <cert>] [--param key=value] [--body raw] [--force]")
		return
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	request := cloudflarebridge.Request{}
	switch op {
	case "list":
		request.Method = http.MethodGet
		request.Path = "/certificates"
		request.Params = parseCloudflareParams(params)
	case "create":
		request.Method = http.MethodPost
		request.Path = "/certificates"
		if strings.TrimSpace(*body) != "" {
			request.RawBody = strings.TrimSpace(*body)
		} else {
			request.JSONBody = parseCloudflareBodyParams(params)
		}
	case "revoke", "delete", "remove", "rm":
		if strings.TrimSpace(*id) == "" {
			fatal(fmt.Errorf("--id is required for revoke"))
		}
		if err := requireCloudflareConfirmation("revoke origin cert "+strings.TrimSpace(*id), *force); err != nil {
			fatal(err)
		}
		request.Method = http.MethodDelete
		request.Path = "/certificates/" + strings.TrimSpace(*id)
	default:
		printUnknown("cloudflare tls origin-cert", op)
		return
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareCachePurge(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := cloudflareCommonFlagSet("cloudflare cache purge", args)
	everything := fs.Bool("everything", false, "purge entire cache")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	tags := multiFlag{}
	hosts := multiFlag{}
	prefixes := multiFlag{}
	fs.Var(&tags, "tag", "cache tag (repeatable)")
	fs.Var(&hosts, "host", "host (repeatable)")
	fs.Var(&prefixes, "prefix", "prefix (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare cache purge [--everything|--tag t --host h --prefix p] [--force] [--json]")
		return
	}
	if !*everything && len(tags) == 0 && len(hosts) == 0 && len(prefixes) == 0 {
		fatal(fmt.Errorf("specify --everything or at least one --tag/--host/--prefix"))
	}
	if err := requireCloudflareConfirmation("purge cache", *force); err != nil {
		fatal(err)
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath("/zones/{zone_id}/purge_cache", runtime, "")
	if err != nil {
		fatal(err)
	}
	body := map[string]any{}
	if *everything {
		body["purge_everything"] = true
	}
	if len(tags) > 0 {
		body["tags"] = []string(tags)
	}
	if len(hosts) > 0 {
		body["hosts"] = []string(hosts)
	}
	if len(prefixes) > 0 {
		body["prefixes"] = []string(prefixes)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: http.MethodPost, Path: path, JSONBody: body})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareCacheSettings(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare cache settings <get|set> --setting <name> [--value <value>]")
		return
	}
	op := strings.ToLower(strings.TrimSpace(args[0]))
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true})
	fs := cloudflareCommonFlagSet("cloudflare cache settings "+op, args)
	setting := fs.String("setting", "", "zone setting name")
	value := fs.String("value", "", "setting value for set")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*setting) == "" {
		printUsage("usage: si cloudflare cache settings <get|set> --setting <name> [--value <value>]")
		return
	}
	if op == "set" && strings.TrimSpace(*value) == "" {
		fatal(fmt.Errorf("--value is required for set"))
	}
	runtime, client := mustCloudflareClient(*commonAccount, *commonEnv, *commonZone, *commonZoneID, *commonToken, *commonBaseURL, *commonAccountID)
	path, err := cloudflareResolvePath("/zones/{zone_id}/settings/"+strings.TrimSpace(*setting), runtime, "")
	if err != nil {
		fatal(err)
	}
	request := cloudflarebridge.Request{Method: http.MethodGet, Path: path}
	if op == "set" {
		request.Method = http.MethodPatch
		request.JSONBody = map[string]any{"value": strings.TrimSpace(*value)}
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}
