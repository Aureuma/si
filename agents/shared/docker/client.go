package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

type Client struct {
	api *client.Client
}

func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	if err := pingClient(cli); err == nil {
		return &Client{api: cli}, nil
	} else if os.Getenv("DOCKER_HOST") != "" {
		_ = cli.Close()
		return nil, err
	}
	_ = cli.Close()
	if host, ok := AutoDockerHost(); ok {
		alt, altErr := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
		if altErr != nil {
			return nil, err
		}
		if pingErr := pingClient(alt); pingErr == nil {
			return &Client{api: alt}, nil
		}
		_ = alt.Close()
	}
	return nil, err
}

func pingClient(cli *client.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := cli.Ping(ctx)
	return err
}

func (c *Client) Close() error {
	if c == nil || c.api == nil {
		return nil
	}
	return c.api.Close()
}

func (c *Client) EnsureNetwork(ctx context.Context, name string, labels map[string]string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("network name required")
	}
	args := filters.NewArgs()
	args.Add("name", name)
	list, err := c.api.NetworkList(ctx, types.NetworkListOptions{Filters: args})
	if err != nil {
		return "", err
	}
	for _, item := range list {
		if item.Name == name {
			return item.ID, nil
		}
	}
	resp, err := c.api.NetworkCreate(ctx, name, types.NetworkCreate{
		CheckDuplicate: true,
		Driver:         "bridge",
		Labels:         labels,
	})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) EnsureVolume(ctx context.Context, name string, labels map[string]string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("volume name required")
	}
	list, err := c.api.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", err
	}
	for _, item := range list.Volumes {
		if item.Name == name {
			return item.Name, nil
		}
	}
	resp, err := c.api.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: labels,
	})
	if err != nil {
		return "", err
	}
	return resp.Name, nil
}

func (c *Client) ContainerByName(ctx context.Context, name string) (string, *types.ContainerJSON, error) {
	if strings.TrimSpace(name) == "" {
		return "", nil, errors.New("container name required")
	}
	info, err := c.api.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "", nil, nil
		}
		return "", nil, err
	}
	return info.ID, &info, nil
}

func (c *Client) ContainerByLabels(ctx context.Context, labels map[string]string) (string, *types.ContainerJSON, error) {
	args := filters.NewArgs()
	for key, val := range labels {
		if key == "" || val == "" {
			continue
		}
		args.Add("label", key+"="+val)
	}
	list, err := c.api.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return "", nil, err
	}
	if len(list) == 0 {
		return "", nil, nil
	}
	var selected types.Container
	selected = list[0]
	for _, item := range list {
		if item.State == "running" {
			selected = item
			break
		}
	}
	info, err := c.api.ContainerInspect(ctx, selected.ID)
	if err != nil {
		return "", nil, err
	}
	return info.ID, &info, nil
}

func (c *Client) ListContainers(ctx context.Context, all bool, labels map[string]string) ([]types.Container, error) {
	args := filters.NewArgs()
	for key, val := range labels {
		if key == "" || val == "" {
			continue
		}
		args.Add("label", key+"="+val)
	}
	return c.api.ContainerList(ctx, container.ListOptions{
		All:     all,
		Filters: args,
	})
}

type ExecOptions struct {
	Env        []string
	WorkDir    string
	User       string
	Privileged bool
	TTY        bool
}

func (c *Client) Exec(ctx context.Context, containerID string, cmd []string, opts ExecOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	if strings.TrimSpace(containerID) == "" {
		return errors.New("container id required")
	}
	if len(cmd) == 0 {
		return errors.New("command required")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	execResp, err := c.api.ContainerExecCreate(ctx, containerID, types.ExecConfig{
		AttachStdout: true,
		AttachStderr: !opts.TTY,
		AttachStdin:  stdin != nil,
		Cmd:          cmd,
		Env:          opts.Env,
		WorkingDir:   opts.WorkDir,
		User:         opts.User,
		Privileged:   opts.Privileged,
		Tty:          opts.TTY,
	})
	if err != nil {
		return err
	}

	attach, err := c.api.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{Tty: opts.TTY})
	if err != nil {
		return err
	}
	defer attach.Close()

	errCh := make(chan error, 1)
	go func() {
		if stdin == nil {
			errCh <- nil
			return
		}
		_, err := io.Copy(attach.Conn, stdin)
		if cw, ok := attach.Conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errCh <- err
	}()

	if opts.TTY {
		_, err = io.Copy(stdout, attach.Reader)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, attach.Reader)
	}
	if err != nil {
		return err
	}
	if ioErr := <-errCh; ioErr != nil {
		return ioErr
	}

	inspect, err := c.api.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("exec exit code %d", inspect.ExitCode)
	}
	return nil
}

