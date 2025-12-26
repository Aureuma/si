package main

import (
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func dyadDeploymentName(dyad string) string {
	return "silexa-dyad-" + strings.TrimSpace(dyad)
}

func (m *monitor) buildDyadResources(dyad, role, dept string) (*corev1.PersistentVolumeClaim, *appsv1.Deployment, error) {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return nil, nil, fmt.Errorf("dyad name required")
	}
	if role == "" {
		role = "generic"
	}
	if dept == "" {
		dept = role
	}

	actorEffort, criticEffort := codexEffortForRole(role)

	actorImage := envOr("ACTOR_IMAGE", "silexa/actor:local")
	criticImage := envOr("CRITIC_IMAGE", "silexa/critic:local")
	codexModel := envOr("CODEX_MODEL", "gpt-5.1-codex-max")
	telegramURL := envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify")
	telegramChatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	managerURL := envOr("MANAGER_URL", "http://silexa-manager:9090")
	repoURL := strings.TrimSpace(os.Getenv("SILEXA_REPO_URL"))
	repoRef := envOr("SILEXA_REPO_REF", "main")

	labels := map[string]string{
		"app":               "silexa-dyad",
		"silexa.dyad":       dyad,
		"silexa.role":       role,
		"silexa.department": dept,
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "codex-" + dyad,
			Labels: labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resourceQty("2Gi"),
				},
			},
		},
	}

	configItems := []corev1.KeyToPath{
		{Key: "codex-mcp-config.toml", Path: "codex-mcp-config.toml"},
		{Key: "codex_accounts.json", Path: "codex_accounts.json"},
		{Key: "router_rules.json", Path: "router_rules.json"},
		{Key: "dyad_roster.json", Path: "dyad_roster.json"},
		{Key: "ssh_target", Path: "ssh_target"},
		{Key: "programs-web_hosting.json", Path: "programs/web_hosting.json"},
		{Key: "programs-releaseparty.json", Path: "programs/releaseparty/releaseparty.json"},
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dyadDeploymentName(dyad),
			Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":         "silexa-dyad",
					"silexa.dyad": dyad,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "silexa-dyad",
					Volumes: []corev1.Volume{
						{
							Name: "codex",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "codex-" + dyad,
								},
							},
						},
						{
							Name: "workspace",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "configs",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "silexa-configs"},
									Items:                configItems,
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "repo-sync",
							Image: "alpine/git:2.45.2",
							Env: []corev1.EnvVar{
								{Name: "SILEXA_REPO_URL", Value: repoURL},
								{Name: "SILEXA_REPO_REF", Value: repoRef},
							},
							Command: []string{"sh", "-lc"},
							Args: []string{`if [ -z "${SILEXA_REPO_URL}" ]; then echo "SILEXA_REPO_URL not set; skipping repo sync"; exit 0; fi
if [ ! -d /workspace/silexa/.git ]; then
  git clone --branch "${SILEXA_REPO_REF}" "${SILEXA_REPO_URL}" /workspace/silexa
else
  cd /workspace/silexa
  git fetch origin "${SILEXA_REPO_REF}" || true
  git checkout "${SILEXA_REPO_REF}" || true
  git pull --ff-only origin "${SILEXA_REPO_REF}" || true
fi`},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: "/workspace"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:       "actor",
							Image:      actorImage,
							WorkingDir: "/workspace/silexa/apps",
							Env: []corev1.EnvVar{
								{Name: "ROLE", Value: role},
								{Name: "DEPARTMENT", Value: dept},
								{Name: "DYAD_NAME", Value: dyad},
								{Name: "DYAD_MEMBER", Value: "actor"},
								{Name: "CODEX_INIT_FORCE", Value: "1"},
								{Name: "CODEX_MODEL", Value: codexModel},
								{Name: "CODEX_REASONING_EFFORT", Value: actorEffort},
							},
							Command: []string{"bash", "-lc", "npm i -g @openai/codex >/dev/null 2>&1 || true; /workspace/silexa/bin/codex-init.sh >/proc/1/fd/1 2>/proc/1/fd/2 || true; exec tail -f /dev/null"},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "codex", MountPath: "/root/.codex"},
								{Name: "workspace", MountPath: "/workspace"},
							},
						},
						{
							Name:  "critic",
							Image: criticImage,
							Env: []corev1.EnvVar{
								{Name: "MANAGER_URL", Value: managerURL},
								{Name: "TELEGRAM_NOTIFY_URL", Value: telegramURL},
								{Name: "TELEGRAM_CHAT_ID", Value: telegramChatID},
								{Name: "DEPARTMENT", Value: dept},
								{Name: "ROLE", Value: role},
								{Name: "DYAD_NAME", Value: dyad},
								{Name: "DYAD_MEMBER", Value: "critic"},
								{Name: "ACTOR_CONTAINER", Value: "actor"},
								{Name: "CODEX_MODEL", Value: codexModel},
								{Name: "CODEX_REASONING_EFFORT", Value: criticEffort},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
									},
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "configs", MountPath: "/configs"},
								{Name: "codex", MountPath: "/root/.codex"},
								{Name: "workspace", MountPath: "/workspace"},
							},
						},
					},
				},
			},
		},
	}

	return pvc, deploy, nil
}

func codexEffortForRole(role string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "infra", "research":
		return "xhigh", "high"
	case "program_manager", "pm":
		return "low", "xhigh"
	case "webdev", "web":
		return "high", "high"
	default:
		return "high", "medium"
	}
}

func int32Ptr(v int32) *int32 {
	return &v
}

func resourceQty(value string) resource.Quantity {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.MustParse("2Gi")
	}
	return q
}
