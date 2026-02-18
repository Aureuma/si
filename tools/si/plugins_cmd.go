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

	"si/tools/si/internal/pluginmarket"
)

const pluginsUsageText = "usage: si plugins <list|catalog|info|install|update|uninstall|enable|disable|doctor|register|scaffold|policy>"

func cmdPlugins(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, pluginsUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(pluginsUsageText)
	case "list", "ls":
		cmdPluginsList(rest)
	case "catalog":
		cmdPluginsCatalog(rest)
	case "info":
		cmdPluginsInfo(rest)
	case "install", "add":
		cmdPluginsInstall(rest)
	case "update", "upgrade":
		cmdPluginsUpdate(rest)
	case "uninstall", "remove", "rm", "delete":
		cmdPluginsUninstall(rest)
	case "enable":
		cmdPluginsEnableDisable(rest, true)
	case "disable":
		cmdPluginsEnableDisable(rest, false)
	case "doctor", "health":
		cmdPluginsDoctor(rest)
	case "register":
		cmdPluginsRegister(rest)
	case "scaffold", "init":
		cmdPluginsScaffold(rest)
	case "policy":
		cmdPluginsPolicy(rest)
	default:
		printUnknown("plugins", sub)
		printUsage(pluginsUsageText)
	}
}

func cmdPluginsCatalog(args []string) {
	if len(args) == 0 {
		cmdPluginsList(args)
		return
	}
	first := strings.TrimSpace(args[0])
	if strings.HasPrefix(first, "-") {
		cmdPluginsList(args)
		return
	}
	sub := strings.ToLower(first)
	rest := args[1:]
	switch sub {
	case "list", "ls":
		cmdPluginsList(rest)
	case "build":
		cmdPluginsCatalogBuild(rest)
	case "validate", "check":
		cmdPluginsCatalogValidate(rest)
	default:
		printUnknown("plugins catalog", sub)
		printUsage("usage: si plugins catalog <list|build|validate>")
	}
}

func loadPluginRuntime() (pluginmarket.Paths, pluginmarket.Catalog, pluginmarket.State, []pluginmarket.Diagnostic, error) {
	paths, err := pluginmarket.DefaultPaths()
	if err != nil {
		return pluginmarket.Paths{}, pluginmarket.Catalog{}, pluginmarket.State{}, nil, err
	}
	catalog, catalogDiagnostics, err := pluginmarket.LoadCatalog(paths)
	if err != nil {
		return pluginmarket.Paths{}, pluginmarket.Catalog{}, pluginmarket.State{}, nil, err
	}
	state, err := pluginmarket.LoadState(paths)
	if err != nil {
		return pluginmarket.Paths{}, pluginmarket.Catalog{}, pluginmarket.State{}, nil, err
	}
	return paths, catalog, state, catalogDiagnostics, nil
}

