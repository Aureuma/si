package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

func usage() {
	fmt.Print(colorizeHelp(`si [command] [args]

Holistic CLI for si. This help includes all commands, flags, and core features.

Features:
  - Dyads: spawn paired actor/critic containers with a critic-driven closed loop, exec into them, manage logs.
  - Codex containers: spawn/respawn/list/status/report/login/logout/swap/ps/run/logs/tail/clone/remove/stop/start.
  - Vault: encrypted dotenv secrets; format, encrypt, and inject into processes/containers.
  - Stripe bridge: account context, CRUD, reporting, raw API access, and live-to-sandbox sync.
  - GitHub bridge: App/OAuth-auth context, REST/GraphQL access, and repository automation commands.
  - Cloudflare bridge: account/env context, common edge operations, reporting, and raw API access.
  - Google Places bridge: account/env context, autocomplete/search/details/photos, local reports, and raw API access.
  - Google Play bridge: service-account auth/context, custom app create, listing/details/assets automation, release track orchestration, and raw API access.
  - Apple App Store bridge: JWT auth/context, bundle/app onboarding, listing localization automation, and raw App Store Connect API access.
  - Google YouTube bridge: auth/context flows, channel/video/playlist/live/caption operations, uploads, and usage reporting.
  - Social bridge: unified platform flows for Facebook, Instagram, X, LinkedIn, and Reddit (auth/context/resources/raw/report).
  - WorkOS bridge: account context, organization/user/member/invitation/directory management, and raw API access.
  - Publish bridge: DistributionKit-backed launch catalog plus publishing workflows for Dev.to, Hashnode, Reddit, Hacker News, and Product Hunt.
  - AWS bridge: auth/context diagnostics plus IAM, STS, S3, EC2, Lambda, ECR, Secrets, KMS, DynamoDB, SSM, CloudWatch, Logs, Bedrock AI/LLM (runtime, batch, agents), and signed raw API access.
  - GCP bridge: Service Usage API flows (enable/disable/list/get services) and raw access.
  - OpenAI bridge: auth/context diagnostics, model/project/admin-key/service-account controls, usage/cost monitoring (including Codex usage views), and raw API access.
  - OCI bridge: signed identity/network/compute orchestration helpers plus raw API access.
  - Self-management: build or upgrade the si binary from the current checkout.
  - Codex one-off run: run codex in an isolated container (with MCP disabled if desired).
  - Static analysis: run go vet + golangci-lint across go.work modules.
  - Image build for local dev.
  - Docker passthrough for raw docker CLI calls.
  - Containers ship /usr/local/bin/si, so you can run "si vault ..." inside dyad/codex containers (or inject secrets from host with "si vault docker exec").

Usage:
  si <command> [args...]
  si help | -h | --help
  si version | --version | -v

Core:
  si dyad spawn|list|remove|recreate|status|peek|exec|run|logs|start|stop|restart|cleanup
  si spawn|respawn|list|status|report|login|logout|swap|ps|run|logs|tail|clone|remove|stop|start
  si vault <init|status|check|hooks|fmt|encrypt|set|unset|get|list|run|docker|trust|recipients>   (alias: creds)
  si stripe <auth|context|doctor|object|raw|report|sync>
  si github <auth|context|doctor|repo|branch|pr|issue|workflow|release|secret|raw|graphql>
  si cloudflare <auth|context|doctor|status|smoke|zone|dns|email|tls|ssl|origin|cert|cache|waf|ruleset|firewall|ratelimit|workers|pages|r2|d1|kv|queue|access|token|tokens|tunnel|tunnels|lb|analytics|logs|report|raw|api>
  si google <places|play|youtube|youtube-data>
  si apple <appstore>
  si social <facebook|instagram|x|linkedin|reddit>
  si workos <auth|context|doctor|organization|user|membership|invitation|directory|raw>
  si publish <catalog|devto|hashnode|reddit|hackernews|producthunt>
  si aws <auth|context|doctor|iam|sts|s3|ec2|lambda|ecr|secrets|kms|dynamodb|ssm|cloudwatch|logs|bedrock|raw>
  si gcp <auth|context|doctor|service|iam|apikey|gemini|generativelanguage|vertex|ai|raw>
  si openai <auth|context|doctor|model|project|key|usage|monitor|codex|raw>
  si oci <auth|context|doctor|identity|network|compute|oracular|raw>
  si providers <characteristics|health> [--provider <id>] [--json]
  si build <image|self>
  si paas [--context <name>] <target|app|deploy|rollback|logs|alert|secret|ai|context|doctor|agent|events> [args...]
  si analyze|lint [--module <path>] [--skip-vet] [--skip-lint] [--fix] [--no-fail]
  si docker <args...>

Build:
  si build image                  (builds aureuma/si:local; no extra args)
  si build self                   (upgrades installed si by default; see "build" below)

Profiles:
  si status [profile]      (codex profiles)
  si swap [profile]        (swap host ~/.codex auth to a configured codex profile)
  si persona <profile-name> (markdown profiles)
  si skill <role>

Command details
---------------

dyad:
  Running si dyad with no subcommand opens an interactive command picker.
  si dyad help prints dyad-only usage.

  si dyad spawn <name> [role]
    --role <role>
    --profile <profile>
    --skip-auth / --skip-auth=false
    --actor-image <image>
    --critic-image <image>
    --codex-model <model>
    --codex-effort-actor <effort>
    --codex-effort-critic <effort>
    --codex-model-low <model>
    --codex-model-medium <model>
    --codex-model-high <model>
    --codex-effort-low <effort>
    --codex-effort-medium <effort>
    --codex-effort-high <effort>
    --workspace <host path>       (default: current dir)
    --configs <host path>
    --forward-ports <range>
    --docker-socket / --docker-socket=false
    Note: dyads use existing si login profiles (no separate dyad login command).

  si dyad list                    (no flags)
  si dyad remove <name>           (aliases: teardown, destroy, rm, delete)
  si dyad remove --all
  si dyad recreate <name> [role] [--profile <profile>] [--skip-auth]
  si dyad status <name>
  si dyad peek [--member actor|critic|both] [--detached] [--session <name>] <dyad>
  si dyad exec [--member actor|critic] [--tty] <dyad> -- <cmd...>
  si dyad run  [--member actor|critic] [--tty] <dyad> -- <cmd...>   (alias)
    --member <actor|critic>
    --tty
  si dyad logs [--member actor|critic] [--tail N] <dyad>
    --member <actor|critic>
    --tail <lines>
  si dyad start <name>
  si dyad stop <name>
  si dyad restart <name>
  si dyad cleanup

codex:
  si spawn [name]
  si respawn [name] [--volumes]
    --image <docker image>
    --workspace <host path>       (default: current dir)
    --network <network>
    --repo <Org/Repo>
    --gh-pat <token>
    --cmd <command>
    --workdir <path>
    --codex-volume <volume>
    --gh-volume <volume>
    --docker-socket / --docker-socket=false
    --profile <profile>
    --clean-slate / --clean-slate=false
    --detach / --detach=false
    --env KEY=VALUE        (repeatable)
    --port HOST:CONTAINER  (repeatable)
    Note: when a profile is selected, container name defaults to that profile ID.
    One codex container is enforced per profile.

  si list [--json]
    --json

  si status [name|profile]
    --json
    --raw
    --timeout <duration>
    --profile <profile>
    --profiles
    --no-status

  si report <name>
    --json
    --raw
    --ansi
    --turn-timeout <duration>
    --ready-timeout <duration>
    --poll-interval <duration>
    --submit-attempts <n>
    --submit-delay <duration>
    --prompt-lines <n>
    --allow-mcp-startup
    --tmux-capture <alt|main>
    --tmux-keep
    --debug
    --lock-timeout <duration>
    --lock-stale <duration>
    --prompts-file <path>
    --prompt <text>         (repeatable)

  si login [profile] [--device-auth] [--open-url] [--open-url-cmd <command>] [--safari-profile <name>]
    --device-auth / --device-auth=false
    --open-url / --open-url=false
    --open-url-cmd <command>
    --safari-profile <name>

  si logout [--force] [--all]
    --force
    --all

  si run (two modes, alias: exec)
    One-off run (isolated container):
      si run --prompt "..." [--output-only] [--no-mcp]
      si run "..." [--output-only] [--no-mcp]
      --one-off
      --prompt <text>
      --output-only
      --no-mcp
      --profile <profile>
      --image <docker image>
      --workspace <host path>
      --workdir <path>
      --network <network>
      --codex-volume <volume>
      --gh-volume <volume>
      --docker-socket / --docker-socket=false
      --model <model>
      --effort <effort>
      --keep
      --env KEY=VALUE        (repeatable)

    Run in existing container:
      si run [name]           (tmux default)
      si run [name] --no-tmux
      si run <name> --no-tmux <command>
      --no-tmux
      --tmux

  si logs <name> [--tail N]
  si tail <name> [--tail N]
  si clone <name> <Org/Repo> [--gh-pat TOKEN]
  si remove <name> [--volumes]
  si remove --all [--volumes]
  si stop <name>
  si start <name>

  si warmup enable [--profile <profile>] [--quiet] [--no-reconcile]
  si warmup reconcile [--profile <profile>] [--force-bootstrap] [--quiet] [--max-attempts N] [--prompt <text>]
  si warmup status [--json]
  si warmup disable [--quiet]

  Legacy compatibility:
    si warmup [--profile <profile>] [--ofelia-install|--ofelia-write|--ofelia-remove] ...

build:
  si build image                  (builds aureuma/si:local; no extra args)
  si build self [--repo <path>] [--install-path <path>] [--no-upgrade] [--output <path>]
  si build self upgrade [--repo <path>] [--install-path <path>]
  si build self run [--repo <path>] [--] [si args...]

  Typical workflows:
    Stable use: run si build self to upgrade installed si from your checkout.
    Active dev: run si build self --no-upgrade --output ./si or si build self run -- <args...> from your checkout.

persona:
  si persona <name>

skill:
  si skill <role>

analyze:
  si analyze
  si analyze --module tools/si
  si analyze --skip-lint
  si analyze --fix
  si analyze --no-fail

  Runs static analysis over go.work modules:
    - go vet ./...
    - golangci-lint run ./...

  Aliases:
    si lint ...   (same as si analyze)

stripe:
  si stripe auth status [--account <alias>] [--env <live|sandbox>] [--json]
  si stripe context list|current|use [--account <alias|acct_id>] [--env <live|sandbox>]
  si stripe doctor [--account <alias|acct_id>] [--env <live|sandbox>] [--public] [--json]
  si stripe object list <object> [--limit N] [--param key=value] [--json]
  si stripe object get <object> <id> [--param key=value] [--json]
  si stripe object create <object> [--param key=value] [--idempotency-key <key>] [--json]
  si stripe object update <object> <id> [--param key=value] [--idempotency-key <key>] [--json]
  si stripe object delete <object> <id> [--force] [--idempotency-key <key>] [--json]
  si stripe raw --method <GET|POST|DELETE> --path <api-path> [--param key=value] [--json]
  si stripe report <revenue-summary|payment-intent-status|subscription-churn|balance-overview> [--json]
  si stripe sync live-to-sandbox plan [--only <family>] [--json]
  si stripe sync live-to-sandbox apply [--only <family>] [--dry-run] [--force] [--json]

  Environment policy:
    CLI uses live and sandbox.
    test is not accepted as a standalone environment mode.

vault:
  Running si vault with no subcommand opens an interactive command picker.
  Running si vault trust/recipients/hooks/docker with no subcommand opens an interactive command picker.

  Target selection (most commands):
    --file <path>               (explicit env file path; otherwise uses vault.file from settings or SI_VAULT_FILE)

	  si vault init [--file <path>] [--key-backend <keyring|keychain|file>] [--key-file <path>]
	  si vault keygen [--key-backend <keyring|keychain|file>] [--key-file <path>]
	  si vault status [--file <path>]
	  si vault check [--file <path>] [--staged] [--all]
	  si vault hooks install|status|uninstall [--force]
	  si vault fmt [--file <path>] [--all] [--check]
	  si vault encrypt [--file <path>] [--format] [--reencrypt]
	  si vault decrypt [--file <path>] [KEY]... [--stdout] [--in-place] [--yes]
	  si vault set <KEY> <VALUE> [--file <path>] [--section <name>] [--stdin] [--format]
	  si vault unset <KEY> [--file <path>] [--format]
	  si vault get <KEY> [--file <path>] [--reveal]
	  si vault list [--file <path>]
	  si vault run [--file <path>] [--allow-plaintext] [--shell] [--shell-interactive] [--shell-path <path>] -- <cmd...>
	  si vault docker exec --container <name|id> [--file <path>] [--allow-insecure-docker-host] [--allow-plaintext] -- <cmd...>
	  si vault trust status|accept|forget [--file <path>]
	  si vault recipients list|add|remove [--file <path>]

  Alias:
    si creds ...

paas:
  Running si paas with no subcommand opens an interactive command picker.
  Running si paas target/app/alert/ai/context/agent/events with no subcommand opens an interactive command picker.

github:
  si github auth status [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>] [--json]
  si github context list|current|use [--account <alias>] [--owner <owner>] [--base-url <url>] [--auth-mode <app|oauth>] [--token-env <env_key>]
  si github doctor [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>] [--public] [--json]
  si github repo list|get|create|update|archive|delete ...
  si github branch list|get|create|delete|protect|unprotect ...
  si github pr list|get|create|comment|merge ...
  si github issue list|get|create|comment|close|reopen ...
  si github workflow list|run|runs|logs ...
  si github workflow run get|cancel|rerun ...
  si github release list|get|create|upload|delete ...
  si github secret repo|env|org set|delete ...
  si github raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]
  si github graphql --query <query> [--var key=json] [--json]

  Auth policy:
    Supports GitHub App auth and OAuth token auth.
    Configure default_auth_mode/auth_mode as app or oauth.
    App credentials use env keys such as GITHUB_<ACCOUNT>_APP_ID and GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM.
    OAuth credentials use env keys such as GITHUB_<ACCOUNT>_OAUTH_ACCESS_TOKEN or GITHUB_TOKEN.

cloudflare:
  si cloudflare auth status [--account <alias>] [--env <prod|staging|dev>] [--json]
  si cloudflare status [--account <alias>] [--env <prod|staging|dev>] [--json]
  si cloudflare smoke [--account <alias>] [--env <prod|staging|dev>] [--no-fail] [--json]
  si cloudflare context list|current|use [--account <alias>] [--env <prod|staging|dev>] [--zone-id <zone>] [--base-url <url>]
  si cloudflare doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]
  si cloudflare zone|dns|waf|ruleset|firewall|ratelimit|queue|tunnel|lb <list|get|create|update|delete> ...
  si cloudflare email rule|address <list|get|create|update|delete> ...
  si cloudflare email settings <get|enable|disable> [--zone-id <zone>] [--force]
  si cloudflare token <list|get|create|update|delete|verify|permission-groups> ...
  si cloudflare workers script|route <list|get|create|update|delete> ...
  si cloudflare workers secret <set|delete> --script <name> --name <secret> [--text <value>]
  si cloudflare pages project <list|get|create|update|delete> ...
  si cloudflare pages domain <list|get|create|delete> --project <name> [--domain <fqdn>]
  si cloudflare pages deploy <list|trigger|rollback> --project <name> [--deployment <id>]
  si cloudflare r2 bucket <list|get|create|update|delete> ...
  si cloudflare r2 object <list|get|put|delete> --bucket <name> [--key <key>]
  si cloudflare d1 db <list|get|create|update|delete> ...
  si cloudflare d1 query --db <id> --sql <statement>
  si cloudflare d1 migration <list|apply> --db <id>
  si cloudflare kv namespace <list|get|create|update|delete> ...
  si cloudflare kv key <list|get|put|delete|bulk> --namespace <id> [--key <key>]
  si cloudflare access app|policy <list|get|create|update|delete> ...
  si cloudflare tls|ssl get|set --setting <name> [--value <value>]
  si cloudflare tls cert <list|get|create|update|delete> ...
  si cloudflare tls origin-cert <list|create|revoke> ...   (aliases: cloudflare origin, cloudflare cert)
  si cloudflare cache purge [--everything|--tag ...|--host ...|--prefix ...] [--force]
  si cloudflare cache settings <get|set> --setting <name> [--value <value>]
  si cloudflare analytics <http|security|cache> ...
  si cloudflare logs job <list|get|create|update|delete> ...
  si cloudflare logs received ...
  si cloudflare report <traffic-summary|security-events|cache-summary|billing-summary> [--from <iso>] [--to <iso>]
  si cloudflare raw|api --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]

  Environment policy:
    CLI uses prod, staging, and dev context labels.
    test is intentionally not used; map sandbox workflows to staging/dev context.

google:
  si google places auth status [--account <alias>] [--env <prod|staging|dev>] [--json]
  si google places context list|current|use [--account <alias>] [--env <prod|staging|dev>]
  si google places doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]

  si google places session new|inspect|end|list ...

  si google places autocomplete --input <text> [--session <token>] [--include-query-predictions] [--field-mask <mask>] [--json]
  si google places search-text --query <text> [--page-size <n>] [--all] [--field-mask <mask>] [--json]
  si google places search-nearby --center <lat,lng> --radius <m> [--included-type <type>] [--all] [--field-mask <mask>] [--json]
  si google places details <place_id_or_name> [--session <token>] [--field-mask <mask>] [--json]
  si google places photo get <photo_name> [--max-width <px>] [--max-height <px>] [--json]
  si google places photo download <photo_name> --output <path> [--max-width <px>] [--max-height <px>] [--json]
  si google places types list|validate ...
  si google places report usage|sessions [--since <ts>] [--until <ts>] [--json]
  si google places raw --method <GET|POST> --path <api-path> [--param key=value] [--body raw] [--field-mask <mask>] [--json]

  si google youtube auth status|login|logout [--account <alias>] [--env <prod|staging|dev>] [--mode <api-key|oauth>] [--json]
  si google youtube context list|current|use [--account <alias>] [--env <prod|staging|dev>]
  si google youtube doctor [--account <alias>] [--env <prod|staging|dev>] [--mode <api-key|oauth>] [--public] [--json]
  si google youtube search list --query <text> [--type <video|channel|playlist>] [--all] [--json]
  si google youtube channel list|get|mine|update ...
  si google youtube video list|get|update|delete|upload|rate|get-rating ...
  si google youtube playlist list|get|create|update|delete ...
  si google youtube playlist-item list|add|update|remove ...
  si google youtube subscription list|create|delete ...
  si google youtube comment list|get|create|update|delete|thread ...
  si google youtube caption list|upload|update|delete|download ...
  si google youtube thumbnail set --video-id <id> --file <path>
  si google youtube live broadcast|stream|chat ...
  si google youtube support languages|regions|categories ...
  si google youtube report usage [--since <ts>] [--until <ts>] [--json]
  si google youtube raw --method <GET|POST|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]

  Environment policy:
    CLI uses prod, staging, and dev context labels.
    test is intentionally not used; map sandbox workflows to staging/dev context.

social:
  si social facebook <auth|context|doctor|profile|page|post|comment|insights|raw|report>
  si social instagram <auth|context|doctor|profile|media|comment|insights|raw|report>
  si social x <auth|context|doctor|user|tweet|search|raw|report>
  si social linkedin <auth|context|doctor|profile|organization|post|raw|report>
  si social reddit <auth|context|doctor|profile|subreddit|post|comment|raw|report>

  Common:
    si social <platform> auth status [--account <alias>] [--env <prod|staging|dev>] [--json]
    si social <platform> context list|current|use ...
    si social <platform> doctor [--json]
    si social <platform> raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param key=value] [--body raw]
    si social <platform> report usage|errors [--since <ts>] [--until <ts>] [--json]

  Environment policy:
    CLI uses prod, staging, and dev context labels.
    test is intentionally not used; map sandbox workflows to staging/dev context.

workos:
  si workos auth status [--account <alias>] [--env <prod|staging|dev>] [--json]
  si workos context list|current|use [--account <alias>] [--env <prod|staging|dev>] [--base-url <url>] [--org-id <id>]
  si workos doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]

  si workos organization list|get|create|update|delete ...
  si workos user list|get|create|update|delete ...
  si workos membership list|get|create|update|delete ...
  si workos invitation list|get|create|revoke ...
  si workos directory list|get|users|groups|sync ...
  si workos raw --method <GET|POST|PUT|PATCH|DELETE> --path <api-path> [--param key=value] [--body raw|--json-body '{...}'] [--json]

  Environment policy:
    CLI uses prod, staging, and dev context labels.
    test is intentionally not used; map sandbox workflows to staging/dev context.

aws:
  si aws auth status [--account <alias>] [--region <aws-region>] [--json]
  si aws context list|current|use ...
  si aws doctor [--account <alias>] [--region <aws-region>] [--public] [--json]
  si aws iam user create --name <user> [--path /system/] [--json]
  si aws iam user attach-policy --user <name> --policy-arn <arn> [--json]
  si aws iam query --action <Action> [--param key=value] [--json]
  si aws sts get-caller-identity|assume-role ...
  si aws s3 bucket|object ...
  si aws ec2 instance list|start|stop|terminate ...
  si aws lambda function list|get|invoke|delete ...
  si aws ecr repository list|create|delete ...
  si aws ecr image list ...
  si aws secrets list|get|create|put|delete ...
  si aws kms key list|describe ...
  si aws kms encrypt|decrypt ...
  si aws dynamodb table list|describe ...
  si aws dynamodb item get|put|delete ...
  si aws ssm parameter list|get|put|delete ...
  si aws cloudwatch metric list ...
  si aws logs group list ...
  si aws logs stream list ...
  si aws logs events ...
  si aws bedrock foundation-model list|get ...
  si aws bedrock inference-profile list|get ...
  si aws bedrock guardrail list|get ...
  si aws bedrock runtime invoke|converse|count-tokens ...
  si aws bedrock job create|get|list|stop ...   (batch model invocation)
  si aws bedrock agent list|get|alias ...
  si aws bedrock knowledge-base list|get|data-source ...
  si aws bedrock agent-runtime invoke-agent|retrieve|retrieve-and-generate ...
  si aws raw [query args...]  (alias of aws iam query)
  si aws raw signed --service <aws-service> --method <GET|POST> --path <api-path> [--param key=value] [--body raw] [--json]

gcp:
  si gcp auth status [--account <alias>] [--env <prod|staging|dev>] [--project <id>] [--json]
  si gcp context list|current|use ...
  si gcp doctor [--account <alias>] [--env <prod|staging|dev>] [--project <id>] [--public] [--json]
  si gcp service enable --name <service.googleapis.com> [--project <id>] [--json]
  si gcp service disable --name <service.googleapis.com> [--check-usage] [--project <id>] [--json]
  si gcp service get --name <service.googleapis.com> [--project <id>] [--json]
  si gcp service list [--limit N] [--filter expr] [--project <id>] [--json]
  si gcp iam service-account|service-account-key|policy|role ...
  si gcp apikey <list|get|create|update|delete|lookup|undelete> ...
  si gcp gemini models|generate|embed|count-tokens|batch-embed ...
  si gcp vertex model|endpoint|batch|pipeline|operation ...
  si gcp ai <gemini|vertex> ...
  si gcp raw --method <GET|POST|PATCH|DELETE> --path <api-path> [--param key=value] [--body raw|--json-body '{...}'] [--json]

  Environment policy:
    CLI uses prod, staging, and dev context labels.
    test is intentionally not used; map sandbox workflows to staging/dev context.

openai:
  si openai auth status [--account <alias>] [--base-url <url>] [--json]
  si openai context list|current|use ...
  si openai doctor [--account <alias>] [--public] [--json]
  si openai model list|get ...
  si openai project list|get|create|update|archive ...
  si openai project rate-limit list|update ...
  si openai project api-key list|get|delete ...
  si openai project service-account list|create|get|delete ...
  si openai key list|get|create|delete ...   (organization admin keys)
  si openai usage <completions|embeddings|images|audio_speeches|audio_transcriptions|moderations|vector_stores|code_interpreter_sessions|costs> ...
  si openai monitor usage|limits ...
  si openai codex usage ...
  si openai raw --method <GET|POST|PATCH|DELETE> --path <api-path> [--param key=value] [--body raw|--json-body '{...}'] [--admin] [--json]

oci:
  si oci auth status [--profile <name>] [--config-file <path>] [--region <region>] [--json]
  si oci context list|current|use ...
  si oci doctor [--profile <name>] [--config-file <path>] [--region <region>] [--public] [--json]

  si oci identity availability-domains list [--tenancy <ocid>] [--json]
  si oci identity compartment create --parent <ocid> --name <name> [--description <text>] [--json]
  si oci network vcn|internet-gateway|route-table|security-list|subnet create ...
  si oci compute image latest-ubuntu --tenancy <ocid> --shape <shape> [--json]
  si oci compute instance create ... [--json]
  si oci oracular cloud-init [--ssh-port <port>] [--json]
  si oci oracular tenancy [--profile <name>] [--config-file <path>] [--json]
  si oci raw --method <GET|POST|PUT|DELETE> --path <api-path> [--service <core|identity>] [--auth <signature|none>] [--json]

image:
  si image unsplash auth status [--json]
  si image pexels auth status [--json]
  si image pixabay auth status [--json]
  si image <unsplash|pexels|pixabay> search --query <text> [--page <n>] [--per-page <n>] [--json]
  si image <unsplash|pexels|pixabay> raw --method <GET|POST> --path <api-path> [--param key=value] [--json]

providers:
  si providers characteristics [--provider <id>] [--json]
  si providers health [--provider <id>] [--json]
  Aliases: si integrations ..., si apis ...

Environment defaults (selected)
-------------------------------
  ACTOR_IMAGE, CRITIC_IMAGE, SI_CODEX_IMAGE, SI_NETWORK
  CODEX_MODEL, CODEX_REASONING_EFFORT, CODEX_MODEL_LOW|MEDIUM|HIGH
  CODEX_REASONING_EFFORT_LOW|MEDIUM|HIGH
  SI_WORKSPACE_HOST, SI_CONFIGS_HOST, SI_DYAD_FORWARD_PORTS
  SI_CODEX_EXEC_VOLUME, GH_PAT, GH_TOKEN, GITHUB_TOKEN
`))
}

