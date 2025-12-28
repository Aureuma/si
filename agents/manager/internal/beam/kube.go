package beam

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type kubeClient struct {
	client    *kubernetes.Clientset
	config    *rest.Config
	namespace string
}

func newKubeClient() (*kubeClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := strings.TrimSpace(os.Getenv("KUBECONFIG"))
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			if home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	ns := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if ns == "" {
		ns = strings.TrimSpace(os.Getenv("SILEXA_NAMESPACE"))
	}
	if ns == "" {
		ns = "silexa"
	}
	return &kubeClient{client: clientset, config: cfg, namespace: ns}, nil
}

func (k *kubeClient) resolveDyadPod(ctx context.Context, dyad string) (string, error) {
	if k == nil || k.client == nil {
		return "", fmt.Errorf("kube client not initialized")
	}
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return "", fmt.Errorf("dyad required")
	}
	list, err := k.client.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("silexa.dyad=%s", dyad),
	})
	if err != nil {
		return "", err
	}
	for _, pod := range list.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		return pod.Name, nil
	}
	if len(list.Items) > 0 {
		return list.Items[0].Name, nil
	}
	return "", fmt.Errorf("no pod found for dyad %s", dyad)
}

func (k *kubeClient) exec(ctx context.Context, podName, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if k == nil || k.client == nil {
		return fmt.Errorf("kube client not initialized")
	}
	if podName == "" {
		return fmt.Errorf("pod name required")
	}
	req := k.client.CoreV1().RESTClient().
		Post().
		Namespace(k.namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    stderr != nil,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k.config, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
}

func (k *kubeClient) execCapture(ctx context.Context, podName, container string, cmd []string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := k.exec(ctx, podName, container, cmd, nil, &stdout, &stderr); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
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

func normalizeContainerName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if name == "actor" || name == "critic" {
		return name
	}
	if strings.HasPrefix(name, "actor-") || strings.HasPrefix(name, "silexa-actor-") {
		return "actor"
	}
	if strings.HasPrefix(name, "critic-") || strings.HasPrefix(name, "silexa-critic-") {
		return "critic"
	}
	return name
}

func kubectlPrefix(namespace string) string {
	args := []string{"kubectl"}
	if ctx := strings.TrimSpace(os.Getenv("KUBECTL_CONTEXT")); ctx != "" {
		args = append(args, "--context", ctx)
	}
	if namespace == "" {
		namespace = strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	}
	if namespace == "" {
		namespace = strings.TrimSpace(os.Getenv("SILEXA_NAMESPACE"))
	}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	return strings.Join(args, " ")
}