func cmdPluginsList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("plugins list", flag.ExitOnError)
	installedOnly := fs.Bool("installed", false, "show only installed plugins")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si plugins list [--installed] [--json]")
		return
	}
	paths, catalog, state, catalogDiagnostics, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	catalogByID := pluginmarket.CatalogByID(catalog)
	idSet := map[string]bool{}
	for id := range catalogByID {
		idSet[id] = true
	}
	for id := range state.Installs {
		idSet[id] = true
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	type row struct {
		ID               string                      `json:"id"`
		Installed        bool                        `json:"installed"`
		Enabled          bool                        `json:"enabled"`
		EffectiveEnabled bool                        `json:"effective_enabled"`
		EffectiveReason  string                      `json:"effective_reason,omitempty"`
		Channel          string                      `json:"channel,omitempty"`
		Verified         bool                        `json:"verified,omitempty"`
		Kind             string                      `json:"kind,omitempty"`
		Maturity         string                      `json:"maturity,omitempty"`
		InstallType      string                      `json:"install_type,omitempty"`
		Summary          string                      `json:"summary,omitempty"`
		InstalledAt      string                      `json:"installed_at,omitempty"`
		Record           *pluginmarket.InstallRecord `json:"record,omitempty"`
	}

	rows := make([]row, 0, len(ids))
	for _, id := range ids {
		record, installed := state.Installs[id]
		entry, inCatalog := catalogByID[id]
		if *installedOnly && !installed {
			continue
		}
		r := row{ID: id, Installed: installed, Enabled: installed && record.Enabled}
		if inCatalog {
			r.Channel = entry.Channel
			r.Verified = entry.Verified
			r.Kind = entry.Manifest.Kind
			r.Maturity = entry.Manifest.Maturity
			r.InstallType = entry.Manifest.Install.Type
			r.Summary = entry.Manifest.Summary
		}
		if installed {
			r.Enabled = record.Enabled
			r.EffectiveEnabled, r.EffectiveReason = pluginmarket.ResolveEnableState(id, record, state.Policy)
			r.InstalledAt = record.InstalledAt
			recordCopy := record
			r.Record = &recordCopy
			if r.Kind == "" {
				r.Kind = record.Manifest.Kind
			}
			if r.Maturity == "" {
				r.Maturity = record.Manifest.Maturity
			}
			if r.InstallType == "" {
				r.InstallType = record.Manifest.Install.Type
			}
			if r.Summary == "" {
				r.Summary = record.Manifest.Summary
			}
		}
		if !installed {
			r.EffectiveEnabled = false
			r.EffectiveReason = "not installed"
		}
		rows = append(rows, r)
	}

	if *jsonOut {
		payload := map[string]interface{}{
			"paths":        paths,
			"policy":       state.Policy,
			"catalog_size": len(catalog.Entries),
			"rows":         rows,
			"diagnostics":  catalogDiagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}

	if len(rows) == 0 {
		infof("no plugin entries found")
		return
	}
	headers := []string{styleHeading("ID"), styleHeading("INSTALLED"), styleHeading("ENABLED"), styleHeading("EFFECTIVE"), styleHeading("CHANNEL"), styleHeading("TYPE"), styleHeading("MATURITY")}
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.ID,
			boolText(r.Installed),
			boolText(r.Enabled),
			boolText(r.EffectiveEnabled),
			orDash(r.Channel),
			orDash(r.InstallType),
			orDash(r.Maturity),
		})
	}
	printAlignedTable(headers, tableRows, 2)
	for _, diagnostic := range catalogDiagnostics {
		fmt.Printf("%s %s\n", styleHeading(strings.ToUpper(diagnostic.Level)+":"), diagnostic.Message)
	}
}

func cmdPluginsInfo(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("plugins info", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si plugins info <id> [--json]")
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	paths, catalog, state, catalogDiagnostics, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	catalogEntry, inCatalog := pluginmarket.CatalogByID(catalog)[id]
	record, installed := state.Installs[id]
	if !inCatalog && !installed {
		fatal(fmt.Errorf("unknown plugin %q", id))
	}
	effectiveEnabled := false
	effectiveReason := "not installed"
	if installed {
		effectiveEnabled, effectiveReason = pluginmarket.ResolveEnableState(id, record, state.Policy)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"id":                  id,
			"paths":               paths,
			"policy":              state.Policy,
			"in_catalog":          inCatalog,
			"installed":           installed,
			"effective_enabled":   effectiveEnabled,
			"effective_reason":    effectiveReason,
			"catalog_entry":       catalogEntry,
			"installed_record":    record,
			"catalog_diagnostics": catalogDiagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Plugin:"), id)
	fmt.Printf("  in_catalog=%s installed=%s\n", boolText(inCatalog), boolText(installed))
	fmt.Printf("  effective_enabled=%s reason=%s\n", boolText(effectiveEnabled), effectiveReason)
	if inCatalog {
		fmt.Printf("  channel=%s verified=%s\n", orDash(catalogEntry.Channel), boolText(catalogEntry.Verified))
		fmt.Printf("  install_type=%s\n", orDash(catalogEntry.Manifest.Install.Type))
		if summary := strings.TrimSpace(catalogEntry.Manifest.Summary); summary != "" {
			fmt.Printf("  summary=%s\n", summary)
		}
	}
	if installed {
		fmt.Printf("  enabled=%s source=%s\n", boolText(record.Enabled), record.Source)
		fmt.Printf("  installed_at=%s\n", record.InstalledAt)
		if record.InstallDir != "" {
			fmt.Printf("  install_dir=%s\n", record.InstallDir)
		}
	}
	for _, diagnostic := range catalogDiagnostics {
		fmt.Printf("%s %s\n", styleHeading(strings.ToUpper(diagnostic.Level)+":"), diagnostic.Message)
	}
}

func cmdPluginsInstall(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "disabled": true})
	fs := flag.NewFlagSet("plugins install", flag.ExitOnError)
	disabled := fs.Bool("disabled", false, "install plugin as disabled")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si plugins install <id-or-path> [--disabled] [--json]")
		return
	}
	target := strings.TrimSpace(fs.Arg(0))
	paths, catalog, state, catalogDiagnostics, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	enabled := !*disabled
	now := time.Now().UTC()
	var record pluginmarket.InstallRecord
	if _, statErr := os.Stat(target); statErr == nil {
		record, err = pluginmarket.InstallFromSource(paths, target, enabled, now)
		if err != nil {
			fatal(err)
		}
	} else {
		entry, ok := pluginmarket.CatalogByID(catalog)[target]
		if !ok {
			fatal(fmt.Errorf("unknown plugin %q (not found as path or catalog id)", target))
		}
		record, err = pluginmarket.InstallFromCatalog(paths, entry, enabled, now)
		if err != nil {
			fatal(err)
		}
	}
	if state.Installs == nil {
		state.Installs = map[string]pluginmarket.InstallRecord{}
	}
	state.Installs[record.ID] = record
	if err := pluginmarket.SaveState(paths, state); err != nil {
		fatal(err)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":                  true,
			"record":              record,
			"catalog_diagnostics": catalogDiagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("plugin installed: %s (enabled=%s)", record.ID, boolText(record.Enabled))
}

