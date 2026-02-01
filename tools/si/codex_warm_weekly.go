package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	shared "si/agents/shared/docker"
)

const weeklyWarmPrompt = "You are warming the Codex weekly limit. Read the following brief project memo and reply with exactly two bullet points labeled 'A' and 'B' that summarize the two biggest risks. Keep the response under 40 words.\n\n" +
	"Project memo: We are preparing a phased rollout of a developer tool that integrates a local agent runner with remote services. The rollout has four phases: internal dogfood, private beta, public beta, and general availability. The engineering team has completed the core runtime, but the configuration surface is still shifting and the documentation is inconsistent across repos. The most recent feedback indicates that authentication handoffs can fail when tokens are rotated, and the CLI error messages are too vague to guide the user to a fix. In addition, the system depends on several third-party APIs with tight rate limits and intermittent latency spikes. The platform team is optimistic about the stability of the backend, but we have not completed load testing for the largest anticipated enterprise tenant. The security team has requested a review of the permissions model, especially around automated file edits and command execution. The product team expects a weekly release cadence and wants upgrade notes to be concise. Support has flagged a backlog of tickets related to onboarding flow confusion, and the current FAQ has not been updated since the last UI change. There is also an open question about whether the new multi-profile feature should default to the last-used profile or prompt the user every time. Finally, the legal team has asked for a clear statement of data retention policy, but no owner has been assigned.\n\n" +
	"Additional context: The integration must support macOS and Linux. We plan to ship a background scheduler that triggers a maintenance task weekly. If the scheduler misfires, we could see noisy alerts and wasted tokens. The team has proposed sending a small prompt to verify the pipeline, but this prompt should be large enough to actually consume tokens so the weekly limit timer advances. The marketing team wants to announce the rollout with a blog post, but this is blocked on finalizing the SLA language. The roadmap includes adding a UI dashboard to show usage limits, but the backend endpoint is still evolving. An internal review suggested that a short, consistent warm-up prompt would be easiest to maintain. The deployment checklist is long and includes updating image tags, verifying auth storage, and confirming the rate limit resets are correctly detected. We have one week to stabilize, and any delays will impact the target launch date."

