package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cmdApp(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si app <init|adopt|list|build|deploy|remove|status|secrets>")
		return
	}
	switch args[0] {
	case "init":
		cmdAppInit(args[1:])
	case "adopt":
		cmdAppAdopt(args[1:])
	case "list":
		cmdAppList(args[1:])
	case "build":
		cmdAppBuild(args[1:])
	case "deploy":
		cmdAppDeploy(args[1:])
	case "remove":
		cmdAppRemove(args[1:])
	case "status":
		cmdAppStatus(args[1:])
	case "secrets":
		cmdAppSecrets(args[1:])
	default:
		fmt.Println("unknown app command:", args[0])
	}
}

func cmdAppInit(args []string) {
	fs := flag.NewFlagSet("app init", flag.ExitOnError)
	noDB := fs.Bool("no-db", false, "skip db provisioning")
	dbPort := fs.String("db-port", "", "db port (unused; reserved)")
	webPath := fs.String("web-path", "web", "web path")
	backendPath := fs.String("backend-path", "", "backend path")
	infraPath := fs.String("infra-path", "infra", "infra path")
	contentPath := fs.String("content-path", "", "content path")
	kind := fs.String("kind", "saas", "app kind")
	status := fs.String("status", "idea", "status")
	webStack := fs.String("web-stack", "go", "web stack")
	backendStack := fs.String("backend-stack", "", "backend stack")
	lang := fs.String("language", "go", "language")
	ui := fs.String("ui", "none", "ui")
	runtime := fs.String("runtime", "docker", "runtime")
	dbKind := fs.String("db", "postgres", "db kind")
	orm := fs.String("orm", "drizzle", "orm")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: si app init <app-name> [options]")
		return
	}
	app := fs.Arg(0)
	root := mustRepoRoot()
	appDir := filepath.Join(root, "apps", app)

	if err := os.MkdirAll(appDir, 0o755); err != nil {
		fatal(err)
	}
	dirs := []string{
		filepath.Join(appDir, "docs"),
		filepath.Join(appDir, "ui-tests"),
		filepath.Join(appDir, ".artifacts", "visual"),
		filepath.Join(appDir, "migrations"),
	}
	if *webPath != "" && *webPath != "." {
		dirs = append(dirs, filepath.Join(appDir, *webPath))
	}
	if *backendPath != "" && *backendPath != "." {
		dirs = append(dirs, filepath.Join(appDir, *backendPath))
	}
	if *infraPath != "" && *infraPath != "." {
		dirs = append(dirs, filepath.Join(appDir, *infraPath))
	}
	if *contentPath != "" && *contentPath != "." {
		dirs = append(dirs, filepath.Join(appDir, *contentPath))
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatal(err)
		}
	}

	planPath := filepath.Join(appDir, "docs", "plan.md")
	if !exists(planPath) {
		plan := fmt.Sprintf(`# %s Plan

## Vision & outcomes
- What problem are we solving?
- Target users, success metrics, constraints (perf, compliance, budget).

## Scope & requirements
- User journeys
- Must-haves vs nice-to-haves
- Integrations (auth, payments, notifications)

## Team & dyads
- Planner:
- Builder:
- QA:
- Infra:
- Marketing:
- Creds:

## Architecture notes
- Frontend stack:
- Backend/API:
- Data model:
- External services:

## Testing
- Unit/integration:
- Visual (qa-visual):
- Accessibility:
- Smoke:

## Deployment & rollout
- Environments:
- Migration plan:
- Launch/rollout steps:

## Risks & mitigations
- ...

## Budget & cost guardrails
- ...
`, app)
		if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
			fatal(err)
		}
	}

	targetsPath := filepath.Join(appDir, "ui-tests", "targets.json")
	if !exists(targetsPath) {
		targets := `{
  "baseURL": "http://localhost:3000",
  "routes": [
    { "path": "/", "name": "home", "waitFor": "body" }
  ],
  "viewports": [
    { "width": 1280, "height": 720, "name": "desktop" },
    { "width": 375, "height": 667, "name": "mobile" }
  ]
}
`
		if err := os.WriteFile(targetsPath, []byte(targets), 0o644); err != nil {
			fatal(err)
		}
	}

	metaPath := filepath.Join(appDir, "app.json")
	if !exists(metaPath) {
		meta := map[string]interface{}{
			"name": app,
			"kind": *kind,
			"stack": map[string]string{
				"web":      *webStack,
				"backend":  *backendStack,
				"language": *lang,
				"ui":       *ui,
				"runtime":  *runtime,
			},
			"paths": map[string]string{
				"web":     *webPath,
				"backend": *backendPath,
				"infra":   *infraPath,
				"content": *contentPath,
			},
			"modules": []string{},
			"owners": map[string]string{
				"department": "",
				"dyad":       "",
			},
			"data": map[string]string{
				"db":    *dbKind,
				"orm":   *orm,
				"cache": "",
			},
			"integrations": []string{},
			"status":       *status,
		}
		raw, _ := json.MarshalIndent(meta, "", "  ")
		if err := os.WriteFile(metaPath, raw, 0o644); err != nil {
			fatal(err)
		}
	}

	if *infraPath != "" {
		composePath := filepath.Join(appDir, *infraPath, "compose.yml")
		if !exists(composePath) {
			compose := buildCompose(app, *backendPath != "")
			if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
				fatal(err)
			}
		}
	}

	if !*noDB && *dbKind == "postgres" {
		if *dbPort != "" {
			_ = dbPort
		}
		fmt.Println("db provisioning is not automated; create a database and update secrets/app-" + app + ".env")
	}
	fmt.Println("app init complete")
}