const siVersion = "v0.45.0"

func printVersion() {
	fmt.Println(siVersion)
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func isEscCancelInput(value string) bool {
	return strings.ContainsRune(value, '\x1b')
}

func hostUserEnv() []string {
	uid := os.Getuid()
	gid := os.Getgid()
	if uid <= 0 || gid <= 0 {
		return nil
	}
	return []string{
		"SI_HOST_UID=" + strconv.Itoa(uid),
		"SI_HOST_GID=" + strconv.Itoa(gid),
	}
}

func mustRepoRoot() string {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	return root
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return repoRootFrom(dir)
}

func repoRootFrom(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("repo root not found (empty start dir)")
	}
	dir = filepath.Clean(dir)
	for {
		if exists(filepath.Join(dir, "configs")) && exists(filepath.Join(dir, "agents")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("repo root not found (expected configs/ and agents/)")
}

func repoRootFromExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return repoRootFrom(filepath.Dir(exe))
}

func resolveConfigsHost(workspaceHost string) (string, error) {
	workspaceHost = strings.TrimSpace(workspaceHost)
	if workspaceHost != "" {
		if root, err := repoRootFrom(workspaceHost); err == nil {
			return filepath.Join(root, "configs"), nil
		}
	}
	if root, err := repoRoot(); err == nil {
		return filepath.Join(root, "configs"), nil
	}
	if root, err := repoRootFromExecutable(); err == nil {
		return filepath.Join(root, "configs"), nil
	}
	return "", fmt.Errorf("configs dir not found; use --configs or run from the si repo root")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, styleError(err.Error()))
	os.Exit(1)
}

