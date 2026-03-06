package docker

import (
	"strconv"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

func TestResolveHostIdentityFromEnv(t *testing.T) {
	t.Setenv(HostUIDEnvKey, "501")
	t.Setenv(HostGIDEnvKey, "20")

	identity, ok := ResolveHostIdentity()
	if !ok {
		t.Fatalf("expected host identity to resolve")
	}
	if identity.UID != 501 || identity.GID != 20 {
		t.Fatalf("unexpected host identity: %+v", identity)
	}
}

func TestResolveHostIdentityRejectsInvalidEnv(t *testing.T) {
	t.Setenv(HostUIDEnvKey, "not-a-number")
	t.Setenv(HostGIDEnvKey, "-1")

	identity, ok := ResolveHostIdentity()
	if !ok {
		t.Fatalf("expected fallback host identity to resolve")
	}
	if identity.UID <= 0 || identity.GID <= 0 {
		t.Fatalf("expected positive fallback uid/gid, got %+v", identity)
	}
}

func TestContainerMatchesHostIdentity(t *testing.T) {
	identity := HostIdentity{UID: 1000, GID: 1000}
	info := &types.ContainerJSON{
		Config: &container.Config{
			Env: []string{
				HostUIDEnvKey + "=1000",
				HostGIDEnvKey + "=1000",
			},
			Labels: map[string]string{
				HostUIDLabelKey: "1000",
				HostGIDLabelKey: "1000",
			},
		},
	}
	if !ContainerMatchesHostIdentity(info, identity) {
		t.Fatalf("expected identity match")
	}

	info.Config.Env[0] = HostUIDEnvKey + "=2000"
	if ContainerMatchesHostIdentity(info, identity) {
		t.Fatalf("expected mismatch when uid env diverges")
	}
	info.Config.Env[0] = HostUIDEnvKey + "=1000"

	info.Config.Labels[HostGIDLabelKey] = strconv.Itoa(2000)
	if ContainerMatchesHostIdentity(info, identity) {
		t.Fatalf("expected mismatch when gid label diverges")
	}
}
