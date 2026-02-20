package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	paasSSHBinEnvKey            = "SI_PAAS_SSH_BIN"
	paasSCPBinEnvKey            = "SI_PAAS_SCP_BIN"
	paasSSHEngineEnvKey         = "SI_PAAS_SSH_ENGINE"
	paasSSHPasswordEnvKey       = "SI_PAAS_SSH_PASSWORD"
	paasSSHPrivateKeyEnvKey     = "SI_PAAS_SSH_PRIVATE_KEY"
	paasKnownHostsFileEnvKey    = "SI_PAAS_KNOWN_HOSTS_FILE"
	paasSSHPasswordTargetPrefix = "SI_PAAS_SSH_PASSWORD_"
)

const (
	paasSSHEngineAuto = "auto"
	paasSSHEngineGo   = "go"
	paasSSHEngineExec = "exec"
)

var paasKnownHostsWriteMu sync.Mutex

func resolvePaasSSHTransportEngine() string {
	engine := strings.ToLower(strings.TrimSpace(os.Getenv(paasSSHEngineEnvKey)))
	switch engine {
	case "", paasSSHEngineAuto:
		if strings.TrimSpace(os.Getenv(paasSSHBinEnvKey)) != "" || strings.TrimSpace(os.Getenv(paasSCPBinEnvKey)) != "" {
			return paasSSHEngineExec
		}
		return paasSSHEngineGo
	case paasSSHEngineGo:
		return paasSSHEngineGo
	case paasSSHEngineExec:
		return paasSSHEngineExec
	default:
		return paasSSHEngineGo
	}
}

func runPaasSSHCommand(ctx context.Context, target paasTarget, remoteCmd string) (string, error) {
	if isPaasLocalTarget(target) {
		return runPaasSSHCommandLocal(ctx, remoteCmd)
	}
	switch resolvePaasSSHTransportEngine() {
	case paasSSHEngineExec:
		return runPaasSSHCommandExec(ctx, target, remoteCmd)
	default:
		return runPaasSSHCommandGo(ctx, target, remoteCmd)
	}
}

