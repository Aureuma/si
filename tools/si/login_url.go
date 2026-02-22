package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unicode"
)

var (
	loginURLRegex          = regexp.MustCompile(`https?://[^\s"'<>]+`)
	ansiEscapeRegex        = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	ansiEscapeOSCRegex     = regexp.MustCompile(`\x1b\][^\x07]*\x07`)
	ansiEscapeEncodedRegex = regexp.MustCompile(`(?i)%1b%5b[0-9;]*[a-z]`)
	deviceCodeRegex        = regexp.MustCompile(`\b[A-Z0-9]{4,8}-[A-Z0-9]{4,8}\b`)
)

var (
	runShellCommandFn      = runShellCommand
	openSafariProfileURLFn = openSafariProfileURL
	openChromeProfileURLFn = openChromeProfileURL
	chromeProfilesFn       = chromeProfiles
)

type loginURLWatcher struct {
	mu     sync.Mutex
	buf    []byte
	opened bool
	opener func(string)
	code   bool
	copier func(string)
}

func newLoginURLWatcher(opener func(string), copier func(string)) *loginURLWatcher {
	return &loginURLWatcher{opener: opener, copier: copier}
}

func (w *loginURLWatcher) Feed(chunk []byte) {
	if w == nil || len(chunk) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, chunk...)
	cleaned := stripANSIEscapes(string(w.buf))
	matches := loginURLRegex.FindAllString(cleaned, -1)
	if !w.opened && w.opener != nil && len(matches) > 0 {
		if target := pickLoginURL(matches); target != "" {
			w.opened = true
			go w.opener(target)
		}
	}
	if !w.code && w.copier != nil {
		codes := deviceCodeRegex.FindAllString(cleaned, -1)
		if len(codes) > 0 {
			code := strings.TrimSpace(codes[len(codes)-1])
			if code != "" {
				w.code = true
				go w.copier(code)
			}
		}
	}
	if len(w.buf) > 2048 {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-1024:]...)
	}
}

func pickLoginURL(urls []string) string {
	fallback := ""
	for _, raw := range urls {
		cleaned := cleanLoginURL(raw)
		if cleaned == "" {
			continue
		}
		if strings.HasPrefix(cleaned, "https://") {
			return cleaned
		}
		if fallback == "" {
			fallback = cleaned
		}
	}
	return fallback
}

func cleanLoginURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = stripANSIEscapes(trimmed)
	return strings.TrimRight(trimmed, ".,);]>\"'")
}

func stripANSIEscapes(value string) string {
	value = ansiEscapeRegex.ReplaceAllString(value, "")
	value = ansiEscapeOSCRegex.ReplaceAllString(value, "")
	value = ansiEscapeEncodedRegex.ReplaceAllString(value, "")
	return value
}

func openLoginURL(url string, profile codexProfile, command string, safariProfileOverride string) {
	url = cleanLoginURL(url)
	if url == "" {
		return
	}
	cmdTemplate := strings.TrimSpace(command)
	if cmdTemplate == "" {
		cmdTemplate = defaultLoginOpenCommand()
	}
	if cmdTemplate == "" {
		warnf("open login url skipped: no opener configured")
		return
	}
	if isSafariProfileCommand(cmdTemplate) {
		if err := openSafariProfileURLFn(url, profile, safariProfileOverride); err != nil {
			warnf("open login url failed: %v", err)
			if hint := safariAccessibilityHint(err); hint != "" {
				warnf("%s", hint)
			}
		}
		return
	}
	if isChromeProfileCommand(cmdTemplate) {
		if err := openChromeProfileURLFn(url, profile); err != nil {
			warnf("open login url failed: %v", err)
		}
		return
	}
	cmdLine := expandLoginOpenCommand(cmdTemplate, url, profile)
	if !strings.Contains(cmdTemplate, "{url}") {
		cmdLine = strings.TrimSpace(cmdLine + " " + shellSingleQuote(url))
	}
	if err := runShellCommandFn(cmdLine); err != nil {
		warnf("open login url failed: %v", err)
	}
}

