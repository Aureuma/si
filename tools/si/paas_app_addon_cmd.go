package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const paasAddonMergeStrategyAdditiveNoOverride = "additive_no_override"

var paasAppAddonActions = []subcommandAction{
	{Name: "contract", Description: "show add-on/service-pack contract"},
	{Name: "enable", Description: "enable add-on pack for app"},
	{Name: "list", Description: "list configured add-ons"},
	{Name: "disable", Description: "disable add-on pack for app"},
}

type paasAddonPackContract struct {
	Pack           string `json:"pack"`
	Class          string `json:"class"`
	Description    string `json:"description"`
	DefaultVersion string `json:"default_version"`
	DefaultService string `json:"default_service"`
	MergeStrategy  string `json:"merge_strategy"`
}

type paasAddonStore struct {
	Apps map[string][]paasAddonRecord `json:"apps,omitempty"`
}

type paasAddonRecord struct {
	App           string            `json:"app"`
	Name          string            `json:"name"`
	Pack          string            `json:"pack"`
	Class         string            `json:"class"`
	Service       string            `json:"service"`
	Version       string            `json:"version"`
	MergeStrategy string            `json:"merge_strategy"`
	FragmentPath  string            `json:"fragment_path"`
	Vars          map[string]string `json:"vars,omitempty"`
	UpdatedAt     string            `json:"updated_at,omitempty"`
}

var paasAddonCatalog = map[string]paasAddonPackContract{
	"postgres": {
		Pack:           "postgres",
		Class:          "db",
		Description:    "PostgreSQL stateful database add-on",
		DefaultVersion: "16",
		DefaultService: "postgres",
		MergeStrategy:  paasAddonMergeStrategyAdditiveNoOverride,
	},
	"redis": {
		Pack:           "redis",
		Class:          "cache",
		Description:    "Redis cache add-on",
		DefaultVersion: "7",
		DefaultService: "redis",
		MergeStrategy:  paasAddonMergeStrategyAdditiveNoOverride,
	},
	"nats": {
		Pack:           "nats",
		Class:          "queue",
		Description:    "NATS message queue add-on",
		DefaultVersion: "2",
		DefaultService: "nats",
		MergeStrategy:  paasAddonMergeStrategyAdditiveNoOverride,
	},
	"supabase-walg": {
		Pack:           "supabase-walg",
		Class:          "backup",
		Description:    "Supabase WAL-G backup/restore sidecars for object-storage snapshots",
		DefaultVersion: "latest",
		DefaultService: "supabase-walg",
		MergeStrategy:  paasAddonMergeStrategyAdditiveNoOverride,
	},
	"databasus": {
		Pack:           "databasus",
		Class:          "backup",
		Description:    "Databasus private metadata service (no host web exposure)",
		DefaultVersion: "latest",
		DefaultService: "databasus",
		MergeStrategy:  paasAddonMergeStrategyAdditiveNoOverride,
	},
}

func cmdPaasAppAddon(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasAppAddonAction)
	if showUsage {
		printUsage("usage: si paas app addon <contract|enable|list|disable> [args...]")
		return
	}
	if !ok {
		return
	}
	args = resolved
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage("usage: si paas app addon <contract|enable|list|disable> [args...]")
	case "contract":
		cmdPaasAppAddonContract(rest)
	case "enable":
		cmdPaasAppAddonEnable(rest)
	case "list":
		cmdPaasAppAddonList(rest)
	case "disable", "remove", "rm":
		cmdPaasAppAddonDisable(rest)
	default:
		printUnknown("paas app addon", sub)
		printUsage("usage: si paas app addon <contract|enable|list|disable> [args...]")
		os.Exit(1)
	}
}

func selectPaasAppAddonAction() (string, bool) {
	return selectSubcommandAction("PaaS app addon commands:", paasAppAddonActions)
}