func cmdAppAdopt(args []string) {
	withDB := false
	clean := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--with-db" {
			withDB = true
			continue
		}
		clean = append(clean, arg)
	}
	if len(clean) < 1 {
		fmt.Println("usage: si app adopt <app-name> [options]")
		return
	}
	if !withDB {
		clean = append([]string{"--no-db"}, clean...)
	}
	cmdAppInit(clean)
}

func cmdAppList(args []string) {
	root := mustRepoRoot()
	appsDir := filepath.Join(root, "apps")
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		fatal(err)
	}
	type row struct {
		Name   string
		Kind   string
		Status string
		Path   string
	}
	rows := []row{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		appPath := filepath.Join(appsDir, entry.Name())
		metaPath := filepath.Join(appPath, "app.json")
		if !exists(metaPath) {
			rows = append(rows, row{Name: "", Kind: "", Status: "", Path: appPath})
			continue
		}
		var meta struct {
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		}
		raw, err := os.ReadFile(metaPath)
		if err == nil {
			_ = json.Unmarshal(raw, &meta)
		}
		rows = append(rows, row{Name: meta.Name, Kind: meta.Kind, Status: meta.Status, Path: appPath})
	}
	if len(rows) == 0 {
		fmt.Println("no apps found")
		return
	}
	widths := map[string]int{"name": 4, "kind": 4, "status": 6, "path": 4}
	for _, r := range rows {
		widths["name"] = max(widths["name"], len(r.Name))
		widths["kind"] = max(widths["kind"], len(r.Kind))
		widths["status"] = max(widths["status"], len(r.Status))
		widths["path"] = max(widths["path"], len(r.Path))
	}
	fmt.Printf("%-*s  %-*s  %-*s  %s\n", widths["name"], "name", widths["kind"], "kind", widths["status"], "status", "path")
	for _, r := range rows {
		fmt.Printf("%-*s  %-*s  %-*s  %s\n", widths["name"], r.Name, widths["kind"], r.Kind, widths["status"], r.Status, r.Path)
	}
	_ = args
}

type appMeta struct {
	Name  string `json:"name"`
	Paths struct {
		Web     string `json:"web"`
		Backend string `json:"backend"`
		Infra   string `json:"infra"`
	} `json:"paths"`
	Stack struct {
		Web string `json:"web"`
	} `json:"stack"`
}

