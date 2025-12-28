# Dyad roster (arrangement)

The roster defines the desired dyad lineup (roles, teams, assignments, and availability).
It is the single place to arrange dyads before spawning.

## File
- `configs/dyad_roster.json`

Example entry:
```json
{
  "name": "web-builder",
  "role": "builder",
  "department": "engineering",
  "team": "web",
  "assignment": "apps",
  "tags": ["builder", "sveltekit"],
  "available": true,
  "spawn": false
}
```

## Apply roster
- Update registry and metadata: `bin/dyad-roster-apply.sh`
- Spawn dyads marked `spawn: true`: `bin/dyad-roster-apply.sh --spawn`
- Dry run: `bin/dyad-roster-apply.sh --dry-run`

## Status
- `bin/dyad-roster-status.sh` prints dyads with team/assignment/availability.

## Notes
- `spawn-dyad.sh` requires dyads to be registered; roster apply handles that.
- Use `spawn-dyad.sh --temporal` (or `bin/beam-dyad-bootstrap.sh`) to run provisioning as a Temporal Beam workflow.
- Heartbeats never overwrite `team`, `assignment`, or `tags` (arrangement stays stable).
