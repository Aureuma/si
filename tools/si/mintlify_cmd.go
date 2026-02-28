package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

const mintlifyUsageText = "usage: si mintlify <init|dev|validate|broken-links|openapi-check|a11y|rename|update|upgrade|migrate-mdx|version|raw> [args...]"

func cmdMintlify(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, mintlifyUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(mintlifyUsageText)
	case "init":
		cmdMintlifyInit(rest)
	case "dev":
		cmdMintlifyNpx("dev", rest, "usage: si mintlify dev [--repo <path>] [--json] [-- mint args...]")
	case "validate":
		cmdMintlifyNpx("validate", rest, "usage: si mintlify validate [--repo <path>] [--json] [-- mint args...]")
	case "broken-links":
		cmdMintlifyNpx("broken-links", rest, "usage: si mintlify broken-links [--repo <path>] [--json] [-- mint args...]")
	case "openapi-check":
		cmdMintlifyNpx("openapi-check", rest, "usage: si mintlify openapi-check [--repo <path>] [--json] [-- mint args...]")
	case "a11y", "accessibility":
		cmdMintlifyNpx("a11y", rest, "usage: si mintlify a11y [--repo <path>] [--json] [-- mint args...]")
	case "rename":
		cmdMintlifyNpx("rename", rest, "usage: si mintlify rename [--repo <path>] [--json] [-- mint args...]")
	case "update":
		cmdMintlifyNpx("update", rest, "usage: si mintlify update [--repo <path>] [--json] [-- mint args...]")
	case "upgrade":
		cmdMintlifyNpx("upgrade", rest, "usage: si mintlify upgrade [--repo <path>] [--json] [-- mint args...]")
	case "migrate-mdx":
		cmdMintlifyNpx("migrate-mdx", rest, "usage: si mintlify migrate-mdx [--repo <path>] [--json] [-- mint args...]")
	case "version":
		cmdMintlifyNpx("version", rest, "usage: si mintlify version [--repo <path>] [--json] [-- mint args...]")
	case "raw":
		cmdMintlifyRaw(rest)
	default:
		printUnknown("mintlify", sub)
		printUsage(mintlifyUsageText)
	}
}

func cmdMintlifyInit(args []string) {
	fs := flag.NewFlagSet("mintlify init", flag.ExitOnError)
	repo := fs.String("repo", "", "si repository root")
	docsDir := fs.String("docs-dir", "docs", "docs directory for Mintlify pages")
	siteName := fs.String("name", "si", "Mintlify site name")
	siteURL := fs.String("site-url", "https://si.aureuma.ai", "production docs site URL")
	force := fs.Bool("force", false, "overwrite docs.json and index page")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si mintlify init [--repo <path>] [--docs-dir <path>] [--name <site>] [--site-url <url>] [--force] [--json]")
		return
	}

	root, err := resolveMintlifyRepoRoot(strings.TrimSpace(*repo))
	if err != nil {
		fatal(err)
	}
	resolvedDocsDir := strings.TrimSpace(*docsDir)
	if resolvedDocsDir == "" {
		resolvedDocsDir = "docs"
	}
	if filepath.IsAbs(resolvedDocsDir) {
		resolvedDocsDir, err = filepath.Rel(root, resolvedDocsDir)
		if err != nil {
			fatal(err)
		}
	}
	resolvedDocsDir = filepath.Clean(resolvedDocsDir)
	docsAbsPath := filepath.Join(root, resolvedDocsDir)
	if err := os.MkdirAll(docsAbsPath, 0o755); err != nil {
		fatal(err)
	}

	docsConfigPath := filepath.Join(root, "docs.json")
	if err := writeMintlifyDocsConfig(docsConfigPath, resolvedDocsDir, strings.TrimSpace(*siteName), strings.TrimSpace(*siteURL), *force); err != nil {
		fatal(err)
	}

	indexPagePath := filepath.Join(docsAbsPath, "index.mdx")
	if err := writeMintlifyIndexPage(indexPagePath, strings.TrimSpace(*siteName), *force); err != nil {
		fatal(err)
	}

	if *jsonOut {
		printMintlifyJSON(map[string]any{
			"ok":          true,
			"command":     "mintlify init",
			"repo_root":   root,
			"docs_dir":    resolvedDocsDir,
			"docs_config": docsConfigPath,
			"index_page":  indexPagePath,
		})
		return
	}

	successf("mintlify config initialized")
	fmt.Printf("  repo_root=%s\n", root)
	fmt.Printf("  docs_config=%s\n", docsConfigPath)
	fmt.Printf("  docs_dir=%s\n", docsAbsPath)
	fmt.Printf("  index_page=%s\n", indexPagePath)
	fmt.Printf("  next: si mintlify validate\n")
}

func cmdMintlifyNpx(subcommand string, args []string, usageText string) {
	fs := flag.NewFlagSet("mintlify "+subcommand, flag.ExitOnError)
	repo := fs.String("repo", "", "si repository root")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)

	root, err := resolveMintlifyRepoRoot(strings.TrimSpace(*repo))
	if err != nil {
		fatal(err)
	}
	extra := fs.Args()
	cmdArgs := []string{"-y", "mint", subcommand}
	cmdArgs = append(cmdArgs, extra...)
	if *jsonOut {
		printMintlifyJSON(map[string]any{
			"ok":      true,
			"command": "mintlify " + subcommand,
			"repo":    root,
			"runner":  "npx",
			"args":    append([]string{"mint", subcommand}, extra...),
		})
		return
	}
	if err := runMintlifyNpx(root, cmdArgs); err != nil {
		fatal(err)
	}
	if usageText == "" {
		_ = usageText
	}
}

