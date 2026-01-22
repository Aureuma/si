package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdRoster(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si roster <apply|status>")
		return
	}
	switch args[0] {
	case "apply":
		cmdRosterApply(args[1:])
	case "status":
		cmdRosterStatus(args[1:])
	default:
		fmt.Println("unknown roster command:", args[0])
	}
}

type rosterFile struct {
	Defaults rosterEntry   `json:"defaults"`
	Dyads    []rosterEntry `json:"dyads"`
}

type rosterEntry struct {
	Name       string      `json:"name"`
	Role       string      `json:"role"`
	Department string      `json:"department"`
	Team       string      `json:"team"`
	Assignment string      `json:"assignment"`
	Status     string      `json:"status"`
	Message    string      `json:"message"`
	Available  *bool       `json:"available"`
	Tags       interface{} `json:"tags"`
	Spawn      *bool       `json:"spawn"`
}

func cmdRosterApply(args []string) {
	fs := flag.NewFlagSet("roster apply", flag.ExitOnError)
	fileFlag := fs.String("file", "", "roster file")
	spawn := fs.Bool("spawn", false, "spawn dyads marked spawn:true")
	dryRun := fs.Bool("dry-run", false, "dry run")
	fs.Parse(args)

	root := mustRepoRoot()
	rosterPath := *fileFlag
	if rosterPath == "" {
		rosterPath = envOr("DYAD_ROSTER_FILE", filepath.Join(root, "configs", "dyad_roster.json"))
	}
	data, err := os.ReadFile(rosterPath)
	if err != nil {
		fatal(err)
	}
	var roster rosterFile
	if err := json.Unmarshal(data, &roster); err != nil {
		fatal(err)
	}
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")

	for _, entry := range roster.Dyads {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		role := pick(entry.Role, roster.Defaults.Role, "generic")
		dept := pick(entry.Department, roster.Defaults.Department, role)
		team := pick(entry.Team, roster.Defaults.Team, "")
		assignment := pick(entry.Assignment, roster.Defaults.Assignment, "")
		status := pick(entry.Status, roster.Defaults.Status, "")
		message := pick(entry.Message, roster.Defaults.Message, "")
		available := boolOr(entry.Available, roster.Defaults.Available, true)
		tags := normalizeTags(entry.Tags, roster.Defaults.Tags)

		update := map[string]interface{}{
			"dyad":       name,
			"role":       role,
			"department": dept,
			"available":  available,
		}
		if team != "" {
			update["team"] = team
		}
		if assignment != "" {
			update["assignment"] = assignment
		}
		if status != "" {
			update["status"] = status
		}
		if message != "" {
			update["message"] = message
		}
		if len(tags) > 0 {
			update["tags"] = tags
		}

		if *dryRun {
			fmt.Printf("would update dyad: %s\n", name)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := postJSON(ctx, strings.TrimRight(managerURL, "/")+"/dyads", update, nil); err != nil {
				cancel()
				fatal(err)
			}
			cancel()
			fmt.Printf("updated dyad: %s\n", name)
		}

		if *spawn && boolOr(entry.Spawn, roster.Defaults.Spawn, false) {
			if *dryRun {
				fmt.Printf("would spawn dyad: %s\n", name)
			} else {
				if err := spawnDyadFromEnv(name, role, dept); err != nil {
					fatal(err)
				}
			}
		}
		fmt.Println()
	}
}

func cmdRosterStatus(args []string) {
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	var rows []map[string]interface{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/dyads", &rows); err != nil {
		fatal(err)
	}
	if len(rows) == 0 {
		fmt.Println("no dyads registered")
		return
	}
	type row struct {
		Dyad       string
		Team       string
		Assignment string
		Role       string
		Dept       string
		Available  string
		State      string
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{
			Dyad:       strVal(r["dyad"]),
			Team:       strVal(r["team"]),
			Assignment: strVal(r["assignment"]),
			Role:       strVal(r["role"]),
			Dept:       strVal(r["department"]),
			Available:  boolLabel(r["available"]),
			State:      strVal(r["state"]),
		})
	}
	widths := map[string]int{"dyad": 4, "team": 4, "assignment": 10, "role": 4, "dept": 4, "avail": 5, "state": 5}
	for _, r := range out {
		widths["dyad"] = max(widths["dyad"], len(r.Dyad))
		widths["team"] = max(widths["team"], len(r.Team))
		widths["assignment"] = max(widths["assignment"], len(r.Assignment))
		widths["role"] = max(widths["role"], len(r.Role))
		widths["dept"] = max(widths["dept"], len(r.Dept))
		widths["avail"] = max(widths["avail"], len(r.Available))
		widths["state"] = max(widths["state"], len(r.State))
	}
	fmt.Printf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
		widths["dyad"], "dyad",
		widths["team"], "team",
		widths["assignment"], "assignment",
		widths["role"], "role",
		widths["dept"], "dept",
		widths["avail"], "avail",
		widths["state"], "state",
	)
	fmt.Println(strings.Repeat("-", widths["dyad"]+widths["team"]+widths["assignment"]+widths["role"]+widths["dept"]+widths["avail"]+widths["state"]+12))
	for _, r := range out {
		fmt.Printf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
			widths["dyad"], r.Dyad,
			widths["team"], r.Team,
			widths["assignment"], r.Assignment,
			widths["role"], r.Role,
			widths["dept"], r.Dept,
			widths["avail"], r.Available,
			widths["state"], r.State,
		)
	}
	_ = args
}

func pick(primary, fallback, def string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return def
}

func boolOr(primary, fallback *bool, def bool) bool {
	if primary != nil {
		return *primary
	}
	if fallback != nil {
		return *fallback
	}
	return def
}

func normalizeTags(primary interface{}, fallback interface{}) []string {
	tags := normalizeTagList(primary)
	if len(tags) == 0 {
		tags = normalizeTagList(fallback)
	}
	return tags
}

func normalizeTagList(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	default:
		return nil
	}
}

func strVal(raw interface{}) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func boolLabel(raw interface{}) string {
	if raw == nil {
		return "no"
	}
	switch v := raw.(type) {
	case bool:
		if v {
			return "yes"
		}
		return "no"
	default:
		return ""
	}
}

