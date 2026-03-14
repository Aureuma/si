package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const vivaNodeUsageText = "usage: si viva node <list|show|set|delete|set-default|doctor|ssh|mosh|rsync|bootstrap> [args]"

var runVivaNodeSSHExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

var runVivaNodeMoshExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

var runVivaNodeRsyncExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

var runVivaNodeSSHExternalWithInput = func(bin string, args []string, input string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if input == "" {
		cmd.Stdin = os.Stdin
	} else {
		cmd.Stdin = strings.NewReader(input)
	}
	return cmd.Run()
}

var resolveVivaNodeVaultKeyValue = resolveVaultKeyValue
var resolveVivaNodeGitRemoteOriginURL = gitRemoteOriginURL

func cmdVivaNode(args []string) {
	if len(args) == 0 {
		printUsage(vivaNodeUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "ls":
		cmdVivaNodeList(rest)
	case "show", "get":
		cmdVivaNodeShow(rest)
	case "set", "upsert":
		cmdVivaNodeSet(rest)
	case "delete", "remove", "rm":
		cmdVivaNodeDelete(rest)
	case "set-default", "default":
		cmdVivaNodeSetDefault(rest)
	case "doctor", "check":
		cmdVivaNodeDoctor(rest)
	case "ssh", "shell", "connect":
		cmdVivaNodeSSH(rest)
	case "mosh":
		cmdVivaNodeMosh(rest)
	case "rsync", "sync":
		cmdVivaNodeRsync(rest)
	case "bootstrap", "provision":
		cmdVivaNodeBootstrap(rest)
	default:
		fatal(fmt.Errorf("unknown viva node command: %s", sub))
	}
}

func cmdVivaNodeList(args []string) {
	fs := flag.NewFlagSet("viva node list", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node list [--json]"))
	}
	settings := loadSettingsOrDefault()
	keys := vivaNodeSortedKeys(settings.Viva.Node.Entries)
	if *jsonOut {
		rows := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			entry := settings.Viva.Node.Entries[key]
			rows = append(rows, map[string]any{
				"node":      key,
				"name":      entry.Name,
				"default":   key == settings.Viva.Node.DefaultNode,
				"host":      strings.TrimSpace(entry.Host),
				"host_ref":  strings.TrimSpace(entry.HostEnvKey),
				"user":      strings.TrimSpace(entry.User),
				"user_ref":  strings.TrimSpace(entry.UserEnvKey),
				"port":      strings.TrimSpace(entry.Port),
				"port_ref":  strings.TrimSpace(entry.PortEnvKey),
				"protocols": vivaNodeProtocolLabels(entry),
			})
		}
		printJSONMap(map[string]any{"ok": true, "default_node": settings.Viva.Node.DefaultNode, "nodes": rows})
		return
	}
	if len(keys) == 0 {
		infof("no viva nodes configured (set one with: si viva node set --node <name> --host <host> --user <user>)")
		return
	}
	headers := []string{styleHeading("#"), styleHeading("NODE"), styleHeading("HOST"), styleHeading("USER"), styleHeading("PORT"), styleHeading("DEFAULT"), styleHeading("PROTOCOLS")}
	rows := make([][]string, 0, len(keys))
	for idx, key := range keys {
		entry := settings.Viva.Node.Entries[key]
		rows = append(rows, []string{
			strconv.Itoa(idx + 1),
			key,
			vivaNodeFieldDisplay(entry.Host, entry.HostEnvKey),
			vivaNodeFieldDisplay(entry.User, entry.UserEnvKey),
			vivaNodeFieldDisplay(entry.Port, entry.PortEnvKey),
			boolLabel(key == settings.Viva.Node.DefaultNode),
			strings.Join(vivaNodeProtocolLabels(entry), ","),
		})
	}
	printAlignedTable(headers, rows, 2)
}

func cmdVivaNodeShow(args []string) {
	fs := flag.NewFlagSet("viva node show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	jsonOut := fs.Bool("json", false, "output json")
	resolve := fs.Bool("resolve", false, "resolve host/user/port refs from env/vault")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node show [--node <name>] [--resolve] [--json]"))
	}
	settings := loadSettingsOrDefault()
	key, entry, err := resolveVivaNodeSelection(settings, strings.TrimSpace(*node), "show")
	if err != nil {
		fatal(err)
	}
	if *resolve {
		conn, err := resolveVivaNodeConnection(settings, key, entry, vivaNodeConnectionOverrides{})
		if err != nil {
			fatal(err)
		}
		if *jsonOut {
			printJSONMap(map[string]any{"ok": true, "node": key, "resolved": conn})
			return
		}
		fmt.Printf("node: %s\n", key)
		fmt.Printf("  host=%s\n", conn.Host)
		fmt.Printf("  user=%s\n", conn.User)
		fmt.Printf("  port=%s\n", conn.Port)
		fmt.Printf("  identity_file=%s\n", conn.IdentityFile)
		fmt.Printf("  known_hosts_file=%s\n", conn.KnownHostsFile)
		fmt.Printf("  strict_host_key_checking=%s\n", conn.StrictHostKeyChecking)
		fmt.Printf("  protocols=%s\n", strings.Join(conn.Protocols, ","))
		return
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "node": key, "value": entry, "default": key == settings.Viva.Node.DefaultNode})
		return
	}
	fmt.Printf("node: %s\n", key)
	fmt.Printf("  default=%s\n", boolLabel(key == settings.Viva.Node.DefaultNode))
	fmt.Printf("  name=%s\n", strings.TrimSpace(entry.Name))
	fmt.Printf("  description=%s\n", strings.TrimSpace(entry.Description))
	fmt.Printf("  host=%s\n", vivaNodeFieldDisplay(entry.Host, entry.HostEnvKey))
	fmt.Printf("  user=%s\n", vivaNodeFieldDisplay(entry.User, entry.UserEnvKey))
	fmt.Printf("  port=%s\n", vivaNodeFieldDisplay(entry.Port, entry.PortEnvKey))
	fmt.Printf("  identity_file=%s\n", vivaNodeFieldDisplay(entry.IdentityFile, entry.IdentityFileEnvKey))
	fmt.Printf("  strict_host_key_checking=%s\n", strings.TrimSpace(entry.StrictHostKeyChecking))
	fmt.Printf("  protocols=%s\n", strings.Join(vivaNodeProtocolLabels(entry), ","))
}

