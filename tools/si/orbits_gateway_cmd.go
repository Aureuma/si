package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/orbitals"
)

const (
	orbitsGatewayUsageText    = "usage: si orbits gateway <build|push|pull|status>"
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
	case "push", "publish":
		cmdOrbitsGatewayPush(rest)
	case "pull", "sync":
		cmdOrbitsGatewayPull(rest)
	case "status":
		cmdOrbitsGatewayStatus(rest)
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

func cmdOrbitsGatewayPush(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"verified": true, "json": true})
	fs := flag.NewFlagSet("orbits gateway push", flag.ExitOnError)
	source := fs.String("source", "", "manifest source file or directory")
	registry := fs.String("registry", "", "gateway registry name")
	slots := fs.Int("slots", 0, "slots per namespace")
	channel := fs.String("channel", "community", "catalog channel")
	verified := fs.Bool("verified", false, "mark built entries as verified")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if strings.TrimSpace(*source) == "" || fs.NArg() > 0 {
		printUsage("usage: si orbits gateway push --source <path> [--registry <name>] [--slots <n>] [--channel <name>] [--verified] [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	targetRegistry := orbitGatewayRegistryName(settings, *registry)
	targetSlots := orbitGatewaySlots(settings, *slots)
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
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
	indexPayload, err := json.Marshal(index)
	if err != nil {
		fatal(err)
	}
	ctx := sunContext(settings)
	indexPut, err := client.putIntegrationRegistryIndex(ctx, index.Registry, indexPayload, nil)
	if err != nil {
		fatal(err)
	}

	shardKeys := make([]string, 0, len(shards))
	for key := range shards {
		shardKeys = append(shardKeys, key)
	}
	sort.Strings(shardKeys)
	for _, key := range shardKeys {
		shard := shards[key]
		payload, err := json.Marshal(shard)
		if err != nil {
			fatal(err)
		}
		if _, err := client.putIntegrationRegistryShard(ctx, index.Registry, key, payload, nil); err != nil {
			fatal(err)
		}
	}

	if *jsonOut {
		payload := map[string]interface{}{
			"ok":             true,
			"registry":       index.Registry,
			"total_entries":  index.TotalEntries,
			"shards_written": len(shards),
			"index_revision": indexPut.Result.Object.LatestRevision,
			"diagnostics":    diagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("gateway pushed registry=%s entries=%d shards=%d revision=%d", index.Registry, index.TotalEntries, len(shards), indexPut.Result.Object.LatestRevision)
	for _, d := range diagnostics {
		fmt.Printf("%s %s\n", styleHeading(strings.ToUpper(d.Level)+":"), d.Message)
	}
}

func cmdOrbitsGatewayPull(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("orbits gateway pull", flag.ExitOnError)
	registry := fs.String("registry", "", "gateway registry name")
	namespace := fs.String("namespace", "", "optional namespace filter")
	capability := fs.String("capability", "", "optional capability filter")
	prefix := fs.String("prefix", "", "optional orbit id prefix filter")
	limit := fs.Int("limit", 0, "optional max entries")
	outPath := fs.String("out", "", "catalog output path")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si orbits gateway pull [--registry <name>] [--namespace <namespace>] [--capability <capability>] [--prefix <id-prefix>] [--limit <n>] [--out <file>] [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	targetRegistry := orbitGatewayRegistryName(settings, *registry)
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}

	ctx := sunContext(settings)
	indexPayload, err := client.getIntegrationRegistryIndex(ctx, targetRegistry)
	if err != nil {
		fatal(err)
	}
	var index orbitals.GatewayIndex
	if err := json.Unmarshal(indexPayload, &index); err != nil {
		fatal(fmt.Errorf("decode gateway index: %w", err))
	}

	filter := orbitals.GatewaySelectFilter{
		Namespace:  strings.TrimSpace(*namespace),
		Capability: strings.TrimSpace(*capability),
		Prefix:     strings.TrimSpace(*prefix),
		Limit:      *limit,
	}
	keys := orbitals.SelectGatewayShards(index, filter)
	shards := map[string]orbitals.GatewayShard{}
	for _, key := range keys {
		raw, err := client.getIntegrationRegistryShard(ctx, index.Registry, key)
		if err != nil {
			fatal(err)
		}
		var shard orbitals.GatewayShard
		if err := json.Unmarshal(raw, &shard); err != nil {
			fatal(fmt.Errorf("decode gateway shard %s: %w", key, err))
		}
		shards[key] = shard
	}
	catalog := orbitals.MaterializeGatewayCatalog(index, shards, filter)
	targetPath, err := orbitGatewayOutputPath(*outPath, index.Registry)
	if err != nil {
		fatal(err)
	}
	if err := writeGatewayCatalog(targetPath, catalog); err != nil {
		fatal(err)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":             true,
			"registry":       index.Registry,
			"entries":        len(catalog.Entries),
			"shards_fetched": len(shards),
			"path":           targetPath,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("gateway pulled registry=%s entries=%d shards=%d", index.Registry, len(catalog.Entries), len(shards))
	infof("catalog written to %s", targetPath)
}

func cmdOrbitsGatewayStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("orbits gateway status", flag.ExitOnError)
	registry := fs.String("registry", "", "gateway registry name")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si orbits gateway status [--registry <name>] [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	targetRegistry := orbitGatewayRegistryName(settings, *registry)
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	raw, err := client.getIntegrationRegistryIndex(sunContext(settings), targetRegistry)
	if err != nil {
		fatal(err)
	}
	var index orbitals.GatewayIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		fatal(fmt.Errorf("decode gateway index: %w", err))
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]interface{}{
			"ok":            true,
			"registry":      index.Registry,
			"generated_at":  index.GeneratedAt,
			"total_entries": index.TotalEntries,
			"shards":        len(index.Shards),
			"namespaces":    len(index.Namespaces),
		}); err != nil {
			fatal(err)
		}
		return
	}
	successf("gateway registry=%s entries=%d shards=%d namespaces=%d generated_at=%s", index.Registry, index.TotalEntries, len(index.Shards), len(index.Namespaces), index.GeneratedAt)
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

func orbitGatewayRegistryName(settings Settings, explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return strings.ToLower(trimmed)
	}
	if env := envSunOrbitGatewayRegistry(); env != "" {
		return strings.ToLower(env)
	}
	if trimmed := strings.TrimSpace(settings.Sun.OrbitGatewayRegistry); trimmed != "" {
		return strings.ToLower(trimmed)
	}
	return defaultOrbitGatewayName
}

func orbitGatewaySlots(settings Settings, explicit int) int {
	if explicit > 0 {
		return explicit
	}
	if env := strings.TrimSpace(envSunOrbitGatewaySlots()); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			return parsed
		}
	}
	if settings.Sun.OrbitGatewaySlots > 0 {
		return settings.Sun.OrbitGatewaySlots
	}
	return orbitals.GatewayDefaultSlotsPerNamespace
}