func cmdPaasAppAddonContract(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app addon contract", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas app addon contract [--json]")
		return
	}
	rows := listPaasAddonContracts()
	if jsonOut {
		payload := map[string]any{
			"ok":             true,
			"command":        "app addon contract",
			"context":        currentPaasContext(),
			"mode":           "live",
			"merge_strategy": paasAddonMergeStrategyAdditiveNoOverride,
			"count":          len(rows),
			"data":           rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("app addon contract", "succeeded", "live", map[string]string{
			"count": intString(len(rows)),
		}, nil)
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas app addon contract:"), len(rows))
	for _, row := range rows {
		fmt.Printf("  pack=%s class=%s default_version=%s default_service=%s merge_strategy=%s\n", row.Pack, row.Class, row.DefaultVersion, row.DefaultService, row.MergeStrategy)
		fmt.Printf("    %s\n", styleDim(row.Description))
	}
	_ = recordPaasAuditEvent("app addon contract", "succeeded", "live", map[string]string{
		"count": intString(len(rows)),
	}, nil)
}

func cmdPaasAppAddonEnable(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app addon enable", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	name := fs.String("name", "", "addon name (defaults to pack)")
	pack := fs.String("pack", "", "addon pack (postgres|redis|nats|supabase-walg|databasus)")
	service := fs.String("service", "", "compose service name override")
	version := fs.String("version", "", "pack image version override")
	setVars := fs.String("set", "", "addon variables as key=value CSV")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas app addon enable --app <slug> --pack <postgres|redis|nats|supabase-walg|databasus> [--name <addon>] [--service <name>] [--version <value>] [--set <k=v,...>] [--json]")
		return
	}
	if !requirePaasValue(*app, "app", "usage: si paas app addon enable --app <slug> --pack <postgres|redis|nats|supabase-walg|databasus> [--name <addon>] [--service <name>] [--version <value>] [--set <k=v,...>] [--json]") {
		return
	}
	if !requirePaasValue(*pack, "pack", "usage: si paas app addon enable --app <slug> --pack <postgres|redis|nats|supabase-walg|databasus> [--name <addon>] [--service <name>] [--version <value>] [--set <k=v,...>] [--json]") {
		return
	}
	contract, ok := resolvePaasAddonContract(*pack)
	if !ok {
		printUnknown("paas app addon pack", strings.TrimSpace(*pack))
		printUsage("usage: si paas app addon enable --app <slug> --pack <postgres|redis|nats|supabase-walg|databasus> [--name <addon>] [--service <name>] [--version <value>] [--set <k=v,...>] [--json]")
		return
	}
	vars, err := parsePaasAddonSetVars(*setVars)
	if err != nil {
		failPaasCommand("app addon enable", jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass --set entries as key=value pairs",
			err,
		), nil)
	}
	resolvedApp := strings.TrimSpace(*app)
	resolvedName := strings.TrimSpace(*name)
	if resolvedName == "" {
		resolvedName = contract.Pack
	}
	resolvedService := strings.TrimSpace(*service)
	if resolvedService == "" {
		resolvedService = contract.DefaultService + "-" + sanitizePaasComposeProjectSegment(resolvedName)
	}
	resolvedVersion := strings.TrimSpace(*version)
	if resolvedVersion == "" {
		resolvedVersion = contract.DefaultVersion
	}
	fragmentPath, err := resolvePaasAddonFragmentPath(resolvedApp, resolvedName)
	if err != nil {
		failPaasCommand("app addon enable", jsonOut, err, nil)
	}
	record := paasAddonRecord{
		App:           resolvedApp,
		Name:          resolvedName,
		Pack:          contract.Pack,
		Class:         contract.Class,
		Service:       resolvedService,
		Version:       resolvedVersion,
		MergeStrategy: contract.MergeStrategy,
		FragmentPath:  fragmentPath,
		Vars:          vars,
		UpdatedAt:     utcNowRFC3339(),
	}
	fragment := renderPaasAddonComposeFragment(record)
	if err := os.MkdirAll(filepath.Dir(fragmentPath), 0o700); err != nil {
		failPaasCommand("app addon enable", jsonOut, err, nil)
	}
	if err := os.WriteFile(fragmentPath, []byte(fragment), 0o600); err != nil {
		failPaasCommand("app addon enable", jsonOut, err, nil)
	}
	store, err := loadPaasAddonStore()
	if err != nil {
		failPaasCommand("app addon enable", jsonOut, err, nil)
	}
	appKey := sanitizePaasReleasePathSegment(resolvedApp)
	store.Apps[appKey] = upsertPaasAddonRecord(store.Apps[appKey], record)
	if err := savePaasAddonStore(store); err != nil {
		failPaasCommand("app addon enable", jsonOut, err, nil)
	}
	printPaasScaffold("app addon enable", map[string]string{
		"app":            resolvedApp,
		"name":           resolvedName,
		"pack":           contract.Pack,
		"class":          contract.Class,
		"service":        resolvedService,
		"version":        resolvedVersion,
		"merge_strategy": contract.MergeStrategy,
		"fragment_path":  fragmentPath,
		"set_count":      intString(len(vars)),
	}, jsonOut)
}

