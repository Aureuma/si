package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	codexDir    = "/home/si/.codex"
	ghConfigDir = "/home/si/.config/gh"
)

func main() {
	_ = os.MkdirAll(codexDir, 0o755)
	_ = os.MkdirAll(ghConfigDir, 0o755)
	_ = os.MkdirAll("/workspace", 0o755)

	if os.Geteuid() == 0 {
		applyHostIDs()
		syncCodexSkills()
		ensureCodexHomePermissions()
		if !isMountpoint("/home/si") {
			_ = os.Chown("/home/si", uidOf("si"), gidOf("si"))
		}
		configureGitSafeDirectories("root")
		configureGitSafeDirectories("si")
	}

	if err := maybeCloneRepo(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(0)
	}

	if os.Geteuid() == 0 {
		quoted := make([]string, 0, len(args))
		for _, arg := range args {
			quoted = append(quoted, shellQuote(arg))
		}
		cmdLine := strings.Join(quoted, " ")
		suPath, err := exec.LookPath("su")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		env := os.Environ()
		execArgs := []string{suPath, "-s", "/bin/bash", "si", "-c", cmdLine}
		if err := syscall.Exec(suPath, execArgs, env); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cmdPath, err := exec.LookPath(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := syscall.Exec(cmdPath, append([]string{cmdPath}, args[1:]...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func decodeMountinfoPath(encoded string) string {
	replacer := strings.NewReplacer(
		`\\040`, " ",
		`\\011`, "\t",
		`\\012`, "\n",
		`\\134`, `\\`,
	)
	return replacer.Replace(encoded)
}

func readMountpoints() map[string]struct{} {
	result := map[string]struct{}{}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return result
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 5 {
			continue
		}
		mountpoint := decodeMountinfoPath(fields[4])
		resolved := mountpoint
		if r, err := filepath.EvalSymlinks(mountpoint); err == nil {
			resolved = r
		}
		result[resolved] = struct{}{}
	}
	return result
}

func isMountpoint(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	resolved := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	}
	_, ok := readMountpoints()[resolved]
	return ok
}

func syncCodexSkills() {
	bundleDir := envOr("SI_CODEX_SKILLS_BUNDLE_DIR", "/opt/si/codex-skills")
	targetDir := "/home/si/.codex/skills"
	sharedDir := "/home/si/.si/codex/skills"

	_ = os.MkdirAll("/home/si/.codex", 0o755)
	_ = os.MkdirAll(targetDir, 0o755)
	if isDir(bundleDir) {
		_ = copyDirContents(bundleDir, targetDir)
	}
	if isDir("/home/si/.si") && !isMountpoint("/home/si/.si") {
		_ = os.MkdirAll(sharedDir, 0o755)
		_ = copyDirContents(targetDir, sharedDir)
	}
}

func ensureCodexHomePermissions() {
	_ = os.MkdirAll("/home/si/.codex", 0o755)
	_ = os.MkdirAll("/home/si/.codex/skills", 0o755)
	uid := uidOf("si")
	gid := gidOf("si")
	if uid < 0 || gid < 0 {
		return
	}
	_ = chownR("/home/si/.codex", uid, gid)
	_ = chownR("/home/si/.codex/skills", uid, gid)
}

func applyHostIDs() {
	uid := strings.TrimSpace(os.Getenv("SI_HOST_UID"))
	gid := strings.TrimSpace(os.Getenv("SI_HOST_GID"))
	if uid == "" || gid == "" || uid == "0" || gid == "0" {
		return
	}
	currentUID := strings.TrimSpace(runOutput("id", "-u", "si"))
	currentGID := strings.TrimSpace(runOutput("id", "-g", "si"))
	if currentUID == "" || currentGID == "" {
		return
	}

	groupName := firstField(runOutput("getent", "group", gid), ":")
	if groupName != "" && groupName != "si" {
		_ = runCmd("usermod", "-g", gid, "si")
	} else if currentGID != gid {
		_ = runCmd("groupmod", "-g", gid, "si")
	}

	userName := firstField(runOutput("getent", "passwd", uid), ":")
	if userName != "" && userName != "si" {
		return
	}
	if currentUID != uid {
		_ = runCmd("usermod", "-u", uid, "-g", gid, "si")
	}
}

func isGitRepoRoot(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if isDir(filepath.Join(path, ".git")) {
		return true
	}
	if fi, err := os.Stat(filepath.Join(path, ".git")); err == nil && !fi.IsDir() {
		return true
	}
	return false
}

func addGitSafeDirectory(identity string, path string) {
	if !isGitRepoRoot(path) {
		return
	}
	if identity == "root" {
		_ = runCmd("git", "config", "--global", "--add", "safe.directory", path)
		return
	}
	escaped := shellQuote(path)
	_ = runCmd("su", "-s", "/bin/bash", "si", "-c", "git config --global --add safe.directory "+escaped+" >/dev/null 2>&1 || true")
}

func configureGitSafeDirectories(identity string) {
	addGitSafeDirectory(identity, "/workspace")
	if cwd, err := os.Getwd(); err == nil {
		addGitSafeDirectory(identity, cwd)
	}

	for mount := range readMountpoints() {
		addGitSafeDirectory(identity, mount)
		if filepath.Base(mount) != "Development" || !isDir(mount) {
			continue
		}
		entries, err := os.ReadDir(mount)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				addGitSafeDirectory(identity, filepath.Join(mount, entry.Name()))
			}
		}
	}
}

func maybeCloneRepo() error {
	repo := strings.TrimSpace(os.Getenv("SI_REPO"))
	if repo == "" {
		return nil
	}
	repoName := repo
	if strings.Contains(repoName, "/") {
		parts := strings.Split(repoName, "/")
		repoName = parts[len(parts)-1]
	}
	targetDir := filepath.Join("/workspace", repoName)
	if exists(targetDir) && !isGitRepoRoot(targetDir) {
		return fmt.Errorf("clone target exists but is not a git repo: %s", targetDir)
	}
	if !isGitRepoRoot(targetDir) {
		_ = os.Setenv("GIT_TERMINAL_PROMPT", "0")
		token := firstNonEmpty(os.Getenv("SI_GH_PAT"), os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
		if token != "" {
			_ = os.Setenv("GH_TOKEN", token)
			_ = os.Setenv("GITHUB_TOKEN", token)
			if err := runCmdQuiet("gh", "repo", "clone", repo, targetDir); err != nil {
				if err := runCmd("git", "clone", "https://"+token+"@github.com/"+repo+".git", targetDir); err != nil {
					return fmt.Errorf("failed to clone repo: %s", repo)
				}
			}
		} else {
			if err := runCmd("git", "clone", "https://github.com/"+repo+".git", targetDir); err != nil {
				return fmt.Errorf("failed to clone repo: %s", repo)
			}
		}
	}
	if isGitRepoRoot(targetDir) {
		uid := uidOf("si")
		gid := gidOf("si")
		if uid >= 0 && gid >= 0 {
			_ = chownR(targetDir, uid, gid)
		}
	}
	return nil
}

func copyDirContents(src string, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
				continue
			}
			_ = copyDirContents(srcPath, dstPath)
			continue
		}
		_ = copyFile(srcPath, dstPath, info.Mode().Perm())
	}
	return nil
}

func copyFile(src string, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func chownR(root string, uid int, gid int) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		_ = os.Chown(path, uid, gid)
		return nil
	})
}

func uidOf(user string) int {
	out := strings.TrimSpace(runOutput("id", "-u", user))
	if out == "" {
		return -1
	}
	v, err := strconv.Atoi(out)
	if err != nil {
		return -1
	}
	return v
}

func gidOf(user string) int {
	out := strings.TrimSpace(runOutput("id", "-g", user))
	if out == "" {
		return -1
	}
	v, err := strconv.Atoi(out)
	if err != nil {
		return -1
	}
	return v
}

func firstField(value string, sep string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, sep, 2)
	return strings.TrimSpace(parts[0])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func envOr(name string, fallback string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	return v
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func runOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmdQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\r\"'`$\\!#&*()[]{};<>?|~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
