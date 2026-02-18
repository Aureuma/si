package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const browserUsageText = "usage: si browser <build|start|stop|status|logs|proxy> [args...]"

const (
	defaultBrowserImage      = "si-playwright-mcp-headed:1.58.2"
	defaultBrowserContainer  = "si-playwright-mcp-headed"
	defaultBrowserHostBind   = "127.0.0.1"
	defaultBrowserMCPPort    = 8931
	defaultBrowserHostMCP    = 8932
	defaultBrowserNoVNCPort  = 6080
	defaultBrowserHostNoVNC  = 6080
	defaultBrowserMCPVersion = "0.0.64"
)

type browserConfig struct {
	ImageName      string `json:"image_name"`
	ContainerName  string `json:"container_name"`
	ProfileDir     string `json:"profile_dir"`
	HostBind       string `json:"host_bind"`
	HostMCPPort    int    `json:"host_mcp_port"`
	HostNoVNCPort  int    `json:"host_novnc_port"`
	MCPPort        int    `json:"mcp_port"`
	NoVNCPort      int    `json:"novnc_port"`
	VNCPassword    string `json:"vnc_password,omitempty"`
	MCPVersion     string `json:"mcp_version"`
	BrowserChannel string `json:"browser_channel"`
	AllowedHosts   string `json:"allowed_hosts"`
}

type browserStatusPayload struct {
	OK                  bool   `json:"ok"`
	ContainerName       string `json:"container_name"`
	ContainerRunning    bool   `json:"container_running"`
	ContainerStatusLine string `json:"container_status_line,omitempty"`
	MCPURL              string `json:"mcp_url"`
	NoVNCURL            string `json:"novnc_url"`
	MCPHostCode         int    `json:"mcp_host_code,omitempty"`
	MCPContainerCode    int    `json:"mcp_container_code,omitempty"`
	NoVNCHostCode       int    `json:"novnc_host_code,omitempty"`
	NoVNCContainerCode  int    `json:"novnc_container_code,omitempty"`
	MCPReady            bool   `json:"mcp_ready"`
	NoVNCReady          bool   `json:"novnc_ready"`
	Error               string `json:"error,omitempty"`
}

func cmdBrowser(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, browserUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(browserUsageText)
	case "build":
		cmdBrowserBuild(rest)
	case "start":
		cmdBrowserStart(rest)
	case "stop":
		cmdBrowserStop(rest)
	case "status":
		cmdBrowserStatus(rest)
	case "logs":
		cmdBrowserLogs(rest)
	case "proxy":
		cmdBrowserProxy(rest)
	default:
		printUnknown("browser", sub)
		printUsage(browserUsageText)
	}
}

func defaultBrowserConfig() browserConfig {
	home, err := os.UserHomeDir()
	profileDir := "/tmp/.si-browser-profile"
	if err == nil && strings.TrimSpace(home) != "" {
		profileDir = filepath.Join(home, ".si", "browser", "profile")
	}
	vncPassword := strings.TrimSpace(os.Getenv("SI_BROWSER_VNC_PASSWORD"))
	if vncPassword == "" {
		vncPassword = "si"
	}
	return browserConfig{
		ImageName:      envOr("SI_BROWSER_IMAGE", defaultBrowserImage),
		ContainerName:  envOr("SI_BROWSER_CONTAINER", defaultBrowserContainer),
		ProfileDir:     envOr("SI_BROWSER_PROFILE_DIR", profileDir),
		HostBind:       envOr("SI_BROWSER_HOST_BIND", defaultBrowserHostBind),
		HostMCPPort:    envOrInt("SI_BROWSER_HOST_MCP_PORT", defaultBrowserHostMCP),
		HostNoVNCPort:  envOrInt("SI_BROWSER_HOST_NOVNC_PORT", defaultBrowserHostNoVNC),
		MCPPort:        envOrInt("SI_BROWSER_MCP_PORT", defaultBrowserMCPPort),
		NoVNCPort:      envOrInt("SI_BROWSER_NOVNC_PORT", defaultBrowserNoVNCPort),
		VNCPassword:    vncPassword,
		MCPVersion:     envOr("SI_BROWSER_MCP_VERSION", defaultBrowserMCPVersion),
		BrowserChannel: envOr("SI_BROWSER_CHANNEL", "chromium"),
		AllowedHosts:   envOr("SI_BROWSER_ALLOWED_HOSTS", "*"),
	}
}

func envOrInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func registerBrowserConfigFlags(fs *flag.FlagSet, cfg *browserConfig) {
	fs.StringVar(&cfg.ImageName, "image", cfg.ImageName, "docker image name")
	fs.StringVar(&cfg.ContainerName, "name", cfg.ContainerName, "container name")
	fs.StringVar(&cfg.ProfileDir, "profile-dir", cfg.ProfileDir, "host profile directory for persisted browser state")
	fs.StringVar(&cfg.HostBind, "host-bind", cfg.HostBind, "host bind address")
	fs.IntVar(&cfg.HostMCPPort, "host-mcp-port", cfg.HostMCPPort, "host MCP port")
	fs.IntVar(&cfg.HostNoVNCPort, "host-novnc-port", cfg.HostNoVNCPort, "host noVNC port")
	fs.IntVar(&cfg.MCPPort, "mcp-port", cfg.MCPPort, "container MCP port")
	fs.IntVar(&cfg.NoVNCPort, "novnc-port", cfg.NoVNCPort, "container noVNC port")
	fs.StringVar(&cfg.VNCPassword, "vnc-password", cfg.VNCPassword, "VNC password")
	fs.StringVar(&cfg.MCPVersion, "mcp-version", cfg.MCPVersion, "@playwright/mcp package version")
	fs.StringVar(&cfg.BrowserChannel, "browser", cfg.BrowserChannel, "browser channel (chromium|chrome|msedge)")
	fs.StringVar(&cfg.AllowedHosts, "allowed-hosts", cfg.AllowedHosts, "comma-delimited allowed hosts for MCP")
}

func cmdBrowserBuild(args []string) {
	fs := flag.NewFlagSet("browser build", flag.ExitOnError)
	cfg := defaultBrowserConfig()
	repo := fs.String("repo", "", "si repository root path")
	contextDir := fs.String("context", "", "docker build context path")
	dockerfile := fs.String("dockerfile", "", "dockerfile path")
	_ = fs.String("image", cfg.ImageName, "docker image name")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si browser build [--image <name>] [--repo <path>] [--context <path>] [--dockerfile <path>] [--json]")
		return
	}
	cfg.ImageName = strings.TrimSpace(fs.Lookup("image").Value.String())
	assetsRoot, err := resolveBrowserAssetsRoot(strings.TrimSpace(*repo))
	if err != nil {
		fatal(err)
	}
	resolvedContext := strings.TrimSpace(*contextDir)
	if resolvedContext == "" {
		resolvedContext = assetsRoot
	}
	resolvedDockerfile := strings.TrimSpace(*dockerfile)
	if resolvedDockerfile == "" {
		resolvedDockerfile = filepath.Join(assetsRoot, "Dockerfile")
	}
	cmd := dockerCommand("build", "-t", cfg.ImageName, "-f", resolvedDockerfile, resolvedContext)
	if err := runStreamingCommand(cmd); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printBrowserJSON(map[string]any{
			"ok":         true,
			"command":    "browser build",
			"image":      cfg.ImageName,
			"dockerfile": resolvedDockerfile,
			"context":    resolvedContext,
		})
		return
	}
	successf("browser image built: %s", cfg.ImageName)
}