func expandLoginOpenCommand(template string, url string, profile codexProfile) string {
	out := template
	replacements := map[string]string{
		"{url}":           shellSingleQuote(url),
		"{profile}":       shellSingleQuote(profile.ID),
		"{profile_id}":    shellSingleQuote(profile.ID),
		"{profile_name}":  shellSingleQuote(profile.Name),
		"{profile_email}": shellSingleQuote(profile.Email),
	}
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}

func defaultLoginOpenCommand() string {
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "linux":
		return "xdg-open"
	case "windows":
		return "cmd /c start"
	default:
		return ""
	}
}

func runShellCommand(cmdLine string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdLine)
	} else {
		cmd = exec.Command("sh", "-lc", cmdLine)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyDeviceCodeToClipboard(code string) {
	code = strings.TrimSpace(stripANSIEscapes(code))
	if code == "" {
		return
	}
	if err := copyToClipboard(code); err != nil {
		warnf("copy device code failed: %v", err)
		return
	}
	successf("📋 copied device one-time code to clipboard")
}

func loginOpenCommandForBrowser(browser string) string {
	switch normalizeLoginDefaultBrowser(browser) {
	case "safari":
		return "safari-profile"
	case "chrome":
		return "chrome-profile"
	default:
		return ""
	}
}

func normalizeLoginDefaultBrowser(browser string) string {
	switch strings.ToLower(strings.TrimSpace(browser)) {
	case "safari":
		return "safari"
	case "chrome":
		return "chrome"
	default:
		return ""
	}
}

func isLikelyHeadlessMachine() bool {
	if forced, ok := boolEnv("SI_HEADLESS"); ok {
		return forced
	}
	if forced, ok := boolEnv("HEADLESS"); ok {
		return forced
	}
	if forced, ok := boolEnv("CI"); ok && forced {
		return true
	}
	if runtime.GOOS == "linux" {
		display := strings.TrimSpace(os.Getenv("DISPLAY"))
		wayland := strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY"))
		if display == "" && wayland == "" {
			return true
		}
	}
	return false
}

func boolEnv(key string) (bool, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return false, false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off", "":
		return false, true
	default:
		return false, false
	}
}

func copyToClipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		return runClipboardCmd("pbcopy", text)
	case "linux":
		if path, err := exec.LookPath("wl-copy"); err == nil {
			return runClipboardCmd(path, text)
		}
		if path, err := exec.LookPath("xclip"); err == nil {
			return runClipboardCmd(path, text, "-selection", "clipboard")
		}
		if path, err := exec.LookPath("xsel"); err == nil {
			return runClipboardCmd(path, text, "--clipboard", "--input")
		}
		return fmt.Errorf("no clipboard tool found (install wl-copy, xclip, or xsel)")
	case "windows":
		return runClipboardCmd("cmd", text, "/c", "clip")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
}

func runClipboardCmd(cmdPath string, text string, args ...string) error {
	cmd := exec.Command(cmdPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		_ = stdin.Close()
		return err
	}
	if err := stdin.Close(); err != nil {
		return err
	}
	return cmd.Wait()
}

func isSafariProfileCommand(command string) bool {
	command = strings.TrimSpace(strings.ToLower(command))
	return command == "safari-profile" || command == "safari-profile-window"
}

func isChromeProfileCommand(command string) bool {
	command = strings.TrimSpace(strings.ToLower(command))
	return command == "chrome-profile"
}

