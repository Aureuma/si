package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func usage() {
	fmt.Print(`si <command> [args]

Core:
  si stack up|down|status
  si dyad spawn|list|remove|recreate|status|exec|logs|restart|register|cleanup
  si codex spawn|list|status|login|ps|exec|logs|tail|clone|remove|stop|start
  si task add|add-dyad|update
  si human add|complete
  si feedback add|broadcast
  si access request|resolve
  si resource request
  si metric post
  si notify <message>
  si report status|escalate|review|dyad
  si roster apply|status
  si mcp scout|sync|apply-config
  si docker <args...>

Build/app:
  si images build
  si image build -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] <context>
  si app init|adopt|list|build|deploy|remove|status|secrets

Profiles:
  si profile <profile-name>
  si capability <role>
`)
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func readFileTrim(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

func mustRepoRoot() string {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	return root
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(dir, "configs")) && exists(filepath.Join(dir, "agents")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("repo root not found (expected configs/ and agents/)")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

func validateSlug(name string) error {
	if name == "" {
		return errors.New("name required")
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return fmt.Errorf("invalid name %q (allowed: letters, numbers, - and _)", name)
	}
	return nil
}
