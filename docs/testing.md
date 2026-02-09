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

## Static analysis
Run static analysis from the repo root:

```bash
./si analyze
```

Scope it to the CLI module when iterating locally:

```bash
./si analyze --module tools/si
```

## Image build smoke check
The canonical local image build command is:

```bash
./si build image
```