func openSafariProfileURL(url string, profile codexProfile, override string) error {
	if runtime.GOOS != "darwin" {
		cmd := strings.TrimSpace(defaultLoginOpenCommand())
		if cmd == "" {
			return nil
		}
		return runShellCommandFn(cmd + " " + shellSingleQuote(url))
	}
	candidates := safariProfileNameCandidates(profile, override)
	if len(candidates) == 0 {
		return runShellCommandFn("open -a \"Safari\" " + shellSingleQuote(url))
	}
	menuItems, err := safariProfileMenuItems()
	if err != nil {
		_ = runShellCommandFn("open -a \"Safari\" " + shellSingleQuote(url))
		return err
	}
	profileName := ""
	for _, candidate := range candidates {
		if menuItems["New "+candidate+" Window"] {
			profileName = candidate
			break
		}
	}
	if profileName == "" {
		return runShellCommandFn("open -a \"Safari\" " + shellSingleQuote(url))
	}
	script := []string{
		"tell application \"Safari\" to activate",
		"set __si_profile to " + appleScriptString(profileName),
		"set __si_url to " + appleScriptString(url),
		"tell application \"System Events\"",
		"  tell process \"Safari\"",
		"    click menu item (\"New \" & __si_profile & \" Window\") of menu 1 of menu item \"New Window\" of menu \"File\" of menu bar 1",
		"  end tell",
		"end tell",
		"delay 0.2",
		"tell application \"Safari\" to open location __si_url",
	}
	cmdArgs := make([]string, 0, len(script)*2)
	for _, line := range script {
		cmdArgs = append(cmdArgs, "-e", line)
	}
	// #nosec G204 -- osascript command and generated script lines are controlled locally.
	out, err := exec.Command("osascript", cmdArgs...).CombinedOutput()
	if len(out) > 0 {
		_, _ = os.Stderr.Write(out)
	}
	if err != nil {
		_ = runShellCommandFn("open -a \"Safari\" " + shellSingleQuote(url))
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return fmt.Errorf("osascript safari profile automation failed: %s", trimmed)
		}
		return fmt.Errorf("osascript safari profile automation failed: %w", err)
	}
	return nil
}

func safariAccessibilityHint(err error) string {
	return safariAccessibilityHintForOS(runtime.GOOS, err)
}

func safariAccessibilityHintForOS(goos string, err error) string {
	if goos != "darwin" || err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "system events") ||
		strings.Contains(msg, "accessibility") ||
		strings.Contains(msg, "not allowed assistive access") ||
		strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "not permitted") {
		return "Safari profile automation was blocked. Grant Accessibility permission to your terminal in System Settings > Privacy & Security > Accessibility, then retry `si login`. SI already fell back to opening Safari without profile selection."
	}
	return "Safari profile opening failed. If you have not granted Accessibility permission to your terminal, enable it in System Settings > Privacy & Security > Accessibility, then retry `si login`."
}

func openChromeProfileURL(url string, profile codexProfile) error {
	url = cleanLoginURL(url)
	if url == "" {
		return nil
	}
	profiles := chromeProfilesFn()
	dir := selectChromeProfileDir(profiles, profile)
	switch runtime.GOOS {
	case "darwin":
		if dir != "" {
			return runShellCommandFn("open -a \"Google Chrome\" --args --profile-directory=" + shellSingleQuote(dir) + " " + shellSingleQuote(url))
		}
		return runShellCommandFn("open -a \"Google Chrome\" " + shellSingleQuote(url))
	case "linux":
		chromeCmd := "google-chrome"
		if _, err := exec.LookPath(chromeCmd); err != nil {
			chromeCmd = "chromium-browser"
		}
		if _, err := exec.LookPath(chromeCmd); err == nil {
			if dir != "" {
				return runShellCommandFn(chromeCmd + " --profile-directory=" + shellSingleQuote(dir) + " " + shellSingleQuote(url))
			}
			return runShellCommandFn(chromeCmd + " " + shellSingleQuote(url))
		}
		return runShellCommandFn("xdg-open " + shellSingleQuote(url))
	case "windows":
		if dir != "" {
			return runShellCommandFn("cmd /c start \"\" chrome --profile-directory=" + shellSingleQuote(dir) + " " + shellSingleQuote(url))
		}
		return runShellCommandFn("cmd /c start \"\" chrome " + shellSingleQuote(url))
	default:
		cmd := strings.TrimSpace(defaultLoginOpenCommand())
		if cmd == "" {
			return nil
		}
		return runShellCommandFn(cmd + " " + shellSingleQuote(url))
	}
}

