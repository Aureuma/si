package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const paasDeployPruneUsageText = "usage: si paas deploy prune [--app <slug>] [--bundle-root <path>] [--keep <n>] [--max-age <duration>] [--events-max-age <duration>] [--dry-run] [--json]"

type paasReleaseCandidate struct {
	App     string
	Release string
	Path    string
	Created time.Time
}

type paasPruneSummary struct {
	AppsScanned       int
	ReleasesScanned   int
	ReleasesRemoved   int
	EventsScanned     int
	EventsRemoved     int
	ProtectedReleases int
}

func cmdPaasDeployPrune(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy prune", flag.ExitOnError)
	app := fs.String("app", "", "restrict pruning to one app slug")
	bundleRoot := fs.String("bundle-root", "", "release bundle root path")
	keep := fs.Int("keep", 10, "keep newest N releases per app (excluding protected current release)")
	maxAge := fs.String("max-age", "", "remove releases older than duration (for example 720h)")
	eventsMaxAge := fs.String("events-max-age", "720h", "remove deploy event entries older than duration")
	dryRun := fs.Bool("dry-run", false, "preview candidates without deleting files")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasDeployPruneUsageText)
		return
	}
	if *keep < 0 {
		failPaasCommand("deploy prune", jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass --keep >= 0",
			fmt.Errorf("invalid --keep %d", *keep),
		), nil)
	}
	maxAgeValue, err := parseOptionalDuration(strings.TrimSpace(*maxAge))
	if err != nil {
		failPaasCommand("deploy prune", jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a valid --max-age duration (for example 720h) or leave empty to disable age pruning",
			err,
		), nil)
	}
	eventsMaxAgeValue, err := parseOptionalDuration(strings.TrimSpace(*eventsMaxAge))
	if err != nil {
		failPaasCommand("deploy prune", jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a valid --events-max-age duration (for example 720h) or leave empty to disable event pruning",
			err,
		), nil)
	}

	summary, err := prunePaasDeployArtifacts(strings.TrimSpace(*bundleRoot), strings.TrimSpace(*app), *keep, maxAgeValue, eventsMaxAgeValue, *dryRun)
	if err != nil {
		failPaasCommand("deploy prune", jsonOut, err, nil)
	}

	fields := map[string]string{
		"app":                strings.TrimSpace(*app),
		"bundle_root":        strings.TrimSpace(*bundleRoot),
		"dry_run":            boolString(*dryRun),
		"events_max_age":     formatOptionalDuration(eventsMaxAgeValue),
		"events_removed":     intString(summary.EventsRemoved),
		"events_scanned":     intString(summary.EventsScanned),
		"keep":               intString(*keep),
		"max_age":            formatOptionalDuration(maxAgeValue),
		"protected_releases": intString(summary.ProtectedReleases),
		"releases_removed":   intString(summary.ReleasesRemoved),
		"releases_scanned":   intString(summary.ReleasesScanned),
		"scanned_apps":       intString(summary.AppsScanned),
		"status":             "completed",
	}
	if eventPath := recordPaasDeployEvent("deploy prune", "succeeded", fields, nil); strings.TrimSpace(eventPath) != "" {
		fields["event_log"] = eventPath
	}
	printPaasScaffold("deploy prune", fields, jsonOut)
}

func prunePaasDeployArtifacts(bundleRoot, app string, keep int, maxAge, eventsMaxAge time.Duration, dryRun bool) (paasPruneSummary, error) {
	summary := paasPruneSummary{}
	root, err := resolvePaasReleaseBundleRoot(bundleRoot)
	if err != nil {
		return summary, newPaasOperationFailure(
			paasFailureUnknown,
			"prune_resolve_root",
			"",
			"verify state root configuration and filesystem access",
			err,
		)
	}
	now := time.Now().UTC()
	apps, err := enumeratePaasReleaseApps(root, app)
	if err != nil {
		return summary, err
	}
	for _, appName := range apps {
		summary.AppsScanned++
		candidates, err := loadPaasReleaseCandidates(root, appName)
		if err != nil {
			return summary, err
		}
		summary.ReleasesScanned += len(candidates)
		current, err := resolvePaasCurrentRelease(appName)
		if err != nil {
			return summary, err
		}
		shouldRemove := selectPaasReleaseRemovals(candidates, current, keep, maxAge, now)
		for _, item := range shouldRemove {
			if dryRun {
				summary.ReleasesRemoved++
				continue
			}
			if err := os.RemoveAll(item.Path); err != nil {
				return summary, newPaasOperationFailure(
					paasFailureUnknown,
					"prune_release_remove",
					"",
					"verify local file permissions and retry prune",
					err,
				)
			}
			summary.ReleasesRemoved++
		}
		if strings.TrimSpace(current) != "" {
			summary.ProtectedReleases++
		}
	}

	eventsScanned, eventsRemoved, err := prunePaasDeployEvents(eventsMaxAge, dryRun, now)
	if err != nil {
		return summary, err
	}
	summary.EventsScanned = eventsScanned
	summary.EventsRemoved = eventsRemoved
	return summary, nil
}

