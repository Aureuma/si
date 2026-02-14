package main

import (
	"strings"
	"testing"
)

func TestOfeliaInlineConfigCommand(t *testing.T) {
	cmd := ofeliaInlineConfigCommand("[job-run \"x\"]\nimage = test\n")
	if !strings.Contains(cmd, "cat > /etc/ofelia/config.ini <<'__SI_OFELIA_CONFIG__'") {
		t.Fatalf("expected heredoc config write, got: %q", cmd)
	}
	if !strings.Contains(cmd, "exec ofelia daemon --config /etc/ofelia/config.ini") {
		t.Fatalf("expected ofelia daemon command, got: %q", cmd)
	}
}