func cmdVivaNodeSet(args []string) {
	fs := flag.NewFlagSet("viva node set", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	name := fs.String("name", "", "display name")
	description := fs.String("description", "", "description")
	host := fs.String("host", "", "host literal or env ref (env:KEY or ${KEY})")
	port := fs.String("port", "", "port literal or env ref (env:KEY or ${KEY})")
	user := fs.String("user", "", "user literal or env ref (env:KEY or ${KEY})")
	hostEnvKey := fs.String("host-env-key", "", "host env key")
	portEnvKey := fs.String("port-env-key", "", "port env key")
	userEnvKey := fs.String("user-env-key", "", "user env key")
	identityFile := fs.String("identity-file", "", "identity file path literal or env ref")
	identityFileEnvKey := fs.String("identity-file-env-key", "", "identity file env key")
	knownHostsFile := fs.String("known-hosts-file", "", "known_hosts file path")
	strictHostKeyChecking := fs.String("strict-host-key-checking", "", "yes|accept-new|no")
	connectTimeoutSeconds := fs.Int("connect-timeout-seconds", 0, "ssh connect timeout seconds")
	serverAliveIntervalSeconds := fs.Int("server-alive-interval-seconds", 0, "ssh server alive interval seconds")
	serverAliveCountMax := fs.Int("server-alive-count-max", 0, "ssh server alive count max")
	compression := fs.String("compression", "", "enable ssh compression (true|false)")
	multiplex := fs.String("multiplex", "", "enable ssh multiplexing (true|false)")
	controlPersist := fs.String("control-persist", "", "ssh control persist duration")
	controlPath := fs.String("control-path", "", "ssh control path")
	moshPort := fs.String("mosh-port", "", "mosh udp port or range")
	enableSSH := fs.String("enable-ssh", "", "enable ssh protocol (true|false)")
	enableMosh := fs.String("enable-mosh", "", "enable mosh protocol (true|false)")
	enableRsync := fs.String("enable-rsync", "", "enable rsync protocol (true|false)")
	setDefault := fs.Bool("set-default", false, "set as default node")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node set --node <name> [--host <value>] [--user <value>] [--port <value>] [--set-default] [--json]"))
	}
	key := strings.ToLower(strings.TrimSpace(*node))
	if key == "" {
		fatal(errors.New("--node is required"))
	}
	settings := loadSettingsOrDefault()
	if settings.Viva.Node.Entries == nil {
		settings.Viva.Node.Entries = map[string]VivaNodeProfile{}
	}
	entry := settings.Viva.Node.Entries[key]
	if vivaFlagProvided(fs, "name") {
		entry.Name = strings.TrimSpace(*name)
	}
	if vivaFlagProvided(fs, "description") {
		entry.Description = strings.TrimSpace(*description)
	}
	if vivaFlagProvided(fs, "host") {
		entry.Host = strings.TrimSpace(*host)
	}
	if vivaFlagProvided(fs, "port") {
		entry.Port = strings.TrimSpace(*port)
	}
	if vivaFlagProvided(fs, "user") {
		entry.User = strings.TrimSpace(*user)
	}
	if vivaFlagProvided(fs, "host-env-key") {
		entry.HostEnvKey = strings.TrimSpace(*hostEnvKey)
	}
	if vivaFlagProvided(fs, "port-env-key") {
		entry.PortEnvKey = strings.TrimSpace(*portEnvKey)
	}
	if vivaFlagProvided(fs, "user-env-key") {
		entry.UserEnvKey = strings.TrimSpace(*userEnvKey)
	}
	if vivaFlagProvided(fs, "identity-file") {
		entry.IdentityFile = strings.TrimSpace(*identityFile)
	}
	if vivaFlagProvided(fs, "identity-file-env-key") {
		entry.IdentityFileEnvKey = strings.TrimSpace(*identityFileEnvKey)
	}
	if vivaFlagProvided(fs, "known-hosts-file") {
		entry.KnownHostsFile = strings.TrimSpace(*knownHostsFile)
	}
	if vivaFlagProvided(fs, "strict-host-key-checking") {
		entry.StrictHostKeyChecking = strings.TrimSpace(*strictHostKeyChecking)
	}
	if vivaFlagProvided(fs, "connect-timeout-seconds") {
		entry.ConnectTimeoutSeconds = *connectTimeoutSeconds
	}
	if vivaFlagProvided(fs, "server-alive-interval-seconds") {
		entry.ServerAliveIntervalSeconds = *serverAliveIntervalSeconds
	}
	if vivaFlagProvided(fs, "server-alive-count-max") {
		entry.ServerAliveCountMax = *serverAliveCountMax
	}
	if vivaFlagProvided(fs, "control-persist") {
		entry.ControlPersist = strings.TrimSpace(*controlPersist)
	}
	if vivaFlagProvided(fs, "control-path") {
		entry.ControlPath = strings.TrimSpace(*controlPath)
	}
	if vivaFlagProvided(fs, "mosh-port") {
		entry.MoshPort = strings.TrimSpace(*moshPort)
	}
	if vivaFlagProvided(fs, "compression") {
		value, err := strconv.ParseBool(strings.TrimSpace(*compression))
		if err != nil {
			fatal(fmt.Errorf("invalid --compression value %q (expected true|false)", *compression))
		}
		entry.Compression = boolPtr(value)
	}
	if vivaFlagProvided(fs, "multiplex") {
		value, err := strconv.ParseBool(strings.TrimSpace(*multiplex))
		if err != nil {
			fatal(fmt.Errorf("invalid --multiplex value %q (expected true|false)", *multiplex))
		}
		entry.Multiplex = boolPtr(value)
	}
	if vivaFlagProvided(fs, "enable-ssh") {
		value, err := strconv.ParseBool(strings.TrimSpace(*enableSSH))
		if err != nil {
			fatal(fmt.Errorf("invalid --enable-ssh value %q (expected true|false)", *enableSSH))
		}
		entry.Protocols.SSH = boolPtr(value)
	}
	if vivaFlagProvided(fs, "enable-mosh") {
		value, err := strconv.ParseBool(strings.TrimSpace(*enableMosh))
		if err != nil {
			fatal(fmt.Errorf("invalid --enable-mosh value %q (expected true|false)", *enableMosh))
		}
		entry.Protocols.Mosh = boolPtr(value)
	}
	if vivaFlagProvided(fs, "enable-rsync") {
		value, err := strconv.ParseBool(strings.TrimSpace(*enableRsync))
		if err != nil {
			fatal(fmt.Errorf("invalid --enable-rsync value %q (expected true|false)", *enableRsync))
		}
		entry.Protocols.Rsync = boolPtr(value)
	}
	entry = normalizeVivaNodeProfile(entry)
	settings.Viva.Node.Entries[key] = entry
	if *setDefault || settings.Viva.Node.DefaultNode == "" {
		settings.Viva.Node.DefaultNode = key
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "node": key, "default_node": settings.Viva.Node.DefaultNode, "value": entry})
		return
	}
	fmt.Printf("si viva node set: %s\n", key)
}

