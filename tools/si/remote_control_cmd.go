package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const remoteControlUsageText = "usage: si remote-control [--repo <path>] [--bin <path>] [--build] [--json] -- <remote-control-args...>\n       si remote-control <remote-control-args...>\n       si remote-control safari-smoke [--repo <path>] [--runner-bin <path>] [--build] -- [runner-args...]\n       si rc <remote-control-args...>"
const remoteControlConfigUsageText = "usage: si remote-control config <show|set> [args]"

var runRemoteControlExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

var runRemoteControlSafariSmokeExternal = func(bin string, args []string, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env
	return cmd.Run()
}

var resolveRemoteControlVaultKeyValue = resolveVaultKeyValue

func cmdRemoteControl(args []string) {
	if len(args) == 0 {
		printUsage(remoteControlUsageText)
		return
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "safari-smoke") {
		cmdRemoteControlSafariSmoke(args[1:])
		return
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		cmdRemoteControlConfig(args[1:])
		return
	}
	if len(args) == 1 {
		head := strings.TrimSpace(strings.ToLower(args[0]))
		if head == "help" || head == "-h" || head == "--help" {
			printUsage(remoteControlUsageText)
			return
		}
	}

	fs := flag.NewFlagSet("remote-control", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "remote-control repository path")
	bin := fs.String("bin", "", "remote-control binary path")
	build := fs.Bool("build", false, "build remote-control from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(remoteControlUsageText)
		fatal(err)
	}
	buildFlagSet := remoteControlFlagProvided(fs, "build")
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(remoteControlUsageText)
		return
	}

	settings := loadSettingsOrDefault()
	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = strings.TrimSpace(settings.RemoteControl.Repo)
	}
	if resolvedRepo == "" {
		resolvedRepo = defaultRemoteControlRepoPath()
	}

	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = strings.TrimSpace(settings.RemoteControl.Bin)
	}
	if resolvedBin == "" {
		resolvedBin = detectRemoteControlBinary(resolvedRepo)
	}

	buildRequested := *build
	if !buildFlagSet && settings.RemoteControl.Build != nil {
		buildRequested = *settings.RemoteControl.Build
	}
	if buildRequested {
		if err := buildRemoteControlBinary(resolvedRepo, resolvedBin); err != nil {
			if *jsonOut {
				printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin})
			}
			fatal(err)
		}
	}

	if _, err := os.Stat(resolvedBin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(fmt.Errorf("remote-control binary not found at %s (use --build or --bin)", resolvedBin))
		}
		fatal(err)
	}
	if err := runRemoteControlExternal(resolvedBin, rest); err != nil {
		if *jsonOut {
			printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin, "args": rest})
		}
		fatal(err)
	}
}

func cmdRemoteControlConfig(args []string) {
	if len(args) == 0 {
		printUsage(remoteControlConfigUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show", "get":
		cmdRemoteControlConfigShow(rest)
	case "set":
		cmdRemoteControlConfigSet(rest)
	default:
		fatal(fmt.Errorf("unknown remote-control config command: %s", sub))
	}
}

func cmdRemoteControlConfigShow(args []string) {
	fs := flag.NewFlagSet("remote-control config show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si remote-control config show [--json]"))
	}
	settings := loadSettingsOrDefault()
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "remote_control": settings.RemoteControl})
		return
	}
	fmt.Printf("si remote-control config\n")
	fmt.Printf("  repo=%s\n", strings.TrimSpace(settings.RemoteControl.Repo))
	fmt.Printf("  bin=%s\n", strings.TrimSpace(settings.RemoteControl.Bin))
	if settings.RemoteControl.Build != nil {
		fmt.Printf("  build=%t\n", *settings.RemoteControl.Build)
	} else {
		fmt.Printf("  build=\n")
	}
	fmt.Printf("  safari.ssh.host_env_key=%s\n", strings.TrimSpace(settings.RemoteControl.Safari.SSH.HostEnvKey))
	fmt.Printf("  safari.ssh.port_env_key=%s\n", strings.TrimSpace(settings.RemoteControl.Safari.SSH.PortEnvKey))
	fmt.Printf("  safari.ssh.user_env_key=%s\n", strings.TrimSpace(settings.RemoteControl.Safari.SSH.UserEnvKey))
}

