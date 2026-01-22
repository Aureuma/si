package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func usage() {
	fmt.Println(`silexa <command> [args]

Core:
  silexa stack up|down|status
  silexa dyad spawn|list|remove|recreate|status|exec|logs|restart|register|cleanup
  silexa task add|add-dyad|update
  silexa human add|complete
  silexa feedback add|broadcast
  silexa access request|resolve
  silexa resource request
  silexa metric post
  silexa notify <message>
  silexa report status|escalate|review|dyad
  silexa roster apply|status
  silexa mcp scout|sync|apply-config

Build/app:
  silexa images build
  silexa image build -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] <context>
  silexa app init|adopt|list|build|deploy|remove|status|secrets

Profiles:
  silexa profile <profile-name>
  silexa capability <role>
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