func cmdAppBuild(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si app build <app-name>")
		return
	}
	app := args[0]
	root := mustRepoRoot()
	appDir := filepath.Join(root, "apps", app)
	metaPath := filepath.Join(appDir, "app.json")
	if !exists(metaPath) {
		fatal(fmt.Errorf("missing %s", metaPath))
	}
	var meta appMeta
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		fatal(err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		fatal(err)
	}

	if meta.Paths.Web != "" {
		webPath := meta.Paths.Web
		webDir := appDir
		if webPath != "." {
			webDir = filepath.Join(appDir, webPath)
		}
		webImage := fmt.Sprintf("silexa/app-%s-web:local", app)
		webDockerfile := filepath.Join(webDir, "Dockerfile")
		if exists(webDockerfile) {
			if err := runDockerBuild(imageBuildSpec{
				tag:        webImage,
				contextDir: webDir,
				dockerfile: webDockerfile,
			}); err != nil {
				fatal(err)
			}
		} else {
			fmt.Fprintln(os.Stderr, "warning: no Dockerfile for web:", webDockerfile)
		}
	}

	if meta.Paths.Backend != "" {
		backendDir := filepath.Join(appDir, meta.Paths.Backend)
		backendDockerfile := filepath.Join(backendDir, "Dockerfile")
		if !exists(backendDockerfile) {
			fmt.Fprintln(os.Stderr, "warning: no Dockerfile for backend:", backendDockerfile)
		} else {
			backendImage := fmt.Sprintf("silexa/app-%s-backend:local", app)
			if err := runDockerBuild(imageBuildSpec{
				tag:        backendImage,
				contextDir: backendDir,
				dockerfile: backendDockerfile,
			}); err != nil {
				fatal(err)
			}
		}
	}
}

func cmdAppDeploy(args []string) {
	fs := flag.NewFlagSet("app deploy", flag.ExitOnError)
	noBuild := fs.Bool("no-build", false, "skip build")
	fileFlag := fs.String("file", "", "compose file")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: si app deploy <app-name> [--no-build] [--file path]")
		return
	}
	app := fs.Arg(0)
	if !*noBuild {
		cmdAppBuild([]string{app})
	}
	composeFile := resolveComposeFile(app, *fileFlag)
	project := "silexa-" + app
	cmd := exec.Command("docker", "compose", "-f", composeFile, "--project-name", project, "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func cmdAppRemove(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si app remove <app-name> [--file path]")
		return
	}
	app := args[0]
	composeFile := resolveComposeFile(app, "")
	project := "silexa-" + app
	cmd := exec.Command("docker", "compose", "-f", composeFile, "--project-name", project, "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func cmdAppStatus(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si app status <app-name> [--file path]")
		return
	}
	app := args[0]
	composeFile := resolveComposeFile(app, "")
	project := "silexa-" + app
	cmd := exec.Command("docker", "compose", "-f", composeFile, "--project-name", project, "ps")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func cmdAppSecrets(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si app secrets <app-name>")
		return
	}
	app := args[0]
	root := mustRepoRoot()
	secretPath := filepath.Join(root, "secrets", "app-"+app+".env")
	if !exists(secretPath) {
		template := "APP_NAME=" + app + "\n"
		if err := os.WriteFile(secretPath, []byte(template), 0o600); err != nil {
			fatal(err)
		}
		fmt.Println("created secrets file:", secretPath)
		return
	}
	fmt.Println("secrets file:", secretPath)
}

func resolveComposeFile(app, override string) string {
	if override != "" {
		return override
	}
	root := mustRepoRoot()
	appDir := filepath.Join(root, "apps", app)
	metaPath := filepath.Join(appDir, "app.json")
	infra := "infra"
	if exists(metaPath) {
		var meta appMeta
		if raw, err := os.ReadFile(metaPath); err == nil {
			_ = json.Unmarshal(raw, &meta)
			if strings.TrimSpace(meta.Paths.Infra) != "" {
				infra = meta.Paths.Infra
			}
		}
	}
	return filepath.Join(appDir, infra, "compose.yml")
}

func buildCompose(app string, includeBackend bool) string {
	lines := []string{
		`version: "3.9"`,
		"",
		"services:",
		"  web:",
		fmt.Sprintf("    image: silexa/app-%s-web:local", app),
		"    environment:",
		"      NODE_ENV: production",
		"      HOST: 0.0.0.0",
		"      PORT: 3000",
		"    ports:",
		"      - \"${APP_WEB_PORT:-3000}:3000\"",
		"    env_file:",
		fmt.Sprintf("      - ../../secrets/app-%s.env", app),
	}
	if includeBackend {
		lines = append(lines,
			"",
			"  backend:",
			fmt.Sprintf("    image: silexa/app-%s-backend:local", app),
			"    environment:",
			"      NODE_ENV: production",
			"      PORT: 8080",
			"    ports:",
			"      - \"${APP_BACKEND_PORT:-8080}:8080\"",
			"    env_file:",
			fmt.Sprintf("      - ../../secrets/app-%s.env", app),
		)
	}
	lines = append(lines,
		"",
		"networks:",
		"  default:",
		"    name: ${SILEXA_NETWORK:-silexa}",
		"    external: true",
	)
	return strings.Join(lines, "\n") + "\n"
}
