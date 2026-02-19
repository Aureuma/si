---
title: OCI Command Guide
description: Oracle Cloud Infrastructure workflows in SI for identity, networking, compute orchestration, and signed raw API access.
---

# OCI Command Guide (`si oci`)

![OCI](/docs/images/integrations/oci.svg)

`si oci` provides signed Oracle Cloud Infrastructure API workflows for tenancy bootstrap and infrastructure operations.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si oci <auth|context|doctor|identity|network|compute|oracular|raw>
```

## Auth, context, and diagnostics

```bash
si oci auth status --profile DEFAULT --config-file ~/.oci/config --region us-ashburn-1 --json
si oci context list --json
si oci context current --json
si oci context use --account core --profile DEFAULT --region us-ashburn-1
si oci doctor --profile DEFAULT --region us-ashburn-1 --public --json
```

## Identity and tenancy data

```bash
si oci identity availability-domains list --tenancy <tenancy_ocid> --json
si oci identity compartment create --parent <compartment_ocid> --name apps --description "application compartment" --json
```

## Network bootstrap

```bash
si oci network vcn create --compartment <ocid> --cidr 10.0.0.0/16 --display-name core-vcn --json
si oci network internet-gateway create --compartment <ocid> --vcn-id <vcn_ocid> --display-name igw --enabled --json
si oci network security-list create --compartment <ocid> --vcn-id <vcn_ocid> --ssh-port 22 --json
si oci network subnet create --compartment <ocid> --vcn-id <vcn_ocid> --route-table-id <rt_ocid> --security-list-id <sl_ocid> --dhcp-options-id <dhcp_ocid> --json
```

## Compute operations

```bash
si oci compute image latest-ubuntu --tenancy <ocid> --shape VM.Standard.E4.Flex --json
si oci compute instance create --compartment <ocid> --ad <availability_domain> --subnet-id <subnet_ocid> --image-id <image_ocid> --json
```

## Oracular helpers

```bash
si oci oracular cloud-init --ssh-port 22 --json
si oci oracular tenancy --profile DEFAULT --config-file ~/.oci/config --json
```

## Raw API mode

```bash
si oci raw --service core --method GET --path /instances --json
si oci raw --service identity --method GET --path /tenancies/<tenancy_ocid> --json
```

## Safety guidance

- Keep OCI private key references in secure paths and avoid embedding key material in scripts.
- Validate compartment and region before network or compute writes.
- Prefer generated cloud-init artifacts reviewed in version control.
- Use `--json` for auditable operation logs.

## Troubleshooting

1. `si oci auth status --json`
2. `si oci doctor --json`
3. `si providers health --provider oci_core --json`
4. Verify profile/config path and region alignment.
