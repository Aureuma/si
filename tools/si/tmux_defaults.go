package main

import (
	"context"
	"strings"
)

const siTmuxHistoryLimit = "200000"

func applyTmuxSessionDefaults(ctx context.Context, session string) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	_, _ = tmuxOutput(ctx, "set-option", "-t", session, "remain-on-exit", "off")
	_, _ = tmuxOutput(ctx, "set-option", "-t", session, "mouse", "on")
	_, _ = tmuxOutput(ctx, "set-option", "-t", session, "history-limit", siTmuxHistoryLimit)
}
