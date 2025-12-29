# Gatekeeper policies

This directory contains Gatekeeper constraint templates and constraints used by Silexa.

## Install Gatekeeper
```
GATEKEEPER_VERSION=v3.15.0 bin/gatekeeper-up.sh
```

## Policies
- `templates/restrict-secret-refs.yaml`: denies secret refs (volumes/env) in dyad pods unless the service account is allow-listed.
- `constraints/silexa-dyad-secret-refs.yaml`: applies the restriction to pods labeled `app=silexa-dyad` in the `silexa` namespace.
