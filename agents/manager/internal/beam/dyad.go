package beam

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func dyadDeploymentName(dyad string) string {
	return "silexa-dyad-" + strings.TrimSpace(dyad)
}

func (a *Activities) ApplyDyadResources(ctx context.Context, req DyadBootstrapRequest) (DyadBootstrapResult, error) {
	if a.kube == nil || a.kube.client == nil {
		return DyadBootstrapResult{}, fmt.Errorf("kube client unavailable")
	}
	pvc, deploy, err := buildDyadResources(req.Dyad, req.Role, req.Department)
	if err != nil {
		return DyadBootstrapResult{}, err
	}
	ns := a.kube.namespace

	pvcClient := a.kube.client.CoreV1().PersistentVolumeClaims(ns)
	currentPVC, err := pvcClient.Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			if _, err := pvcClient.Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
				return DyadBootstrapResult{}, err
			}
		} else {
			return DyadBootstrapResult{}, err
		}
	} else {
		// PVC specs are largely immutable after creation; keep the existing claim as-is.
		_ = currentPVC
	}

	deployClient := a.kube.client.AppsV1().Deployments(ns)
	currentDeploy, err := deployClient.Get(ctx, deploy.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			if _, err := deployClient.Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
				return DyadBootstrapResult{}, err
			}
		} else {
			return DyadBootstrapResult{}, err
		}
	} else {
		deploy.ResourceVersion = currentDeploy.ResourceVersion
		if _, err := deployClient.Update(ctx, deploy, metav1.UpdateOptions{}); err != nil {
			return DyadBootstrapResult{}, err
		}
	}

	return DyadBootstrapResult{Deployment: deploy.Name}, nil
}