func cmdRemoteControlConfigSet(args []string) {
	fs := flag.NewFlagSet("remote-control config set", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "default remote-control repo path")
	bin := fs.String("bin", "", "default remote-control binary path")
	build := fs.String("build", "", "default build behavior (true|false)")
	sshHostEnvKey := fs.String("ssh-host-env-key", "", "ssh host env key for safari smoke runner")
	sshPortEnvKey := fs.String("ssh-port-env-key", "", "ssh port env key for safari smoke runner")
	sshUserEnvKey := fs.String("ssh-user-env-key", "", "ssh user env key for safari smoke runner")
	sshHost := fs.String("ssh-host", "", "ssh host literal or env reference (env:KEY or ${KEY})")
	sshPort := fs.String("ssh-port", "", "ssh port literal or env reference (env:KEY or ${KEY})")
	sshUser := fs.String("ssh-user", "", "ssh user literal or env reference (env:KEY or ${KEY})")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si remote-control config set [--repo <path>] [--bin <path>] [--build true|false] [--ssh-host-env-key <key>] [--ssh-port-env-key <key>] [--ssh-user-env-key <key>] [--ssh-host <value>] [--ssh-port <value>] [--ssh-user <value>] [--json]"))
	}
	settings := loadSettingsOrDefault()
	updated, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		RepoProvided:          remoteControlFlagProvided(fs, "repo"),
		Repo:                  strings.TrimSpace(*repo),
		BinProvided:           remoteControlFlagProvided(fs, "bin"),
		Bin:                   strings.TrimSpace(*bin),
		BuildProvided:         remoteControlFlagProvided(fs, "build"),
		BuildRaw:              strings.TrimSpace(*build),
		SSHHostEnvKeyProvided: remoteControlFlagProvided(fs, "ssh-host-env-key"),
		SSHHostEnvKey:         strings.TrimSpace(*sshHostEnvKey),
		SSHPortEnvKeyProvided: remoteControlFlagProvided(fs, "ssh-port-env-key"),
		SSHPortEnvKey:         strings.TrimSpace(*sshPortEnvKey),
		SSHUserEnvKeyProvided: remoteControlFlagProvided(fs, "ssh-user-env-key"),
		SSHUserEnvKey:         strings.TrimSpace(*sshUserEnvKey),
		SSHHostProvided:       remoteControlFlagProvided(fs, "ssh-host"),
		SSHHost:               strings.TrimSpace(*sshHost),
		SSHPortProvided:       remoteControlFlagProvided(fs, "ssh-port"),
		SSHPort:               strings.TrimSpace(*sshPort),
		SSHUserProvided:       remoteControlFlagProvided(fs, "ssh-user"),
		SSHUser:               strings.TrimSpace(*sshUser),
	})
	if err != nil {
		fatal(err)
	}
	if !updated {
		fatal(errors.New("no settings provided; use one or more --repo/--bin/--build flags"))
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "remote_control": settings.RemoteControl})
		return
	}
	fmt.Println("si remote-control config set: updated")
}

type remoteControlConfigSetInput struct {
	RepoProvided bool
	Repo         string

	BinProvided bool
	Bin         string

	BuildProvided bool
	BuildRaw      string

	SSHHostEnvKeyProvided bool
	SSHHostEnvKey         string

	SSHPortEnvKeyProvided bool
	SSHPortEnvKey         string

	SSHUserEnvKeyProvided bool
	SSHUserEnvKey         string

	SSHHostProvided bool
	SSHHost         string

	SSHPortProvided bool
	SSHPort         string

	SSHUserProvided bool
	SSHUser         string
}

func applyRemoteControlConfigSet(settings *Settings, in remoteControlConfigSetInput) (bool, error) {
	if settings == nil {
		return false, errors.New("settings is nil")
	}
	changed := false
	if in.RepoProvided {
		settings.RemoteControl.Repo = strings.TrimSpace(in.Repo)
		changed = true
	}
	if in.BinProvided {
		settings.RemoteControl.Bin = strings.TrimSpace(in.Bin)
		changed = true
	}
	if in.BuildProvided {
		if strings.TrimSpace(in.BuildRaw) == "" {
			settings.RemoteControl.Build = nil
			changed = true
		} else {
			parsed, err := strconv.ParseBool(strings.TrimSpace(in.BuildRaw))
			if err != nil {
				return false, fmt.Errorf("invalid --build value %q (expected true|false)", in.BuildRaw)
			}
			settings.RemoteControl.Build = boolPtr(parsed)
			changed = true
		}
	}
	if in.SSHHostEnvKeyProvided {
		settings.RemoteControl.Safari.SSH.HostEnvKey = strings.TrimSpace(in.SSHHostEnvKey)
		changed = true
	}
	if in.SSHPortEnvKeyProvided {
		settings.RemoteControl.Safari.SSH.PortEnvKey = strings.TrimSpace(in.SSHPortEnvKey)
		changed = true
	}
	if in.SSHUserEnvKeyProvided {
		settings.RemoteControl.Safari.SSH.UserEnvKey = strings.TrimSpace(in.SSHUserEnvKey)
		changed = true
	}
	if in.SSHHostProvided {
		settings.RemoteControl.Safari.SSH.Host = strings.TrimSpace(in.SSHHost)
		changed = true
	}
	if in.SSHPortProvided {
		settings.RemoteControl.Safari.SSH.Port = strings.TrimSpace(in.SSHPort)
		changed = true
	}
	if in.SSHUserProvided {
		settings.RemoteControl.Safari.SSH.User = strings.TrimSpace(in.SSHUser)
		changed = true
	}
	return changed, nil
}

func remoteControlFlagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func defaultRemoteControlRepoPath() string {
	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "remote-control")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		cand := filepath.Join(home, "Development", "remote-control")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	return ""
}

func detectRemoteControlBinary(repo string) string {
	if p, err := exec.LookPath("remote-control"); err == nil {
		return p
	}
	if strings.TrimSpace(repo) == "" {
		return "remote-control"
	}
	return filepath.Join(repo, "bin", "remote-control")
}

func buildRemoteControlBinary(repo, out string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo is required for --build")
	}
	if out == "remote-control" {
		out = filepath.Join(repo, "bin", "remote-control")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", out, "./cmd/remote-control")
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdRemoteControlSafariSmoke(args []string) {
	fs := flag.NewFlagSet("remote-control safari-smoke", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "remote-control repository path")
	runnerBin := fs.String("runner-bin", "", "rc-safari-smoke binary path")
	build := fs.Bool("build", false, "build rc-safari-smoke from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(remoteControlUsageText)
		fatal(err)
	}
	buildFlagSet := remoteControlFlagProvided(fs, "build")
	rest := fs.Args()

	settings := loadSettingsOrDefault()
	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = strings.TrimSpace(settings.RemoteControl.Repo)
	}
	if resolvedRepo == "" {
		resolvedRepo = defaultRemoteControlRepoPath()
	}

	resolvedRunner := strings.TrimSpace(*runnerBin)
	if resolvedRunner == "" {
		resolvedRunner = detectRemoteControlSafariRunnerBinary(resolvedRepo)
	}
	buildRequested := *build
	if !buildFlagSet && settings.RemoteControl.Build != nil {
		buildRequested = *settings.RemoteControl.Build
	}
	if buildRequested {
		if err := buildRemoteControlSafariRunnerBinary(resolvedRepo, resolvedRunner); err != nil {
			if *jsonOut {
				printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "runner_bin": resolvedRunner})
			}
			fatal(err)
		}
	}
	if _, err := os.Stat(resolvedRunner); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(fmt.Errorf("rc-safari-smoke binary not found at %s (use --build or --runner-bin)", resolvedRunner))
		}
		fatal(err)
	}
	env := append([]string{}, os.Environ()...)
	env = applyRemoteControlSafariSmokeEnv(settings, env)
	if err := runRemoteControlSafariSmokeExternal(resolvedRunner, rest, env); err != nil {
		if *jsonOut {
			printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "runner_bin": resolvedRunner, "args": rest})
		}
		fatal(err)
	}
}

func detectRemoteControlSafariRunnerBinary(repo string) string {
	if p, err := exec.LookPath("rc-safari-smoke"); err == nil {
		return p
	}
	if strings.TrimSpace(repo) == "" {
		return "rc-safari-smoke"
	}
	return filepath.Join(repo, "bin", "rc-safari-smoke")
}

func buildRemoteControlSafariRunnerBinary(repo, out string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo is required for --build")
	}
	if out == "rc-safari-smoke" {
		out = filepath.Join(repo, "bin", "rc-safari-smoke")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", out, "./cmd/rc-safari-smoke")
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func applyRemoteControlSafariSmokeEnv(settings Settings, env []string) []string {
	env = ensureRemoteControlEnvValue(settings, env, "SHAWN_MAC_HOST", settings.RemoteControl.Safari.SSH.Host, settings.RemoteControl.Safari.SSH.HostEnvKey)
	env = ensureRemoteControlEnvValue(settings, env, "SHAWN_MAC_PORT", settings.RemoteControl.Safari.SSH.Port, settings.RemoteControl.Safari.SSH.PortEnvKey)
	env = ensureRemoteControlEnvValue(settings, env, "SHAWN_MAC_USER", settings.RemoteControl.Safari.SSH.User, settings.RemoteControl.Safari.SSH.UserEnvKey)
	return env
}

func ensureRemoteControlEnvValue(settings Settings, env []string, envName, rawValue, envKey string) []string {
	if envHasValue(env, envName) {
		return env
	}
	value := resolveRemoteControlConfigReference(settings, envKey, rawValue)
	if strings.TrimSpace(value) == "" {
		return env
	}
	return append(env, envName+"="+value)
}

func resolveRemoteControlConfigReference(settings Settings, envKey string, rawValue string) string {
	if v := resolveRemoteControlKeyValue(settings, envKey); v != "" {
		return v
	}
	raw := strings.TrimSpace(rawValue)
	if raw == "" {
		return ""
	}
	ref := raw
	if strings.HasPrefix(raw, "env:") {
		ref = strings.TrimSpace(strings.TrimPrefix(raw, "env:"))
	} else if strings.HasPrefix(raw, "${") && strings.HasSuffix(raw, "}") {
		ref = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "${"), "}"))
	}
	if v := resolveRemoteControlKeyValue(settings, ref); v != "" {
		return v
	}
	return raw
}

func resolveRemoteControlKeyValue(settings Settings, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if value, ok := resolveRemoteControlVaultKeyValue(settings, key); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