func cmdPluginsUpdate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "all": true})
	fs := flag.NewFlagSet("plugins update", flag.ExitOnError)
	updateAll := fs.Bool("all", false, "update all installed plugins")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si plugins update <id>|--all [--json]")
		return
	}
	paths, catalog, state, catalogDiagnostics, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	targetIDs := make([]string, 0)
	if *updateAll {
		for id := range state.Installs {
			targetIDs = append(targetIDs, id)
		}
		sort.Strings(targetIDs)
	} else {
		if fs.NArg() != 1 {
			printUsage("usage: si plugins update <id>|--all [--json]")
			return
		}
		targetIDs = append(targetIDs, strings.TrimSpace(fs.Arg(0)))
	}
	if len(targetIDs) == 0 {
		if *jsonOut {
			payload := map[string]interface{}{
				"ok":                  true,
				"updated":             []string{},
				"errors":              []string{},
				"catalog_diagnostics": catalogDiagnostics,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(payload); err != nil {
				fatal(err)
			}
			return
		}
		infof("no installed plugins to update")
		return
	}
	catalogByID := pluginmarket.CatalogByID(catalog)
	now := time.Now().UTC()
	updated := make([]string, 0, len(targetIDs))
	errs := make([]string, 0)
	for _, id := range targetIDs {
		record, ok := state.Installs[id]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: not installed", id))
			continue
		}
		var next pluginmarket.InstallRecord
		source := strings.TrimSpace(record.Source)
		switch {
		case strings.HasPrefix(source, "catalog:"):
			entry, ok := catalogByID[id]
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: catalog entry not found", id))
				continue
			}
			next, err = pluginmarket.InstallFromCatalog(paths, entry, record.Enabled, now)
		case strings.HasPrefix(source, "path:"):
			next, err = pluginmarket.InstallFromSource(paths, strings.TrimPrefix(source, "path:"), record.Enabled, now)
		case strings.HasPrefix(source, "archive:"):
			next, err = pluginmarket.InstallFromSource(paths, strings.TrimPrefix(source, "archive:"), record.Enabled, now)
		default:
			err = fmt.Errorf("unsupported install source %q", source)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		state.Installs[id] = next
		updated = append(updated, id)
	}
	if err := pluginmarket.SaveState(paths, state); err != nil {
		fatal(err)
	}
	ok := len(errs) == 0
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":                  ok,
			"updated":             updated,
			"errors":              errs,
			"catalog_diagnostics": catalogDiagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	for _, id := range updated {
		successf("plugin updated: %s", id)
	}
	for _, item := range errs {
		warnf("%s", item)
	}
	if !ok {
		fatal(fmt.Errorf("plugins update completed with %d error(s)", len(errs)))
	}
}

func cmdPluginsUninstall(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "keep-files": true})
	fs := flag.NewFlagSet("plugins uninstall", flag.ExitOnError)
	keepFiles := fs.Bool("keep-files", false, "keep installed files on disk")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si plugins uninstall <id> [--keep-files] [--json]")
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	paths, _, state, _, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	record, ok := state.Installs[id]
	if !ok {
		fatal(fmt.Errorf("plugin %q is not installed", id))
	}
	delete(state.Installs, id)
	if err := pluginmarket.SaveState(paths, state); err != nil {
		fatal(err)
	}
	if !*keepFiles {
		if err := pluginmarket.RemoveInstallDir(paths, record.InstallDir); err != nil {
			fatal(err)
		}
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":         true,
			"id":         id,
			"keep_files": *keepFiles,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("plugin removed: %s", id)
}