func cmdVivaNodeDelete(args []string) {
	fs := flag.NewFlagSet("viva node delete", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node delete --node <name> [--json]"))
	}
	key := strings.ToLower(strings.TrimSpace(*node))
	if key == "" {
		fatal(errors.New("--node is required"))
	}
	settings := loadSettingsOrDefault()
	if _, ok := settings.Viva.Node.Entries[key]; !ok {
		fatal(fmt.Errorf("node %q not found", key))
	}
	delete(settings.Viva.Node.Entries, key)
	if settings.Viva.Node.DefaultNode == key {
		settings.Viva.Node.DefaultNode = ""
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "deleted": key})
		return
	}
	fmt.Printf("si viva node delete: %s\n", key)
}

func cmdVivaNodeSetDefault(args []string) {
	fs := flag.NewFlagSet("viva node set-default", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node set-default --node <name> [--json]"))
	}
	key := strings.ToLower(strings.TrimSpace(*node))
	if key == "" {
		fatal(errors.New("--node is required"))
	}
	settings := loadSettingsOrDefault()
	if _, ok := settings.Viva.Node.Entries[key]; !ok {
		fatal(fmt.Errorf("node %q not found", key))
	}
	settings.Viva.Node.DefaultNode = key
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "default_node": key})
		return
	}
	fmt.Printf("si viva node set-default: %s\n", key)
}