func (a *Activities) WaitDyadReady(ctx context.Context, req DyadBootstrapRequest) error {
	if a.kube == nil || a.kube.client == nil {
		return fmt.Errorf("kube client unavailable")
	}
	if strings.TrimSpace(req.Dyad) == "" {
		return fmt.Errorf("dyad required")
	}
	name := dyadDeploymentName(req.Dyad)
	timeout := 6 * time.Minute
	deadline := time.Now().Add(timeout)
	for {
		deploy, err := a.kube.client.AppsV1().Deployments(a.kube.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return fmt.Errorf("deployment %s not found", name)
			}
			return err
		}
		want := int32(1)
		if deploy.Spec.Replicas != nil {
			want = *deploy.Spec.Replicas
		}
		if deploy.Status.ReadyReplicas >= want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("deployment %s not ready after %s", name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func buildDyadResources(dyad, role, dept string) (*corev1.PersistentVolumeClaim, *appsv1.Deployment, error) {
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
	codexModel := envOr("CODEX_MODEL", "gpt-5.2-codex")
	codexModelLow := strings.TrimSpace(os.Getenv("CODEX_MODEL_LOW"))
	codexModelMedium := strings.TrimSpace(os.Getenv("CODEX_MODEL_MEDIUM"))
	codexModelHigh := strings.TrimSpace(os.Getenv("CODEX_MODEL_HIGH"))
	codexEffortLow := strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT_LOW"))
	codexEffortMedium := strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT_MEDIUM"))
	codexEffortHigh := strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT_HIGH"))
	telegramURL := envOr("TELEGRAM_NOTIFY_URL", "http://silexa-telegram-bot:8081/notify")
	telegramChatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	managerURL := envOr("MANAGER_SERVICE_URL", envOr("MANAGER_URL", "http://silexa-manager:9090"))
	repoURL := strings.TrimSpace(os.Getenv("SILEXA_REPO_URL"))
	repoRef := envOr("SILEXA_REPO_REF", "main")
	approverEnv := credentialsApproverEnv(dyad)

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
					ServiceAccountName: serviceAccountForDyad(dyad),
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
							Args: []string{`mkdir -p /workspace/silexa/apps
if [ -z "${SILEXA_REPO_URL}" ]; then echo "SILEXA_REPO_URL not set; skipping repo sync"; exit 0; fi
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
							Env: appendCodexTuningEnv(append([]corev1.EnvVar{
								{Name: "ROLE", Value: role},
								{Name: "DEPARTMENT", Value: dept},
								{Name: "DYAD_NAME", Value: dyad},
								{Name: "DYAD_MEMBER", Value: "actor"},
								{Name: "CODEX_INIT_FORCE", Value: "1"},
								{Name: "CODEX_MODEL", Value: codexModel},
								{Name: "CODEX_REASONING_EFFORT", Value: actorEffort},
							}, approverEnv...), codexModelLow, codexModelMedium, codexModelHigh, codexEffortLow, codexEffortMedium, codexEffortHigh),
							Command: []string{"tini", "-s", "--", "bash", "-lc", `npm i -g @openai/codex >/dev/null 2>&1 || true; if [ -x /workspace/silexa/bin/codex-init.sh ]; then /workspace/silexa/bin/codex-init.sh >/proc/1/fd/1 2>/proc/1/fd/2 || true; else echo "codex-init skipped: /workspace/silexa/bin/codex-init.sh not found"; fi; exec tail -f /dev/null`},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "codex", MountPath: "/root/.codex"},
								{Name: "workspace", MountPath: "/workspace"},
							},
						},
						{
							Name:  "critic",
							Image: criticImage,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  int64Ptr(0),
								RunAsGroup: int64Ptr(0),
							},
							Env: appendCodexTuningEnv(append([]corev1.EnvVar{
								{Name: "MANAGER_URL", Value: managerURL},
								{Name: "TELEGRAM_NOTIFY_URL", Value: telegramURL},
								{Name: "TELEGRAM_CHAT_ID", Value: telegramChatID},
								{Name: "DEPARTMENT", Value: dept},
								{Name: "ROLE", Value: role},
								{Name: "DYAD_NAME", Value: dyad},
								{Name: "DYAD_MEMBER", Value: "critic"},
								{Name: "ACTOR_CONTAINER", Value: "actor"},
								{Name: "CODEX_INIT_FORCE", Value: "1"},
								{Name: "HOME", Value: "/root"},
								{Name: "CODEX_HOME", Value: "/root/.codex"},
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
							}, approverEnv...), codexModelLow, codexModelMedium, codexModelHigh, codexEffortLow, codexEffortMedium, codexEffortHigh),
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

func serviceAccountForDyad(dyad string) string {
	name := strings.ToLower(strings.TrimSpace(dyad))
	if name == "silexa-credentials" {
		return "silexa-credentials"
	}
	return "silexa-dyad"
}

func credentialsApproverEnv(dyad string) []corev1.EnvVar {
	name := strings.ToLower(strings.TrimSpace(dyad))
	if name != "silexa-credentials" {
		return nil
	}
	return []corev1.EnvVar{
		{
			Name: "CREDENTIALS_APPROVER_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "silexa-credentials-secrets"},
					Key:                  "credentials_mcp_token",
				},
			},
		},
	}
}

func codexEffortForRole(role string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "infra":
		return "xhigh", "xhigh"
	case "research":
		return "high", "high"
	case "program_manager", "pm":
		return "high", "xhigh"
	case "webdev", "web":
		return "medium", "high"
	default:
		return "medium", "medium"
	}
}

func appendCodexTuningEnv(envs []corev1.EnvVar, modelLow, modelMedium, modelHigh, effortLow, effortMedium, effortHigh string) []corev1.EnvVar {
	envs = appendEnvIfSet(envs, "CODEX_MODEL_LOW", modelLow)
	envs = appendEnvIfSet(envs, "CODEX_MODEL_MEDIUM", modelMedium)
	envs = appendEnvIfSet(envs, "CODEX_MODEL_HIGH", modelHigh)
	envs = appendEnvIfSet(envs, "CODEX_REASONING_EFFORT_LOW", effortLow)
	envs = appendEnvIfSet(envs, "CODEX_REASONING_EFFORT_MEDIUM", effortMedium)
	envs = appendEnvIfSet(envs, "CODEX_REASONING_EFFORT_HIGH", effortHigh)
	return envs
}

func appendEnvIfSet(envs []corev1.EnvVar, key, val string) []corev1.EnvVar {
	if strings.TrimSpace(val) == "" {
		return envs
	}
	return append(envs, corev1.EnvVar{Name: key, Value: val})
}

func int32Ptr(v int32) *int32 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func resourceQty(value string) resource.Quantity {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.MustParse("2Gi")
	}
	return q
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}