func cmdPaasAppAddonList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app addon list", flag.ExitOnError)
	app := fs.String("app", "", "app slug filter")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas app addon list [--app <slug>] [--json]")
		return
	}
	store, err := loadPaasAddonStore()
	if err != nil {
		failPaasCommand("app addon list", jsonOut, err, nil)
	}
	rows := listPaasAddonRecords(store, strings.TrimSpace(*app))
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "app addon list",
			"context": currentPaasContext(),
			"mode":    "live",
			"app":     strings.TrimSpace(*app),
			"count":   len(rows),
			"data":    rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("app addon list", "succeeded", "live", map[string]string{
			"app":   strings.TrimSpace(*app),
			"count": intString(len(rows)),
		}, nil)
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas app addon list:"), len(rows))
	for _, row := range rows {
		fmt.Printf("  app=%s name=%s pack=%s class=%s service=%s version=%s merge=%s\n", row.App, row.Name, row.Pack, row.Class, row.Service, row.Version, row.MergeStrategy)
		fmt.Printf("    fragment=%s\n", styleDim(row.FragmentPath))
	}
	_ = recordPaasAuditEvent("app addon list", "succeeded", "live", map[string]string{
		"app":   strings.TrimSpace(*app),
		"count": intString(len(rows)),
	}, nil)
}

func cmdPaasAppAddonDisable(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app addon disable", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	name := fs.String("name", "", "addon name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas app addon disable --app <slug> --name <addon> [--json]")
		return
	}
	if !requirePaasValue(*app, "app", "usage: si paas app addon disable --app <slug> --name <addon> [--json]") {
		return
	}
	if !requirePaasValue(*name, "name", "usage: si paas app addon disable --app <slug> --name <addon> [--json]") {
		return
	}
	store, err := loadPaasAddonStore()
	if err != nil {
		failPaasCommand("app addon disable", jsonOut, err, nil)
	}
	appKey := sanitizePaasReleasePathSegment(*app)
	before := store.Apps[appKey]
	after := make([]paasAddonRecord, 0, len(before))
	removed := false
	removedFragment := ""
	for _, row := range before {
		if strings.EqualFold(strings.TrimSpace(row.Name), strings.TrimSpace(*name)) {
			removed = true
			removedFragment = strings.TrimSpace(row.FragmentPath)
			continue
		}
		after = append(after, row)
	}
	if len(after) == 0 {
		delete(store.Apps, appKey)
	} else {
		store.Apps[appKey] = after
	}
	if removed {
		_ = os.Remove(removedFragment)
	}
	if err := savePaasAddonStore(store); err != nil {
		failPaasCommand("app addon disable", jsonOut, err, nil)
	}
	printPaasScaffold("app addon disable", map[string]string{
		"app":              strings.TrimSpace(*app),
		"name":             strings.TrimSpace(*name),
		"removed":          boolString(removed),
		"removed_fragment": removedFragment,
	}, jsonOut)
}

func listPaasAddonContracts() []paasAddonPackContract {
	keys := make([]string, 0, len(paasAddonCatalog))
	for key := range paasAddonCatalog {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]paasAddonPackContract, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, paasAddonCatalog[key])
	}
	return rows
}