func cmdVivaNodeDoctor(args []string) {
	fs := flag.NewFlagSet("viva node doctor", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node doctor [--node <name>] [--json]"))
	}
	settings := loadSettingsOrDefault()
	var keys []string
	if strings.TrimSpace(*node) != "" {
		keys = []string{strings.ToLower(strings.TrimSpace(*node))}
	} else {
		keys = vivaNodeSortedKeys(settings.Viva.Node.Entries)
	}
	if len(keys) == 0 {
		fatal(errors.New("no viva nodes configured"))
	}
	type doctorRow struct {
		Node     string   `json:"node"`
		OK       bool     `json:"ok"`
		Host     string   `json:"host,omitempty"`
		User     string   `json:"user,omitempty"`
		Port     string   `json:"port,omitempty"`
		Binaries []string `json:"binaries,omitempty"`
		Errors   []string `json:"errors,omitempty"`
	}
	rows := make([]doctorRow, 0, len(keys))
	for _, key := range keys {
		entry, ok := settings.Viva.Node.Entries[key]
		if !ok {
			rows = append(rows, doctorRow{Node: key, OK: false, Errors: []string{fmt.Sprintf("node %q not found", key)}})
			continue
		}
		conn, err := resolveVivaNodeConnection(settings, key, entry, vivaNodeConnectionOverrides{})
		row := doctorRow{Node: key, OK: true}
		if err != nil {
			row.OK = false
			row.Errors = append(row.Errors, err.Error())
		}
		row.Host = conn.Host
		row.User = conn.User
		row.Port = conn.Port
		if conn.SSHEnabled {
			if _, lookErr := exec.LookPath("ssh"); lookErr != nil {
				row.OK = false
				row.Errors = append(row.Errors, "ssh binary not found in PATH")
			} else {
				row.Binaries = append(row.Binaries, "ssh")
			}
		}
		if conn.MoshEnabled {
			if _, lookErr := exec.LookPath("mosh"); lookErr != nil {
				row.Errors = append(row.Errors, "mosh binary not found in PATH")
			} else {
				row.Binaries = append(row.Binaries, "mosh")
			}
		}
		if conn.RsyncEnabled {
			if _, lookErr := exec.LookPath("rsync"); lookErr != nil {
				row.Errors = append(row.Errors, "rsync binary not found in PATH")
			} else {
				row.Binaries = append(row.Binaries, "rsync")
			}
		}
		if len(row.Errors) > 0 {
			row.OK = false
		}
		rows = append(rows, row)
	}
	if *jsonOut {
		ok := true
		for _, row := range rows {
			if !row.OK {
				ok = false
				break
			}
		}
		printJSONMap(map[string]any{"ok": ok, "nodes": rows})
		return
	}
	headers := []string{styleHeading("NODE"), styleHeading("OK"), styleHeading("HOST"), styleHeading("USER"), styleHeading("PORT"), styleHeading("BINARIES"), styleHeading("ERRORS")}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{row.Node, boolLabel(row.OK), row.Host, row.User, row.Port, strings.Join(row.Binaries, ","), strings.Join(row.Errors, " | ")})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdVivaNodeSSH(args []string) {
	fs := flag.NewFlagSet("viva node ssh", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	host := fs.String("host", "", "host override (literal or env ref)")
	port := fs.String("port", "", "port override (literal or env ref)")
	user := fs.String("user", "", "user override (literal or env ref)")
	identityFile := fs.String("identity-file", "", "identity file override (literal or env ref)")
	sshBin := fs.String("ssh-bin", "ssh", "ssh binary")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	key, entry, err := resolveVivaNodeSelection(settings, strings.TrimSpace(*node), "ssh")
	if err != nil {
		fatal(err)
	}
	conn, err := resolveVivaNodeConnection(settings, key, entry, vivaNodeConnectionOverrides{
		Host:         strings.TrimSpace(*host),
		Port:         strings.TrimSpace(*port),
		User:         strings.TrimSpace(*user),
		IdentityFile: strings.TrimSpace(*identityFile),
	})
	if err != nil {
		fatal(err)
	}
	if !conn.SSHEnabled {
		fatal(fmt.Errorf("node %q has ssh protocol disabled", key))
	}
	remoteArgs := fs.Args()
	if len(remoteArgs) == 0 && !isInteractiveTerminal() {
		fatal(errors.New("non-interactive mode requires a remote command (example: si viva node ssh --node <name> -- uname -a)"))
	}
	argsOut := buildVivaNodeSSHArgs(conn, remoteArgs)
	if err := runVivaNodeSSHExternal(strings.TrimSpace(*sshBin), argsOut); err != nil {
		fatal(err)
	}
}

func cmdVivaNodeMosh(args []string) {
	fs := flag.NewFlagSet("viva node mosh", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	host := fs.String("host", "", "host override (literal or env ref)")
	port := fs.String("port", "", "ssh port override (literal or env ref)")
	user := fs.String("user", "", "user override (literal or env ref)")
	identityFile := fs.String("identity-file", "", "identity file override (literal or env ref)")
	moshBin := fs.String("mosh-bin", "mosh", "mosh binary")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	key, entry, err := resolveVivaNodeSelection(settings, strings.TrimSpace(*node), "mosh")
	if err != nil {
		fatal(err)
	}
	conn, err := resolveVivaNodeConnection(settings, key, entry, vivaNodeConnectionOverrides{
		Host:         strings.TrimSpace(*host),
		Port:         strings.TrimSpace(*port),
		User:         strings.TrimSpace(*user),
		IdentityFile: strings.TrimSpace(*identityFile),
	})
	if err != nil {
		fatal(err)
	}
	if !conn.MoshEnabled {
		fatal(fmt.Errorf("node %q has mosh protocol disabled", key))
	}
	argsOut := []string{"--ssh=" + buildVivaNodeSSHCommandString(conn)}
	if strings.TrimSpace(conn.MoshPort) != "" {
		argsOut = append(argsOut, "--port="+strings.TrimSpace(conn.MoshPort))
	}
	argsOut = append(argsOut, fmt.Sprintf("%s@%s", conn.User, conn.Host))
	argsOut = append(argsOut, fs.Args()...)
	if err := runVivaNodeMoshExternal(strings.TrimSpace(*moshBin), argsOut); err != nil {
		fatal(err)
	}
}

func cmdVivaNodeRsync(args []string) {
	fs := flag.NewFlagSet("viva node rsync", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	host := fs.String("host", "", "host override (literal or env ref)")
	port := fs.String("port", "", "ssh port override (literal or env ref)")
	user := fs.String("user", "", "user override (literal or env ref)")
	identityFile := fs.String("identity-file", "", "identity file override (literal or env ref)")
	src := fs.String("src", "", "source path")
	dst := fs.String("dst", "", "destination path")
	reverse := fs.Bool("reverse", false, "pull from remote to local")
	deleteMode := fs.Bool("delete", false, "delete extraneous destination files")
	dryRun := fs.Bool("dry-run", false, "preview without writing")
	rsyncBin := fs.String("rsync-bin", "rsync", "rsync binary")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*src) == "" || strings.TrimSpace(*dst) == "" {
		fatal(errors.New("usage: si viva node rsync --node <name> --src <path> --dst <path> [--reverse] [--delete] [--dry-run]"))
	}
	settings := loadSettingsOrDefault()
	key, entry, err := resolveVivaNodeSelection(settings, strings.TrimSpace(*node), "rsync")
	if err != nil {
		fatal(err)
	}
	conn, err := resolveVivaNodeConnection(settings, key, entry, vivaNodeConnectionOverrides{
		Host:         strings.TrimSpace(*host),
		Port:         strings.TrimSpace(*port),
		User:         strings.TrimSpace(*user),
		IdentityFile: strings.TrimSpace(*identityFile),
	})
	if err != nil {
		fatal(err)
	}
	if !conn.RsyncEnabled {
		fatal(fmt.Errorf("node %q has rsync protocol disabled", key))
	}
	argsOut := buildVivaNodeRsyncArgs(conn, strings.TrimSpace(*src), strings.TrimSpace(*dst), *reverse, *deleteMode, *dryRun)
	if err := runVivaNodeRsyncExternal(strings.TrimSpace(*rsyncBin), argsOut); err != nil {
		fatal(err)
	}
}

type vivaNodeBootstrapRepo struct {
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
	RemoteURL string `json:"remote_url"`
}

type vivaNodeBootstrapSecrets struct {
	GitHubToken string `json:"-"`
}

type vivaNodeBootstrapRuntime struct {
	SourceRoot      string
	WorkspaceDir    string
	ShellProfile    string
	EnvFile         string
	Repos           []vivaNodeBootstrapRepo
	BuildSI         bool
	PullLatest      bool
	InstallOrbitals []string
	Secrets         vivaNodeBootstrapSecrets
}

type vivaNodeBootstrapResult struct {
	Node            string                  `json:"node"`
	Host            string                  `json:"host"`
	User            string                  `json:"user"`
	Port            string                  `json:"port"`
	Status          string                  `json:"status"`
	Message         string                  `json:"message,omitempty"`
	WorkspaceDir    string                  `json:"workspace_dir,omitempty"`
	Repos           []vivaNodeBootstrapRepo `json:"repos,omitempty"`
	InstallOrbitals []string                `json:"install_orbitals,omitempty"`
	SSHArgs         []string                `json:"ssh_args,omitempty"`
}

func cmdVivaNodeBootstrap(args []string) {
	fs := flag.NewFlagSet("viva node bootstrap", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	node := fs.String("node", "", "node name")
	all := fs.Bool("all", false, "run on all configured nodes")
	sourceRoot := fs.String("source-root", "", "local source root containing repositories")
	workspaceDir := fs.String("workspace-dir", "", "remote workspace directory")
	reposRaw := fs.String("repos", "", "comma-separated repository names")
	shellProfile := fs.String("shell-profile", "", "remote shell profile path")
	envFile := fs.String("env-file", "", "remote env file path")
	githubTokenKey := fs.String("github-token-key", "", "vault/env key for GitHub token")
	buildSIRaw := fs.String("build-si", "", "build si on remote node (true|false)")
	pullLatestRaw := fs.String("pull-latest", "", "pull latest commits for existing repos (true|false)")
	installOrbitalsRaw := fs.String("install-orbitals", "", "comma-separated orbit names to install after bootstrap")
	sshBin := fs.String("ssh-bin", "ssh", "ssh binary")
	dryRun := fs.Bool("dry-run", false, "print plan without executing remote bootstrap")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva node bootstrap [--node <name>|--all] [--source-root <path>] [--workspace-dir <path>] [--repos <a,b>] [--dry-run] [--json]"))
	}
	if *all && strings.TrimSpace(*node) != "" {
		fatal(errors.New("--all cannot be combined with --node"))
	}
	settings := loadSettingsOrDefault()
	targets, err := resolveVivaNodeBootstrapTargets(settings, strings.TrimSpace(*node), *all)
	if err != nil {
		fatal(err)
	}

	cfg := settings.Viva.Node.Bootstrap
	githubTokenKeyProvided := vivaFlagProvided(fs, "github-token-key")
	if vivaFlagProvided(fs, "source-root") {
		cfg.SourceRoot = strings.TrimSpace(*sourceRoot)
	}
	if vivaFlagProvided(fs, "workspace-dir") {
		cfg.WorkspaceDir = strings.TrimSpace(*workspaceDir)
	}
	if vivaFlagProvided(fs, "repos") {
		cfg.Repos = normalizeVivaNodeBootstrapRepos(vivaNodeSplitCSV(*reposRaw))
	}
	if vivaFlagProvided(fs, "shell-profile") {
		cfg.ShellProfile = strings.TrimSpace(*shellProfile)
	}
	if vivaFlagProvided(fs, "env-file") {
		cfg.EnvFile = strings.TrimSpace(*envFile)
	}
	if githubTokenKeyProvided {
		cfg.GitHubTokenKey = strings.TrimSpace(*githubTokenKey)
	}
	if vivaFlagProvided(fs, "build-si") {
		parsed, parseErr := strconv.ParseBool(strings.TrimSpace(*buildSIRaw))
		if parseErr != nil {
			fatal(fmt.Errorf("invalid --build-si value %q (expected true|false)", *buildSIRaw))
		}
		cfg.BuildSI = boolPtr(parsed)
	}
	if vivaFlagProvided(fs, "pull-latest") {
		parsed, parseErr := strconv.ParseBool(strings.TrimSpace(*pullLatestRaw))
		if parseErr != nil {
			fatal(fmt.Errorf("invalid --pull-latest value %q (expected true|false)", *pullLatestRaw))
		}
		cfg.PullLatest = boolPtr(parsed)
	}
	if vivaFlagProvided(fs, "install-orbitals") {
		cfg.InstallOrbitals = normalizeVivaNodeBootstrapRepos(vivaNodeSplitCSV(*installOrbitalsRaw))
	}
	cfg = normalizeVivaNodeBootstrapSettings(cfg)
	if githubTokenKeyProvided {
		cfg.GitHubTokenKey = strings.TrimSpace(*githubTokenKey)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	cfg, err = applyVivaNodeBootstrapPathDefaults(&settings, cfg, cwd, isInteractiveTerminal())
	if err != nil {
		fatal(err)
	}

	runtime, err := resolveVivaNodeBootstrapRuntime(settings, cfg, !*dryRun)
	if err != nil {
		fatal(err)
	}

	results := make([]vivaNodeBootstrapResult, 0, len(targets))
	failed := []string{}
	for _, target := range targets {
		conn, connErr := resolveVivaNodeConnection(settings, target.Key, target.Entry, vivaNodeConnectionOverrides{})
		if connErr != nil {
			results = append(results, vivaNodeBootstrapResult{
				Node:    target.Key,
				Status:  "error",
				Message: connErr.Error(),
			})
			failed = append(failed, fmt.Sprintf("%s: %v", target.Key, connErr))
			continue
		}
		if !conn.SSHEnabled {
			err := fmt.Errorf("node %q has ssh protocol disabled", target.Key)
			results = append(results, vivaNodeBootstrapResult{
				Node:    target.Key,
				Host:    conn.Host,
				User:    conn.User,
				Port:    conn.Port,
				Status:  "error",
				Message: err.Error(),
			})
			failed = append(failed, err.Error())
			continue
		}
		sshArgs := buildVivaNodeSSHArgs(conn, []string{"bash", "-se"})
		result := vivaNodeBootstrapResult{
			Node:            target.Key,
			Host:            conn.Host,
			User:            conn.User,
			Port:            conn.Port,
			Status:          "ok",
			WorkspaceDir:    runtime.WorkspaceDir,
			Repos:           runtime.Repos,
			InstallOrbitals: runtime.InstallOrbitals,
			SSHArgs:         append([]string{}, sshArgs...),
		}
		if *dryRun {
			result.Message = "dry-run"
			results = append(results, result)
			continue
		}
		script := buildVivaNodeBootstrapScript(runtime)
		if runErr := runVivaNodeSSHExternalWithInput(strings.TrimSpace(*sshBin), sshArgs, script); runErr != nil {
			result.Status = "error"
			result.Message = runErr.Error()
			failed = append(failed, fmt.Sprintf("%s: %v", target.Key, runErr))
		}
		results = append(results, result)
	}
	if *jsonOut {
		ok := len(failed) == 0
		printJSONMap(map[string]any{"ok": ok, "dry_run": *dryRun, "results": results})
		if !ok {
			fatal(errors.New(strings.Join(failed, "; ")))
		}
		return
	}
	headers := []string{styleHeading("NODE"), styleHeading("HOST"), styleHeading("STATUS"), styleHeading("WORKSPACE"), styleHeading("REPOS"), styleHeading("MESSAGE")}
	rows := make([][]string, 0, len(results))
	for _, row := range results {
		rows = append(rows, []string{
			row.Node,
			row.Host,
			row.Status,
			row.WorkspaceDir,
			strconv.Itoa(len(row.Repos)),
			row.Message,
		})
	}
	printAlignedTable(headers, rows, 2)
	if len(failed) > 0 {
		fatal(errors.New(strings.Join(failed, "; ")))
	}
}

func applyVivaNodeBootstrapPathDefaults(settings *Settings, cfg VivaNodeBootstrapSettings, cwd string, interactive bool) (VivaNodeBootstrapSettings, error) {
	if strings.TrimSpace(cfg.SourceRoot) == "" {
		resolved, err := resolveWorkspaceRootDirectory(false, "", strings.TrimSpace(os.Getenv("SI_WORKSPACE_ROOT")), settings, cwd)
		if err != nil {
			return cfg, fmt.Errorf("viva.node.bootstrap.source_root is required: %w", err)
		}
		cfg.SourceRoot = resolved.Path
		maybePersistWorkspaceRootDefault(settings, resolved.Path, interactive)
	}
	if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		cfg.WorkspaceDir = filepath.Join("~", filepath.Base(strings.TrimSpace(cfg.SourceRoot)))
	}
	return cfg, nil
}

type vivaNodeBootstrapTarget struct {
	Key   string
	Entry VivaNodeProfile
}

func resolveVivaNodeBootstrapTargets(settings Settings, requested string, all bool) ([]vivaNodeBootstrapTarget, error) {
	if all {
		keys := vivaNodeSortedKeys(settings.Viva.Node.Entries)
		if len(keys) == 0 {
			return nil, errors.New("no viva nodes configured (use: si viva node set --node <name> --host <host> --user <user>)")
		}
		targets := make([]vivaNodeBootstrapTarget, 0, len(keys))
		for _, key := range keys {
			targets = append(targets, vivaNodeBootstrapTarget{Key: key, Entry: settings.Viva.Node.Entries[key]})
		}
		return targets, nil
	}
	key, entry, err := resolveVivaNodeSelection(settings, requested, "bootstrap")
	if err != nil {
		return nil, err
	}
	return []vivaNodeBootstrapTarget{{Key: key, Entry: entry}}, nil
}

func resolveVivaNodeBootstrapRuntime(settings Settings, cfg VivaNodeBootstrapSettings, requireSecrets bool) (vivaNodeBootstrapRuntime, error) {
	sourceRoot := strings.TrimSpace(cfg.SourceRoot)
	if sourceRoot == "" {
		return vivaNodeBootstrapRuntime{}, errors.New("viva.node.bootstrap.source_root is required")
	}
	sourceRoot = filepath.Clean(expandTilde(sourceRoot))
	if !filepath.IsAbs(sourceRoot) {
		abs, err := filepath.Abs(sourceRoot)
		if err != nil {
			return vivaNodeBootstrapRuntime{}, fmt.Errorf("resolve source root: %w", err)
		}
		sourceRoot = abs
	}
	if stat, err := os.Stat(sourceRoot); err != nil || !stat.IsDir() {
		return vivaNodeBootstrapRuntime{}, fmt.Errorf("source root not found: %s", sourceRoot)
	}
	repos, err := resolveVivaNodeBootstrapRepos(sourceRoot, cfg.Repos)
	if err != nil {
		return vivaNodeBootstrapRuntime{}, err
	}
	secrets, err := resolveVivaNodeBootstrapSecrets(settings, cfg, requireSecrets)
	if err != nil {
		return vivaNodeBootstrapRuntime{}, err
	}
	return vivaNodeBootstrapRuntime{
		SourceRoot:      sourceRoot,
		WorkspaceDir:    strings.TrimSpace(cfg.WorkspaceDir),
		ShellProfile:    strings.TrimSpace(cfg.ShellProfile),
		EnvFile:         strings.TrimSpace(cfg.EnvFile),
		Repos:           repos,
		BuildSI:         cfg.BuildSI != nil && *cfg.BuildSI,
		PullLatest:      cfg.PullLatest != nil && *cfg.PullLatest,
		InstallOrbitals: append([]string{}, cfg.InstallOrbitals...),
		Secrets:         secrets,
	}, nil
}

func resolveVivaNodeBootstrapSecrets(settings Settings, cfg VivaNodeBootstrapSettings, required bool) (vivaNodeBootstrapSecrets, error) {
	read := func(key string) (string, error) {
		key = strings.TrimSpace(key)
		if key == "" {
			return "", nil
		}
		value := strings.TrimSpace(resolveVivaNodeKeyValue(settings, key))
		if value == "" && required {
			return "", fmt.Errorf("secret key %q was not found in env or vault", key)
		}
		return value, nil
	}
	githubToken, err := read(cfg.GitHubTokenKey)
	if err != nil {
		return vivaNodeBootstrapSecrets{}, err
	}
	return vivaNodeBootstrapSecrets{
		GitHubToken: githubToken,
	}, nil
}

func resolveVivaNodeBootstrapRepos(sourceRoot string, names []string) ([]vivaNodeBootstrapRepo, error) {
	if len(names) == 0 {
		return nil, errors.New("no bootstrap repositories configured")
	}
	repos := make([]vivaNodeBootstrapRepo, 0, len(names))
	for _, name := range names {
		repoName := strings.TrimSpace(name)
		if repoName == "" {
			continue
		}
		if !vivaNodeSafeIdentifier(repoName) {
			return nil, fmt.Errorf("invalid bootstrap repository name %q", repoName)
		}
		localPath := filepath.Join(sourceRoot, repoName)
		stat, err := os.Stat(localPath)
		if err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("bootstrap repository %q not found at %s", repoName, localPath)
		}
		remoteURL, err := resolveVivaNodeGitRemoteOriginURL(localPath)
		if err != nil {
			return nil, fmt.Errorf("resolve origin for %s: %w", repoName, err)
		}
		repos = append(repos, vivaNodeBootstrapRepo{
			Name:      repoName,
			LocalPath: localPath,
			RemoteURL: remoteURL,
		})
	}
	return repos, nil
}

func gitRemoteOriginURL(repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	remote := strings.TrimSpace(string(out))
	if remote == "" {
		return "", errors.New("origin remote is empty")
	}
	return remote, nil
}

func buildVivaNodeBootstrapScript(runtime vivaNodeBootstrapRuntime) string {
	lines := []string{
		"set -euo pipefail",
		"umask 077",
		"if ! command -v git >/dev/null 2>&1; then echo \"error: git is required\" >&2; exit 1; fi",
		"WORKSPACE_DIR=" + vivaShellQuote(runtime.WorkspaceDir),
		"SHELL_PROFILE=" + vivaShellQuote(runtime.ShellProfile),
		"ENV_FILE=" + vivaShellQuote(runtime.EnvFile),
		"mkdir -p \"$WORKSPACE_DIR\"",
		"touch \"$SHELL_PROFILE\"",
		"mkdir -p \"$(dirname \"$ENV_FILE\")\"",
		"touch \"$ENV_FILE\"",
		"chmod 600 \"$ENV_FILE\"",
	}
	envLines := []string{}
	if strings.TrimSpace(runtime.Secrets.GitHubToken) != "" {
		envLines = append(envLines, "export GH_PAT_AUREUMA="+vivaShellQuote(runtime.Secrets.GitHubToken))
	}
	lines = append(lines, "cat > \"$ENV_FILE\" <<'SIEOF'")
	lines = append(lines, envLines...)
	lines = append(lines, "SIEOF")
	sourceLine := "[ -f " + vivaShellQuote(runtime.EnvFile) + " ] && source " + vivaShellQuote(runtime.EnvFile)
	lines = append(lines,
		"if ! grep -Fq "+vivaShellQuote(sourceLine)+" \"$SHELL_PROFILE\"; then",
		"  printf '\\n# added by si viva node bootstrap\\n"+sourceLine+"\\n' >> \"$SHELL_PROFILE\"",
		"fi",
	)
	for _, repo := range runtime.Repos {
		repoDirExpr := "\"$WORKSPACE_DIR/" + repo.Name + "\""
		lines = append(lines,
			"if [ -d "+repoDirExpr+"/.git ]; then",
		)
		if runtime.PullLatest {
			lines = append(lines,
				"  git -C "+repoDirExpr+" fetch --all --prune",
				"  if ! git -C "+repoDirExpr+" pull --ff-only; then",
				"    echo \"warning: pull failed for "+repo.Name+"; keeping local branch\" >&2",
				"  fi",
			)
		} else {
			lines = append(lines, "  echo \"skip pull for "+repo.Name+"\"")
		}
		lines = append(lines,
			"else",
			"  git clone "+vivaShellQuote(repo.RemoteURL)+" "+repoDirExpr,
			"fi",
		)
	}
	if runtime.BuildSI {
		siPath := "\"$WORKSPACE_DIR/si\""
		lines = append(lines,
			"if ! command -v go >/dev/null 2>&1; then echo \"error: go is required for build-si\" >&2; exit 1; fi",
			"if [ -d "+siPath+" ]; then",
			"  mkdir -p \"$WORKSPACE_DIR/si/bin\"",
			"  (cd \"$WORKSPACE_DIR/si\" && go build -o bin/si ./tools/si)",
			"fi",
		)
	}
	if len(runtime.InstallOrbitals) > 0 {
		lines = append(lines,
			"SI_BIN=\"$WORKSPACE_DIR/si/bin/si\"",
			"if [ ! -x \"$SI_BIN\" ]; then SI_BIN=\"$(command -v si || true)\"; fi",
			"if [ -z \"$SI_BIN\" ]; then",
			"  echo \"error: si binary not found for orbital installation\" >&2",
			"  exit 1",
			"fi",
		)
		for _, orbit := range runtime.InstallOrbitals {
			lines = append(lines, "$SI_BIN orbits install "+vivaShellQuote(orbit))
		}
	}
	lines = append(lines, "echo \"si viva node bootstrap completed\"")
	return strings.Join(lines, "\n") + "\n"
}

func vivaNodeSplitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func vivaNodeSafeIdentifier(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

type vivaNodeConnectionOverrides struct {
	Host         string
	Port         string
	User         string
	IdentityFile string
}

type vivaNodeConnection struct {
	Node                       string   `json:"node"`
	Host                       string   `json:"host"`
	Port                       string   `json:"port"`
	User                       string   `json:"user"`
	IdentityFile               string   `json:"identity_file,omitempty"`
	KnownHostsFile             string   `json:"known_hosts_file,omitempty"`
	StrictHostKeyChecking      string   `json:"strict_host_key_checking"`
	ConnectTimeoutSeconds      int      `json:"connect_timeout_seconds"`
	ServerAliveIntervalSeconds int      `json:"server_alive_interval_seconds"`
	ServerAliveCountMax        int      `json:"server_alive_count_max"`
	Compression                bool     `json:"compression"`
	Multiplex                  bool     `json:"multiplex"`
	ControlPersist             string   `json:"control_persist"`
	ControlPath                string   `json:"control_path"`
	MoshPort                   string   `json:"mosh_port,omitempty"`
	SSHEnabled                 bool     `json:"ssh_enabled"`
	MoshEnabled                bool     `json:"mosh_enabled"`
	RsyncEnabled               bool     `json:"rsync_enabled"`
	Protocols                  []string `json:"protocols"`
}

func resolveVivaNodeSelection(settings Settings, requested string, action string) (string, VivaNodeProfile, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if len(settings.Viva.Node.Entries) == 0 {
		return "", VivaNodeProfile{}, errors.New("no viva nodes configured (use: si viva node set --node <name> --host <host> --user <user>)")
	}
	if requested != "" {
		entry, ok := settings.Viva.Node.Entries[requested]
		if !ok {
			return "", VivaNodeProfile{}, fmt.Errorf("node %q not found", requested)
		}
		return requested, entry, nil
	}
	if def := strings.ToLower(strings.TrimSpace(settings.Viva.Node.DefaultNode)); def != "" {
		if entry, ok := settings.Viva.Node.Entries[def]; ok {
			return def, entry, nil
		}
	}
	if len(settings.Viva.Node.Entries) == 1 {
		for key, entry := range settings.Viva.Node.Entries {
			return key, entry, nil
		}
	}
	if !isInteractiveTerminal() {
		return "", VivaNodeProfile{}, fmt.Errorf("node is required (re-run with: si viva node %s --node <name>)", strings.TrimSpace(action))
	}
	keys := vivaNodeSortedKeys(settings.Viva.Node.Entries)
	headers := []string{styleHeading("#"), styleHeading("NODE"), styleHeading("HOST"), styleHeading("USER"), styleHeading("PORT")}
	rows := make([][]string, 0, len(keys))
	for idx, key := range keys {
		entry := settings.Viva.Node.Entries[key]
		rows = append(rows, []string{
			strconv.Itoa(idx + 1),
			key,
			vivaNodeFieldDisplay(entry.Host, entry.HostEnvKey),
			vivaNodeFieldDisplay(entry.User, entry.UserEnvKey),
			vivaNodeFieldDisplay(entry.Port, entry.PortEnvKey),
		})
	}
	fmt.Println(styleHeading("Available viva nodes:"))
	printAlignedTable(headers, rows, 2)
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select node [1-%d] or name (Enter/Esc to cancel):", len(keys))))
	line, err := promptLine(os.Stdin)
	if err != nil {
		return "", VivaNodeProfile{}, err
	}
	idx, err := parseMenuSelection(line, keys)
	if err != nil {
		return "", VivaNodeProfile{}, fmt.Errorf("invalid selection")
	}
	if idx < 0 {
		return "", VivaNodeProfile{}, errors.New("selection cancelled")
	}
	key := keys[idx]
	return key, settings.Viva.Node.Entries[key], nil
}

func resolveVivaNodeConnection(settings Settings, key string, entry VivaNodeProfile, overrides vivaNodeConnectionOverrides) (vivaNodeConnection, error) {
	entry = normalizeVivaNodeProfile(entry)
	host := resolveVivaNodePreferredValue(settings, strings.TrimSpace(overrides.Host), entry.HostEnvKey, entry.Host)
	user := resolveVivaNodePreferredValue(settings, strings.TrimSpace(overrides.User), entry.UserEnvKey, entry.User)
	port := resolveVivaNodePreferredValue(settings, strings.TrimSpace(overrides.Port), entry.PortEnvKey, entry.Port)
	identityFile := resolveVivaNodePreferredValue(settings, strings.TrimSpace(overrides.IdentityFile), entry.IdentityFileEnvKey, entry.IdentityFile)
	if strings.TrimSpace(host) == "" {
		return vivaNodeConnection{}, fmt.Errorf("node %q host is required", key)
	}
	if strings.TrimSpace(user) == "" {
		return vivaNodeConnection{}, fmt.Errorf("node %q user is required", key)
	}
	if strings.TrimSpace(port) == "" {
		port = "22"
	}
	if _, err := strconv.Atoi(strings.TrimSpace(port)); err != nil {
		return vivaNodeConnection{}, fmt.Errorf("node %q invalid port %q", key, port)
	}
	conn := vivaNodeConnection{
		Node:                       key,
		Host:                       host,
		User:                       user,
		Port:                       port,
		IdentityFile:               strings.TrimSpace(identityFile),
		KnownHostsFile:             strings.TrimSpace(entry.KnownHostsFile),
		StrictHostKeyChecking:      strings.TrimSpace(entry.StrictHostKeyChecking),
		ConnectTimeoutSeconds:      entry.ConnectTimeoutSeconds,
		ServerAliveIntervalSeconds: entry.ServerAliveIntervalSeconds,
		ServerAliveCountMax:        entry.ServerAliveCountMax,
		Compression:                entry.Compression != nil && *entry.Compression,
		Multiplex:                  entry.Multiplex != nil && *entry.Multiplex,
		ControlPersist:             strings.TrimSpace(entry.ControlPersist),
		ControlPath:                strings.TrimSpace(entry.ControlPath),
		MoshPort:                   strings.TrimSpace(entry.MoshPort),
		SSHEnabled:                 entry.Protocols.SSH != nil && *entry.Protocols.SSH,
		MoshEnabled:                entry.Protocols.Mosh != nil && *entry.Protocols.Mosh,
		RsyncEnabled:               entry.Protocols.Rsync != nil && *entry.Protocols.Rsync,
	}
	conn.Protocols = vivaNodeEnabledProtocols(conn)
	return conn, nil
}

func vivaNodeEnabledProtocols(conn vivaNodeConnection) []string {
	out := make([]string, 0, 3)
	if conn.SSHEnabled {
		out = append(out, "ssh")
	}
	if conn.MoshEnabled {
		out = append(out, "mosh")
	}
	if conn.RsyncEnabled {
		out = append(out, "rsync")
	}
	return out
}

func resolveVivaNodePreferredValue(settings Settings, overrideRaw, envKey, configRaw string) string {
	if strings.TrimSpace(overrideRaw) != "" {
		return resolveVivaNodeConfigReference(settings, "", overrideRaw)
	}
	return resolveVivaNodeConfigReference(settings, envKey, configRaw)
}

func resolveVivaNodeConfigReference(settings Settings, envKey string, rawValue string) string {
	if v := resolveVivaNodeKeyValue(settings, envKey); v != "" {
		return v
	}
	raw := strings.TrimSpace(rawValue)
	if raw == "" {
		return ""
	}
	if ref, ok := vivaNodeEnvReference(raw); ok {
		return resolveVivaNodeKeyValue(settings, ref)
	}
	return raw
}

func vivaNodeEnvReference(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "env:") {
		ref := strings.TrimSpace(strings.TrimPrefix(raw, "env:"))
		return ref, ref != ""
	}
	if strings.HasPrefix(raw, "${") && strings.HasSuffix(raw, "}") {
		ref := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "${"), "}"))
		return ref, ref != ""
	}
	return "", false
}

