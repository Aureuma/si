---
title: Integration Gateway Architecture
description: Sharded SI/Sun integration registry model for large-scale orbit ecosystems.
---

# Integration Gateway Architecture

This document defines the SI + Sun architecture for scaling orbit/integration catalogs toward very large ecosystems (10k+ integrations) without turning orbit resolution into a monolithic read path.

## Design Goals

- Keep SI core stable while integration count grows.
- Make registry reads selective (namespace/capability/prefix) instead of full-catalog downloads.
- Preserve local-first operation with explicit cloud sync.
- Keep trust/policy and install lifecycle separate from catalog transport.

## External Lessons Applied

### Zapier

- Integration schemas evolve over time and should avoid hard breaks where possible.
- Operationally, hidden/deprecated fields are safer than destructive contract changes.

SI mapping:
- Orbit manifests remain schema-versioned and backward-compatible.
- Gateway index/shard payloads are explicit JSON contracts, allowing additive evolution.

### n8n

- Node packaging and runtime execution are decoupled from orchestration.
- Large ecosystems rely on metadata discovery + selective enablement, not global eager load.

SI mapping:
- `si orbits ...` install/policy workflows are distinct from gateway publish/pull transport.
- Registry metadata is fetched first (index), then only relevant shards are loaded.

### TON (workchain + shardchain model)

- Partition first by coarse domain, then by deterministic shard routing.
- Keep routing deterministic so clients can independently find the same partition.

SI mapping:
- Namespace partition (`namespace/*`) acts as the coarse “workchain”.
- Deterministic slot sharding (`namespace--NN`) acts as shard routing.
- A compact index maps shard keys and capability summaries.

## Implemented Model

### SI (`tools/si`)

- `orbitals` now includes sharded gateway types:
  - `GatewayIndex`
  - `GatewayShard`
  - deterministic `GatewayShardKey(...)`
  - `BuildGateway(...)`
  - `MaterializeGatewayCatalog(...)`
- New command family:
  - `si orbits gateway build`
  - `si orbits gateway push`
  - `si orbits gateway pull`
  - `si orbits gateway status`
- New Sun-aware settings:
  - `sun.orbit_gateway_registry`
  - `sun.orbit_gateway_slots`
  - `SI_SUN_ORBIT_GATEWAY_REGISTRY`
  - `SI_SUN_ORBIT_GATEWAY_SLOTS`

### Sun (`../sun`)

- New API endpoints:
  - `GET/PUT /v1/integrations/registries/{name}`
  - `GET/PUT /v1/integrations/registries/{name}/shards/{shard}`
  - `GET /v1/integrations/registries/{name}/entries?...`
- New scopes:
  - `integrations:read`
  - `integrations:write`
- Backward compatibility:
  - Integration endpoints also accept object scopes (`objects:read`/`objects:write`) so existing tokens still work.

## Data Path

1. Build local manifests into a regular catalog.
2. Partition catalog into `namespace--slot` shards.
3. Publish index + shards to Sun.
4. Pull index, select shards by filter, materialize a local catalog file.
5. Existing `si orbits list/install/...` works on the pulled catalog with existing policy gates.

## Scale Envelope

- Slot sharding controls per-shard payload size and update fanout.
- Index-first fetch avoids loading all manifests for common scoped queries.
- Namespace + capability filters reduce read amplification for large catalogs.

## Future Hardening

- Signed index/shards and trust policy enforcement.
- Differential shard sync (checksum-aware pull).
- Optional server-side secondary indexes for capability-heavy queries.