func resolvePaasAddonContract(raw string) (paasAddonPackContract, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	row, ok := paasAddonCatalog[key]
	return row, ok
}

func resolvePaasAddonStorePathForContext(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "addons.json"), nil
}

func loadPaasAddonStoreForContext(contextName string) (paasAddonStore, error) {
	path, err := resolvePaasAddonStorePathForContext(contextName)
	if err != nil {
		return paasAddonStore{}, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- local state path derived from context root.
	if err != nil {
		if os.IsNotExist(err) {
			return paasAddonStore{Apps: map[string][]paasAddonRecord{}}, nil
		}
		return paasAddonStore{}, err
	}
	var store paasAddonStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasAddonStore{}, fmt.Errorf("invalid addon store: %w", err)
	}
	if store.Apps == nil {
		store.Apps = map[string][]paasAddonRecord{}
	}
	for appKey, rows := range store.Apps {
		normalized := make([]paasAddonRecord, 0, len(rows))
		for _, row := range rows {
			normalized = append(normalized, normalizePaasAddonRecord(appKey, row))
		}
		store.Apps[appKey] = normalized
	}
	return store, nil
}

func loadPaasAddonStore() (paasAddonStore, error) {
	return loadPaasAddonStoreForContext(currentPaasContext())
}

func savePaasAddonStoreForContext(contextName string, store paasAddonStore) error {
	path, err := resolvePaasAddonStorePathForContext(contextName)
	if err != nil {
		return err
	}
	if store.Apps == nil {
		store.Apps = map[string][]paasAddonRecord{}
	}
	for appKey, rows := range store.Apps {
		normalized := make([]paasAddonRecord, 0, len(rows))
		for _, row := range rows {
			normalized = append(normalized, normalizePaasAddonRecord(appKey, row))
		}
		store.Apps[appKey] = normalized
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func savePaasAddonStore(store paasAddonStore) error {
	return savePaasAddonStoreForContext(currentPaasContext(), store)
}

func normalizePaasAddonRecord(appKey string, in paasAddonRecord) paasAddonRecord {
	out := paasAddonRecord{
		App:           strings.TrimSpace(in.App),
		Name:          strings.TrimSpace(in.Name),
		Pack:          strings.ToLower(strings.TrimSpace(in.Pack)),
		Class:         strings.ToLower(strings.TrimSpace(in.Class)),
		Service:       strings.TrimSpace(in.Service),
		Version:       strings.TrimSpace(in.Version),
		MergeStrategy: strings.TrimSpace(in.MergeStrategy),
		FragmentPath:  strings.TrimSpace(in.FragmentPath),
		Vars:          map[string]string{},
		UpdatedAt:     strings.TrimSpace(in.UpdatedAt),
	}
	if out.App == "" {
		out.App = appKey
	}
	if out.MergeStrategy == "" {
		out.MergeStrategy = paasAddonMergeStrategyAdditiveNoOverride
	}
	for key, value := range in.Vars {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		out.Vars[k] = strings.TrimSpace(value)
	}
	return out
}

func parsePaasAddonSetVars(raw string) (map[string]string, error) {
	out := map[string]string{}
	items := parseCSV(raw)
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --set entry %q", item)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid --set entry %q", item)
		}
		out[key] = value
	}
	return out, nil
}

func resolvePaasAddonFragmentPath(app, name string) (string, error) {
	contextDir, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		return "", err
	}
	return filepath.Join(
		contextDir,
		"addons",
		sanitizePaasReleasePathSegment(app),
		sanitizePaasReleasePathSegment(name)+".compose.yaml",
	), nil
}