type chromeProfileMeta struct {
	Directory string
	Name      string
	UserName  string
}

func chromeProfiles() map[string]chromeProfileMeta {
	path := chromeLocalStatePath()
	if strings.TrimSpace(path) == "" {
		return map[string]chromeProfileMeta{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]chromeProfileMeta{}
	}
	var parsed struct {
		Profile struct {
			InfoCache map[string]struct {
				Name         string `json:"name"`
				UserName     string `json:"user_name"`
				ShortcutName string `json:"shortcut_name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return map[string]chromeProfileMeta{}
	}
	out := map[string]chromeProfileMeta{}
	for dir, entry := range parsed.Profile.InfoCache {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = strings.TrimSpace(entry.ShortcutName)
		}
		out[dir] = chromeProfileMeta{
			Directory: dir,
			Name:      name,
			UserName:  strings.TrimSpace(entry.UserName),
		}
	}
	return out
}

func chromeLocalStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Local State")
	case "linux":
		return filepath.Join(home, ".config", "google-chrome", "Local State")
	case "windows":
		localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if localAppData == "" {
			return ""
		}
		return filepath.Join(localAppData, "Google", "Chrome", "User Data", "Local State")
	default:
		return ""
	}
}

func selectChromeProfileDir(profiles map[string]chromeProfileMeta, profile codexProfile) string {
	if len(profiles) == 0 {
		return ""
	}
	candidates := safariProfileNameCandidates(profile, "")
	normalized := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(strings.ToLower(candidate))
		if candidate != "" {
			normalized = append(normalized, candidate)
		}
	}
	for _, candidate := range normalized {
		for dir, entry := range profiles {
			if strings.EqualFold(strings.TrimSpace(dir), candidate) {
				return dir
			}
			if strings.EqualFold(strings.TrimSpace(entry.Name), candidate) {
				return dir
			}
			if strings.EqualFold(strings.TrimSpace(entry.UserName), candidate) {
				return dir
			}
		}
	}
	if _, ok := profiles["Default"]; ok {
		return "Default"
	}
	return ""
}

func safariProfileNameCandidates(profile codexProfile, override string) []string {
	override = strings.TrimSpace(override)
	if override != "" {
		return []string{override}
	}
	candidates := make([]string, 0, 3)
	if name := strings.TrimSpace(profile.Name); name != "" {
		candidates = append(candidates, name)
		if stripped := strings.TrimSpace(stripLeadingNonAlnum(name)); stripped != "" && stripped != name {
			candidates = append(candidates, stripped)
		}
	}
	if id := strings.TrimSpace(profile.ID); id != "" {
		candidates = append(candidates, id)
	}
	return candidates
}

func appleScriptString(value string) string {
	if value == "" {
		return "\"\""
	}
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func stripLeadingNonAlnum(value string) string {
	return strings.TrimLeftFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func safariProfileMenuItems() (map[string]bool, error) {
	script := []string{
		"tell application \"Safari\" to activate",
		"delay 0.5",
		"tell application \"System Events\" to tell process \"Safari\" to get name of menu items of menu 1 of menu item \"New Window\" of menu \"File\" of menu bar 1",
	}
	cmdArgs := make([]string, 0, len(script)*2)
	for _, line := range script {
		cmdArgs = append(cmdArgs, "-e", line)
	}
	// #nosec G204 -- osascript command and generated script lines are controlled locally.
	out, err := exec.Command("osascript", cmdArgs...).CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return nil, fmt.Errorf("osascript safari menu query failed: %s", trimmed)
		}
		return nil, fmt.Errorf("osascript safari menu query failed: %w", err)
	}
	items := map[string]bool{}
	for _, item := range strings.Split(string(out), ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			items[trimmed] = true
		}
	}
	return items, nil
}
