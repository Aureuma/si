# Testing

## Go workspace
This repo uses a `go.work` workspace that aggregates the modules under `agents/` and `tools/`.
Running `go test ./...` from the repo root will fail because the root directory is not itself a module.

## Running tests
Use the repo test runner from the root:

```bash
./tools/test.sh
```

That script runs `go test` across the workspace modules listed in `go.work`.
Make sure the Go toolchain is installed and on your `PATH` before running tests.
The script expects to be run from the repo root so it can find `go.work` and will
error with a short message if prerequisites are missing.
Use `./tools/test.sh --help` for a quick usage reminder.
Use `./tools/test.sh --list` to print the module list without running tests.

## Installer smoke tests
To validate the `si` installer script end-to-end, run:

```bash
./tools/test-install-si.sh
```

Use `./tools/test-install-si.sh --help` for a quick usage reminder.

## PaaS matrix
For `si paas` quality-gate coverage (unit/integration/e2e regression matrix), use:

```bash
cat docs/PAAS_TEST_MATRIX.md
```

For failure-injection and rollback drill procedures, use:

```bash
cat docs/PAAS_FAILURE_DRILLS.md
./tools/paas-failure-drills.sh
```

For PaaS security review checklist and threat model guidance, use:

```bash
cat docs/PAAS_SECURITY_THREAT_MODEL.md
```

## Static analysis
Run static analysis from the repo root:

```bash
./si analyze
```

Use non-failing mode for local iteration while keeping CI strict with default `./si analyze`:

```bash
./si analyze --no-fail
```

If you only changed the CLI, this is the fastest local scope:

```bash
./si analyze --module tools/si
```

## Image build smoke check
The canonical local image build command is:

```bash
./si build image
```
