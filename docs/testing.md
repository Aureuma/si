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
./si image build
```
