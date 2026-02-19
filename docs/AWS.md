---
title: AWS Command Guide
description: AWS integration workflows in SI for IAM, STS, S3, EC2, Lambda, ECR, Secrets, KMS, DynamoDB, SSM, CloudWatch, Logs, Bedrock, and raw APIs.
---

# AWS Command Guide (`si aws`)

![AWS](/docs/images/integrations/aws.svg)

`si aws` is SI's signed AWS bridge for account context, diagnostics, and resource operations.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si aws <auth|context|doctor|iam|sts|s3|ec2|lambda|ecr|secrets|kms|dynamodb|ssm|cloudwatch|logs|bedrock|raw>
```

## Auth and context

```bash
si aws auth status --account core --region us-east-1 --json
si aws context list --json
si aws context current --json
si aws context use --account core --region us-east-1
si aws doctor --account core --region us-east-1 --public --json
```

Use `si vault run -- <cmd>` when injecting sensitive env values in automation.

## Core operations

### STS and IAM

```bash
si aws sts get-caller-identity --json
si aws sts assume-role --role-arn arn:aws:iam::123456789012:role/Deploy --session-name si-session --json
si aws iam user list --json
si aws iam role list --json
```

### S3

```bash
si aws s3 bucket list --json
si aws s3 bucket create my-release-bucket --region us-east-1 --json
si aws s3 object put --bucket my-release-bucket --key release-notes.txt --body "hello" --json
si aws s3 object list --bucket my-release-bucket --json
```

### Compute and serverless

```bash
si aws ec2 instance list --json
si aws ec2 instance start --id i-0123456789abcdef0 --json
si aws lambda function list --json
si aws lambda function invoke my-fn --payload '{"ping":true}' --json
```

### Registry and secrets

```bash
si aws ecr repository list --json
si aws ecr image list --repository my-service --json
si aws secrets list --json
si aws secrets get prod/db-password --json
```

### KMS, DynamoDB, SSM

```bash
si aws kms key list --json
si aws dynamodb table list --json
si aws dynamodb item get --table users --key-json '{"pk":{"S":"u#1"}}' --json
si aws ssm parameter list --json
```

### Observability and AI

```bash
si aws cloudwatch metric list --namespace AWS/EC2 --json
si aws logs group list --json
si aws logs events --group /aws/lambda/my-fn --limit 50 --json
si aws bedrock model list --json
```

## Raw API mode

```bash
si aws raw --service iam --method GET --path / --param Action=ListUsers --param Version=2010-05-08 --json
```

Use raw mode when a higher-level resource command is not yet available.

## Safety guidance

- Always verify caller identity before writes (`si aws sts get-caller-identity`).
- Prefer explicit `--region` in automation.
- Keep destructive operations behind `--force` in scripted flows.
- Treat assumed-role credentials as short-lived; avoid persisting session secrets.

## Troubleshooting

1. `si aws auth status --json`
2. `si aws doctor --json`
3. `si providers health --provider aws_iam --json`
4. Re-check context selection and credential source precedence.