func cmdMintlifyRaw(args []string) {
	fs := flag.NewFlagSet("mintlify raw", flag.ExitOnError)
	repo := fs.String("repo", "", "si repository root")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if len(fs.Args()) == 0 {
		printUsage("usage: si mintlify raw [--repo <path>] [--json] -- <mint args...>")
		return
	}
	root, err := resolveMintlifyRepoRoot(strings.TrimSpace(*repo))
	if err != nil {
		fatal(err)
	}
	cmdArgs := []string{"-y", "mint"}
	cmdArgs = append(cmdArgs, fs.Args()...)
	if *jsonOut {
		printMintlifyJSON(map[string]any{
			"ok":      true,
			"command": "mintlify raw",
			"repo":    root,
			"runner":  "npx",
			"args":    append([]string{"mint"}, fs.Args()...),
		})
		return
	}
	if err := runMintlifyNpx(root, cmdArgs); err != nil {
		fatal(err)
	}
}

func writeMintlifyDocsConfig(pathValue, docsDir, siteName, siteURL string, force bool) error {
	if !force {
		if _, err := os.Stat(pathValue); err == nil {
			return nil
		}
	}
	docsDir = strings.TrimSpace(docsDir)
	siteName = strings.TrimSpace(siteName)
	if siteName == "" {
		siteName = "si"
	}
	siteURL = strings.TrimSpace(siteURL)
	if siteURL == "" {
		siteURL = "https://si.aureuma.ai"
	}
	config := map[string]any{
		"$schema": "https://mintlify.com/docs.json",
		"name":    siteName,
		"theme":   "mint",
		"colors":  map[string]string{"primary": "#0f6d5f", "light": "#8bf0c9", "dark": "#0c4b42"},
		"favicon": "/docs/images/si-hero.png",
		"logo":    map[string]string{"light": "/docs/images/si-hero.png", "dark": "/docs/images/si-hero.png"},
		"navigation": map[string]any{
			"global": map[string]any{
				"anchors": []map[string]string{
					{"anchor": "Website", "href": siteURL},
				},
			},
			"tabs": []map[string]any{
				{
					"tab": "Get Started",
					"groups": []map[string]any{
						{
							"group": "Overview",
							"pages": []string{
								path.Join(docsDir, "index"),
								path.Join(docsDir, "CLI_REFERENCE"),
								path.Join(docsDir, "INTEGRATIONS_OVERVIEW"),
								path.Join(docsDir, "testing"),
							},
						},
					},
				},
				{
					"tab": "Core Runtime",
					"groups": []map[string]any{
						{
							"group": "Runtime",
							"pages": []string{
								path.Join(docsDir, "DYAD"),
								path.Join(docsDir, "VAULT"),
								path.Join(docsDir, "BROWSER"),
								path.Join(docsDir, "ORBITS"),
							},
						},
					},
				},
				{
					"tab": "Integrations",
					"groups": []map[string]any{
						{
							"group": "Provider Guides",
							"pages": []string{
								path.Join(docsDir, "GITHUB"),
								path.Join(docsDir, "CLOUDFLARE"),
								path.Join(docsDir, "GCP"),
								path.Join(docsDir, "AWS"),
								path.Join(docsDir, "OPENAI"),
								path.Join(docsDir, "STRIPE"),
							},
						},
					},
				},
				{
					"tab": "PaaS",
					"groups": []map[string]any{
						{
							"group": "Operations",
							"pages": []string{
								path.Join(docsDir, "PAAS_TEST_MATRIX"),
								path.Join(docsDir, "PAAS_CONTEXT_OPERATIONS_RUNBOOK"),
								path.Join(docsDir, "PAAS_BACKUP_RESTORE_POLICY"),
								path.Join(docsDir, "PAAS_INCIDENT_RUNBOOK"),
							},
						},
					},
				},
			},
		},
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(pathValue, raw, 0o644)
}

func writeMintlifyIndexPage(pathValue, siteName string, force bool) error {
	if !force {
		if _, err := os.Stat(pathValue); err == nil {
			return nil
		}
	}
	siteName = strings.TrimSpace(siteName)
	if siteName == "" {
		siteName = "si"
	}
	body := strings.Join([]string{
		"---",
		"title: " + siteName,
		"description: AI-first CLI for dyads, provider bridges, and docker-native PaaS operations.",
		"---",
		"",
		"# " + siteName,
		"",
		"`si` is a single CLI for agent workflows, provider integrations, and Docker-native PaaS operations.",
		"",
		"## Start Here",
		"",
		"1. `si build image`",
		"2. `si dyad spawn <name> --profile <profile>`",
		"3. `si vault status`",
		"4. `si paas --help`",
		"",
		"## Docs",
		"",
		"Use the navigation to browse platform guides, runbooks, and command references.",
	}, "\n")
	return os.WriteFile(pathValue, []byte(body+"\n"), 0o644)
}

func runMintlifyNpx(root string, args []string) error {
	cmd := exec.Command("npx", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func resolveMintlifyRepoRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) != "" {
		return resolveSelfRepoRoot(strings.TrimSpace(raw))
	}
	if root, err := resolveSelfRepoRoot(""); err == nil {
		return root, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return cwd, nil
}

func printMintlifyJSON(payload map[string]any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fatal(err)
	}
}