func runPaasSSHCommandLocal(ctx context.Context, remoteCmd string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-lc", remoteCmd)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runPaasSSHCommandExec(ctx context.Context, target paasTarget, remoteCmd string) (string, error) {
	bin := strings.TrimSpace(os.Getenv(paasSSHBinEnvKey))
	if bin == "" {
		bin = "ssh"
	}
	args := []string{
		"-p", strconv.Itoa(resolvePaasPort(target.Port)),
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", target.User, target.Host),
		remoteCmd,
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runPaasSSHCommandGo(ctx context.Context, target paasTarget, remoteCmd string) (string, error) {
	return runPaasSSHCommandGoWithAuth(ctx, target, remoteCmd, nil)
}

func runPaasSSHCommandGoWithAuth(ctx context.Context, target paasTarget, remoteCmd string, explicitAuth []ssh.AuthMethod) (string, error) {
	client, err := dialPaasSSHClient(ctx, target, explicitAuth)
	if err != nil {
		return "", err
	}
	defer client.Close()
	stopCancelWatch := closePaasSSHClientOnContextCancel(ctx, client)
	defer stopCancelWatch()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput(remoteCmd)
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}

func runPaasSCPUpload(ctx context.Context, target paasTarget, srcPath, remoteDir string) error {
	absSrc, err := filepath.Abs(strings.TrimSpace(srcPath))
	if err != nil {
		return err
	}
	if isPaasLocalTarget(target) {
		return runPaasLocalFileCopy(absSrc, remoteDir)
	}
	switch resolvePaasSSHTransportEngine() {
	case paasSSHEngineExec:
		return runPaasSCPUploadExec(ctx, target, absSrc, remoteDir)
	default:
		return runPaasSCPUploadGo(ctx, target, absSrc, remoteDir)
	}
}

func runPaasLocalFileCopy(absSrc, remoteDir string) error {
	resolvedRemoteDir := strings.TrimSpace(remoteDir)
	if resolvedRemoteDir == "" {
		return fmt.Errorf("remote directory is required")
	}
	if err := os.MkdirAll(resolvedRemoteDir, 0o755); err != nil {
		return err
	}
	destPath := filepath.Join(resolvedRemoteDir, filepath.Base(absSrc))
	srcFile, err := os.Open(absSrc)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	dstFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}
	defer dstFile.Close()
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
}

func runPaasSCPUploadExec(ctx context.Context, target paasTarget, absSrc, remoteDir string) error {
	scpBin := strings.TrimSpace(os.Getenv(paasSCPBinEnvKey))
	if scpBin == "" {
		scpBin = "scp"
	}
	dest := fmt.Sprintf("%s@%s:%s/", target.User, target.Host, strings.TrimSpace(remoteDir))
	args := []string{
		"-P", strconv.Itoa(resolvePaasPort(target.Port)),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5",
		absSrc,
		dest,
	}
	cmd := exec.CommandContext(ctx, scpBin, args...)
	var stderr bytes.Buffer
	cmd.Stdout = ioDiscard{}
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func runPaasSCPUploadGo(ctx context.Context, target paasTarget, absSrc, remoteDir string) error {
	client, err := dialPaasSSHClient(ctx, target, nil)
	if err != nil {
		return err
	}
	defer client.Close()
	stopCancelWatch := closePaasSSHClientOnContextCancel(ctx, client)
	defer stopCancelWatch()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	session.Stderr = &stderr

	remotePath := strings.TrimSpace(remoteDir)
	if remotePath == "" {
		return fmt.Errorf("remote directory is required")
	}
	if err := session.Start("scp -t " + quoteSingle(remotePath)); err != nil {
		return err
	}

	ackReader := bufio.NewReader(stdout)
	if err := readPaasSCPAck(ackReader); err != nil {
		return formatPaasSCPError(err, stderr.String())
	}

	srcFile, err := os.Open(absSrc)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	mode := srcInfo.Mode().Perm() & 0o777
	header := fmt.Sprintf("C%04o %d %s\n", mode, srcInfo.Size(), filepath.Base(absSrc))
	if _, err := io.WriteString(stdin, header); err != nil {
		return err
	}
	if err := readPaasSCPAck(ackReader); err != nil {
		return formatPaasSCPError(err, stderr.String())
	}
	if _, err := io.Copy(stdin, srcFile); err != nil {
		return err
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}
	if err := readPaasSCPAck(ackReader); err != nil {
		return formatPaasSCPError(err, stderr.String())
	}
	if err := stdin.Close(); err != nil {
		return err
	}
	if err := session.Wait(); err != nil {
		return formatPaasSCPError(err, stderr.String())
	}
	return nil
}

func readPaasSCPAck(reader *bufio.Reader) error {
	code, err := reader.ReadByte()
	if err != nil {
		return err
	}
	switch code {
	case 0:
		return nil
	case 1, 2:
		message, _ := reader.ReadString('\n')
		message = strings.TrimSpace(message)
		if message == "" {
			message = "remote scp returned an error"
		}
		return errors.New(message)
	default:
		return fmt.Errorf("unexpected scp protocol response: %d", code)
	}
}

func formatPaasSCPError(err error, stderrText string) error {
	message := strings.TrimSpace(stderrText)
	if message == "" {
		message = strings.TrimSpace(err.Error())
	}
	if message == "" {
		message = "scp upload failed"
	}
	return fmt.Errorf("%s", message)
}

func closePaasSSHClientOnContextCancel(ctx context.Context, client *ssh.Client) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}

func dialPaasSSHClient(ctx context.Context, target paasTarget, explicitAuth []ssh.AuthMethod) (*ssh.Client, error) {
	config, err := buildPaasSSHClientConfig(target, explicitAuth)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(strings.TrimSpace(target.Host), strconv.Itoa(resolvePaasPort(target.Port)))
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func buildPaasSSHClientConfig(target paasTarget, explicitAuth []ssh.AuthMethod) (*ssh.ClientConfig, error) {
	user := strings.TrimSpace(target.User)
	if user == "" {
		return nil, fmt.Errorf("target user is required")
	}
	methods := make([]ssh.AuthMethod, 0)
	if len(explicitAuth) > 0 {
		methods = append(methods, explicitAuth...)
	} else {
		resolved, err := resolvePaasSSHAuthMethods(target)
		if err != nil {
			return nil, err
		}
		methods = append(methods, resolved...)
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no ssh auth methods available")
	}
	hostKeyCallback, err := buildPaasHostKeyCallback()
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         5 * time.Second,
	}, nil
}

func resolvePaasSSHAuthMethods(target paasTarget) ([]ssh.AuthMethod, error) {
	resolvedAuthMethod := normalizePaasAuthMethod(target.AuthMethod)
	password := resolvePaasSSHPassword(target)

	methods := make([]ssh.AuthMethod, 0)
	if resolvedAuthMethod == paasAuthMethodPassword {
		passwordMethods, err := buildPaasPasswordAuthMethods(password)
		if err != nil {
			return nil, err
		}
		methods = append(methods, passwordMethods...)
		return methods, nil
	}

	keyMethods, err := buildPaasKeyAuthMethods()
	if err != nil {
		return nil, err
	}
	methods = append(methods, keyMethods...)
	if len(methods) > 0 {
		return methods, nil
	}

	if password != "" {
		passwordMethods, err := buildPaasPasswordAuthMethods(password)
		if err != nil {
			return nil, err
		}
		methods = append(methods, passwordMethods...)
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no key signers found; set %s or configure a local private key", paasSSHPasswordEnvKey)
	}
	return methods, nil
}

func buildPaasKeyAuthMethods() ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0)
	localSigners, err := loadPaasLocalPrivateKeySigners()
	if err != nil {
		return nil, err
	}
	if len(localSigners) > 0 {
		methods = append(methods, ssh.PublicKeys(localSigners...))
	}
	if strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")) != "" {
		methods = append(methods, ssh.PublicKeysCallback(loadPaasAgentSigners))
	}
	return methods, nil
}

