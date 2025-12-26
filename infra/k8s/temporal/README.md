# Temporal on Kubernetes

This directory provides a minimal values file for the official Temporal Helm chart.

## Prerequisites
- A Postgres instance reachable as `temporal-postgresql:5432` in the same namespace.
- Helm installed and configured for your cluster.

## Install (example)
```bash
helm repo add temporal https://helm.temporal.io
helm repo update
helm upgrade --install temporal temporal/temporal -f infra/k8s/temporal/values.yaml
```

## Notes
- Adjust `infra/k8s/temporal/values.yaml` for production (replicas, resources, TLS, secrets).
- Temporal frontend service name from the chart is `temporal-frontend`.