func validateSlug(name string) error {
	if name == "" {
		return errors.New("name required")
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return fmt.Errorf("invalid name %q (allowed: letters, numbers, - and _)", name)
	}
	return nil
}

func isValidSlug(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

var ansiEnabled = initAnsiEnabled()

func initAnsiEnabled() bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" || strings.TrimSpace(os.Getenv("SI_NO_COLOR")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	if force := strings.TrimSpace(os.Getenv("SI_COLOR")); force != "" {
		return force == "1" || strings.EqualFold(force, "true")
	}
	if force := strings.TrimSpace(os.Getenv("CLICOLOR_FORCE")); force != "" && force != "0" {
		return true
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func ansi(codes ...string) string {
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func colorize(s string, codes ...string) string {
	if !ansiEnabled || s == "" {
		return s
	}
	return ansi(codes...) + s + ansi("0")
}

func styleHeading(s string) string { return colorize(s, "1", "36") }
func styleSection(s string) string { return colorize(s, "1", "34") }
func styleCmd(s string) string     { return colorize(s, "1", "32") }
func styleFlag(s string) string    { return colorize(s, "33") }
func styleArg(s string) string     { return colorize(s, "35") }
func styleDim(s string) string     { return colorize(s, "90") }
func styleInfo(s string) string    { return colorize(s, "36") }
func styleSuccess(s string) string { return colorize(s, "32") }
func styleWarn(s string) string    { return colorize(s, "33") }
func styleError(s string) string   { return colorize(s, "31") }
func styleUsage(s string) string   { return colorize(s, "1", "33") }

func styleLimitTextByPct(text string, pct float64) string {
	if strings.TrimSpace(text) == "" || pct < 0 {
		return text
	}
	rounded := int(math.Round(pct))
	switch {
	case rounded >= 100:
		return colorize(text, "1", "37")
	case rounded <= 25:
		return colorize(text, "35")
	default:
		return colorize(text, "32")
	}
}

func styleStatus(s string) string {
	val := strings.ToLower(strings.TrimSpace(s))
	switch val {
	case "running", "ok", "ready", "done", "success", "yes", "true", "available", "up":
		return styleSuccess(s)
	case "blocked", "warning", "warn", "pending":
		return styleWarn(s)
	case "failed", "error", "missing", "stopped", "exited", "not found", "no", "false", "down":
		return styleError(s)
	default:
		return styleInfo(s)
	}
}

func printUsage(line string) {
	raw := strings.TrimSpace(line)
	if strings.HasPrefix(raw, "usage:") {
		rest := strings.TrimSpace(strings.TrimPrefix(raw, "usage:"))
		fmt.Printf("%s %s\n", styleUsage("usage:"), rest)
		return
	}
	fmt.Println(styleUsage(raw))
}

func printUnknown(kind, cmd string) {
	kind = strings.TrimSpace(kind)
	if kind != "" {
		kind = kind + " "
	}
	fmt.Fprintf(os.Stderr, "%s %s%s\n", styleError("unknown"), kind+"command:", styleCmd(cmd))
}

func warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if containsANSI(msg) {
		fmt.Fprintln(os.Stderr, styleWarn("warning:")+" "+msg)
		return
	}
	fmt.Fprintln(os.Stderr, styleWarn("warning:")+" "+msg)
}

func infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if containsANSI(msg) {
		fmt.Println(msg)
		return
	}
	fmt.Println(styleInfo(msg))
}