func cmdBrowserStart(args []string) {
	fs := flag.NewFlagSet("browser start", flag.ExitOnError)
	cfg := defaultBrowserConfig()
	repo := fs.String("repo", "", "si repository root path")
	skipBuild := fs.Bool("skip-build", false, "skip docker image build")
	jsonOut := fs.Bool("json", false, "output json")
	registerBrowserConfigFlags(fs, &cfg)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si browser start [--skip-build] [--repo <path>] [--image <name>] [--name <container>] [--profile-dir <path>] [--host-bind <addr>] [--host-mcp-port <n>] [--host-novnc-port <n>] [--mcp-port <n>] [--novnc-port <n>] [--vnc-password <pwd>] [--mcp-version <ver>] [--browser <name>] [--allowed-hosts <list>] [--json]")
		return
	}
	if strings.TrimSpace(cfg.ContainerName) == "" {
		fatal(fmt.Errorf("container name is required"))
	}
	if strings.TrimSpace(cfg.ImageName) == "" {
		fatal(fmt.Errorf("image name is required"))
	}
	if strings.TrimSpace(cfg.ProfileDir) == "" {
		fatal(fmt.Errorf("profile dir is required"))
	}
	if err := os.MkdirAll(cfg.ProfileDir, 0o700); err != nil {
		fatal(err)
	}
	if !*skipBuild {
		cmdBrowserBuild(append([]string{}, "--repo", strings.TrimSpace(*repo), "--image", cfg.ImageName))
	}
	if err := removeDockerContainer(cfg.ContainerName); err != nil {
		fatal(err)
	}
	if cfg.ContainerName != "playwright-mcp-headed" {
		_ = removeDockerContainer("playwright-mcp-headed")
	}
	runArgs := []string{
		"run", "-d",
		"--name", cfg.ContainerName,
		"--restart", "unless-stopped",
		"--init",
		"--ipc=host",
		"--user", "pwuser",
		"-e", "VNC_PASSWORD=" + cfg.VNCPassword,
		"-e", "MCP_VERSION=" + cfg.MCPVersion,
		"-e", "BROWSER_CHANNEL=" + cfg.BrowserChannel,
		"-e", "ALLOWED_HOSTS=" + cfg.AllowedHosts,
		"-e", "MCP_PORT=" + strconv.Itoa(cfg.MCPPort),
		"-e", "NOVNC_PORT=" + strconv.Itoa(cfg.NoVNCPort),
		"-p", fmt.Sprintf("%s:%d:%d", cfg.HostBind, cfg.HostMCPPort, cfg.MCPPort),
		"-p", fmt.Sprintf("%s:%d:%d", cfg.HostBind, cfg.HostNoVNCPort, cfg.NoVNCPort),
		"-v", fmt.Sprintf("%s:/home/pwuser/.playwright-mcp-profile", cfg.ProfileDir),
		cfg.ImageName,
	}
	if out, err := runDockerOutput(runArgs...); err != nil {
		fatal(fmt.Errorf("docker run failed: %w", err))
	} else if strings.TrimSpace(out) == "" {
		warnf("browser container started without container id output")
	}
	status, err := waitForBrowserStatus(cfg, 12, 1*time.Second)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printBrowserJSON(map[string]any{
			"ok":             true,
			"command":        "browser start",
			"config":         cfg,
			"status":         status,
			"mcp_url":        browserMCPURL(cfg),
			"novnc_url":      browserNoVNCURL(cfg),
			"host_connect":   browserHostConnect(cfg.HostBind),
			"profile_dir":    cfg.ProfileDir,
			"container_name": cfg.ContainerName,
		})
		return
	}
	fmt.Printf("%s %s\n", styleHeading("si browser:"), "start")
	fmt.Printf("  container=%s image=%s\n", cfg.ContainerName, cfg.ImageName)
	fmt.Printf("  mcp_url=%s\n", browserMCPURL(cfg))
	fmt.Printf("  novnc_url=%s\n", browserNoVNCURL(cfg))
	fmt.Printf("  profile_dir=%s\n", cfg.ProfileDir)
}

func cmdBrowserStop(args []string) {
	fs := flag.NewFlagSet("browser stop", flag.ExitOnError)
	cfg := defaultBrowserConfig()
	jsonOut := fs.Bool("json", false, "output json")
	fs.StringVar(&cfg.ContainerName, "name", cfg.ContainerName, "container name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si browser stop [--name <container>] [--json]")
		return
	}
	if err := removeDockerContainer(cfg.ContainerName); err != nil {
		fatal(err)
	}
	if cfg.ContainerName != "playwright-mcp-headed" {
		_ = removeDockerContainer("playwright-mcp-headed")
	}
	if *jsonOut {
		printBrowserJSON(map[string]any{
			"ok":             true,
			"command":        "browser stop",
			"container_name": cfg.ContainerName,
		})
		return
	}
	successf("browser container removed: %s", cfg.ContainerName)
}

