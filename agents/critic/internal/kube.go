package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	podName   string
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
	podName := strings.TrimSpace(os.Getenv("POD_NAME"))
	return &kubeClient{client: clientset, config: cfg, namespace: ns, podName: podName}, nil
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

func (k *kubeClient) exec(ctx context.Context, podName, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer, tty bool) error {
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
			TTY:       tty,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k.config, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}

func (k *kubeClient) logs(ctx context.Context, podName, container string, since time.Time, tail int, timestamps bool) (string, error) {
	if k == nil || k.client == nil {
		return "", fmt.Errorf("kube client not initialized")
	}
	if podName == "" {
		return "", fmt.Errorf("pod name required")
	}
	opts := &corev1.PodLogOptions{
		Container:  container,
		Timestamps: timestamps,
	}
	if !since.IsZero() {
		ts := metav1.NewTime(since)
		opts.SinceTime = &ts
	}
	if tail > 0 {
		t := int64(tail)
		opts.TailLines = &t
	}
	req := k.client.CoreV1().Pods(k.namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(stream, 256*1024)); err != nil {
		return "", err
	}
	return buf.String(), nil
}
