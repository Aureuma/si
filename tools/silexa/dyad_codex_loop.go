package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	shared "silexa/agents/shared/docker"
)

type dyadTaskSnapshot struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Dyad        string    `json:"dyad"`
	Actor       string    `json:"actor"`
	Critic      string    `json:"critic"`
	Notes       string    `json:"notes"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func cmdDyadCodexLoopTest(args []string) {
	fs := flag.NewFlagSet("dyad codex-loop-test", flag.ExitOnError)
	title := fs.String("title", "codex loop test", "task title")
	description := fs.String("description", "Verify critic-to-actor Codex loop execution.", "task description")
	priority := fs.String("priority", "high", "task priority")
	timeout := fs.Duration("timeout", 20*time.Minute, "max time to wait for completion")
	wait := fs.Bool("wait", true, "wait for the task to complete")
	spawn := fs.Bool("spawn", false, "spawn the dyad if missing")
	role := fs.String("role", "", "dyad role (only with --spawn)")
	dept := fs.String("department", "", "dyad department (only with --spawn)")
	installCodex := fs.Bool("install-codex", true, "install codex CLI in the actor container if missing")
	requireLogin := fs.Bool("require-login", true, "require actor codex login before starting")
	managerURL := fs.String("manager-url", envOr("MANAGER_URL", "http://localhost:9090"), "manager URL")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("usage: si dyad codex-loop-test <dyad> [--wait] [--spawn]")
		return
	}

	dyad := fs.Arg(0)
	if err := validateSlug(dyad); err != nil {
		fatal(err)
	}

	ctx := context.Background()
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()

	found, running, err := client.DyadStatus(ctx, dyad)
	if err != nil {
		fatal(err)
	}
	if !found {
		if !*spawn {
			fatal(fmt.Errorf("dyad %s not found; run `si dyad spawn %s` or pass --spawn", dyad, dyad))
		}
		if err := spawnDyadFromEnv(dyad, strings.TrimSpace(*role), strings.TrimSpace(*dept)); err != nil {
			fatal(err)
		}
		found = true
		running = true
	}
	if found && !running {
		if err := client.RestartDyad(ctx, dyad); err != nil {
			fatal(err)
		}
		time.Sleep(2 * time.Second)
	}

	actorName := shared.DyadContainerName(dyad, "actor")
	actorID, _, err := client.ContainerByName(ctx, actorName)
	if err != nil || actorID == "" {
		fatal(fmt.Errorf("actor container not found: %s", actorName))
	}

	if *installCodex {
		if err := ensureCodexInstalled(ctx, client, actorID); err != nil {
			fatal(err)
		}
	}

	if *requireLogin {
		ok, status, err := codexLoginStatus(ctx, client, actorID)
		if err != nil {
			fatal(err)
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "actor codex login required (status: %s)\n", status)
			fmt.Fprintf(os.Stderr, "run: si dyad exec %s --member actor -- codex login\n", dyad)
			os.Exit(1)
		}
	}

	payload := dyadTaskPayload{
		Title:       strings.TrimSpace(*title),
		Description: strings.TrimSpace(*description),
		Kind:        "test.codex_loop",
		Priority:    strings.TrimSpace(*priority),
		Dyad:        dyad,
		RequestedBy: envOr("REQUESTED_BY", "si"),
	}
	task := dyadTaskSnapshot{}
	ctxPost, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := postJSON(ctxPost, strings.TrimRight(*managerURL, "/")+"/dyad-tasks", payload, &task); err != nil {
		cancel()
		fatal(err)
	}
	cancel()
	fmt.Printf("dyad task #%d created (status=%s)\n", task.ID, task.Status)

	if !*wait {
		return
	}

	final, err := waitForDyadTask(ctx, strings.TrimRight(*managerURL, "/"), task.ID, *timeout)
	if err != nil {
		fatal(err)
	}
	printCodexLoopSummary(final)
	if strings.ToLower(strings.TrimSpace(final.Status)) == "blocked" {
		os.Exit(1)
	}
}

func ensureCodexInstalled(ctx context.Context, client *shared.Client, containerID string) error {
	out, err := execInContainerCapture(ctx, client, containerID, []string{"bash", "-lc", "command -v codex"})
	if err == nil && strings.TrimSpace(out) != "" {
		return nil
	}
	ctxInstall, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if _, err := execInContainerCapture(ctxInstall, client, containerID, []string{"bash", "-lc", "npm i -g @openai/codex"}); err != nil {
		return fmt.Errorf("install codex: %w", err)
	}
	out, err = execInContainerCapture(ctx, client, containerID, []string{"bash", "-lc", "command -v codex"})
	if err != nil || strings.TrimSpace(out) == "" {
		return fmt.Errorf("codex CLI not found after install")
	}
	return nil
}

func codexLoginStatus(ctx context.Context, client *shared.Client, containerID string) (bool, string, error) {
	out, err := execInContainerCapture(ctx, client, containerID, []string{"codex", "login", "status"})
	if err != nil {
		return false, "", err
	}
	status := strings.TrimSpace(out)
	return strings.Contains(status, "Logged in"), status, nil
}

func waitForDyadTask(ctx context.Context, managerURL string, id int, timeout time.Duration) (dyadTaskSnapshot, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus, lastPhase, lastLast string
	for {
		task, ok, err := fetchDyadTask(ctx, managerURL, id)
		if err != nil {
			return dyadTaskSnapshot{}, err
		}
		if ok {
			state := parseTaskNotes(task.Notes)
			status := strings.ToLower(strings.TrimSpace(task.Status))
			phase := state["codex_test.phase"]
			last := state["codex_test.last"]
			if status != lastStatus || phase != lastPhase || last != lastLast {
				fmt.Printf("status=%s phase=%s last=%s\n", task.Status, phase, last)
				lastStatus = status
				lastPhase = phase
				lastLast = last
			}
			if status == "done" || status == "blocked" {
				return task, nil
			}
		}
		if time.Now().After(deadline) {
			return dyadTaskSnapshot{}, fmt.Errorf("timeout waiting for dyad task %d", id)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return dyadTaskSnapshot{}, ctx.Err()
		}
	}
}

func fetchDyadTask(ctx context.Context, managerURL string, id int) (dyadTaskSnapshot, bool, error) {
	ctxFetch, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tasks := []dyadTaskSnapshot{}
	if err := getJSON(ctxFetch, strings.TrimRight(managerURL, "/")+"/dyad-tasks", &tasks); err != nil {
		return dyadTaskSnapshot{}, false, err
	}
	for _, t := range tasks {
		if t.ID == id {
			return t, true, nil
		}
	}
	return dyadTaskSnapshot{}, false, nil
}

func printCodexLoopSummary(task dyadTaskSnapshot) {
	fmt.Printf("final status: %s\n", task.Status)
	state := parseTaskNotes(task.Notes)
	if v := state["codex_test.result"]; v != "" {
		fmt.Printf("result: %s\n", v)
	}
	if v := state["codex_test.last"]; v != "" {
		fmt.Printf("last output: %s\n", v)
	}
	if v := state["codex_test.error"]; v != "" {
		fmt.Printf("error: %s\n", v)
	}
	if v := state["codex.session_id"]; v != "" {
		fmt.Printf("session: %s\n", v)
	}
	if v := state["codex.exec.last"]; v != "" {
		fmt.Printf("exec last: %s\n", v)
	}
}

func parseTaskNotes(notes string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "]=") {
			continue
		}
		end := strings.Index(line, "]=")
		if end <= 1 {
			continue
		}
		key := strings.TrimSpace(line[1:end])
		val := strings.TrimSpace(line[end+2:])
		if key != "" {
			out[key] = val
		}
	}
	return out
}

func execInContainerCapture(ctx context.Context, client *shared.Client, containerID string, cmd []string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := client.Exec(ctx, containerID, cmd, shared.ExecOptions{}, nil, &stdout, &stderr); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	if out == "" {
		return errOut, nil
	}
	if errOut != "" {
		return out + "\n" + errOut, nil
	}
	return out, nil
}
