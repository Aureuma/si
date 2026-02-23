package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

type codexSwapOptions struct {
	Home    string
	Profile codexProfile
}

type codexSwapResult struct {
	ProfileID string
	AuthSrc   string
	AuthDest  string
	Logout    codexLogoutResult
}

func cmdCodexSwap(args []string) {
	fs := flag.NewFlagSet("swap", flag.ExitOnError)
	force := fs.Bool("force", false, "swap without prompting (required for non-interactive use)")
	_ = fs.Parse(args)

	if fs.NArg() > 1 {
		printUsage("usage: si swap [profile] [--force]")
		return
	}
	profileKey := ""
	if fs.NArg() == 1 {
		profileKey = strings.TrimSpace(fs.Arg(0))
	}

	settings := loadSettingsOrDefault()
	defaultKey := codexDefaultProfileKey(settings)

	var profile codexProfile
	if profileKey != "" {
		parsed, err := requireCodexProfile(profileKey)
		if err != nil {
			fatal(err)
		}
		profile = parsed
	} else if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		selected, ok := selectCodexProfile("swap", defaultKey)
		if !ok {
			return
		}
		profile = selected
	} else {
		printUsage("usage: si swap [profile] [--force]")
		return
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = fmt.Errorf("home dir not available")
		}
		fatal(err)
	}

	if !*force {
		target := filepath.Join(home, ".codex")
		confirmed, ok := confirmYN(
			fmt.Sprintf("Swap host codex credentials to profile %s (this will reset %s except config.toml)?", profile.ID, target),
			false,
		)
		if !ok {
			fatal(fmt.Errorf("non-interactive; re-run with --force"))
		}
		if !confirmed {
			return
		}
	}

	res, err := codexSwap(codexSwapOptions{
		Home:    home,
		Profile: profile,
	})
	if err != nil {
		fatal(err)
	}
	if err := updateSettingsProfile(profile); err != nil {
		warnf("settings update failed: %v", err)
	}
	maybeSunAutoSyncProfile("swap", profile)
	successf("swapped host codex auth to profile %s", res.ProfileID)
}

func codexSwap(opts codexSwapOptions) (codexSwapResult, error) {
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		return codexSwapResult{}, fmt.Errorf("home required")
	}
	profileID := strings.TrimSpace(opts.Profile.ID)
	if profileID == "" {
		return codexSwapResult{}, fmt.Errorf("profile id required")
	}

	authSrc := filepath.Join(home, ".si", "codex", "profiles", profileID, "auth.json")
	// #nosec G304 -- authSrc is derived from local profile auth location.
	authBytes, err := os.ReadFile(authSrc)
	if err != nil {
		if os.IsNotExist(err) {
			if recoverCodexAuthCacheFromSun(opts.Profile, 10*time.Second) {
				authBytes, err = os.ReadFile(authSrc)
			}
			if err != nil {
				return codexSwapResult{}, fmt.Errorf("auth cache not found at %s; run `si login %s`", authSrc, profileID)
			}
		}
		if err != nil {
			return codexSwapResult{}, fmt.Errorf("read auth cache %s: %w", authSrc, err)
		}
	}
	if err := codexAuthValidationError(authSrc); err != nil {
		return codexSwapResult{}, fmt.Errorf("invalid auth cache at %s: %v", authSrc, err)
	}

	logoutRes, err := codexLogout(codexLogoutOptions{Home: home})
	if err != nil {
		return codexSwapResult{}, err
	}

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		return codexSwapResult{}, err
	}

	authDest := filepath.Join(codexDir, "auth.json")
	tmp, err := os.CreateTemp(codexDir, "auth-*.json")
	if err != nil {
		return codexSwapResult{}, err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return codexSwapResult{}, err
	}
	if _, err := tmp.Write(authBytes); err != nil {
		_ = tmp.Close()
		return codexSwapResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return codexSwapResult{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return codexSwapResult{}, err
	}
	if err := os.Rename(tmp.Name(), authDest); err != nil {
		return codexSwapResult{}, err
	}

	return codexSwapResult{
		ProfileID: profileID,
		AuthSrc:   authSrc,
		AuthDest:  authDest,
		Logout:    logoutRes,
	}, nil
}
