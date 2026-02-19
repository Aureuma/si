package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const paasStateRootEnvKey = "SI_PAAS_STATE_ROOT"

type paasTarget struct {
	Name             string   `json:"name"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	User             string   `json:"user"`
	AuthMethod       string   `json:"auth_method,omitempty"`
	Labels           []string `json:"labels,omitempty"`
	IngressProvider  string   `json:"ingress_provider,omitempty"`
	IngressDomain    string   `json:"ingress_domain,omitempty"`
	IngressLBMode    string   `json:"ingress_lb_mode,omitempty"`
	IngressACMEEmail string   `json:"ingress_acme_email,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

type paasTargetStore struct {
	CurrentTarget string       `json:"current_target,omitempty"`
	Targets       []paasTarget `json:"targets,omitempty"`
}

func resolvePaasStateRoot() (string, error) {
	if assigned := strings.TrimSpace(os.Getenv(paasStateRootEnvKey)); assigned != "" {
		return filepath.Clean(assigned), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return "", err
	}
	return filepath.Join(home, ".si", "paas"), nil
}

func resolvePaasContextDir(contextName string) (string, error) {
	root, err := resolvePaasStateRoot()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(contextName)
	if name == "" {
		name = defaultPaasContext
	}
	return filepath.Join(root, "contexts", name), nil
}

func resolvePaasTargetStorePath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "targets.json"), nil
}

func loadPaasTargetStore(contextName string) (paasTargetStore, error) {
	path, err := resolvePaasTargetStorePath(contextName)
	if err != nil {
		return paasTargetStore{}, err
	}
	// #nosec G304 -- path is derived from local CLI state root.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return paasTargetStore{}, nil
		}
		return paasTargetStore{}, err
	}
	var store paasTargetStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasTargetStore{}, fmt.Errorf("invalid target store: %w", err)
	}
	store.CurrentTarget = strings.TrimSpace(store.CurrentTarget)
	for i := range store.Targets {
		store.Targets[i] = normalizePaasTarget(store.Targets[i])
	}
	if store.CurrentTarget != "" && findPaasTarget(store, store.CurrentTarget) == -1 {
		store.CurrentTarget = ""
	}
	return store, nil
}

func savePaasTargetStore(contextName string, store paasTargetStore) error {
	path, err := resolvePaasTargetStorePath(contextName)
	if err != nil {
		return err
	}
	store.CurrentTarget = strings.TrimSpace(store.CurrentTarget)
	for i := range store.Targets {
		store.Targets[i] = normalizePaasTarget(store.Targets[i])
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func findPaasTarget(store paasTargetStore, name string) int {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return -1
	}
	return slices.IndexFunc(store.Targets, func(row paasTarget) bool {
		return strings.ToLower(strings.TrimSpace(row.Name)) == needle
	})
}

func normalizePaasTarget(target paasTarget) paasTarget {
	target.Name = strings.TrimSpace(target.Name)
	target.Host = strings.TrimSpace(target.Host)
	target.User = strings.TrimSpace(target.User)
	target.AuthMethod = normalizePaasAuthMethod(target.AuthMethod)
	if target.Port <= 0 {
		target.Port = 22
	}
	target.Labels = parseCSV(strings.Join(target.Labels, ","))
	target.IngressProvider = strings.ToLower(strings.TrimSpace(target.IngressProvider))
	target.IngressDomain = strings.ToLower(strings.TrimSpace(target.IngressDomain))
	target.IngressLBMode = normalizeIngressLBMode(target.IngressLBMode)
	target.IngressACMEEmail = strings.TrimSpace(target.IngressACMEEmail)
	target.CreatedAt = strings.TrimSpace(target.CreatedAt)
	target.UpdatedAt = strings.TrimSpace(target.UpdatedAt)
	return target
}

func normalizeIngressLBMode(value string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "", "dns":
		return "dns"
	case "l4", "lb", "loadbalancer", "load-balancer":
		return "l4"
	default:
		return mode
	}
}

func utcNowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