func upsertPaasAddonRecord(rows []paasAddonRecord, record paasAddonRecord) []paasAddonRecord {
	out := make([]paasAddonRecord, 0, len(rows)+1)
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Name), strings.TrimSpace(record.Name)) {
			continue
		}
		out = append(out, row)
	}
	out = append(out, record)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func listPaasAddonRecords(store paasAddonStore, appFilter string) []paasAddonRecord {
	filter := strings.TrimSpace(appFilter)
	rows := []paasAddonRecord{}
	if filter != "" {
		appKey := sanitizePaasReleasePathSegment(filter)
		for _, row := range store.Apps[appKey] {
			rows = append(rows, normalizePaasAddonRecord(appKey, row))
		}
		sort.SliceStable(rows, func(i, j int) bool {
			return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
		})
		return rows
	}
	appKeys := make([]string, 0, len(store.Apps))
	for appKey := range store.Apps {
		appKeys = append(appKeys, appKey)
	}
	sort.Strings(appKeys)
	for _, appKey := range appKeys {
		for _, row := range store.Apps[appKey] {
			rows = append(rows, normalizePaasAddonRecord(appKey, row))
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ai := strings.ToLower(rows[i].App)
		aj := strings.ToLower(rows[j].App)
		if ai == aj {
			return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
		}
		return ai < aj
	})
	return rows
}

