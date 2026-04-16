---
title: AWS Command Guide
description: AWS integration workflows in SI for IAM, STS, S3, EC2, Lambda, ECR, Secrets, KMS, DynamoDB, SSM, CloudWatch, Logs, Bedrock, and raw APIs.
---

# AWS Command Guide (`si orbit aws`)

![AWS](/docs/images/integrations/aws.svg)

`si orbit aws` is SI's signed AWS bridge for account context, diagnostics, and resource operations.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si orbit aws <auth|context|doctor|iam|sts|s3|ec2|lambda|ecr|secrets|kms|dynamodb|ssm|cloudwatch|logs|bedrock|raw>
```

## Auth and context

```bash
si orbit aws auth status --account core --region us-east-1 --json
si orbit aws context list --json
si orbit aws context current --json
si orbit aws context use --account core --region us-east-1
si orbit aws doctor --account core --region us-east-1 --public --json
```

For host/admin automation, use `si vault run -- <cmd>` when injecting sensitive env values.
For SI runtime workers, use `si fort ...` for secret access.

## Core operations

### STS and IAM

```bash
si orbit aws sts whoami --json
si orbit aws sts assume --role-arn arn:aws:iam::123456789012:role/Deploy --session-name si-session --json
si orbit aws iam user list --json
si orbit aws iam user get deploy-bot --json
si orbit aws iam user attached deploy-bot --json
si orbit aws iam role list --json
```

### S3

```bash
si orbit aws s3 bucket list --json
si orbit aws s3 bucket create my-release-bucket --region us-east-1 --json
si orbit aws s3 object put --bucket my-release-bucket --key release-notes.txt --body "hello" --json
si orbit aws s3 object list --bucket my-release-bucket --json
```

### Compute and serverless

```bash
si orbit aws ec2 instance list --json
si orbit aws ec2 instance start --id i-0123456789abcdef0 --json
si orbit aws lambda function list --json
si orbit aws lambda function invoke my-fn --payload '{"ping":true}' --json
```

### Registry and secrets

```bash
si orbit aws ecr repository list --json
si orbit aws ecr image list --repository my-service --json
si orbit aws secrets list --json
si orbit aws secrets get prod/db-password --json
```

### KMS, DynamoDB, SSM

```bash
si orbit aws kms key list --json
si orbit aws dynamodb table list --json
si orbit aws dynamodb item get --table users --key-json '{"pk":{"S":"u#1"}}' --json
si orbit aws ssm parameter list --json
```

### Observability and AI

```bash
si orbit aws cloudwatch metric list --namespace AWS/EC2 --json
si orbit aws logs group list --json
si orbit aws logs events --group /aws/lambda/my-fn --limit 50 --json
si orbit aws bedrock model list --json
```

## Raw API mode

```bash
si orbit aws raw --service iam --method GET --path / --param Action=ListUsers --param Version=2010-05-08 --json
```

Use raw mode when a higher-level resource command is not yet available.

## Safety guidance

- Always verify caller identity before writes (`si orbit aws sts whoami`).
- Prefer explicit `--region` in automation.
- Keep destructive operations behind `--force` in scripted flows.
- Treat assumed-role credentials as short-lived; avoid persisting session secrets.

## Troubleshooting

1. `si orbit aws auth status --json`
2. `si orbit aws doctor --json`
3. `si orbit list --provider aws_iam --json`
4. Re-check context selection and credential source precedence.