func cmdBrowserStatus(args []string) {
	fs := flag.NewFlagSet("browser status", flag.ExitOnError)
	cfg := defaultBrowserConfig()
	jsonOut := fs.Bool("json", false, "output json")
	registerBrowserConfigFlags(fs, &cfg)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si browser status [--image <name>] [--name <container>] [--host-bind <addr>] [--host-mcp-port <n>] [--host-novnc-port <n>] [--mcp-port <n>] [--novnc-port <n>] [--json]")
		return
	}
	status, err := evaluateBrowserStatus(cfg)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printBrowserJSON(map[string]any{
			"ok":      status.OK,
			"command": "browser status",
			"status":  status,
		})
		if !status.OK {
			os.Exit(1)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("si browser:"), "status")
	fmt.Printf("  container=%s running=%s\n", status.ContainerName, boolString(status.ContainerRunning))
	if strings.TrimSpace(status.ContainerStatusLine) != "" {
		fmt.Printf("  docker_ps=%s\n", status.ContainerStatusLine)
	}
	fmt.Printf("  mcp_url=%s\n", status.MCPURL)
	fmt.Printf("  novnc_url=%s\n", status.NoVNCURL)
	fmt.Printf("  mcp_ready=%s host_code=%d container_code=%d\n", boolString(status.MCPReady), status.MCPHostCode, status.MCPContainerCode)
	fmt.Printf("  novnc_ready=%s host_code=%d container_code=%d\n", boolString(status.NoVNCReady), status.NoVNCHostCode, status.NoVNCContainerCode)
	if strings.TrimSpace(status.Error) != "" {
		fmt.Printf("  error=%s\n", status.Error)
	}
	if !status.OK {
		os.Exit(1)
	}
}

func cmdBrowserLogs(args []string) {
	fs := flag.NewFlagSet("browser logs", flag.ExitOnError)
	cfg := defaultBrowserConfig()
	tail := fs.Int("tail", 200, "tail line count")
	follow := fs.Bool("follow", true, "follow logs")
	fs.StringVar(&cfg.ContainerName, "name", cfg.ContainerName, "container name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si browser logs [--name <container>] [--tail <n>] [--follow] [--follow=false]")
		return
	}
	logArgs := []string{"logs", "--tail", strconv.Itoa(*tail)}
	if *follow {
		logArgs = append(logArgs, "-f")
	}
	logArgs = append(logArgs, cfg.ContainerName)
	cmd := dockerCommand(logArgs...)
	if err := runStreamingCommand(cmd); err != nil {
		fatal(err)
	}
}

