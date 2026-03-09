package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	shared "si/agents/shared/docker"

	"github.com/docker/docker/api/types"
)

const (
	fortDefaultPort                 = 8088
	fortDefaultTokenFileRelative    = ".si/fort/bootstrap/admin.token"
	fortProfileStateDirName         = "fort"
	fortProfileSessionStateFileName = "session.json"
	fortProfileAccessTokenFileName  = "access.token"
	fortProfileRefreshTokenFileName = "refresh.token"
)

type fortRequestAuth struct {
	BearerToken string
}

type fortSessionOpenResult struct {
	SessionID        string
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  string
	RefreshExpiresAt string
}

type fortSessionRefreshResult struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt string
}

type fortProfileSessionState struct {
	ProfileID        string `json:"profile_id"`
	AgentID          string `json:"agent_id"`
	SessionID        string `json:"session_id,omitempty"`
	Host             string `json:"host,omitempty"`
	ContainerHost    string `json:"container_host,omitempty"`
	AccessTokenPath  string `json:"access_token_path,omitempty"`
	RefreshTokenPath string `json:"refresh_token_path,omitempty"`
	AccessExpiresAt  string `json:"access_expires_at,omitempty"`
	RefreshExpiresAt string `json:"refresh_expires_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type codexFortBootstrap struct {
	ProfileID                 string
	AgentID                   string
	SessionID                 string
	HostURL                   string
	ContainerHostURL          string
	AccessTokenHostPath       string
	RefreshTokenHostPath      string
	AccessTokenContainerPath  string
	RefreshTokenContainerPath string
}

func (b codexFortBootstrap) env() []string {
	env := []string{}
	if strings.TrimSpace(b.ContainerHostURL) != "" {
		env = append(env, "FORT_HOST="+strings.TrimSpace(b.ContainerHostURL))
	}
	if strings.TrimSpace(b.AccessTokenContainerPath) != "" {
		env = append(env, "FORT_TOKEN_PATH="+strings.TrimSpace(b.AccessTokenContainerPath))
	}
	if strings.TrimSpace(b.RefreshTokenContainerPath) != "" {
		env = append(env, "FORT_REFRESH_TOKEN_PATH="+strings.TrimSpace(b.RefreshTokenContainerPath))
	}
	if strings.TrimSpace(b.AgentID) != "" {
		env = append(env, "FORT_AGENT_ID="+strings.TrimSpace(b.AgentID))
	}
	if strings.TrimSpace(b.ProfileID) != "" {
		env = append(env, "FORT_PROFILE_ID="+strings.TrimSpace(b.ProfileID))
	}
	return env
}

type fortBootstrapConfig struct {
	HostURL          string
	ContainerHostURL string
	BearerToken      string
}

type fortDockerHint struct {
	Name             string
	HostURL          string
	ContainerHostURL string
}

func ensureCodexProfileFortSession(ctx context.Context, client *shared.Client, profile codexProfile, preferredNetwork string) (codexFortBootstrap, error) {
	profileID := strings.TrimSpace(profile.ID)
	if profileID == "" {
		return codexFortBootstrap{}, fmt.Errorf("profile id required")
	}
	if !isValidSlug(profileID) {
		return codexFortBootstrap{}, fmt.Errorf("invalid profile id %q", profileID)
	}

	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		return codexFortBootstrap{}, err
	}
	cfg, err := resolveFortBootstrapConfig(ctx, client, preferredNetwork)
	if err != nil {
		return codexFortBootstrap{}, err
	}
	agentID := fortAgentIDForProfile(profileID)
	if err := fortEnsureAgent(ctx, cfg, agentID); err != nil {
		return codexFortBootstrap{}, err
	}
	if err := fortRequireAgentPolicyBindings(ctx, cfg, agentID); err != nil {
		return codexFortBootstrap{}, err
	}
	session, err := fortOpenSession(ctx, cfg, agentID)
	if err != nil {
		return codexFortBootstrap{}, err
	}
	if err := writeSecretFile(paths.AccessTokenHostPath, session.AccessToken); err != nil {
		return codexFortBootstrap{}, err
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, session.RefreshToken); err != nil {
		return codexFortBootstrap{}, err
	}
	state := fortProfileSessionState{
		ProfileID:        profileID,
		AgentID:          agentID,
		SessionID:        session.SessionID,
		Host:             cfg.HostURL,
		ContainerHost:    cfg.ContainerHostURL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
		AccessExpiresAt:  strings.TrimSpace(session.AccessExpiresAt),
		RefreshExpiresAt: strings.TrimSpace(session.RefreshExpiresAt),
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		return codexFortBootstrap{}, err
	}
	return codexFortBootstrap{
		ProfileID:                 profileID,
		AgentID:                   agentID,
		SessionID:                 session.SessionID,
		HostURL:                   cfg.HostURL,
		ContainerHostURL:          cfg.ContainerHostURL,
		AccessTokenHostPath:       paths.AccessTokenHostPath,
		RefreshTokenHostPath:      paths.RefreshTokenHostPath,
		AccessTokenContainerPath:  paths.AccessTokenContainerPath,
		RefreshTokenContainerPath: paths.RefreshTokenContainerPath,
	}, nil
}

func loadCodexFortBootstrapFromProfileState(profile codexProfile) (codexFortBootstrap, error) {
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		return codexFortBootstrap{}, err
	}
	state, err := loadFortProfileSessionState(paths.SessionStateHostPath)
	if err != nil {
		return codexFortBootstrap{}, err
	}
	profileID := strings.TrimSpace(profile.ID)
	if profileID == "" {
		profileID = strings.TrimSpace(state.ProfileID)
	}
	if profileID == "" {
		return codexFortBootstrap{}, fmt.Errorf("profile id required")
	}
	agentID := strings.TrimSpace(state.AgentID)
	if agentID == "" {
		agentID = fortAgentIDForProfile(profileID)
	}
	containerHostURL := strings.TrimSpace(state.ContainerHost)
	if containerHostURL == "" {
		containerHostURL = fortHostURLForContainer(strings.TrimSpace(state.Host))
	}
	if containerHostURL == "" {
		return codexFortBootstrap{}, fmt.Errorf("fort container host is missing in session state")
	}
	return codexFortBootstrap{
		ProfileID:                 profileID,
		AgentID:                   agentID,
		SessionID:                 strings.TrimSpace(state.SessionID),
		HostURL:                   strings.TrimSpace(state.Host),
		ContainerHostURL:          containerHostURL,
		AccessTokenHostPath:       paths.AccessTokenHostPath,
		RefreshTokenHostPath:      paths.RefreshTokenHostPath,
		AccessTokenContainerPath:  paths.AccessTokenContainerPath,
		RefreshTokenContainerPath: paths.RefreshTokenContainerPath,
	}, nil
}

type fortProfilePaths struct {
	ProfileRootHostPath       string
	FortRootHostPath          string
	AccessTokenHostPath       string
	RefreshTokenHostPath      string
	SessionStateHostPath      string
	AccessTokenContainerPath  string
	RefreshTokenContainerPath string
}

func fortProfileStatePaths(profile codexProfile) (fortProfilePaths, error) {
	profileDir, err := ensureCodexProfileDir(profile)
	if err != nil {
		return fortProfilePaths{}, err
	}
	fortDir := filepath.Join(profileDir, fortProfileStateDirName)
	if err := os.MkdirAll(fortDir, 0o700); err != nil {
		return fortProfilePaths{}, err
	}
	accessHost := filepath.Join(fortDir, fortProfileAccessTokenFileName)
	refreshHost := filepath.Join(fortDir, fortProfileRefreshTokenFileName)
	sessionHost := filepath.Join(fortDir, fortProfileSessionStateFileName)
	accessContainer, err := fortContainerPathFromHost(accessHost)
	if err != nil {
		return fortProfilePaths{}, err
	}
	refreshContainer, err := fortContainerPathFromHost(refreshHost)
	if err != nil {
		return fortProfilePaths{}, err
	}
	return fortProfilePaths{
		ProfileRootHostPath:       profileDir,
		FortRootHostPath:          fortDir,
		AccessTokenHostPath:       accessHost,
		RefreshTokenHostPath:      refreshHost,
		SessionStateHostPath:      sessionHost,
		AccessTokenContainerPath:  accessContainer,
		RefreshTokenContainerPath: refreshContainer,
	}, nil
}

func fortContainerPathFromHost(hostPath string) (string, error) {
	hostPath = filepath.Clean(strings.TrimSpace(hostPath))
	if hostPath == "" {
		return "", fmt.Errorf("host path required")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = fmt.Errorf("home dir not available")
		}
		return "", err
	}
	hostSiRoot := filepath.Join(home, ".si")
	rel, err := filepath.Rel(hostSiRoot, hostPath)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", fmt.Errorf("host path %s is outside %s", hostPath, hostSiRoot)
	}
	return filepath.ToSlash(filepath.Join("/home/si/.si", rel)), nil
}

func saveFortProfileSessionState(path string, state fortProfileSessionState) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("session path required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fortNormalizeFileOwnership(dir, 0o700)
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	fortNormalizeFileOwnership(path, 0o600)
	return nil
}

func loadFortProfileSessionState(path string) (fortProfileSessionState, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fortProfileSessionState{}, fmt.Errorf("session path required")
	}
	// #nosec G304 -- path is derived from local ~/.si profile state.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fortProfileSessionState{}, err
	}
	var state fortProfileSessionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fortProfileSessionState{}, err
	}
	state.ProfileID = strings.TrimSpace(state.ProfileID)
	state.AgentID = strings.TrimSpace(state.AgentID)
	state.SessionID = strings.TrimSpace(state.SessionID)
	state.Host = strings.TrimSpace(state.Host)
	state.ContainerHost = strings.TrimSpace(state.ContainerHost)
	state.AccessTokenPath = strings.TrimSpace(state.AccessTokenPath)
	state.RefreshTokenPath = strings.TrimSpace(state.RefreshTokenPath)
	state.AccessExpiresAt = strings.TrimSpace(state.AccessExpiresAt)
	state.RefreshExpiresAt = strings.TrimSpace(state.RefreshExpiresAt)
	state.UpdatedAt = strings.TrimSpace(state.UpdatedAt)
	return state, nil
}

func writeSecretFile(path string, value string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	value = strings.TrimSpace(value)
	if path == "" {
		return fmt.Errorf("secret path required")
	}
	if value == "" {
		return fmt.Errorf("secret value required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fortNormalizeFileOwnership(dir, 0o700)
	tmp, err := os.CreateTemp(dir, "fort-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	fortNormalizeFileOwnership(path, 0o600)
	return nil
}

func fortNormalizeFileOwnership(path string, mode os.FileMode) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return
	}
	_ = os.Chmod(path, mode)
	uid, gid, ok := fortDesiredFileOwnership()
	if !ok {
		return
	}
	if os.Geteuid() != 0 && (uid != os.Getuid() || gid != os.Getgid()) {
		return
	}
	_ = os.Chown(path, uid, gid)
}

func fortDesiredFileOwnership() (int, int, bool) {
	uid := os.Getuid()
	gid := os.Getgid()
	if parsed, ok := parsePositiveInt(strings.TrimSpace(os.Getenv(shared.HostUIDEnvKey))); ok {
		uid = parsed
	}
	if parsed, ok := parsePositiveInt(strings.TrimSpace(os.Getenv(shared.HostGIDEnvKey))); ok {
		gid = parsed
	}
	if uid <= 0 || gid <= 0 {
		return 0, 0, false
	}
	return uid, gid, true
}

func parsePositiveInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func readSecretFile(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	// #nosec G304 -- path is derived from local ~/.si profile state.
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func fortAgentIDForProfile(profileID string) string {
	profileID = strings.ToLower(strings.TrimSpace(profileID))
	if profileID == "" {
		return "si-codex-profile"
	}
	var b strings.Builder
	for _, r := range profileID {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if r == '-' || r == '_' || r == '.' {
			b.WriteRune('-')
			continue
		}
	}
	slug := strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
	if slug == "" {
		slug = "profile"
	}
	return "si-codex-" + slug
}

func resolveFortBootstrapConfig(ctx context.Context, client *shared.Client, preferredNetwork string) (fortBootstrapConfig, error) {
	discoverDocker := fortEnvBool("SI_FORT_DISCOVER_DOCKER")
	bearerToken, bearerErr := fortResolveBootstrapBearerToken()
	if bearerErr != nil {
		return fortBootstrapConfig{}, bearerErr
	}
	settings := loadSettingsOrDefault()
	cfg := fortBootstrapConfig{
		HostURL:          strings.TrimSpace(os.Getenv("FORT_HOST")),
		ContainerHostURL: strings.TrimSpace(os.Getenv("SI_FORT_CONTAINER_HOST")),
		BearerToken:      bearerToken,
	}
	if strings.TrimSpace(cfg.HostURL) == "" {
		cfg.HostURL = strings.TrimSpace(os.Getenv("SI_FORT_HOST"))
	}
	if strings.TrimSpace(cfg.HostURL) == "" {
		cfg.HostURL = strings.TrimSpace(settings.Fort.Host)
	}
	if strings.TrimSpace(cfg.ContainerHostURL) == "" {
		cfg.ContainerHostURL = strings.TrimSpace(settings.Fort.ContainerHost)
	}
	var hint fortDockerHint
	hintOK := false
	if discoverDocker {
		hint, hintOK = detectFortDockerHint(ctx, client, preferredNetwork)
		if strings.TrimSpace(cfg.HostURL) == "" && hintOK && strings.TrimSpace(hint.HostURL) != "" {
			cfg.HostURL = hint.HostURL
		}
	}
	cfg.HostURL = strings.TrimSpace(cfg.HostURL)
	if strings.TrimSpace(cfg.ContainerHostURL) == "" {
		if discoverDocker && hintOK && strings.TrimSpace(hint.ContainerHostURL) != "" {
			cfg.ContainerHostURL = strings.TrimSpace(hint.ContainerHostURL)
		} else {
			cfg.ContainerHostURL = strings.TrimSpace(cfg.HostURL)
		}
	}
	if strings.TrimSpace(cfg.HostURL) == "" {
		return fortBootstrapConfig{}, fmt.Errorf("fort host is required (set ~/.si/fort/settings.toml [fort].host or FORT_HOST)")
	}
	if err := fortValidateHostedURL(cfg.HostURL); err != nil {
		return fortBootstrapConfig{}, fmt.Errorf("invalid fort host %q: %w", cfg.HostURL, err)
	}
	if strings.TrimSpace(cfg.ContainerHostURL) == "" {
		cfg.ContainerHostURL = strings.TrimSpace(cfg.HostURL)
	}
	if err := fortValidateHostedURL(cfg.ContainerHostURL); err != nil {
		return fortBootstrapConfig{}, fmt.Errorf("invalid fort container host %q: %w", cfg.ContainerHostURL, err)
	}
	return cfg, nil
}

func fortResolveBootstrapBearerToken() (string, error) {
	tokenFile := strings.TrimSpace(os.Getenv("FORT_TOKEN_FILE"))
	if tokenFile == "" {
		tokenFile = fortDefaultTokenFilePath()
	}
	if tokenFile == "" {
		return "", fmt.Errorf("fort admin auth is required (set FORT_TOKEN_FILE)")
	}
	token, err := readStrictSecretFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("fort admin auth is required (token file %s): %w", tokenFile, err)
	}
	return token, nil
}

func fortDefaultTokenFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(fortDefaultTokenFileRelative))
}

func readStrictSecretFile(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}
	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		return "", fmt.Errorf("insecure permissions %03o (require 0600 or stricter)", perm)
	}
	// #nosec G304 -- path comes from local trusted config/env.
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	return token, nil
}

func fortEnvBool(key string) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func fortInsecureHostAllowed() bool {
	return fortEnvBool("SI_FORT_ALLOW_INSECURE_HOST")
}

func fortValidateHostedURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("host is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return fmt.Errorf("missing host")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		return fmt.Errorf("missing URL scheme")
	}
	if fortInsecureHostAllowed() {
		return nil
	}
	if scheme != "https" {
		return fmt.Errorf("scheme must be https (set SI_FORT_ALLOW_INSECURE_HOST=1 only for local tests)")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return fmt.Errorf("loopback hosts are not allowed in hosted mode")
	}
	return nil
}

func detectFortDockerHint(ctx context.Context, client *shared.Client, preferredNetwork string) (fortDockerHint, bool) {
	if client == nil {
		return fortDockerHint{}, false
	}
	containers, err := client.ListContainers(ctx, true, nil)
	if err != nil || len(containers) == 0 {
		return fortDockerHint{}, false
	}
	type candidate struct {
		hint  fortDockerHint
		score int
	}
	candidates := []candidate{}
	for _, item := range containers {
		if strings.TrimSpace(item.State) != "running" {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		_, info, err := client.ContainerByName(ctx, id)
		if err != nil || info == nil || info.Config == nil || info.NetworkSettings == nil {
			continue
		}
		score := 0
		containerName := strings.TrimPrefix(strings.TrimSpace(info.Name), "/")
		image := strings.ToLower(strings.TrimSpace(info.Config.Image))
		if strings.Contains(strings.ToLower(containerName), "fort") {
			score += 3
		}
		if strings.Contains(image, "fort") {
			score += 2
		}
		if envValue(info.Config.Env, "FORT_ADDR") != "" {
			score += 2
		}
		if score == 0 {
			continue
		}
		port := fortPortFromContainerEnv(info.Config.Env)
		networkName, ip := fortContainerNetworkIP(info, preferredNetwork)
		if ip == "" {
			continue
		}
		hostURL := fmt.Sprintf("http://%s:%d", ip, port)
		containerURL := ""
		if containerName != "" {
			containerURL = fmt.Sprintf("http://%s:%d", containerName, port)
		}
		if preferredNetwork != "" && networkName == preferredNetwork {
			score += 2
		}
		candidates = append(candidates, candidate{
			hint: fortDockerHint{
				Name:             containerName,
				HostURL:          hostURL,
				ContainerHostURL: containerURL,
			},
			score: score,
		})
	}
	if len(candidates) == 0 {
		return fortDockerHint{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].hint.Name < candidates[j].hint.Name
		}
		return candidates[i].score > candidates[j].score
	})
	return candidates[0].hint, true
}

func fortContainerNetworkIP(info *types.ContainerJSON, preferredNetwork string) (string, string) {
	if info == nil || info.NetworkSettings == nil || len(info.NetworkSettings.Networks) == 0 {
		return "", ""
	}
	preferredNetwork = strings.TrimSpace(preferredNetwork)
	if preferredNetwork != "" {
		if item, ok := info.NetworkSettings.Networks[preferredNetwork]; ok {
			ip := strings.TrimSpace(item.IPAddress)
			if ip != "" {
				return preferredNetwork, ip
			}
		}
	}
	keys := make([]string, 0, len(info.NetworkSettings.Networks))
	for name := range info.NetworkSettings.Networks {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		item := info.NetworkSettings.Networks[name]
		ip := strings.TrimSpace(item.IPAddress)
		if ip != "" {
			return name, ip
		}
	}
	return "", ""
}

func fortPortFromContainerEnv(env []string) int {
	raw := strings.TrimSpace(envValue(env, "FORT_ADDR"))
	if raw == "" {
		return fortDefaultPort
	}
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	idx := strings.LastIndex(raw, ":")
	if idx < 0 || idx+1 >= len(raw) {
		return fortDefaultPort
	}
	portRaw := strings.TrimSpace(raw[idx+1:])
	port, err := strconv.Atoi(portRaw)
	if err != nil || port <= 0 {
		return fortDefaultPort
	}
	return port
}

func fortHostURLForContainer(hostURL string) string {
	hostURL = strings.TrimSpace(hostURL)
	if hostURL == "" {
		settings := loadSettingsOrDefault()
		if strings.TrimSpace(settings.Fort.ContainerHost) != "" {
			return strings.TrimSpace(settings.Fort.ContainerHost)
		}
		return strings.TrimSpace(settings.Fort.Host)
	}
	u, err := url.Parse(hostURL)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return hostURL
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return hostURL
	}
	if host == "127.0.0.1" || host == "localhost" || host == "0.0.0.0" || host == "::1" {
		port := u.Port()
		if port == "" {
			port = strconv.Itoa(fortDefaultPort)
		}
		u.Host = "host.docker.internal:" + port
		return strings.TrimSpace(u.String())
	}
	return hostURL
}

func fortEnsureAgent(ctx context.Context, cfg fortBootstrapConfig, agentID string) error {
	if strings.TrimSpace(agentID) == "" {
		return fmt.Errorf("fort agent id is required")
	}
	auth := fortRequestAuth{
		BearerToken: strings.TrimSpace(cfg.BearerToken),
	}
	createBody := map[string]any{
		"id":     agentID,
		"type":   "workload",
		"status": "active",
	}
	status, body, err := fortAPIRequest(ctx, strings.TrimSpace(cfg.HostURL), http.MethodPost, "/v1/agents", createBody, auth)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusConflict {
		return fmt.Errorf("fort agent create failed (host=%s status=%d): %s", strings.TrimSpace(cfg.HostURL), status, fortAPIError(body))
	}
	status, body, err = fortAPIRequest(ctx, strings.TrimSpace(cfg.HostURL), http.MethodPost, "/v1/agents/"+url.PathEscape(agentID)+"/enable", map[string]any{}, auth)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("fort agent enable failed (host=%s status=%d): %s", strings.TrimSpace(cfg.HostURL), status, fortAPIError(body))
	}
	return nil
}

type fortPolicyBinding struct {
	Repo string   `json:"repo"`
	Env  string   `json:"env"`
	Ops  []string `json:"ops"`
}

func fortRequireAgentPolicyBindings(ctx context.Context, cfg fortBootstrapConfig, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("fort agent id is required")
	}
	auth := fortRequestAuth{
		BearerToken: strings.TrimSpace(cfg.BearerToken),
	}
	path := "/v1/agents/" + url.PathEscape(agentID) + "/policy"
	status, body, err := fortAPIRequest(ctx, strings.TrimSpace(cfg.HostURL), http.MethodGet, path, nil, auth)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("fort agent policy get failed (host=%s status=%d): %s", strings.TrimSpace(cfg.HostURL), status, fortAPIError(body))
	}
	bindings := fortPolicyBindingsFromBody(body)
	if !fortPolicyBindingsUsable(bindings) {
		return fmt.Errorf("fort agent %s has no usable policy bindings; policy must be provisioned explicitly", agentID)
	}
	return nil
}

func fortPolicyBindingsUsable(bindings []fortPolicyBinding) bool {
	for _, binding := range bindings {
		if strings.TrimSpace(binding.Repo) == "" || strings.TrimSpace(binding.Env) == "" {
			continue
		}
		for _, op := range binding.Ops {
			if strings.TrimSpace(op) != "" {
				return true
			}
		}
	}
	return false
}

func fortPolicyBindingsFromBody(body map[string]any) []fortPolicyBinding {
	if body == nil {
		return nil
	}
	raw, ok := body["bindings"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]fortPolicyBinding, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		repo := strings.TrimSpace(fmt.Sprint(entry["repo"]))
		env := strings.TrimSpace(fmt.Sprint(entry["env"]))
		opsAny, _ := entry["ops"].([]any)
		ops := make([]string, 0, len(opsAny))
		for _, opItem := range opsAny {
			op := strings.TrimSpace(fmt.Sprint(opItem))
			if op != "" {
				ops = append(ops, op)
			}
		}
		if repo == "" || env == "" || len(ops) == 0 {
			continue
		}
		out = append(out, fortPolicyBinding{
			Repo: repo,
			Env:  env,
			Ops:  ops,
		})
	}
	return out
}

func fortOpenSession(ctx context.Context, cfg fortBootstrapConfig, agentID string) (fortSessionOpenResult, error) {
	auth := fortRequestAuth{
		BearerToken: strings.TrimSpace(cfg.BearerToken),
	}
	reqBody := map[string]any{
		"agent_id":    strings.TrimSpace(agentID),
		"aud":         "fort-api",
		"access_ttl":  envOr("SI_FORT_ACCESS_TTL", "15m"),
		"refresh_ttl": envOr("SI_FORT_REFRESH_TTL", "168h"),
	}
	status, body, err := fortAPIRequest(ctx, strings.TrimSpace(cfg.HostURL), http.MethodPost, "/v1/auth/session/open", reqBody, auth)
	if err != nil {
		return fortSessionOpenResult{}, err
	}
	if status != http.StatusOK {
		return fortSessionOpenResult{}, fmt.Errorf("fort auth session open failed (status=%d): %s", status, fortAPIError(body))
	}
	result := fortSessionOpenResult{
		SessionID:        strings.TrimSpace(fmt.Sprint(body["session_id"])),
		AccessToken:      strings.TrimSpace(fmt.Sprint(body["access_token"])),
		RefreshToken:     strings.TrimSpace(fmt.Sprint(body["refresh_token"])),
		AccessExpiresAt:  strings.TrimSpace(fmt.Sprint(body["access_expires_at"])),
		RefreshExpiresAt: strings.TrimSpace(fmt.Sprint(body["refresh_expires_at"])),
	}
	if result.SessionID == "" || result.AccessToken == "" || result.RefreshToken == "" {
		return fortSessionOpenResult{}, fmt.Errorf("fort auth session open returned incomplete payload")
	}
	return result, nil
}

func fortRefreshSession(ctx context.Context, hostURL string, refreshToken string) (fortSessionRefreshResult, error) {
	reqBody := map[string]any{"refresh_token": strings.TrimSpace(refreshToken)}
	status, body, err := fortAPIRequest(ctx, strings.TrimSpace(hostURL), http.MethodPost, "/v1/auth/session/refresh", reqBody, fortRequestAuth{})
	if err != nil {
		return fortSessionRefreshResult{}, err
	}
	if status != http.StatusOK {
		return fortSessionRefreshResult{}, fmt.Errorf("fort auth session refresh failed (status=%d): %s", status, fortAPIError(body))
	}
	result := fortSessionRefreshResult{
		AccessToken:     strings.TrimSpace(fmt.Sprint(body["access_token"])),
		RefreshToken:    strings.TrimSpace(fmt.Sprint(body["refresh_token"])),
		AccessExpiresAt: strings.TrimSpace(fmt.Sprint(body["access_expires_at"])),
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		return fortSessionRefreshResult{}, fmt.Errorf("fort auth session refresh returned incomplete payload")
	}
	return result, nil
}

func fortAPIRequest(ctx context.Context, hostURL string, method string, apiPath string, body any, auth fortRequestAuth) (int, map[string]any, error) {
	hostURL = strings.TrimRight(strings.TrimSpace(hostURL), "/")
	if hostURL == "" {
		return 0, nil, fmt.Errorf("fort host is required")
	}
	apiPath = strings.TrimSpace(apiPath)
	if apiPath == "" {
		apiPath = "/"
	}
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, hostURL+apiPath, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(auth.BearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	bodyMap := map[string]any{}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&bodyMap)
	return resp.StatusCode, bodyMap, nil
}

func fortAPIError(body map[string]any) string {
	if body == nil {
		return "unknown error"
	}
	if msg := strings.TrimSpace(fmt.Sprint(body["error"])); msg != "" && msg != "<nil>" {
		return msg
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "unknown error"
	}
	return string(payload)
}

func fortSessionPathsFromEnv() (string, string, string, fortProfileSessionState) {
	profileID := strings.TrimSpace(os.Getenv("SI_CODEX_PROFILE_ID"))
	if !isValidSlug(profileID) {
		profileID = ""
	}
	home, _ := os.UserHomeDir()
	home = strings.TrimSpace(home)
	defaultTokenPath := strings.TrimSpace(os.Getenv("FORT_TOKEN_PATH"))
	defaultRefreshPath := strings.TrimSpace(os.Getenv("FORT_REFRESH_TOKEN_PATH"))
	sessionPath := ""
	state := fortProfileSessionState{}
	if profileID != "" && home != "" {
		base := filepath.Join(home, ".si", "codex", "profiles", profileID, fortProfileStateDirName)
		if defaultTokenPath == "" {
			defaultTokenPath = filepath.Join(base, fortProfileAccessTokenFileName)
		}
		if defaultRefreshPath == "" {
			defaultRefreshPath = filepath.Join(base, fortProfileRefreshTokenFileName)
		}
		sessionPath = filepath.Join(base, fortProfileSessionStateFileName)
		if loaded, err := loadFortProfileSessionState(sessionPath); err == nil {
			state = loaded
		}
	}
	return defaultTokenPath, defaultRefreshPath, sessionPath, state
}

func prepareFortRuntimeAuth(rest []string) (string, error) {
	tokenPath, refreshPath, sessionPath, state := fortSessionPathsFromEnv()
	settings := loadSettingsOrDefault()
	if tokenPath != "" {
		_ = os.Setenv("FORT_TOKEN_PATH", tokenPath)
	}
	if refreshPath != "" {
		_ = os.Setenv("FORT_REFRESH_TOKEN_PATH", refreshPath)
	}
	if strings.TrimSpace(os.Getenv("FORT_HOST")) == "" {
		host := strings.TrimSpace(state.ContainerHost)
		if host == "" {
			host = strings.TrimSpace(state.Host)
		}
		if host == "" {
			host = strings.TrimSpace(settings.Fort.ContainerHost)
		}
		if host == "" {
			host = strings.TrimSpace(settings.Fort.Host)
		}
		if strings.TrimSpace(host) != "" {
			if err := fortValidateHostedURL(host); err == nil {
				_ = os.Setenv("FORT_HOST", host)
			}
		}
	}
	accessToken := readSecretFile(tokenPath)
	refreshToken := readSecretFile(refreshPath)
	if fortShouldSkipAutoRefresh(rest) {
		return accessToken, nil
	}
	host := strings.TrimSpace(os.Getenv("FORT_HOST"))
	if host == "" || refreshToken == "" {
		return accessToken, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	refreshed, err := fortRefreshSession(ctx, host, refreshToken)
	if err != nil {
		// Keep existing token-file behavior intact and continue.
		return accessToken, nil
	}
	accessToken = strings.TrimSpace(refreshed.AccessToken)
	if tokenPath != "" {
		_ = writeSecretFile(tokenPath, accessToken)
	}
	if refreshPath != "" {
		_ = writeSecretFile(refreshPath, refreshed.RefreshToken)
	}
	if sessionPath != "" {
		state.AccessExpiresAt = strings.TrimSpace(refreshed.AccessExpiresAt)
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if state.Host == "" {
			state.Host = strings.TrimSpace(os.Getenv("FORT_HOST"))
		}
		_ = saveFortProfileSessionState(sessionPath, state)
	}
	return accessToken, nil
}

func fortShouldSkipAutoRefresh(rest []string) bool {
	if len(rest) < 2 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(rest[0]), "auth") &&
		strings.EqualFold(strings.TrimSpace(rest[1]), "session")
}
