# PaaS Test Matrix

Last updated: 2026-02-17  
Scope: `si paas` workflows in `tools/si`

## Matrix

| Level | Coverage Goal | Primary Areas | Command |
| --- | --- | --- | --- |
| Unit | Validate command parsing, contracts, and pure helper logic | command/action wiring, usage contracts, magic-variable resolution, add-on merge conflict checks | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run 'TestPaasActionNamesMatchDispatchSwitches|TestPreparePaasComposeForDeployRejectsAddonMergeConflicts|TestPreparePaasComposeForDeployRejectsUnknownMagicVariable' -count=1` |
| Integration | Validate cross-component behavior in the CLI package | deploy/apply transport flow, rollback orchestration, blue/green cutover, add-on lifecycle, event/audit/log routing | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run 'TestPaasDeployApplyUsesRemoteTransport|TestPaasDeployBlueGreenApplyUsesComposeOnlyCutoverPolicy|TestPaasAppAddonContractEnableListDisable|TestPaasDeployResolvesMagicVariablesAndAddonComposeManifest' -count=1` |
| E2E Regression | Validate end-to-end operational paths with realistic command sequences | upgrade/compatibility path, deploy->health->rollback, webhook/ingress/alert continuity | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run TestPaasRegressionUpgradeDeployRollbackPath -count=1` |
| Broad Smoke | Catch unintended regressions across all current PaaS tests | all `TestPaas*` coverage in `tools/si` | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run TestPaas -count=1` |

## Execution Policy

1. Run `Unit` and `Integration` before every WS merge touching `tools/si/paas_*`.
2. Run `E2E Regression` for deploy/runtime/rollback/webhook/ingress changes.
3. Run `Broad Smoke` before pushing multi-ticket slices.
4. Treat any regression as a merge blocker until fixed or explicitly waived in ticket notes.
