package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquirePaasAgentLockRecoversStaleLock(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	path, err := resolvePaasAgentLockPath(currentPaasContext(), "ops-agent")
	if err != nil {
		t.Fatalf("resolve lock path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	old := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	if err := savePaasAgentLockState(path, paasAgentLockState{
		Agent:       "ops-agent",
		Owner:       "old-owner",
		PID:         123,
		AcquiredAt:  old.Format(time.RFC3339Nano),
		HeartbeatAt: old.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}
	result, err := acquirePaasAgentLock("ops-agent", "new-owner", old.Add(2*paasAgentLockTTL))
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if !result.Acquired || !result.Recovered {
		t.Fatalf("expected recovered lock acquisition, got %#v", result)
	}
}

func TestAcquirePaasAgentLockBlocksWhenActive(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	now := time.Date(2026, 2, 17, 11, 0, 0, 0, time.UTC)
	result, err := acquirePaasAgentLock("ops-agent", "owner-a", now)
	if err != nil {
		t.Fatalf("initial acquire: %v", err)
	}
	if !result.Acquired {
		t.Fatalf("expected initial lock acquisition, got %#v", result)
	}
	second, err := acquirePaasAgentLock("ops-agent", "owner-b", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if second.Acquired {
		t.Fatalf("expected second acquire to be blocked, got %#v", second)
	}
}