func cmdBrowserProxy(args []string) {
	fs := flag.NewFlagSet("browser proxy", flag.ExitOnError)
	repo := fs.String("repo", "", "si repository root path")
	script := fs.String("script", "", "mcp proxy script path")
	bind := fs.String("bind", "127.0.0.1", "proxy bind host")
	port := fs.Int("port", 8931, "proxy bind port")
	upstream := fs.String("upstream", "http://127.0.0.1:8932", "upstream MCP base URL")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si browser proxy [--repo <path>] [--script <path>] [--bind <host>] [--port <n>] [--upstream <url>]")
		return
	}
	scriptPath := strings.TrimSpace(*script)
	if scriptPath == "" {
		assetsRoot, err := resolveBrowserAssetsRoot(strings.TrimSpace(*repo))
		if err != nil {
			fatal(err)
		}
		scriptPath = filepath.Join(assetsRoot, "mcp-proxy.mjs")
	}
	cmd := exec.Command("node", scriptPath)
	cmd.Env = append(os.Environ(),
		"PROXY_BIND="+strings.TrimSpace(*bind),
		"PROXY_PORT="+strconv.Itoa(*port),
		"UPSTREAM_BASE="+strings.TrimSpace(*upstream),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func resolveBrowserAssetsRoot(repo string) (string, error) {
	root, err := resolveSelfRepoRoot(strings.TrimSpace(repo))
	if err != nil {
		return "", err
	}
	assets := filepath.Join(root, "tools", "si-browser")
	if _, err := os.Stat(assets); err != nil {
		return "", fmt.Errorf("browser assets not found at %s", assets)
	}
	return assets, nil
}

func runStreamingCommand(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runDockerOutput(args ...string) (string, error) {
	cmd := dockerCommand(args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func removeDockerContainer(name string) error {
	resolved := strings.TrimSpace(name)
	if resolved == "" {
		return nil
	}
	_, err := runDockerOutput("rm", "-f", resolved)
	if err != nil {
		msg := strings.ToLower(strings.TrimSpace(err.Error()))
		if strings.Contains(msg, "no such container") {
			return nil
		}
		return err
	}
	return nil
}

func waitForBrowserStatus(cfg browserConfig, retries int, delay time.Duration) (browserStatusPayload, error) {
	var last browserStatusPayload
	for i := 0; i < retries; i++ {
		status, err := evaluateBrowserStatus(cfg)
		if err != nil {
			return browserStatusPayload{}, err
		}
		last = status
		if status.OK {
			return status, nil
		}
		time.Sleep(delay)
	}
	if strings.TrimSpace(last.Error) == "" {
		last.Error = "browser health checks did not pass in time"
	}
	return last, fmt.Errorf("%s", last.Error)
}

func evaluateBrowserStatus(cfg browserConfig) (browserStatusPayload, error) {
	status := browserStatusPayload{
		ContainerName: cfg.ContainerName,
		MCPURL:        browserMCPURL(cfg),
		NoVNCURL:      browserNoVNCURL(cfg),
	}
	psOut, err := runDockerOutput("ps", "--filter", "name=^/"+cfg.ContainerName+"$", "--format", "{{.Names}}\t{{.Status}}\t{{.Ports}}")
	if err != nil {
		return status, err
	}
	status.ContainerStatusLine = strings.TrimSpace(psOut)
	status.ContainerRunning = strings.TrimSpace(psOut) != ""
	if !status.ContainerRunning {
		status.Error = "container not running"
		return status, nil
	}

	status.MCPHostCode = probeHTTPStatus(status.MCPURL)
	if status.MCPHostCode == 200 || status.MCPHostCode == 400 {
		status.MCPReady = true
	}
	status.NoVNCHostCode = probeHTTPStatus(status.NoVNCURL)
	if status.NoVNCHostCode >= 200 && status.NoVNCHostCode < 400 {
		status.NoVNCReady = true
	}

	if !status.MCPReady {
		status.MCPContainerCode = probeContainerHTTPStatus(cfg.ContainerName, fmt.Sprintf("http://127.0.0.1:%d/mcp", cfg.MCPPort))
		if status.MCPContainerCode == 200 || status.MCPContainerCode == 400 {
			status.MCPReady = true
		}
	}
	if !status.NoVNCReady {
		status.NoVNCContainerCode = probeContainerHTTPStatus(cfg.ContainerName, fmt.Sprintf("http://127.0.0.1:%d/vnc.html", cfg.NoVNCPort))
		if status.NoVNCContainerCode >= 200 && status.NoVNCContainerCode < 400 {
			status.NoVNCReady = true
		}
	}

	status.OK = status.ContainerRunning && status.MCPReady && status.NoVNCReady
	if !status.OK {
		status.Error = "endpoint checks failed"
	}
	return status, nil
}

func probeHTTPStatus(url string) int {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- URL is local operator-controlled endpoint.
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func probeContainerHTTPStatus(containerName, url string) int {
	out, err := runDockerOutput(
		"exec",
		containerName,
		"sh",
		"-lc",
		"curl -sS -o /dev/null -w '%{http_code}' "+quoteSingle(url),
	)
	if err != nil {
		return 0
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return 0
	}
	return code
}

func browserHostConnect(bind string) string {
	resolved := strings.TrimSpace(bind)
	if resolved == "" || resolved == "0.0.0.0" {
		return "127.0.0.1"
	}
	return resolved
}

func browserMCPURL(cfg browserConfig) string {
	return fmt.Sprintf("http://%s:%d/mcp", browserHostConnect(cfg.HostBind), cfg.HostMCPPort)
}

func browserNoVNCURL(cfg browserConfig) string {
	return fmt.Sprintf("http://%s:%d/vnc.html?autoconnect=1&resize=scale", browserHostConnect(cfg.HostBind), cfg.HostNoVNCPort)
}

func printBrowserJSON(payload map[string]any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fatal(err)
	}
}
