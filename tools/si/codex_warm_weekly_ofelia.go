package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shared "si/agents/shared/docker"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

const (
	defaultOfeliaName  = "si-ofelia"
	defaultOfeliaImage = "mcuadros/ofelia:latest"
)

type ofeliaWarmOptions struct {
	Name        string
	Image       string
	OfeliaImage string
	ConfigPath  string
	PromptPath  string
	TZ          string
	Model       string
	Effort      string
	JitterMin   int
	JitterMax   int
}

type ofeliaWarmJob struct {
	Profile  codexProfile
	ResetAt  time.Time
	Schedule string
}

func ensureOfeliaWarmConfig(jobs []ofeliaWarmJob, prompt string, opts ofeliaWarmOptions) error {
	if strings.TrimSpace(opts.ConfigPath) == "" || strings.TrimSpace(opts.PromptPath) == "" {
		return fmt.Errorf("ofelia config and prompt paths are required")
	}
	configDir := filepath.Dir(opts.ConfigPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	promptDir := filepath.Dir(opts.PromptPath)
	if err := os.MkdirAll(promptDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(opts.PromptPath, []byte(prompt+"\n"), 0o600); err != nil {
		return err
	}

	var buf bytes.Buffer

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Profile.ID < jobs[j].Profile.ID
	})
	for _, job := range jobs {
		jobName := fmt.Sprintf("si-warm-weekly-%s", sanitizeOfeliaName(job.Profile.ID))
		buf.WriteString(fmt.Sprintf("[job-run \"%s\"]\n", jobName))
		buf.WriteString(fmt.Sprintf("schedule = %s\n", job.Schedule))
		buf.WriteString(fmt.Sprintf("image = %s\n", opts.Image))
		buf.WriteString(fmt.Sprintf("command = %s\n", buildOfeliaWarmCommand(opts)))
		if authPath, err := codexProfileAuthPath(job.Profile); err == nil && strings.TrimSpace(authPath) != "" {
			buf.WriteString(fmt.Sprintf("volume = %s:/home/si/.codex/auth.json:ro\n", authPath))
		}
		buf.WriteString(fmt.Sprintf("volume = %s:/etc/si/warm-weekly-prompt.txt:ro\n", opts.PromptPath))
		buf.WriteString("\n")
	}

	return os.WriteFile(opts.ConfigPath, buf.Bytes(), 0o600)
}

func ensureOfeliaWarmContainer(ctx context.Context, opts ofeliaWarmOptions) error {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = defaultOfeliaName
	}
	image := strings.TrimSpace(opts.OfeliaImage)
	if image == "" {
		image = defaultOfeliaImage
	}

	client, err := shared.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()

	if id, _, err := client.ContainerByName(ctx, name); err == nil && id != "" {
		_ = client.RemoveContainer(ctx, id, true)
	}

	mounts := []mount.Mount{}
	if socket, ok := shared.DockerSocketMount(); ok {
		mounts = append(mounts, socket)
	}
	mounts = append(mounts, mount.Mount{
		Type:     mount.TypeBind,
		Source:   opts.ConfigPath,
		Target:   "/etc/ofelia/config.ini",
		ReadOnly: true,
	})

	cfg := &container.Config{
		Image: image,
		Cmd:   []string{"daemon", "--config", "/etc/ofelia/config.ini"},
		Labels: map[string]string{
			"si.component": "ofelia",
			"si.name":      name,
		},
	}
	if strings.TrimSpace(opts.TZ) != "" {
		cfg.Env = append(cfg.Env, "TZ="+strings.TrimSpace(opts.TZ))
	}
	if strings.TrimSpace(opts.Image) != "" {
		cfg.Env = append(cfg.Env, "OFELIA_SI_IMAGE="+strings.TrimSpace(opts.Image))
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Mounts:        mounts,
	}

	id, err := client.CreateContainer(ctx, cfg, hostCfg, nil, name)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") || strings.Contains(err.Error(), "not found") {
			if pullErr := execDockerCLI("pull", image); pullErr == nil {
				id, err = client.CreateContainer(ctx, cfg, hostCfg, nil, name)
			} else {
				return pullErr
			}
		}
		if err != nil {
			return err
		}
	}
	return client.StartContainer(ctx, id)
}

func removeOfeliaWarmContainer(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultOfeliaName
	}
	client, err := shared.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()

	id, _, err := client.ContainerByName(ctx, name)
	if err != nil || id == "" {
		return err
	}
	return client.RemoveContainer(ctx, id, true)
}

func buildOfeliaWarmJob(profile codexProfile, now time.Time) (ofeliaWarmJob, error) {
	auth, err := loadProfileAuthTokens(profile)
	if err != nil {
		return ofeliaWarmJob{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	payload, err := fetchUsagePayload(ctx, profileUsageURL(), auth)
	cancel()
	if err != nil {
		return ofeliaWarmJob{}, err
	}
	resetAt, windowSeconds, ok := weeklyResetTime(payload, now)
	if !ok {
		return ofeliaWarmJob{}, fmt.Errorf("weekly reset not available")
	}
	resetAt = normalizeResetTime(resetAt, windowSeconds, now)
	return ofeliaWarmJob{
		Profile:  profile,
		ResetAt:  resetAt,
		Schedule: cronSchedule(resetAt),
	}, nil
}

func cronSchedule(t time.Time) string {
	return fmt.Sprintf("0 %d %d * * %d", t.Minute(), t.Hour(), int(t.Weekday()))
}

func buildOfeliaWarmCommand(opts ofeliaWarmOptions) string {
	minJitter := opts.JitterMin
	maxJitter := opts.JitterMax
	if minJitter < 0 {
		minJitter = 0
	}
	if maxJitter < minJitter {
		maxJitter = minJitter
	}
	jitterExpr := fmt.Sprintf("j=$((RANDOM %% %d + %d))", maxJitter-minJitter+1, minJitter)
	if minJitter == 0 && maxJitter == 0 {
		jitterExpr = "j=0"
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = "gpt-5.2-codex"
	}
	effort := normalizeReasoningEffort(opts.Effort)
	if effort == "" {
		effort = "medium"
	}
	return fmt.Sprintf("/bin/bash -lc '%s; sleep $((j*60)); export HOME=/home/si CODEX_HOME=/home/si/.codex; codex -m %s -c model_reasoning_effort=%s --dangerously-bypass-approvals-and-sandbox exec \"$(cat /etc/si/warm-weekly-prompt.txt)\"'", jitterExpr, model, effort)
}

func sanitizeOfeliaName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "profile"
	}
	out := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
		case r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-' || r == '_':
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}

func defaultOfeliaPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	base := filepath.Join(home, ".si", "ofelia")
	return filepath.Join(base, "warm-weekly.ini"), filepath.Join(base, "warm-weekly-prompt.txt"), nil
}

func printOfeliaWarmJobs(jobs []ofeliaWarmJob, opts ofeliaWarmOptions) {
	fmt.Println(styleHeading("Ofelia warm-weekly jobs:"))
	for _, job := range jobs {
		fmt.Printf("  %s schedule %s (reset %s)\n", job.Profile.ID, job.Schedule, formatResetAt(job.ResetAt))
	}
	if strings.TrimSpace(opts.ConfigPath) != "" {
		fmt.Printf("  %s %s\n", styleSection("Config:"), styleArg(opts.ConfigPath))
	}
	if strings.TrimSpace(opts.PromptPath) != "" {
		fmt.Printf("  %s %s\n", styleSection("Prompt:"), styleArg(opts.PromptPath))
	}
}
