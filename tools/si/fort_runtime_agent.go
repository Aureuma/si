package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	fortRuntimeAgentCommand      = "__fort-runtime-agent"
	fortRuntimeAgentRefreshRatio = 0.70
	fortRuntimeAgentMinSleep     = 5 * time.Second
	fortRuntimeAgentMaxBackoff   = 30 * time.Second
	fortRuntimeAgentJitter       = 10 * time.Second
)

type fortProfileRuntimeAgentState struct {
	ProfileID   string `json:"profile_id"`
	PID         int    `json:"pid"`
	CommandPath string `json:"command_path,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

var (
	fortRuntimeAgentStartProcess = startFortRuntimeAgentProcess
	fortRuntimeAgentProcessAlive = isFortRuntimeAgentProcessAlive
)

func cmdFortRuntimeAgent(args []string) {
	fs := flag.NewFlagSet(fortRuntimeAgentCommand, flag.ExitOnError)
	profileID := fs.String("profile", "", "profile id")
	once := fs.Bool("once", false, "refresh once then exit")
	_ = fs.Parse(args)
	if fs.NArg() != 0 || strings.TrimSpace(*profileID) == "" {
		fatal(fmt.Errorf("usage: si %s --profile <id> [--once]", fortRuntimeAgentCommand))
	}
	profile := codexProfile{ID: strings.TrimSpace(*profileID)}
	if !isValidSlug(profile.ID) {
		fatal(fmt.Errorf("invalid profile id %q", profile.ID))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := runFortRuntimeAgent(ctx, profile, *once); err != nil && !errors.Is(err, context.Canceled) {
		fatal(err)
	}
}

func runFortRuntimeAgent(ctx context.Context, profile codexProfile, once bool) error {
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		return err
	}
	if err := recordFortRuntimeAgentState(paths, profile, os.Getpid()); err != nil {
		return err
	}
	defer clearFortRuntimeAgentState(paths, os.Getpid())

	backoff := time.Second
	for {
		delay, err := fortRuntimeAgentStep(ctx, profile, paths)
		if err != nil {
			if fortStatusUnauthorized(err) {
				return err
			}
			if err := sleepFortRuntimeAgent(ctx, backoff); err != nil {
				return err
			}
			backoff *= 2
			if backoff > fortRuntimeAgentMaxBackoff {
				backoff = fortRuntimeAgentMaxBackoff
			}
			continue
		}
		backoff = time.Second
		if once {
			return nil
		}
		if err := sleepFortRuntimeAgent(ctx, delay); err != nil {
			return err
		}
	}
}

func fortRuntimeAgentStep(ctx context.Context, profile codexProfile, paths fortProfilePaths) (time.Duration, error) {
	lockFile, err := lockFortProfile(paths)
	if err != nil {
		return 0, err
	}
	defer unlockFortProfile(lockFile)

	if classification, delegated, err := maybeClassifyRustFortSessionState(paths.SessionStateHostPath, time.Now().UTC().Unix()); err != nil {
		return 0, err
	} else if delegated {
		switch classification.State {
		case "bootstrap_required", "closed":
			return 0, fmt.Errorf("fort session state requires bootstrap")
		case "revoked":
			reason := strings.TrimSpace(classification.Reason)
			if reason == "" {
				reason = "revoked"
			}
			return 0, fmt.Errorf("fort session state is %s", reason)
		}
	}
	boot, err := loadCodexFortBootstrapFromPaths(strings.TrimSpace(profile.ID), paths)
	if err != nil {
		return 0, err
	}
	hostURL := strings.TrimSpace(boot.HostURL)
	if hostURL == "" {
		return 0, fmt.Errorf("fort host is missing in bootstrap view")
	}
	if err := fortValidateHostedURL(hostURL); err != nil {
		return 0, fmt.Errorf("invalid fort host in bootstrap view %q: %w", hostURL, err)
	}

	if accessToken, err := readStrictSecretFile(paths.AccessTokenHostPath); err == nil {
		if needsRefresh, _ := fortTokenNeedsRefresh(accessToken); !needsRefresh {
			return fortRuntimeAgentNextDelay(accessToken), nil
		}
	}

	state, err := loadFortProfileSessionState(paths.SessionStateHostPath)
	if err != nil {
		return 0, err
	}

	refreshToken, err := readStrictSecretFile(paths.RefreshTokenHostPath)
	if err != nil {
		return 0, err
	}
	ctxRefresh := ctx
	if ctxRefresh == nil {
		var cancel context.CancelFunc
		ctxRefresh, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	refreshed, err := fortRefreshSession(ctxRefresh, hostURL, refreshToken)
	if err != nil {
		if fortStatusUnauthorized(err) {
			now := time.Now().UTC()
			if transition, delegated, transitionErr := maybeApplyRustFortUnauthorizedRefreshOutcome(paths.SessionStateHostPath, now); transitionErr != nil {
				return 0, transitionErr
			} else if delegated {
				if stateFromRust := transition.State; stateFromRust != (fortProfileSessionState{}) {
					state = stateFromRust
				}
				state.UpdatedAt = now.Format(time.RFC3339)
				if saveErr := saveFortProfileSessionState(paths.SessionStateHostPath, state); saveErr != nil {
					return 0, saveErr
				}
			}
		}
		return 0, err
	}
	if err := writeSecretFile(paths.AccessTokenHostPath, refreshed.AccessToken); err != nil {
		return 0, err
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, refreshed.RefreshToken); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	if transition, delegated, err := maybeApplyRustFortSessionRefreshOutcome(paths.SessionStateHostPath, refreshed, now); err != nil {
		return 0, err
	} else if delegated {
		if stateFromRust := transition.State; stateFromRust != (fortProfileSessionState{}) {
			state = stateFromRust
		}
		if classification := strings.TrimSpace(transition.Classification.State); classification != "" && classification != "resumable" {
			return 0, fmt.Errorf("unexpected rust fort refresh classification: %s", classification)
		}
	} else {
		state.AccessExpiresAt = strings.TrimSpace(refreshed.AccessExpiresAt)
	}
	state.ProfileID = strings.TrimSpace(profile.ID)
	state.AccessTokenPath = paths.AccessTokenHostPath
	state.RefreshTokenPath = paths.RefreshTokenHostPath
	state.UpdatedAt = now.Format(time.RFC3339)
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		return 0, err
	}
	return fortRuntimeAgentNextDelay(refreshed.AccessToken), nil
}

func fortRuntimeAgentNextDelay(accessToken string) time.Duration {
	ttl := 15 * time.Minute
	if expiry, err := fortTokenExpiry(accessToken); err == nil {
		ttl = time.Until(expiry)
	}
	if ttl <= 0 {
		return fortRuntimeAgentMinSleep
	}
	base := time.Duration(float64(ttl) * fortRuntimeAgentRefreshRatio)
	if base < fortRuntimeAgentMinSleep {
		base = fortRuntimeAgentMinSleep
	}
	jitterWindow := int64(fortRuntimeAgentJitter*2 + 1)
	jitter := time.Duration(rand.New(rand.NewSource(time.Now().UnixNano())).Int63n(jitterWindow)) - fortRuntimeAgentJitter
	base += jitter
	if base < fortRuntimeAgentMinSleep {
		base = fortRuntimeAgentMinSleep
	}
	ceiling := ttl - fortRuntimeAgentMinSleep
	if ceiling < fortRuntimeAgentMinSleep {
		ceiling = fortRuntimeAgentMinSleep
	}
	if base > ceiling {
		base = ceiling
	}
	return base
}

func fortTokenExpiry(token string) (time.Time, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return time.Time{}, fmt.Errorf("token is empty")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("token is not a JWT")
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return time.Time{}, err
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return time.Time{}, err
	}
	if claims.Exp <= 0 {
		return time.Time{}, fmt.Errorf("token exp claim missing")
	}
	return time.Unix(claims.Exp, 0).UTC(), nil
}

func ensureFortRuntimeAgentLocked(profile codexProfile, paths fortProfilePaths) error {
	state, err := loadFortRuntimeAgentState(paths.RuntimeAgentStateHostPath)
	if err == nil && state.PID > 0 && fortRuntimeAgentProcessAlive(state, profile) {
		return nil
	}
	started, err := fortRuntimeAgentStartProcess(profile, paths)
	if err != nil {
		return err
	}
	return saveFortRuntimeAgentState(paths.RuntimeAgentStateHostPath, started)
}

func startFortRuntimeAgentProcess(profile codexProfile, paths fortProfilePaths) (fortProfileRuntimeAgentState, error) {
	exe, err := os.Executable()
	if err != nil {
		return fortProfileRuntimeAgentState{}, err
	}
	if err := os.MkdirAll(filepath.Dir(paths.RuntimeAgentLogHostPath), 0o700); err != nil {
		return fortProfileRuntimeAgentState{}, err
	}
	logFile, err := os.OpenFile(paths.RuntimeAgentLogHostPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fortProfileRuntimeAgentState{}, err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, fortRuntimeAgentCommand, "--profile", strings.TrimSpace(profile.ID))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fortProfileRuntimeAgentState{}, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	now := time.Now().UTC().Format(time.RFC3339)
	return fortProfileRuntimeAgentState{
		ProfileID:   strings.TrimSpace(profile.ID),
		PID:         pid,
		CommandPath: exe,
		StartedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func isFortRuntimeAgentProcessAlive(state fortProfileRuntimeAgentState, profile codexProfile) bool {
	if state.PID <= 0 {
		return false
	}
	if strings.TrimSpace(state.ProfileID) != "" && !strings.EqualFold(strings.TrimSpace(state.ProfileID), strings.TrimSpace(profile.ID)) {
		return false
	}
	return syscall.Kill(state.PID, 0) == nil
}

func recordFortRuntimeAgentState(paths fortProfilePaths, profile codexProfile, pid int) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return saveFortRuntimeAgentState(paths.RuntimeAgentStateHostPath, fortProfileRuntimeAgentState{
		ProfileID:   strings.TrimSpace(profile.ID),
		PID:         pid,
		CommandPath: exe,
		StartedAt:   now,
		UpdatedAt:   now,
	})
}

func clearFortRuntimeAgentState(paths fortProfilePaths, pid int) {
	state, err := loadFortRuntimeAgentState(paths.RuntimeAgentStateHostPath)
	if err != nil || state.PID != pid {
		return
	}
	_ = clearFortRuntimeAgentStateFile(paths.RuntimeAgentStateHostPath)
}

func stopFortRuntimeAgentLocked(paths fortProfilePaths) error {
	state, err := loadFortRuntimeAgentState(paths.RuntimeAgentStateHostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}
	if state.PID > 0 {
		_ = syscall.Kill(state.PID, syscall.SIGTERM)
		deadline := time.Now().Add(3 * time.Second)
		for fortRuntimeAgentProcessAlive(state, codexProfile{ID: state.ProfileID}) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = clearFortRuntimeAgentStateFile(paths.RuntimeAgentStateHostPath)
	return nil
}

func closeCodexProfileFortSession(profile codexProfile) error {
	profileDir, err := codexProfileDir(profile)
	if err != nil {
		return err
	}
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	paths, err := fortProfilePathsFromProfileDir(profileDir, false)
	if err != nil {
		return err
	}
	lockFile, err := lockFortProfile(paths)
	if err != nil {
		return err
	}
	defer unlockFortProfile(lockFile)
	return closeCodexProfileFortSessionLocked(paths)
}

func closeCodexProfileFortSessionLocked(paths fortProfilePaths) error {
	_ = stopFortRuntimeAgentLocked(paths)
	if _, err := loadFortProfileSessionState(paths.SessionStateHostPath); err == nil {
		if classification, delegated, err := maybeRunRustFortSessionTeardown(paths.SessionStateHostPath, time.Now().UTC()); err != nil {
			return err
		} else if delegated {
			if stateValue := strings.TrimSpace(classification.State); stateValue != "" && stateValue != "closed" {
				return fmt.Errorf("unexpected rust fort teardown classification: %s", stateValue)
			}
		}
		boot, bootErr := loadCodexFortBootstrapFromPaths("", paths)
		if bootErr != nil {
			return bootErr
		}
		hostURL := strings.TrimSpace(boot.HostURL)
		if hostURL != "" {
			if refreshToken, refreshErr := readStrictSecretFile(paths.RefreshTokenHostPath); refreshErr == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				closeErr := fortCloseSessionByRefreshToken(ctx, hostURL, refreshToken)
				if closeErr != nil && !fortStatusUnauthorized(closeErr) {
					return closeErr
				}
			}
		}
	}
	for _, path := range []string{
		paths.AccessTokenHostPath,
		paths.RefreshTokenHostPath,
		paths.RuntimeAgentLogHostPath,
	} {
		_ = os.Remove(path)
	}
	_ = clearFortSessionStateFile(paths.SessionStateHostPath)
	_ = clearFortRuntimeAgentStateFile(paths.RuntimeAgentStateHostPath)
	return nil
}

func clearFortSessionStateFile(path string) error {
	if delegated, err := maybeClearRustFortSessionState(path); err != nil {
		return err
	} else if delegated {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func clearFortRuntimeAgentStateFile(path string) error {
	if delegated, err := maybeClearRustFortRuntimeAgentState(path); err != nil {
		return err
	} else if delegated {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func writeFortStateFile(path string, value any) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("state path required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fortNormalizeFileOwnership(dir, 0o700)
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, "fort-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	fortNormalizeFileOwnership(path, 0o600)
	return nil
}

func readFortStateFile(path string, out any) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("state path required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("insecure permissions %03o (require 0600 or stricter)", perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func saveFortRuntimeAgentState(path string, state fortProfileRuntimeAgentState) error {
	if delegated, err := maybeSaveRustFortRuntimeAgentState(path, state); err != nil {
		return err
	} else if delegated {
		return nil
	}
	return writeFortStateFile(path, state)
}

func loadFortRuntimeAgentState(path string) (fortProfileRuntimeAgentState, error) {
	if state, delegated, err := maybeLoadRustFortRuntimeAgentState(path); err != nil {
		return fortProfileRuntimeAgentState{}, err
	} else if delegated {
		return state, nil
	}
	var state fortProfileRuntimeAgentState
	if err := readFortStateFile(path, &state); err != nil {
		return fortProfileRuntimeAgentState{}, err
	}
	state.ProfileID = strings.TrimSpace(state.ProfileID)
	state.CommandPath = strings.TrimSpace(state.CommandPath)
	state.StartedAt = strings.TrimSpace(state.StartedAt)
	state.UpdatedAt = strings.TrimSpace(state.UpdatedAt)
	return state, nil
}

func lockFortProfile(paths fortProfilePaths) (*os.File, error) {
	lockPath := filepath.Clean(strings.TrimSpace(paths.RuntimeLockHostPath))
	if lockPath == "" {
		return nil, fmt.Errorf("fort runtime lock path required")
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	fortNormalizeFileOwnership(lockPath, 0o600)
	return file, nil
}

func unlockFortProfile(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func fortStatusUnauthorized(err error) bool {
	var statusErr *fortStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.Status == http.StatusUnauthorized || statusErr.Status == http.StatusForbidden
}

func fortCloseSessionByRefreshToken(ctx context.Context, hostURL string, refreshToken string) error {
	reqBody := map[string]any{"refresh_token": strings.TrimSpace(refreshToken)}
	status, body, err := fortAPIRequest(ctx, strings.TrimSpace(hostURL), http.MethodPost, "/v1/auth/session/close", reqBody, fortRequestAuth{})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return &fortStatusError{Operation: "auth session close", Status: status, Message: fortAPIError(body)}
	}
	return nil
}

func sleepFortRuntimeAgent(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func fortProfilePathsFromProfileDir(profileDir string, ensureDir bool) (fortProfilePaths, error) {
	profileDir = filepath.Clean(strings.TrimSpace(profileDir))
	if profileDir == "" {
		return fortProfilePaths{}, fmt.Errorf("profile dir required")
	}
	fortDir := filepath.Join(profileDir, fortProfileStateDirName)
	if ensureDir {
		if err := os.MkdirAll(fortDir, 0o700); err != nil {
			return fortProfilePaths{}, err
		}
	}
	accessHost := filepath.Join(fortDir, fortProfileAccessTokenFileName)
	refreshHost := filepath.Join(fortDir, fortProfileRefreshTokenFileName)
	sessionHost := filepath.Join(fortDir, fortProfileSessionStateFileName)
	runtimeLockHost := filepath.Join(fortDir, fortProfileRuntimeLockFileName)
	runtimeStateHost := filepath.Join(fortDir, fortProfileRuntimeStateFileName)
	runtimeLogHost := filepath.Join(fortDir, fortProfileRuntimeLogFileName)
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
		RuntimeLockHostPath:       runtimeLockHost,
		RuntimeAgentStateHostPath: runtimeStateHost,
		RuntimeAgentLogHostPath:   runtimeLogHost,
		AccessTokenContainerPath:  accessContainer,
		RefreshTokenContainerPath: refreshContainer,
	}, nil
}