func cmdPluginsEnableDisable(args []string, enabled bool) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	name := "disable"
	if enabled {
		name = "enable"
	}
	fs := flag.NewFlagSet("plugins "+name, flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage(fmt.Sprintf("usage: si plugins %s <id> [--json]", name))
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	paths, _, state, _, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	record, ok := state.Installs[id]
	if !ok {
		fatal(fmt.Errorf("plugin %q is not installed", id))
	}
	record.Enabled = enabled
	state.Installs[id] = record
	if err := pluginmarket.SaveState(paths, state); err != nil {
		fatal(err)
	}
	if *jsonOut {
		payload := map[string]interface{}{"ok": true, "id": id, "enabled": enabled}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if enabled {
		successf("plugin enabled: %s", id)
		return
	}
	successf("plugin disabled: %s", id)
}

func cmdPluginsDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("plugins doctor", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si plugins doctor [--json]")
		return
	}
	paths, catalog, state, catalogDiagnostics, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	diagnostics := append([]pluginmarket.Diagnostic{}, catalogDiagnostics...)
	diagnostics = append(diagnostics, pluginmarket.Doctor(catalog, state, paths)...)
	counts := map[string]int{"info": 0, "warn": 0, "error": 0}
	for _, diagnostic := range diagnostics {
		level := strings.ToLower(strings.TrimSpace(diagnostic.Level))
		switch level {
		case "error", "warn", "info":
			counts[level]++
		default:
			counts["info"]++
		}
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":          counts["error"] == 0,
			"counts":      counts,
			"diagnostics": diagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s info=%d warn=%d error=%d\n", styleHeading("plugins doctor:"), counts["info"], counts["warn"], counts["error"])
	for _, diagnostic := range diagnostics {
		level := strings.ToLower(strings.TrimSpace(diagnostic.Level))
		label := strings.ToUpper(level)
		if label == "" {
			label = "INFO"
		}
		if diagnostic.Source != "" {
			fmt.Printf("%s %s (%s)\n", styleHeading(label+":"), diagnostic.Message, diagnostic.Source)
			continue
		}
		fmt.Printf("%s %s\n", styleHeading(label+":"), diagnostic.Message)
	}
	if counts["error"] == 0 {
		successf("plugins doctor passed")
		return
	}
	fatal(fmt.Errorf("plugins doctor found %d error(s)", counts["error"]))
}

func cmdPluginsRegister(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "verified": true})
	fs := flag.NewFlagSet("plugins register", flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "path to plugin manifest file or directory")
	channel := fs.String("channel", "community", "catalog channel label")
	verified := fs.Bool("verified", false, "mark plugin as verified in catalog metadata")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	pathArg := strings.TrimSpace(*manifestPath)
	if pathArg == "" && fs.NArg() == 1 {
		pathArg = strings.TrimSpace(fs.Arg(0))
	}
	if pathArg == "" || fs.NArg() > 1 {
		printUsage("usage: si plugins register [--manifest <path>|<path>] [--channel <label>] [--verified] [--json]")
		return
	}
	paths, err := pluginmarket.DefaultPaths()
	if err != nil {
		fatal(err)
	}
	manifest, _, err := pluginmarket.ReadManifestFromPath(pathArg)
	if err != nil {
		fatal(err)
	}
	entry := pluginmarket.CatalogEntry{
		Manifest: manifest,
		Channel:  strings.TrimSpace(*channel),
		Verified: *verified,
		AddedAt:  time.Now().UTC().Format("2006-01-02"),
	}
	if entry.Channel == "" {
		entry.Channel = "community"
	}
	if err := pluginmarket.UpsertUserCatalogEntry(paths, entry); err != nil {
		fatal(err)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":           true,
			"id":           manifest.ID,
			"catalog_file": paths.CatalogFile,
			"entry":        entry,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("plugin registered in catalog: %s", manifest.ID)
	fmt.Printf("  catalog_file=%s\n", paths.CatalogFile)
}