func cmdWarmWeekly(args []string) {
	fs := flag.NewFlagSet("warm-weekly", flag.ExitOnError)
	profilesFlag := multiFlag{}
	fs.Var(&profilesFlag, "profile", "codex profile name/email (repeatable)")
	promptFlag := fs.String("prompt", "", "prompt to execute")
	promptFile := fs.String("prompt-file", "", "path to prompt text")
	jitterMin := fs.Int("jitter-min", 1, "min jitter minutes after reset")
	jitterMax := fs.Int("jitter-max", 5, "max jitter minutes after reset")
	dryRun := fs.Bool("dry-run", false, "print schedule without running")
	runNow := fs.Bool("run-now", false, "run immediately using jitter instead of waiting for reset")
	ofeliaInstall := fs.Bool("ofelia-install", false, "install/update ofelia scheduler")
	ofeliaWrite := fs.Bool("ofelia-write", false, "write ofelia config without starting container")
	ofeliaRemove := fs.Bool("ofelia-remove", false, "remove ofelia scheduler container")
	ofeliaName := fs.String("ofelia-name", defaultOfeliaName, "ofelia container name")
	ofeliaImage := fs.String("ofelia-image", defaultOfeliaImage, "ofelia docker image")
	ofeliaConfig := fs.String("ofelia-config", "", "ofelia config path")
	ofeliaPrompt := fs.String("ofelia-prompt", "", "ofelia prompt file path")
	noMcp := fs.Bool("no-mcp", true, "disable MCP servers")
	outputOnly := fs.Bool("output-only", true, "print only the Codex response")
	image := fs.String("image", envOr("SI_CODEX_IMAGE", "aureuma/si:local"), "docker image")
	workspaceHost := fs.String("workspace", envOr("SI_WORKSPACE_HOST", ""), "host path to workspace")
	workdir := fs.String("workdir", "/workspace", "container working directory")
	networkName := fs.String("network", envOr("SI_NETWORK", shared.DefaultNetwork), "docker network")
	codexVolume := fs.String("codex-volume", envOr("SI_CODEX_EXEC_VOLUME", ""), "codex volume name")
	ghVolume := fs.String("gh-volume", "", "gh config volume name")
	model := fs.String("model", envOr("CODEX_MODEL", "gpt-5.2-codex"), "codex model")
	effort := fs.String("effort", envOr("CODEX_REASONING_EFFORT", "medium"), "codex reasoning effort")
	dockerSocket := fs.Bool("docker-socket", true, "mount host docker socket in one-off containers")
	keep := fs.Bool("keep", false, "keep the one-off container after execution")
	_ = fs.Parse(args)

	prompt := strings.TrimSpace(*promptFlag)
	if prompt == "" && strings.TrimSpace(*promptFile) != "" {
		data, err := os.ReadFile(filepath.Clean(*promptFile))
		if err != nil {
			fatal(err)
		}
		prompt = strings.TrimSpace(string(data))
	}
	if prompt == "" {
		prompt = weeklyWarmPrompt
	}

	minJitter := *jitterMin
	maxJitter := *jitterMax
	if minJitter < 0 {
		minJitter = 0
	}
	if maxJitter < minJitter {
		maxJitter = minJitter
	}

	profiles := selectWarmWeeklyProfiles(profilesFlag)
	if len(profiles) == 0 {
		infof("no logged-in profiles found")
		return
	}
	if *ofeliaRemove {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := removeOfeliaWarmContainer(ctx, *ofeliaName); err != nil {
			fatal(err)
		}
		successf("removed ofelia container %s", *ofeliaName)
		return
	}

	if *ofeliaInstall || *ofeliaWrite {
		configPath := strings.TrimSpace(*ofeliaConfig)
		promptPath := strings.TrimSpace(*ofeliaPrompt)
		if configPath == "" || promptPath == "" {
			defaultConfig, defaultPrompt, err := defaultOfeliaPaths()
			if err != nil {
				fatal(err)
			}
			if configPath == "" {
				configPath = defaultConfig
			}
			if promptPath == "" {
				promptPath = defaultPrompt
			}
		}

		jobs := make([]ofeliaWarmJob, 0, len(profiles))
		now := time.Now()
		for _, profile := range profiles {
			job, err := buildOfeliaWarmJob(profile, now)
			if err != nil {
				warnf("profile %s skipped: %v", profile.ID, err)
				continue
			}
			jobs = append(jobs, job)
		}
		if len(jobs) == 0 {
			infof("no ofelia jobs created")
			return
		}

		tz := strings.TrimSpace(os.Getenv("TZ"))
		if tz == "" {
			if loc := time.Now().Location(); loc != nil {
				tz = loc.String()
			}
		}
		opts := ofeliaWarmOptions{
			Name:        strings.TrimSpace(*ofeliaName),
			Image:       strings.TrimSpace(*image),
			OfeliaImage: strings.TrimSpace(*ofeliaImage),
			ConfigPath:  configPath,
			PromptPath:  promptPath,
			TZ:          tz,
			Model:       strings.TrimSpace(*model),
			Effort:      strings.TrimSpace(*effort),
			JitterMin:   minJitter,
			JitterMax:   maxJitter,
		}
		if err := ensureOfeliaWarmConfig(jobs, prompt, opts); err != nil {
			fatal(err)
		}
		printOfeliaWarmJobs(jobs, opts)
		if *ofeliaWrite && !*ofeliaInstall {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := ensureOfeliaWarmContainer(ctx, opts); err != nil {
			fatal(err)
		}
		successf("ofelia scheduler %s running", opts.Name)
		return
	}
	schedules := make([]weeklyWarmSchedule, 0, len(profiles))
	now := time.Now()
	for i, profile := range profiles {
		schedule, err := buildWeeklyWarmSchedule(profile, now, minJitter, maxJitter, int64(i), *runNow)
		if err != nil {
			warnf("profile %s skipped: %v", profile.ID, err)
			continue
		}
		schedules = append(schedules, schedule)
	}
	if len(schedules) == 0 {
		infof("no schedules created")
		return
	}

	printWeeklyWarmSchedules(schedules)
	if *dryRun {
		return
	}

	var wg sync.WaitGroup
	for _, schedule := range schedules {
		schedule := schedule
		wg.Add(1)
		go func() {
			defer wg.Done()
			sleep := time.Until(schedule.FireAt)
			if sleep > 0 {
				time.Sleep(sleep)
			}
			err := runWeeklyWarmPrompt(schedule.Profile, prompt, weeklyWarmExecOptions{
				Image:         *image,
				Workspace:     *workspaceHost,
				Workdir:       *workdir,
				Network:       *networkName,
				CodexVolume:   *codexVolume,
				GHVolume:      *ghVolume,
				Model:         *model,
				Effort:        *effort,
				DisableMCP:    *noMcp,
				OutputOnly:    *outputOnly,
				KeepContainer: *keep,
				DockerSocket:  *dockerSocket,
			})
			if err != nil {
				warnf("weekly warm failed for %s: %v", schedule.Profile.ID, err)
			}
		}()
	}
	wg.Wait()
}

type weeklyWarmSchedule struct {
	Profile    codexProfile
	ResetAt    time.Time
	JitterMins int
	FireAt     time.Time
}

type weeklyWarmExecOptions struct {
	Image         string
	Workspace     string
	Workdir       string
	Network       string
	CodexVolume   string
	GHVolume      string
	Model         string
	Effort        string
	DisableMCP    bool
	OutputOnly    bool
	KeepContainer bool
	DockerSocket  bool
}

func selectWarmWeeklyProfiles(keys []string) []codexProfile {
	if len(keys) == 0 {
		return loggedInProfiles()
	}
	out := make([]codexProfile, 0, len(keys))
	seen := map[string]bool{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		profile, err := requireCodexProfile(key)
		if err != nil {
			warnf("profile %s not found: %v", key, err)
			continue
		}
		if seen[profile.ID] {
			continue
		}
		seen[profile.ID] = true
		if codexProfileAuthStatus(profile).Exists {
			out = append(out, profile)
		}
	}
	return out
}

func loggedInProfiles() []codexProfile {
	items := codexProfileSummaries()
	out := make([]codexProfile, 0, len(items))
	for _, item := range items {
		if !item.AuthCached {
			continue
		}
		out = append(out, codexProfile{ID: item.ID, Name: item.Name, Email: item.Email})
	}
	return out
}

func buildWeeklyWarmSchedule(profile codexProfile, now time.Time, minJitter, maxJitter int, seed int64, runNow bool) (weeklyWarmSchedule, error) {
	auth, err := loadProfileAuthTokens(profile)
	if err != nil {
		return weeklyWarmSchedule{}, err
	}
	resetAt := now
	windowSeconds := int64(0)
	if !runNow {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		payload, err := fetchUsagePayload(ctx, profileUsageURL(), auth)
		if err != nil {
			return weeklyWarmSchedule{}, err
		}
		var ok bool
		resetAt, windowSeconds, ok = weeklyResetTime(payload, now)
		if !ok {
			return weeklyWarmSchedule{}, fmt.Errorf("weekly reset not available")
		}
		resetAt = normalizeResetTime(resetAt, windowSeconds, now)
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + seed))
	jitter := minJitter
	if maxJitter > minJitter {
		jitter = minJitter + rng.Intn(maxJitter-minJitter+1)
	}
	fireAt := resetAt.Add(time.Duration(jitter) * time.Minute)
	if fireAt.Before(now) {
		fireAt = now
	}
	return weeklyWarmSchedule{
		Profile:    profile,
		ResetAt:    resetAt,
		JitterMins: jitter,
		FireAt:     fireAt,
	}, nil
}

