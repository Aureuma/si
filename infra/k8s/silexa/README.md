# Silexa core services on Kubernetes

This directory contains Kubernetes manifests for the core Silexa control plane.

## Apply
```bash
kubectl apply -k infra/k8s/silexa
```

## Notes
- The manager worker must run alongside the manager API.
- Point `TEMPORAL_ADDRESS` at the Temporal frontend service.
- Configure `TELEGRAM_NOTIFY_URL` and `TELEGRAM_CHAT_ID` if using Telegram alerts.