func cmdPluginsScaffold(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "force": true})
	fs := flag.NewFlagSet("plugins scaffold", flag.ExitOnError)
	dir := fs.String("dir", ".", "base directory for scaffold output")
	force := fs.Bool("force", false, "overwrite existing manifest")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si plugins scaffold <namespace/name> [--dir <path>] [--force] [--json]")
		return
	}
	id := strings.TrimSpace(fs.Arg(0))
	manifest, err := pluginmarket.ScaffoldManifest(id)
	if err != nil {
		fatal(err)
	}
	baseDir, err := filepath.Abs(strings.TrimSpace(*dir))
	if err != nil {
		fatal(err)
	}
	relDir := strings.ReplaceAll(manifest.ID, "/", string(filepath.Separator))
	targetDir := filepath.Join(baseDir, relDir)
	manifestPath := filepath.Join(targetDir, pluginmarket.ManifestFileName)
	if _, err := os.Stat(manifestPath); err == nil && !*force {
		fatal(fmt.Errorf("manifest already exists: %s (use --force to overwrite)", manifestPath))
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		fatal(err)
	}
	raw, err := pluginmarket.EncodeManifest(manifest)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
		fatal(err)
	}
	readmePath := filepath.Join(targetDir, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		readme := "# " + manifest.ID + "\n\nDescribe the integration implementation and operational notes here.\n"
		_ = os.WriteFile(readmePath, []byte(readme), 0o644)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":            true,
			"id":            manifest.ID,
			"target_dir":    targetDir,
			"manifest_path": manifestPath,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("plugin scaffold created: %s", manifest.ID)
	fmt.Printf("  dir=%s\n", targetDir)
	fmt.Printf("  manifest=%s\n", manifestPath)
}

func cmdPluginsPolicy(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si plugins policy <show|set>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show", "list", "status":
		cmdPluginsPolicyShow(rest)
	case "set", "update":
		cmdPluginsPolicySet(rest)
	default:
		printUnknown("plugins policy", sub)
		printUsage("usage: si plugins policy <show|set>")
	}
}

func cmdPluginsPolicyShow(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("plugins policy show", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si plugins policy show [--json]")
		return
	}
	_, _, state, _, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"policy": state.Policy,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s enabled=%s\n", styleHeading("plugins policy:"), boolText(state.Policy.Enabled))
	fmt.Printf("  allow=%s\n", joinOrDash(state.Policy.Allow))
	fmt.Printf("  deny=%s\n", joinOrDash(state.Policy.Deny))
}

func cmdPluginsPolicySet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "clear-allow": true, "clear-deny": true})
	fs := flag.NewFlagSet("plugins policy set", flag.ExitOnError)
	enabled := fs.String("enabled", "", "set global plugin policy enabled state (true|false)")
	clearAllow := fs.Bool("clear-allow", false, "clear allowlist before applying --allow entries")
	clearDeny := fs.Bool("clear-deny", false, "clear denylist before applying --deny entries")
	jsonOut := fs.Bool("json", false, "output json")
	var allow multiFlag
	var deny multiFlag
	fs.Var(&allow, "allow", "allowlist plugin id (repeatable)")
	fs.Var(&deny, "deny", "denylist plugin id (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si plugins policy set [--enabled <true|false>] [--allow <id>]... [--deny <id>]... [--clear-allow] [--clear-deny] [--json]")
		return
	}
	paths, _, state, _, err := loadPluginRuntime()
	if err != nil {
		fatal(err)
	}
	policy := state.Policy
	if strings.TrimSpace(*enabled) != "" {
		switch strings.ToLower(strings.TrimSpace(*enabled)) {
		case "true", "1", "yes", "on":
			policy.Enabled = true
		case "false", "0", "no", "off":
			policy.Enabled = false
		default:
			fatal(fmt.Errorf("invalid --enabled value %q (expected true|false)", *enabled))
		}
	}
	if *clearAllow {
		policy.Allow = nil
	}
	if *clearDeny {
		policy.Deny = nil
	}
	if len(allow) > 0 {
		policy.Allow = append(policy.Allow, []string(allow)...)
	}
	if len(deny) > 0 {
		policy.Deny = append(policy.Deny, []string(deny)...)
	}
	policy.Allow = normalizePluginIDList(policy.Allow)
	policy.Deny = normalizePluginIDList(policy.Deny)
	for _, id := range policy.Allow {
		if err := pluginmarket.ValidatePluginID(id); err != nil {
			fatal(fmt.Errorf("invalid --allow id %q: %w", id, err))
		}
	}
	for _, id := range policy.Deny {
		if err := pluginmarket.ValidatePluginID(id); err != nil {
			fatal(fmt.Errorf("invalid --deny id %q: %w", id, err))
		}
	}
	state.Policy = policy
	if err := pluginmarket.SaveState(paths, state); err != nil {
		fatal(err)
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":     true,
			"policy": state.Policy,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("plugins policy updated")
	fmt.Printf("  enabled=%s\n", boolText(state.Policy.Enabled))
	fmt.Printf("  allow=%s\n", joinOrDash(state.Policy.Allow))
	fmt.Printf("  deny=%s\n", joinOrDash(state.Policy.Deny))
}