func enumeratePaasReleaseApps(root, app string) ([]string, error) {
	if strings.TrimSpace(app) != "" {
		return []string{sanitizePaasReleasePathSegment(app)}, nil
	}
	entries, err := os.ReadDir(root) // #nosec G304 -- derived from managed state root.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func loadPaasReleaseCandidates(root, app string) ([]paasReleaseCandidate, error) {
	appDir := filepath.Join(root, sanitizePaasReleasePathSegment(app))
	entries, err := os.ReadDir(appDir) // #nosec G304 -- derived from managed state root.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]paasReleaseCandidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		releaseID := strings.TrimSpace(entry.Name())
		if releaseID == "" {
			continue
		}
		releaseDir := filepath.Join(appDir, releaseID)
		metaPath := filepath.Join(releaseDir, "release.json")
		created := time.Time{}
		if raw, err := os.ReadFile(metaPath); err == nil {
			var meta paasReleaseBundleMetadata
			if jsonErr := json.Unmarshal(raw, &meta); jsonErr == nil {
				if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(meta.CreatedAt)); parseErr == nil {
					created = parsed
				}
			}
		}
		if created.IsZero() {
			info, statErr := os.Stat(releaseDir)
			if statErr == nil {
				created = info.ModTime().UTC()
			}
		}
		out = append(out, paasReleaseCandidate{
			App:     app,
			Release: releaseID,
			Path:    releaseDir,
			Created: created,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Created.After(out[j].Created)
	})
	return out, nil
}

func selectPaasReleaseRemovals(candidates []paasReleaseCandidate, current string, keep int, maxAge time.Duration, now time.Time) []paasReleaseCandidate {
	protected := strings.TrimSpace(current)
	out := make([]paasReleaseCandidate, 0)
	kept := 0
	for _, item := range candidates {
		if protected != "" && strings.EqualFold(strings.TrimSpace(item.Release), protected) {
			continue
		}
		removeByAge := false
		if maxAge > 0 && !item.Created.IsZero() {
			removeByAge = now.Sub(item.Created) > maxAge
		}
		if removeByAge {
			out = append(out, item)
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		out = append(out, item)
	}
	return out
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("duration must be >= 0")
	}
	return d, nil
}

func formatOptionalDuration(d time.Duration) string {
	if d <= 0 {
		return "disabled"
	}
	return d.String()
}

func prunePaasDeployEvents(maxAge time.Duration, dryRun bool, now time.Time) (int, int, error) {
	contextDir, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		return 0, 0, err
	}
	path := filepath.Join(contextDir, "events", "deployments.jsonl")
	file, err := os.Open(path) // #nosec G304 -- derived from managed state root.
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	defer file.Close()

	type row struct {
		raw  string
		drop bool
	}
	rows := make([]row, 0)
	scanner := bufio.NewScanner(file)
	total := 0
	removed := 0
	for scanner.Scan() {
		line := scanner.Text()
		total++
		item := row{raw: line}
		if maxAge > 0 {
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err == nil {
				if ts, ok := event["timestamp"].(string); ok {
					if parsed, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(ts)); parseErr == nil {
						if now.Sub(parsed.UTC()) > maxAge {
							item.drop = true
							removed++
						}
					}
				}
			}
		}
		rows = append(rows, item)
	}
	if err := scanner.Err(); err != nil {
		return total, removed, err
	}
	if dryRun || removed == 0 {
		return total, removed, nil
	}
	tmp := path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return total, removed, err
	}
	for _, item := range rows {
		if item.drop {
			continue
		}
		if _, err := out.WriteString(item.raw + "\n"); err != nil {
			_ = out.Close()
			return total, removed, err
		}
	}
	if err := out.Close(); err != nil {
		return total, removed, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return total, removed, err
	}
	return total, removed, nil
}
