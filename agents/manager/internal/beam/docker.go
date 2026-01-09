package beam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	shared "silexa/agents/shared/docker"
)

type dockerClient struct {
	client *shared.Client
}

func newDockerClient() (*dockerClient, error) {
	cli, err := shared.NewClient()
	if err != nil {
		return nil, err
	}
	return &dockerClient{client: cli}, nil
}

func (d *dockerClient) resolveDyadContainer(ctx context.Context, dyad, member string) (string, error) {
	member = normalizeContainerName(member)
	if member == "" {
		return "", errors.New("container name required")
	}
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return "", errors.New("dyad required")
	}
	name := shared.DyadContainerName(dyad, member)
	if name == "" {
		return "", errors.New("container name required")
	}
	id, _, err := d.client.ContainerByName(ctx, name)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("container %s not found", name)
	}
	return id, nil
}

func (d *dockerClient) exec(ctx context.Context, containerID string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return d.client.Exec(ctx, containerID, cmd, shared.ExecOptions{}, stdin, stdout, stderr)
}

func (d *dockerClient) execCapture(ctx context.Context, containerID string, cmd []string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := d.exec(ctx, containerID, cmd, nil, &stdout, &stderr); err != nil {
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

func (d *dockerClient) hostPortFor(ctx context.Context, containerID string, port int) (string, error) {
	return d.client.HostPortFor(ctx, containerID, port, "tcp")
}