func buildPaasPasswordAuthMethods(password string) ([]ssh.AuthMethod, error) {
	resolved := strings.TrimSpace(password)
	if resolved == "" {
		return nil, fmt.Errorf("password auth requires %s or %s<TARGET_NAME>", paasSSHPasswordEnvKey, paasSSHPasswordTargetPrefix)
	}
	keyboardInteractive := ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range answers {
			answers[i] = resolved
		}
		return answers, nil
	})
	return []ssh.AuthMethod{ssh.Password(resolved), keyboardInteractive}, nil
}

func loadPaasLocalPrivateKeySigners() ([]ssh.Signer, error) {
	paths, err := resolvePaasPrivateKeyPaths()
	if err != nil {
		return nil, err
	}
	signers := make([]ssh.Signer, 0, len(paths))
	for _, keyPath := range paths {
		raw, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers, nil
}

func loadPaasAgentSigners() ([]ssh.Signer, error) {
	sock := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
	if sock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK is not set")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	client := agent.NewClient(conn)
	return client.Signers()
}

func resolvePaasPrivateKeyPaths() ([]string, error) {
	paths := make([]string, 0)
	seen := map[string]bool{}
	appendPath := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if seen[trimmed] {
			return
		}
		seen[trimmed] = true
		paths = append(paths, trimmed)
	}
	for _, item := range parseCSV(os.Getenv(paasSSHPrivateKeyEnvKey)) {
		appendPath(item)
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		appendPath(filepath.Join(home, ".ssh", "id_ed25519"))
		appendPath(filepath.Join(home, ".ssh", "id_ecdsa"))
		appendPath(filepath.Join(home, ".ssh", "id_rsa"))
	}
	return paths, nil
}

func buildPaasHostKeyCallback() (ssh.HostKeyCallback, error) {
	knownHostsPath, err := resolvePaasKnownHostsPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(knownHostsPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.WriteFile(knownHostsPath, []byte{}, 0o600); err != nil {
			return nil, err
		}
	}
	validator, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := validator(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			if appendErr := appendPaasKnownHost(knownHostsPath, hostname, key); appendErr != nil {
				return appendErr
			}
			return nil
		}
		return err
	}, nil
}

func appendPaasKnownHost(pathValue, hostname string, key ssh.PublicKey) error {
	normalized := knownhosts.Normalize(strings.TrimSpace(hostname))
	if normalized == "" {
		return fmt.Errorf("cannot normalize ssh hostname")
	}
	line := knownhosts.Line([]string{normalized}, key)
	paasKnownHostsWriteMu.Lock()
	defer paasKnownHostsWriteMu.Unlock()

	existingRaw, err := os.ReadFile(pathValue)
	if err == nil {
		for _, row := range strings.Split(string(existingRaw), "\n") {
			if strings.TrimSpace(row) == strings.TrimSpace(line) {
				return nil
			}
		}
	}
	file, err := os.OpenFile(pathValue, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}

func resolvePaasKnownHostsPath() (string, error) {
	if assigned := strings.TrimSpace(os.Getenv(paasKnownHostsFileEnvKey)); assigned != "" {
		return filepath.Clean(assigned), nil
	}
	stateRoot, err := resolvePaasStateRoot()
	if err == nil && strings.TrimSpace(stateRoot) != "" {
		return filepath.Join(stateRoot, "known_hosts"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".si", "paas", "known_hosts"), nil
}

func resolvePaasSSHPassword(target paasTarget) string {
	targetSuffix := normalizePaasTargetPasswordEnvSuffix(target.Name)
	if targetSuffix != "" {
		value := strings.TrimSpace(os.Getenv(paasSSHPasswordTargetPrefix + targetSuffix))
		if value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv(paasSSHPasswordEnvKey))
}

func normalizePaasTargetPasswordEnvSuffix(name string) string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return ""
	}
	builder := strings.Builder{}
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('_')
	}
	return strings.ToUpper(strings.Trim(builder.String(), "_"))
}

func resolvePaasPort(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}