func renderPaasAddonComposeFragment(record paasAddonRecord) string {
	service := sanitizePaasComposeProjectSegment(record.Service)
	if service == "" || service == "x" {
		service = "addon"
	}
	version := strings.TrimSpace(record.Version)
	if version == "" {
		version = "latest"
	}
	header := []string{
		"# generated by `si paas app addon enable`",
		"# merge_strategy=" + strings.TrimSpace(record.MergeStrategy),
		"# pack=" + strings.TrimSpace(record.Pack),
		"# class=" + strings.TrimSpace(record.Class),
		"services:",
	}
	switch strings.ToLower(strings.TrimSpace(record.Pack)) {
	case "postgres":
		dbName := firstNonEmptyString(record.Vars["POSTGRES_DB"], "app")
		dbUser := firstNonEmptyString(record.Vars["POSTGRES_USER"], "app")
		lines := append(header,
			fmt.Sprintf("  %s:", service),
			fmt.Sprintf("    image: postgres:%s", version),
			"    restart: unless-stopped",
			"    environment:",
			fmt.Sprintf("      POSTGRES_DB: %q", dbName),
			fmt.Sprintf("      POSTGRES_USER: %q", dbUser),
			"      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}",
			"    volumes:",
			fmt.Sprintf("      - %s-data:/var/lib/postgresql/data", service),
			"volumes:",
			fmt.Sprintf("  %s-data: {}", service),
		)
		return strings.Join(lines, "\n") + "\n"
	case "redis":
		lines := append(header,
			fmt.Sprintf("  %s:", service),
			fmt.Sprintf("    image: redis:%s", version),
			"    restart: unless-stopped",
			"    command: [\"redis-server\", \"--appendonly\", \"yes\"]",
			"    volumes:",
			fmt.Sprintf("      - %s-data:/data", service),
			"volumes:",
			fmt.Sprintf("  %s-data: {}", service),
		)
		return strings.Join(lines, "\n") + "\n"
	case "supabase-walg":
		pgService := firstNonEmptyString(record.Vars["POSTGRES_SERVICE"], "postgres")
		pgDataVolume := firstNonEmptyString(record.Vars["PGDATA_VOLUME"], "postgres-data")
		pgDataPath := firstNonEmptyString(record.Vars["WALG_PGDATA"], "/var/lib/postgresql/data")
		backupService := service + "-backup"
		restoreService := service + "-restore"
		lines := append(header,
			fmt.Sprintf("  %s:", backupService),
			fmt.Sprintf("    image: ghcr.io/wal-g/wal-g:%s", version),
			"    restart: unless-stopped",
			"    depends_on:",
			fmt.Sprintf("      - %s", pgService),
			"    environment:",
			"      WALG_S3_PREFIX: ${WALG_S3_PREFIX}",
			"      AWS_ACCESS_KEY_ID: ${WALG_AWS_ACCESS_KEY_ID}",
			"      AWS_SECRET_ACCESS_KEY: ${WALG_AWS_SECRET_ACCESS_KEY}",
			"      AWS_ENDPOINT: ${WALG_AWS_ENDPOINT}",
			"      AWS_REGION: ${WALG_AWS_REGION:-auto}",
			"      AWS_S3_FORCE_PATH_STYLE: \"true\"",
			"      WALG_COMPRESSION_METHOD: ${WALG_COMPRESSION_METHOD:-zstd}",
			fmt.Sprintf("      WALG_PGDATA: %q", pgDataPath),
			"    command:",
			"      - sh",
			"      - -lc",
			"      - >-",
			"        while true; do",
			"          wal-g backup-push \"${WALG_PGDATA}\";",
			"          wal-g delete retain FULL \"${WALG_RETAIN_FULL:-14}\" --confirm || true;",
			"          sleep \"${WALG_BACKUP_INTERVAL_SECONDS:-21600}\";",
			"        done",
			"    volumes:",
			fmt.Sprintf("      - %s:%s:ro", pgDataVolume, pgDataPath),
			fmt.Sprintf("  %s:", restoreService),
			fmt.Sprintf("    image: ghcr.io/wal-g/wal-g:%s", version),
			"    profiles:",
			"      - restore",
			"    depends_on:",
			fmt.Sprintf("      - %s", pgService),
			"    environment:",
			"      WALG_S3_PREFIX: ${WALG_S3_PREFIX}",
			"      AWS_ACCESS_KEY_ID: ${WALG_AWS_ACCESS_KEY_ID}",
			"      AWS_SECRET_ACCESS_KEY: ${WALG_AWS_SECRET_ACCESS_KEY}",
			"      AWS_ENDPOINT: ${WALG_AWS_ENDPOINT}",
			"      AWS_REGION: ${WALG_AWS_REGION:-auto}",
			"      AWS_S3_FORCE_PATH_STYLE: \"true\"",
			"      WALG_COMPRESSION_METHOD: ${WALG_COMPRESSION_METHOD:-zstd}",
			fmt.Sprintf("      WALG_PGDATA: %q", pgDataPath),
			"      WALG_RESTORE_FROM: ${WALG_RESTORE_FROM:-LATEST}",
			"      WALG_RESTORE_FORCE: ${WALG_RESTORE_FORCE:-false}",
			"    command:",
			"      - sh",
			"      - -lc",
			"      - >-",
			"        restore_from=\"${WALG_RESTORE_FROM}\";",
			"        force_mode=\"${WALG_RESTORE_FORCE}\";",
			"        if [ \"$force_mode\" = \"true\" ]; then",
			"          rm -rf \"${WALG_PGDATA:?}\"/*;",
			"        fi;",
			"        wal-g backup-fetch \"${WALG_PGDATA}\" \"$restore_from\";",
			"        printf \"%s\\n\" \"restore_command = 'wal-g wal-fetch %f %p'\" >> \"${WALG_PGDATA}/postgresql.auto.conf\";",
			"        : > \"${WALG_PGDATA}/recovery.signal\"",
			"    volumes:",
			fmt.Sprintf("      - %s:%s", pgDataVolume, pgDataPath),
		)
		return strings.Join(lines, "\n") + "\n"
	case "databasus":
		pgService := firstNonEmptyString(record.Vars["POSTGRES_SERVICE"], "postgres")
		lines := append(header,
			fmt.Sprintf("  %s:", service),
			fmt.Sprintf("    image: databasus/databasus:%s", version),
			"    restart: unless-stopped",
			"    depends_on:",
			fmt.Sprintf("      - %s", pgService),
			"    environment:",
			"      ENV_MODE: ${DATABASUS_ENV_MODE:-production}",
			"      NEXT_TELEMETRY_DISABLED: \"1\"",
			"    expose:",
			"      - \"4005\"",
			"    volumes:",
			fmt.Sprintf("      - %s-data:/databasus-data", service),
			"volumes:",
			fmt.Sprintf("  %s-data: {}", service),
		)
		return strings.Join(lines, "\n") + "\n"
	default:
		lines := append(header,
			fmt.Sprintf("  %s:", service),
			fmt.Sprintf("    image: nats:%s", version),
			"    restart: unless-stopped",
			"    command: [\"-js\"]",
		)
		return strings.Join(lines, "\n") + "\n"
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
