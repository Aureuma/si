package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	shared "silexa/agents/shared/docker"
)

func cmdMCP(args []string) {
	if len(args) == 0 {
		printUsage("usage: si mcp <scout|sync|apply-config>")
		return
	}
	switch args[0] {
	case "scout":
		cmdMCPScout(args[1:])
	case "sync":
		cmdMCPSync(args[1:])
	case "apply-config":
		cmdMCPApplyConfig(args[1:])
	default:
		printUnknown("mcp", args[0])
	}
}

func cmdMCPScout(args []string) {
	container := envOr("MCP_GATEWAY_CONTAINER", "silexa-mcp-gateway")
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	containerID, _, err := client.ContainerByName(ctx, container)
	if err != nil {
		fatal(err)
	}
	if containerID == "" {
		fatal(fmt.Errorf("container not found: %s", container))
	}
	if err := client.Exec(ctx, containerID, []string{"docker-mcp", "catalog", "ls"}, shared.ExecOptions{}, nil, os.Stdout, os.Stderr); err != nil {
		fatal(err)
	}
	if err := client.Exec(ctx, containerID, []string{"docker-mcp", "catalog", "show", "docker-mcp", "--format", "yaml"}, shared.ExecOptions{}, nil, os.Stdout, os.Stderr); err != nil {
		fatal(err)
	}
	_ = args
}

func cmdMCPSync(args []string) {
	fs := flag.NewFlagSet("mcp sync", flag.ExitOnError)
	catalog := fs.String("catalog", "", "catalog file")
	fs.Parse(args)
	root := mustRepoRoot()
	source := *catalog
	if source == "" {
		source = filepath.Join(root, "data", "mcp-gateway", "catalog.yaml")
	}
	if !exists(source) {
		fatal(fmt.Errorf("catalog not found: %s", source))
	}
	container := envOr("MCP_GATEWAY_CONTAINER", "silexa-mcp-gateway")
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	containerID, _, err := client.ContainerByName(ctx, container)
	if err != nil {
		fatal(err)
	}
	if containerID == "" {
		fatal(fmt.Errorf("container not found: %s", container))
	}
	if err := client.Exec(ctx, containerID, []string{"mkdir", "-p", "/catalog"}, shared.ExecOptions{}, nil, nil, nil); err != nil {
		fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		fatal(err)
	}
	if err := client.CopyFileToContainer(ctx, containerID, "/catalog/catalog.yaml", raw, 0o644); err != nil {
		fatal(err)
	}
	if err := client.RestartContainer(ctx, containerID, 10*time.Second); err != nil {
		fatal(err)
	}
	successf("catalog synced and gateway restarted")
}

func cmdMCPApplyConfig(args []string) {
	fs := flag.NewFlagSet("mcp apply-config", flag.ExitOnError)
	member := fs.String("member", "actor", "actor or critic")
	destDir := fs.String("dest-dir", "/root/.codex", "destination dir")
	fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si mcp apply-config <dyad> [--member actor|critic]")
		return
	}
	dyad := fs.Arg(0)
	root := mustRepoRoot()
	src := filepath.Join(root, "configs", "codex-mcp-config.toml")
	if !exists(src) {
		fatal(fmt.Errorf("missing config: %s", src))
	}
	container := sharedContainerName(dyad, *member)
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	containerID, _, err := client.ContainerByName(ctx, container)
	if err != nil {
		fatal(err)
	}
	if containerID == "" {
		fatal(fmt.Errorf("container not found: %s", container))
	}
	dest := filepath.ToSlash(*destDir)
	if err := client.Exec(ctx, containerID, []string{"mkdir", "-p", dest}, shared.ExecOptions{}, nil, nil, nil); err != nil {
		fatal(err)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		fatal(err)
	}
	destPath := path.Join(dest, "config.toml")
	if err := client.CopyFileToContainer(ctx, containerID, destPath, raw, 0o644); err != nil {
		fatal(err)
	}
	infof("codex mcp config applied to %s", container)
}

func sharedContainerName(dyad, member string) string {
	if dyad == "" {
		return ""
	}
	if member == "" {
		member = "actor"
	}
	return "silexa-" + member + "-" + dyad
}