func resolveVivaNodeKeyValue(settings Settings, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if value, ok := resolveVivaNodeVaultKeyValue(settings, key); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func buildVivaNodeSSHArgs(conn vivaNodeConnection, remoteArgs []string) []string {
	args := []string{"-p", strings.TrimSpace(conn.Port)}
	args = append(args,
		"-o", "StrictHostKeyChecking="+strings.TrimSpace(conn.StrictHostKeyChecking),
		"-o", "ConnectTimeout="+strconv.Itoa(conn.ConnectTimeoutSeconds),
		"-o", "ServerAliveInterval="+strconv.Itoa(conn.ServerAliveIntervalSeconds),
		"-o", "ServerAliveCountMax="+strconv.Itoa(conn.ServerAliveCountMax),
	)
	if conn.Compression {
		args = append(args, "-o", "Compression=yes")
	} else {
		args = append(args, "-o", "Compression=no")
	}
	if strings.TrimSpace(conn.KnownHostsFile) != "" {
		args = append(args, "-o", "UserKnownHostsFile="+strings.TrimSpace(conn.KnownHostsFile))
	}
	if conn.Multiplex {
		args = append(args,
			"-o", "ControlMaster=auto",
			"-o", "ControlPersist="+strings.TrimSpace(conn.ControlPersist),
			"-o", "ControlPath="+strings.TrimSpace(conn.ControlPath),
		)
	}
	if strings.TrimSpace(conn.IdentityFile) != "" {
		args = append(args, "-i", strings.TrimSpace(conn.IdentityFile))
	}
	args = append(args, fmt.Sprintf("%s@%s", strings.TrimSpace(conn.User), strings.TrimSpace(conn.Host)))
	args = append(args, remoteArgs...)
	return args
}

func buildVivaNodeSSHCommandString(conn vivaNodeConnection) string {
	parts := []string{"ssh", "-p", conn.Port}
	parts = append(parts,
		"-o", "StrictHostKeyChecking="+strings.TrimSpace(conn.StrictHostKeyChecking),
		"-o", "ConnectTimeout="+strconv.Itoa(conn.ConnectTimeoutSeconds),
		"-o", "ServerAliveInterval="+strconv.Itoa(conn.ServerAliveIntervalSeconds),
		"-o", "ServerAliveCountMax="+strconv.Itoa(conn.ServerAliveCountMax),
	)
	if conn.Compression {
		parts = append(parts, "-o", "Compression=yes")
	} else {
		parts = append(parts, "-o", "Compression=no")
	}
	if strings.TrimSpace(conn.KnownHostsFile) != "" {
		parts = append(parts, "-o", "UserKnownHostsFile="+strings.TrimSpace(conn.KnownHostsFile))
	}
	if conn.Multiplex {
		parts = append(parts,
			"-o", "ControlMaster=auto",
			"-o", "ControlPersist="+strings.TrimSpace(conn.ControlPersist),
			"-o", "ControlPath="+strings.TrimSpace(conn.ControlPath),
		)
	}
	if strings.TrimSpace(conn.IdentityFile) != "" {
		parts = append(parts, "-i", strings.TrimSpace(conn.IdentityFile))
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, vivaShellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func buildVivaNodeRsyncArgs(conn vivaNodeConnection, src, dst string, reverse bool, deleteMode bool, dryRun bool) []string {
	sshCmd := buildVivaNodeSSHCommandString(conn)
	args := []string{"-Parvzh", "-e", sshCmd}
	if deleteMode {
		args = append(args, "--delete")
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	remote := fmt.Sprintf("%s@%s:%s", conn.User, conn.Host, dst)
	if reverse {
		remote = fmt.Sprintf("%s@%s:%s", conn.User, conn.Host, src)
		args = append(args, remote, dst)
		return args
	}
	args = append(args, src, remote)
	return args
}

func vivaShellQuote(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`!&|;()<>{}") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func vivaNodeSortedKeys(entries map[string]VivaNodeProfile) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func vivaNodeProtocolLabels(entry VivaNodeProfile) []string {
	entry = normalizeVivaNodeProfile(entry)
	labels := make([]string, 0, 3)
	if entry.Protocols.SSH != nil && *entry.Protocols.SSH {
		labels = append(labels, "ssh")
	}
	if entry.Protocols.Mosh != nil && *entry.Protocols.Mosh {
		labels = append(labels, "mosh")
	}
	if entry.Protocols.Rsync != nil && *entry.Protocols.Rsync {
		labels = append(labels, "rsync")
	}
	return labels
}

func vivaNodeFieldDisplay(raw string, envKey string) string {
	raw = strings.TrimSpace(raw)
	envKey = strings.TrimSpace(envKey)
	if raw != "" {
		return raw
	}
	if envKey != "" {
		return "env:" + envKey
	}
	return ""
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