func (c *Client) ExecWithTTY(ctx context.Context, containerID string, cmd []string, stdin io.Reader, stdout io.Writer, rows, cols uint) error {
	execResp, err := c.api.ContainerExecCreate(ctx, containerID, types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  stdin != nil,
		Cmd:          cmd,
		Tty:          true,
	})
	if err != nil {
		return err
	}
	if rows > 0 && cols > 0 {
		_ = c.api.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{Height: rows, Width: cols})
	}
	attach, err := c.api.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{Tty: true})
	if err != nil {
		return err
	}
	defer attach.Close()

	errCh := make(chan error, 1)
	go func() {
		if stdin == nil {
			errCh <- nil
			return
		}
		_, err := io.Copy(attach.Conn, stdin)
		if cw, ok := attach.Conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errCh <- err
	}()

	if stdout == nil {
		stdout = io.Discard
	}
	if _, err := io.Copy(stdout, attach.Reader); err != nil {
		return err
	}
	if ioErr := <-errCh; ioErr != nil {
		return ioErr
	}
	inspect, err := c.api.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("exec exit code %d", inspect.ExitCode)
	}
	return nil
}

func (c *Client) CopyFileToContainer(ctx context.Context, containerID, destPath string, data []byte, mode int64) error {
	if strings.TrimSpace(containerID) == "" {
		return errors.New("container id required")
	}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return errors.New("destination path required")
	}
	if mode == 0 {
		mode = 0o644
	}
	destDir := path.Dir(destPath)
	name := path.Base(destPath)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:    name,
		Mode:    mode,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	return c.api.CopyToContainer(ctx, containerID, destDir, &buf, types.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	})
}

type LogsOptions struct {
	Since      time.Time
	Tail       int
	Timestamps bool
}

func (c *Client) Logs(ctx context.Context, containerID string, opts LogsOptions) (string, error) {
	if strings.TrimSpace(containerID) == "" {
		return "", errors.New("container id required")
	}
	tail := ""
	if opts.Tail > 0 {
		tail = fmt.Sprintf("%d", opts.Tail)
	}
	since := ""
	if !opts.Since.IsZero() {
		since = opts.Since.UTC().Format(time.RFC3339Nano)
	}
	reader, err := c.api.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Since:      since,
		Timestamps: opts.Timestamps,
	})
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, reader); err != nil {
		_, _ = io.Copy(&buf, reader)
	}
	return buf.String(), nil
}

func (c *Client) RestartContainer(ctx context.Context, containerID string, timeout time.Duration) error {
	if strings.TrimSpace(containerID) == "" {
		return errors.New("container id required")
	}
	if timeout <= 0 {
		return c.api.ContainerRestart(ctx, containerID, container.StopOptions{})
	}
	seconds := int(timeout.Seconds())
	return c.api.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &seconds})
}

func (c *Client) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	if strings.TrimSpace(containerID) == "" {
		return errors.New("container id required")
	}
	return c.api.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: true,
	})
}

func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("volume name required")
	}
	return c.api.VolumeRemove(ctx, name, force)
}

func (c *Client) CreateContainer(ctx context.Context, cfg *container.Config, hostCfg *container.HostConfig, netCfg *network.NetworkingConfig, name string) (string, error) {
	resp, err := c.api.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return errors.New("container id required")
	}
	return c.api.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (c *Client) HostPortFor(ctx context.Context, containerID string, containerPort int, protocol string) (string, error) {
	if strings.TrimSpace(containerID) == "" {
		return "", errors.New("container id required")
	}
	if containerPort <= 0 {
		return "", errors.New("container port required")
	}
	if protocol == "" {
		protocol = "tcp"
	}
	info, err := c.api.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}
	if info.NetworkSettings == nil {
		return "", fmt.Errorf("container %s has no network settings", containerID)
	}
	key := nat.Port(fmt.Sprintf("%d/%s", containerPort, protocol))
	bindings, ok := info.NetworkSettings.Ports[key]
	if !ok || len(bindings) == 0 {
		return "", fmt.Errorf("no host port bound for %s", key)
	}
	for _, binding := range bindings {
		if strings.TrimSpace(binding.HostPort) != "" {
			return binding.HostPort, nil
		}
	}
	return "", fmt.Errorf("no host port bound for %s", key)
}