func weeklyResetTime(payload usagePayload, now time.Time) (time.Time, int64, bool) {
	if payload.RateLimit == nil || payload.RateLimit.Secondary == nil {
		return time.Time{}, 0, false
	}
	window := payload.RateLimit.Secondary
	if window.ResetAt != nil && *window.ResetAt > 0 {
		reset := time.Unix(*window.ResetAt, 0).Local()
		return reset, derefInt64(window.LimitWindowSeconds), true
	}
	if window.ResetAfterSeconds != nil && *window.ResetAfterSeconds > 0 {
		reset := now.Add(time.Duration(*window.ResetAfterSeconds) * time.Second)
		return reset, derefInt64(window.LimitWindowSeconds), true
	}
	return time.Time{}, derefInt64(window.LimitWindowSeconds), false
}

func normalizeResetTime(resetAt time.Time, windowSeconds int64, now time.Time) time.Time {
	if resetAt.After(now) {
		return resetAt
	}
	step := windowSeconds
	if step <= 0 {
		step = int64((7 * 24 * time.Hour).Seconds())
	}
	for !resetAt.After(now) {
		resetAt = resetAt.Add(time.Duration(step) * time.Second)
	}
	return resetAt
}

func runWeeklyWarmPrompt(profile codexProfile, prompt string, opts weeklyWarmExecOptions) error {
	oneOff := codexExecOneOffOptions{
		Prompt:        prompt,
		Image:         strings.TrimSpace(opts.Image),
		WorkspaceHost: strings.TrimSpace(opts.Workspace),
		Workdir:       strings.TrimSpace(opts.Workdir),
		Network:       strings.TrimSpace(opts.Network),
		CodexVolume:   strings.TrimSpace(opts.CodexVolume),
		GHVolume:      strings.TrimSpace(opts.GHVolume),
		Model:         strings.TrimSpace(opts.Model),
		Effort:        strings.TrimSpace(opts.Effort),
		DisableMCP:    opts.DisableMCP,
		OutputOnly:    opts.OutputOnly,
		KeepContainer: opts.KeepContainer,
		DockerSocket:  opts.DockerSocket,
		Profile:       &profile,
	}
	return runCodexExecOneOff(oneOff)
}

func printWeeklyWarmSchedules(schedules []weeklyWarmSchedule) {
	fmt.Println(styleHeading("Weekly warm schedule:"))
	for _, schedule := range schedules {
		fmt.Printf("  %s reset %s +%dm -> %s\n",
			schedule.Profile.ID,
			formatResetAt(schedule.ResetAt),
			schedule.JitterMins,
			formatResetAt(schedule.FireAt),
		)
	}
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
