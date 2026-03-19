package docker

import (
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
)

const (
	HostUIDEnvKey = "SI_HOST_UID"
	HostGIDEnvKey = "SI_HOST_GID"

	HostUIDLabelKey = "si.host_uid"
	HostGIDLabelKey = "si.host_gid"
)

// HostIdentity models the host user/group identity that should own files
// written through bind mounts from containers.
type HostIdentity struct {
	UID int
	GID int
}

func (h HostIdentity) Valid() bool {
	return h.UID > 0 && h.GID > 0
}

func (h HostIdentity) EnvVars() []string {
	if !h.Valid() {
		return nil
	}
	return []string{
		HostUIDEnvKey + "=" + strconv.Itoa(h.UID),
		HostGIDEnvKey + "=" + strconv.Itoa(h.GID),
	}
}

func (h HostIdentity) Labels() map[string]string {
	if !h.Valid() {
		return nil
	}
	return map[string]string{
		HostUIDLabelKey: strconv.Itoa(h.UID),
		HostGIDLabelKey: strconv.Itoa(h.GID),
	}
}

// ResolveHostIdentity returns the active host identity policy. It defaults to
// the current process uid/gid, and allows explicit env override for
// root-context launches.
func ResolveHostIdentity() (HostIdentity, bool) {
	uid := os.Getuid()
	gid := os.Getgid()

	if parsed, ok := parsePositiveInt(strings.TrimSpace(os.Getenv(HostUIDEnvKey))); ok {
		uid = parsed
	}
	if parsed, ok := parsePositiveInt(strings.TrimSpace(os.Getenv(HostGIDEnvKey))); ok {
		gid = parsed
	}

	identity := HostIdentity{UID: uid, GID: gid}
	if !identity.Valid() {
		return HostIdentity{}, false
	}
	return identity, true
}

// ContainerMatchesHostIdentity reports whether the container carries the
// current host ownership policy through both env and labels.
func ContainerMatchesHostIdentity(info *types.ContainerJSON, identity HostIdentity) bool {
	if !identity.Valid() {
		return true
	}
	if info == nil || info.Config == nil {
		return false
	}
	if containerEnvValue(info.Config.Env, HostUIDEnvKey) != strconv.Itoa(identity.UID) {
		return false
	}
	if containerEnvValue(info.Config.Env, HostGIDEnvKey) != strconv.Itoa(identity.GID) {
		return false
	}
	if info.Config.Labels == nil {
		return false
	}
	if strings.TrimSpace(info.Config.Labels[HostUIDLabelKey]) != strconv.Itoa(identity.UID) {
		return false
	}
	if strings.TrimSpace(info.Config.Labels[HostGIDLabelKey]) != strconv.Itoa(identity.GID) {
		return false
	}
	return true
}

func parsePositiveInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func containerEnvValue(env []string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	for _, item := range env {
		item = strings.TrimSpace(item)
		if !strings.HasPrefix(item, key+"=") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(item, key+"="))
	}
	return ""
}
