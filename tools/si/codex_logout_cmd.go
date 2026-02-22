package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type codexLogoutOptions struct {
	Home       string
	All        bool
	ProfileIDs []string
}

type codexLogoutResult struct {
	Removed   []string
	Skipped   []string
	Preserved []string
}

func cmdCodexLogout(args []string) {
	fs := flag.NewFlagSet("logout", flag.ExitOnError)
	force := fs.Bool("force", false, "delete without prompting")
	all := fs.Bool("all", false, "also remove si-managed codex auth cache under ~/.si/codex (in addition to ~/.codex)")
	_ = fs.Parse(args)
	if fs.NArg() != 0 {
		printUsage("usage: si logout [--force] [--all]")
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
		prompt := "Delete host codex cache at " + filepath.Join(home, ".codex")
		if *all {
			prompt += " and si-managed codex cache at " + filepath.Join(home, ".si", "codex")
		}
		prompt += "?"
		confirmed, ok := confirmYN(prompt, false)
		if !ok {
			fatal(fmt.Errorf("non-interactive; re-run with --force"))
		}
		if !confirmed {
			return
		}
	}

	res, err := codexLogout(codexLogoutOptions{
		Home:       home,
		All:        *all,
		ProfileIDs: codexProfileIDs(),
	})
	if err != nil {
		fatal(err)
	}

	if len(res.Removed) == 0 && len(res.Skipped) == 0 && len(res.Preserved) == 0 {
		successf("codex logout complete (nothing to remove)")
		return
	}
	for _, p := range res.Removed {
		successf("removed %s", p)
	}
	for _, p := range res.Preserved {
		successf("preserved %s", p)
	}
	for _, p := range res.Skipped {
		infof("skipped %s", p)
	}
}

func codexLogout(opts codexLogoutOptions) (codexLogoutResult, error) {
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		return codexLogoutResult{}, fmt.Errorf("home required")
	}
	res := codexLogoutResult{}

	codexDir := filepath.Join(home, ".codex")
	removed, preserved, err := resetCodexDirPreserveConfig(home, codexDir)
	if err != nil {
		return res, err
	}
	if removed {
		res.Removed = append(res.Removed, codexDir)
	} else {
		res.Skipped = append(res.Skipped, codexDir)
	}
	if preserved {
		res.Preserved = append(res.Preserved, filepath.Join(codexDir, "config.toml"))
	}

	if opts.All {
		blocked := make([]string, 0, len(opts.ProfileIDs)+8)
		blocked = append(blocked, opts.ProfileIDs...)
		blocked = append(blocked, discoverCachedCodexProfileIDs(home)...)

		siCodexDir := filepath.Join(home, ".si", "codex")
		removed, err := removeHomeChildDir(home, siCodexDir)
		if err != nil {
			return res, err
		}
		if removed {
			res.Removed = append(res.Removed, siCodexDir)
		} else {
			res.Skipped = append(res.Skipped, siCodexDir)
		}
		if err := addCodexLogoutBlockedProfiles(home, blocked); err != nil {
			return res, err
		}
	}
	return res, nil
}

func resetCodexDirPreserveConfig(home, codexDir string) (removed bool, preserved bool, err error) {
	home = filepath.Clean(strings.TrimSpace(home))
	codexDir = filepath.Clean(strings.TrimSpace(codexDir))
	configPath := filepath.Join(codexDir, "config.toml")

	var cfgData []byte
	cfgMode := fs.FileMode(0o600)
	if info, statErr := os.Lstat(configPath); statErr == nil {
		if info.Mode().IsRegular() {
			data, readErr := os.ReadFile(configPath)
			if readErr != nil {
				return false, false, readErr
			}
			cfgData = data
			cfgMode = info.Mode().Perm()
		}
	} else if !os.IsNotExist(statErr) {
		return false, false, statErr
	}

	removed, err = removeHomeChildDir(home, codexDir)
	if err != nil {
		return removed, false, err
	}
	if !removed || len(cfgData) == 0 {
		return removed, false, nil
	}
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		return removed, false, err
	}
	if cfgMode == 0 {
		cfgMode = 0o600
	}
	if err := os.WriteFile(configPath, cfgData, cfgMode); err != nil {
		return removed, false, err
	}
	return removed, true, nil
}

// removeHomeChildDir removes the given path if it exists and is a directory (or a symlink to a directory).
// It refuses to remove anything outside the provided home directory, and refuses obviously-dangerous paths.
func removeHomeChildDir(home, target string) (bool, error) {
	home = filepath.Clean(strings.TrimSpace(home))
	target = filepath.Clean(strings.TrimSpace(target))
	if home == "" || target == "" {
		return false, fmt.Errorf("invalid path")
	}
	if target == "/" || target == "." {
		return false, fmt.Errorf("refusing to remove %q", target)
	}

	// Ensure target is inside home.
	rel, err := filepath.Rel(home, target)
	if err != nil {
		return false, err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return false, fmt.Errorf("refusing to remove path outside home: %s", target)
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Remove the symlink itself (do not follow).
		if err := os.Remove(target); err != nil {
			return false, err
		}
		return true, nil
	}
	if !info.IsDir() {
		return false, fmt.Errorf("refusing to remove non-directory %s", target)
	}
	if err := os.RemoveAll(target); err != nil {
		return false, err
	}
	return true, nil
}
