package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/orbitals"
)

const (
	orbitsGatewayUsageText    = "usage: si orbits gateway <build>"
	defaultOrbitGatewayName   = "global"
	defaultGatewayCatalogName = "gateway-%s.json"
)

func cmdOrbitsGateway(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, orbitsGatewayUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "build":
		cmdOrbitsGatewayBuild(rest)
	default:
		printUnknown("orbits gateway", sub)
		printUsage(orbitsGatewayUsageText)
	}
}

func cmdOrbitsGatewayBuild(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"verified": true, "json": true})
	fs := flag.NewFlagSet("orbits gateway build", flag.ExitOnError)
	source := fs.String("source", "", "manifest source file or directory")
	registry := fs.String("registry", "", "gateway registry name")
	slots := fs.Int("slots", 0, "slots per namespace")
	channel := fs.String("channel", "community", "catalog channel")
	verified := fs.Bool("verified", false, "mark built entries as verified")
	outputDir := fs.String("output-dir", "", "write index/shards into this directory")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if strings.TrimSpace(*source) == "" || fs.NArg() > 0 {
		printUsage("usage: si orbits gateway build --source <path> [--registry <name>] [--slots <n>] [--channel <name>] [--verified] [--output-dir <dir>] [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	targetRegistry := orbitGatewayRegistryName(settings, *registry)
	targetSlots := orbitGatewaySlots(settings, *slots)
	catalog, diagnostics, err := orbitals.BuildCatalogFromSource(strings.TrimSpace(*source), orbitals.BuildCatalogOptions{
		Channel:  strings.TrimSpace(*channel),
		Verified: *verified,
	})
	if err != nil {
		fatal(err)
	}
	index, shards, err := orbitals.BuildGateway(catalog, orbitals.GatewayBuildOptions{
		Registry:          targetRegistry,
		SlotsPerNamespace: targetSlots,
		GeneratedAt:       time.Now().UTC(),
	})
	if err != nil {
		fatal(err)
	}
	if trimmed := strings.TrimSpace(*outputDir); trimmed != "" {
		if err := writeGatewayBundle(trimmed, index, shards); err != nil {
			fatal(err)
		}
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":            true,
			"registry":      index.Registry,
			"slots":         index.SlotsPerNamespace,
			"total_entries": index.TotalEntries,
			"shard_count":   len(index.Shards),
			"diagnostics":   diagnostics,
			"output_dir":    strings.TrimSpace(*outputDir),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("gateway build complete registry=%s entries=%d shards=%d", index.Registry, index.TotalEntries, len(index.Shards))
	for _, d := range diagnostics {
		fmt.Printf("%s %s\n", styleHeading(strings.ToUpper(d.Level)+":"), d.Message)
	}
	if strings.TrimSpace(*outputDir) != "" {
		infof("wrote gateway bundle to %s", strings.TrimSpace(*outputDir))
	}
}

func writeGatewayBundle(dir string, index orbitals.GatewayIndex, shards map[string]orbitals.GatewayShard) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("output directory required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	indexRaw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), indexRaw, 0o644); err != nil {
		return err
	}
	shardsDir := filepath.Join(dir, "shards")
	if err := os.MkdirAll(shardsDir, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(shards))
	for key := range shards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw, err := json.MarshalIndent(shards[key], "", "  ")
		if err != nil {
			return err
		}
		safeName := strings.ReplaceAll(strings.ReplaceAll(key, "/", "_"), "--", "_")
		if err := os.WriteFile(filepath.Join(shardsDir, safeName+".json"), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeGatewayCatalog(path string, catalog orbitals.Catalog) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("output path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func orbitGatewayOutputPath(raw string, registry string) (string, error) {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		resolved, err := filepath.Abs(expandTilde(trimmed))
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	}
	paths, err := orbitals.DefaultPaths()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(paths.CatalogDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf(defaultGatewayCatalogName, registry)
	return filepath.Join(paths.CatalogDir, name), nil
}

func orbitGatewayRegistryName(_ Settings, explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return strings.ToLower(trimmed)
	}
	return defaultOrbitGatewayName
}

func orbitGatewaySlots(_ Settings, explicit int) int {
	if explicit > 0 {
		return explicit
	}
	return orbitals.GatewayDefaultSlotsPerNamespace
}
