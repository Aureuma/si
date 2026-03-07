package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const usageText = `Usage:
  tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome>
  tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome> --print
  tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome> --check`

var (
	headerPattern        = regexp.MustCompile(`^[[:space:]]*\[[^]]+\][[:space:]]*$`)
	loginHeaderPattern   = regexp.MustCompile(`^[[:space:]]*\[codex\.login\][[:space:]]*$`)
	defaultBrowserLineRE = regexp.MustCompile(`^[[:space:]]*default_browser[[:space:]]*=`)
)

type config struct {
	SettingsPath string
	Browser      string
	Mode         string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	cfg, showHelp, err := parseArgs(args)
	if showHelp {
		_, _ = fmt.Fprintln(stdout, usageText)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		_, _ = fmt.Fprintln(stderr, usageText)
		return 1
	}

	rendered, existing, err := render(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	switch cfg.Mode {
	case "print":
		_, _ = stdout.Write(rendered)
		return 0
	case "check":
		if existing == nil {
			return 1
		}
		if bytes.Equal(existing, rendered) {
			return 0
		}
		return 1
	default:
		if err := writeFileAtomic(cfg.SettingsPath, rendered, 0o644); err != nil {
			_, _ = fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		return 0
	}
}

func parseArgs(args []string) (config, bool, error) {
	fs := flag.NewFlagSet("install-si-settings-helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	settingsPath := fs.String("settings", "", "settings path")
	browser := fs.String("default-browser", "", "default browser")
	printMode := fs.Bool("print", false, "print mode")
	checkMode := fs.Bool("check", false, "check mode")
	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	if err := fs.Parse(args); err != nil {
		return config{}, false, err
	}
	if *help {
		return config{}, true, nil
	}
	if fs.NArg() > 0 {
		return config{}, false, fmt.Errorf("unknown argument: %s", strings.Join(fs.Args(), " "))
	}

	cfg := config{
		SettingsPath: strings.TrimSpace(*settingsPath),
		Browser:      strings.ToLower(strings.TrimSpace(*browser)),
		Mode:         "write",
	}
	if *printMode {
		cfg.Mode = "print"
	}
	if *checkMode {
		cfg.Mode = "check"
	}
	if cfg.SettingsPath == "" {
		return config{}, false, errors.New("--settings is required")
	}
	if cfg.Browser != "safari" && cfg.Browser != "chrome" {
		return config{}, false, errors.New("--default-browser must be safari or chrome")
	}
	return cfg, false, nil
}

func render(cfg config) ([]byte, []byte, error) {
	existing, err := os.ReadFile(cfg.SettingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []byte(defaultSettingsDoc(cfg.Browser)), nil, nil
		}
		return nil, nil, err
	}
	return []byte(renderSettings(string(existing), cfg.Browser)), existing, nil
}

func renderSettings(current string, browser string) string {
	lines := strings.Split(strings.ReplaceAll(current, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	out := make([]string, 0, len(lines)+4)
	inLogin := false
	sawLogin := false
	wrote := false
	replacement := fmt.Sprintf("default_browser = %q", browser)

	for _, line := range lines {
		if headerPattern.MatchString(line) {
			if inLogin && !wrote {
				out = append(out, replacement)
				wrote = true
			}
			if loginHeaderPattern.MatchString(line) {
				inLogin = true
				sawLogin = true
			} else {
				inLogin = false
			}
			out = append(out, line)
			continue
		}
		if inLogin && defaultBrowserLineRE.MatchString(line) {
			if !wrote {
				out = append(out, replacement)
				wrote = true
			}
			continue
		}
		out = append(out, line)
	}

	if sawLogin && !wrote {
		out = append(out, replacement)
	}
	if !sawLogin {
		out = append(out, "", "[codex.login]", replacement)
	}
	return strings.Join(out, "\n") + "\n"
}

func defaultSettingsDoc(browser string) string {
	return fmt.Sprintf("[codex.login]\ndefault_browser = %q\n", browser)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".install-si-settings-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
