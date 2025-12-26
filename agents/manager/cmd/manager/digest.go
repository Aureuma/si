package main

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"silexa/agents/manager/internal/state"
)

func (s *server) startDyadDigest() {
	if s.notifier == nil || s.notifier.url == "" || s.notifier.chatID == nil {
		return
	}
	interval := env("DYAD_TASK_DIGEST_INTERVAL", "10m")
	d, err := time.ParseDuration(interval)
	if err != nil || d <= 0 {
		d = 10 * time.Minute
	}
	go func() {
		time.Sleep(3 * time.Second)
		for {
			s.sendDyadDigestOnce()
			time.Sleep(d)
		}
	}()
}

func (s *server) sendDyadDigestOnce() {
	if s.notifier == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var tasks []state.DyadTask
	if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
		s.logger.Printf("dyad digest query tasks: %v", err)
		return
	}

	open := make([]state.DyadTask, 0, len(tasks))
	for _, t := range tasks {
		if normalizeStatus(t.Status) == "done" {
			continue
		}
		open = append(open, t)
	}

	sort.Slice(open, func(i, j int) bool {
		pi := priorityRank(open[i].Priority)
		pj := priorityRank(open[j].Priority)
		if pi != pj {
			return pi > pj
		}
		si := statusRank(open[i].Status)
		sj := statusRank(open[j].Status)
		if si != sj {
			return si < sj
		}
		if open[i].Dyad != open[j].Dyad {
			return open[i].Dyad < open[j].Dyad
		}
		return open[i].ID < open[j].ID
	})

	var b strings.Builder
	b.WriteString("\U0001F9ED <b>Dyad Task Board</b>\n")
	b.WriteString("<b>Open:</b> " + strconv.Itoa(len(open)) + "\n")
	b.WriteString("<b>When (UTC):</b> " + formatUTCWhen(time.Now().UTC()) + "\n")
	if len(open) == 0 {
		b.WriteString("\n\u2705 <b>All clear</b>")
	} else {
		b.WriteString("\n")
		for i, t := range open {
			if i >= 20 {
				b.WriteString("...\n")
				break
			}
			line := fmt.Sprintf("%s %s <b>#%d</b> %s",
				statusEmoji(t.Status),
				kindEmoji(t.Kind),
				t.ID,
				html.EscapeString(strings.TrimSpace(t.Title)),
			)
			if strings.TrimSpace(t.Dyad) != "" {
				line += " <i>(" + html.EscapeString(strings.TrimSpace(t.Dyad)) + ")</i>"
			}
			if strings.TrimSpace(t.ClaimedBy) != "" {
				line += " - " + html.EscapeString(strings.TrimSpace(t.ClaimedBy))
			}
			b.WriteString(line + "\n")
		}
	}

	var prevID int
	if err := s.query(ctx, "dyad-digest-message-id", &prevID); err != nil {
		s.logger.Printf("dyad digest query message id: %v", err)
	}

	newID, _ := s.notifier.upsertMessageHTML(strings.TrimSpace(b.String()), prevID)
	if newID > 0 && newID != prevID {
		var updated int
		if err := s.update(ctx, "set_dyad_digest_message_id", &updated, newID); err != nil {
			s.logger.Printf("dyad digest update message id: %v", err)
			return
		}
		s.logger.Printf("dyad digest anchored message_id=%d", newID)
	}
}
