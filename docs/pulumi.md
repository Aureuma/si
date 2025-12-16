# Pulumi Preference for Infra

We prefer Pulumi over Terraform for cloud provisioning.

## Location
- Pulumi project scaffold: `pulumi/infra` (Go runtime). `Pulumi.yaml` + `main.go` placeholder. Add real resources as needed.

## Usage (host or dedicated runner)
1) Install Pulumi CLI and cloud provider SDKs.
2) Set state backend (e.g., `pulumi login s3://...` or Pulumi Service).
3) Configure stack: `cd pulumi/infra && pulumi stack init <stack>`, set config/secrets as needed.
4) Preview/Up: `pulumi preview` / `pulumi up`.

## Brokers
- Infra-broker (`:9092`) queues infra actions (dns/ssl/network). Implement handlers/scripts that map broker requests to Pulumi programs.
- Resource-broker (`:9091`) queues external resource requests (GitHub/Stripe/etc). Keep Pulumi for cloud infra.

## Policy
- Keep provider credentials out of code; use Pulumi config/secret or env vars at runtime.
- Security dyad reviews access/approvals before running `pulumi up`.
