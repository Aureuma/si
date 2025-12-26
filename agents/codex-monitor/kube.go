package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
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

func (k *kubeClient) deploymentReady(ctx context.Context, name string) (bool, bool, error) {
	if k == nil || k.client == nil {
		return false, false, fmt.Errorf("kube client not initialized")
	}
	deploy, err := k.client.AppsV1().Deployments(k.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, false, nil
		}
		return false, false, err
	}
	ready := deploy.Status.ReadyReplicas >= 1
	return true, ready, nil
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

func (k *kubeClient) deletePod(ctx context.Context, podName string) error {
	if k == nil || k.client == nil {
		return fmt.Errorf("kube client not initialized")
	}
	if podName == "" {
		return fmt.Errorf("pod name required")
	}
	return k.client.CoreV1().Pods(k.namespace).Delete(ctx, podName, metav1.DeleteOptions{})
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

func (k *kubeClient) applyDyad(ctx context.Context, dyadDeployment *appsv1.Deployment, pvc *corev1.PersistentVolumeClaim) error {
	if k == nil || k.client == nil {
		return fmt.Errorf("kube client not initialized")
	}
	if pvc != nil {
		if _, err := k.client.CoreV1().PersistentVolumeClaims(k.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				return err
			}
		}
	}
	if dyadDeployment == nil {
		return nil
	}
	if _, err := k.client.AppsV1().Deployments(k.namespace).Create(ctx, dyadDeployment, metav1.CreateOptions{}); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}
	return nil
}

func execOutput(ctx context.Context, kube *kubeClient, podName, container string, cmd []string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := kube.exec(ctx, podName, container, cmd, nil, &stdout, &stderr, false); err != nil {
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