func cmdPluginsCatalogBuild(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "verified": true})
	fs := flag.NewFlagSet("plugins catalog build", flag.ExitOnError)
	source := fs.String("source", "", "manifest file or directory tree")
	output := fs.String("output", "", "output catalog file path")
	channel := fs.String("channel", "community", "catalog channel label")
	addedAt := fs.String("added-at", time.Now().UTC().Format("2006-01-02"), "catalog added_at date (YYYY-MM-DD)")
	verified := fs.Bool("verified", false, "mark all built entries as verified")
	jsonOut := fs.Bool("json", false, "output json")
	var tags multiFlag
	fs.Var(&tags, "tag", "catalog tag to attach to each entry (repeatable)")
	_ = fs.Parse(args)
	if strings.TrimSpace(*source) == "" || fs.NArg() > 0 {
		printUsage("usage: si plugins catalog build --source <path> [--output <path>] [--channel <label>] [--verified] [--tag <value>]... [--added-at YYYY-MM-DD] [--json]")
		return
	}
	catalog, diagnostics, err := pluginmarket.BuildCatalogFromSource(strings.TrimSpace(*source), pluginmarket.BuildCatalogOptions{
		Channel:  strings.TrimSpace(*channel),
		Verified: *verified,
		AddedAt:  strings.TrimSpace(*addedAt),
		Tags:     tags,
	})
	if err != nil {
		fatal(err)
	}
	outputPath := strings.TrimSpace(*output)
	if outputPath != "" {
		resolvedOutput, err := filepath.Abs(outputPath)
		if err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(resolvedOutput), 0o755); err != nil {
			fatal(err)
		}
		raw, err := json.MarshalIndent(catalog, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(resolvedOutput, append(raw, '\n'), 0o644); err != nil {
			fatal(err)
		}
		outputPath = resolvedOutput
	}
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":          true,
			"output":      outputPath,
			"entries":     len(catalog.Entries),
			"catalog":     catalog,
			"diagnostics": diagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("catalog built with %d entries", len(catalog.Entries))
	if outputPath != "" {
		fmt.Printf("  output=%s\n", outputPath)
	}
	for _, diagnostic := range diagnostics {
		fmt.Printf("%s %s\n", styleHeading(strings.ToUpper(diagnostic.Level)+":"), diagnostic.Message)
	}
}

func cmdPluginsCatalogValidate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("plugins catalog validate", flag.ExitOnError)
	source := fs.String("source", "", "manifest file or directory tree")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if strings.TrimSpace(*source) == "" || fs.NArg() > 0 {
		printUsage("usage: si plugins catalog validate --source <path> [--json]")
		return
	}
	catalog, diagnostics, err := pluginmarket.BuildCatalogFromSource(strings.TrimSpace(*source), pluginmarket.BuildCatalogOptions{})
	if err != nil {
		fatal(err)
	}
	counts := map[string]int{"info": 0, "warn": 0, "error": 0}
	for _, diagnostic := range diagnostics {
		switch strings.ToLower(strings.TrimSpace(diagnostic.Level)) {
		case "error", "warn", "info":
			counts[strings.ToLower(strings.TrimSpace(diagnostic.Level))]++
		default:
			counts["info"]++
		}
	}
	ok := counts["error"] == 0
	if *jsonOut {
		payload := map[string]interface{}{
			"ok":          ok,
			"entries":     len(catalog.Entries),
			"counts":      counts,
			"diagnostics": diagnostics,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s entries=%d warn=%d error=%d\n", styleHeading("plugins catalog validate:"), len(catalog.Entries), counts["warn"], counts["error"])
	for _, diagnostic := range diagnostics {
		fmt.Printf("%s %s\n", styleHeading(strings.ToUpper(strings.TrimSpace(diagnostic.Level))+":"), diagnostic.Message)
	}
	if !ok {
		fatal(fmt.Errorf("catalog validation failed"))
	}
	successf("catalog validation passed")
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

func normalizePluginIDList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func boolText(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
