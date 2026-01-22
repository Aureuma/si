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
  "tags": ["builder", "frontend"],
  "available": true,
  "spawn": false
}
```

## Apply roster
- Update registry and metadata: `silexa roster apply`
- Spawn dyads marked `spawn: true`: `silexa roster apply --spawn`
- Dry run: `silexa roster apply --dry-run`

## Status
- `silexa roster status` prints dyads with team/assignment/availability.

## Notes
- `silexa dyad spawn` requires dyads to be registered; roster apply handles that.
- Heartbeats never overwrite `team`, `assignment`, or `tags` (arrangement stays stable).