func successf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if containsANSI(msg) {
		fmt.Println(msg)
		return
	}
	fmt.Println(styleSuccess(msg))
}

func colorizeHelp(text string) string {
	if !ansiEnabled {
		return text
	}
	sectionRe := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 /-]*:$`)
	cmdRe := regexp.MustCompile(`\\b(si|dyad|codex|docker|image|persona|skill|analyze|lint|stripe|github|cloudflare|google|vault|creds|self)\\b`)
	flagRe := regexp.MustCompile(`--[a-zA-Z0-9-]+`)
	shortFlagRe := regexp.MustCompile(`(^|\\s)(-[a-zA-Z])\\b`)
	argRe := regexp.MustCompile(`<[^>]+>`)
	dividerRe := regexp.MustCompile(`^-{3,}$`)

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if dividerRe.MatchString(trimmed) {
			lines[i] = indentLine(line, styleDim(trimmed))
			continue
		}
		if sectionRe.MatchString(trimmed) {
			lines[i] = indentLine(line, styleHeading(trimmed))
			continue
		}
		if strings.HasPrefix(trimmed, "Usage:") || strings.HasPrefix(trimmed, "Features:") || strings.HasPrefix(trimmed, "Core:") || strings.HasPrefix(trimmed, "Build:") || strings.HasPrefix(trimmed, "Profiles:") || strings.HasPrefix(trimmed, "Command details") || strings.HasPrefix(trimmed, "Environment defaults") {
			lines[i] = indentLine(line, styleHeading(trimmed))
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "usage:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				lines[i] = indentLine(line, styleUsage(parts[0]+":")+" "+strings.TrimSpace(parts[1]))
				continue
			}
		}
		line = flagRe.ReplaceAllStringFunc(line, styleFlag)
		line = shortFlagRe.ReplaceAllStringFunc(line, func(m string) string {
			trim := strings.TrimSpace(m)
			if trim == "" {
				return m
			}
			return strings.Replace(m, trim, styleFlag(trim), 1)
		})
		line = argRe.ReplaceAllStringFunc(line, styleArg)
		line = cmdRe.ReplaceAllStringFunc(line, styleCmd)
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func indentLine(line, replacement string) string {
	prefix := line[:len(line)-len(strings.TrimLeft(line, " "))]
	return prefix + replacement
}

var ansiStripRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSIForPad(s string) string {
	return ansiStripRe.ReplaceAllString(s, "")
}

func displayWidth(s string) int {
	s = stripANSIForPad(s)
	width := runewidth.StringWidth(s)
	if width == 0 || !strings.ContainsRune(s, '\ufe0f') {
		return width
	}
	// Some emoji-presentation sequences (base + VS16) are rendered as wide in
	// terminals while runewidth still reports the base symbol width.
	prev := rune(0)
	for _, r := range s {
		if r == '\ufe0f' && prev != 0 && runewidth.RuneWidth(prev) == 1 {
			width++
		}
		prev = r
	}
	return width
}

func padRightANSI(s string, width int) string {
	visible := displayWidth(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

func containsANSI(s string) bool {
	return ansiStripRe.MatchString(s)
}
