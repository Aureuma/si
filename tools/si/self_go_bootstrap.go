package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type goToolchainSpec struct {
	// MinVersion comes from the `go` directive (e.g. "1.25.0").
	MinVersion string
	// ToolchainVersion comes from the `toolchain` directive (e.g. "1.25.7"), if present.
	ToolchainVersion string
}

func parseGoToolchainSpec(goModPath string) (goToolchainSpec, error) {
	f, err := os.Open(filepath.Clean(goModPath))
	if err != nil {
		return goToolchainSpec{}, err
	}
	defer f.Close()

	var spec goToolchainSpec
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "go ") {
			spec.MinVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			continue
		}
		if strings.HasPrefix(line, "toolchain ") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "toolchain "))
			v = strings.TrimPrefix(v, "go")
			spec.ToolchainVersion = strings.TrimSpace(v)
			continue
		}
	}
	if err := sc.Err(); err != nil {
		return goToolchainSpec{}, err
	}
	if strings.TrimSpace(spec.MinVersion) == "" {
		return goToolchainSpec{}, fmt.Errorf("failed to parse Go version from %s", filepath.Clean(goModPath))
	}
	return spec, nil
}

func goCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("HOME not set")
	}
	return filepath.Join(home, ".local", "share", "si", "go"), nil
}

func goToolchainPath(version string) (string, error) {
	base, err := goCacheDir()
	if err != nil {
		return "", err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("go version required")
	}
	// Align with tools/install-si.sh: ~/.local/share/si/go/go<ver>/bin/go
	return filepath.Join(base, "go"+version, "bin", "go"), nil
}

func resolveCachedGo(root string, spec goToolchainSpec) (string, bool) {
	// Prefer toolchain directive when present, otherwise fall back to min version.
	candidates := []string{}
	if strings.TrimSpace(spec.ToolchainVersion) != "" {
		candidates = append(candidates, spec.ToolchainVersion)
	}
	candidates = append(candidates, spec.MinVersion)

	for _, v := range candidates {
		p, err := goToolchainPath(v)
		if err != nil {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

func resolveSiblingGo(output string) (string, bool) {
	dir := filepath.Dir(strings.TrimSpace(output))
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	p := filepath.Join(dir, "go")
	if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return p, true
	}
	return "", false
}

func resolveExecutableSiblingGo() (string, bool) {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "", false
	}
	p := filepath.Join(filepath.Dir(exe), "go")
	if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return p, true
	}
	return "", false
}

func resolveGoForSelfBuild(root string, output string, goBin string) (string, error) {
	goBin = strings.TrimSpace(goBin)
	if goBin == "" {
		goBin = "go"
	}

	// If user provided a path-like value, try it directly.
	if strings.Contains(goBin, "/") || strings.Contains(goBin, string(os.PathSeparator)) {
		if info, err := os.Stat(goBin); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return goBin, nil
		}
		return "", fmt.Errorf("go executable not found: %s", goBin)
	}

	if p, err := exec.LookPath(goBin); err == nil && strings.TrimSpace(p) != "" {
		return p, nil
	}
	if goBin != "go" {
		return "", fmt.Errorf("go executable not found: %s", goBin)
	}

	// Next-best options when "go" is missing from PATH.
	// 0) The toolchain used to build this binary (useful for tests/dev).
	if gr := strings.TrimSpace(runtime.GOROOT()); gr != "" {
		p := filepath.Join(gr, "bin", "go")
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	// 1) A "go" shim next to the target output path (e.g. ~/.local/bin/go).
	if p, ok := resolveSiblingGo(output); ok {
		return p, nil
	}
	// 2) A "go" shim next to the running si executable.
	if p, ok := resolveExecutableSiblingGo(); ok {
		return p, nil
	}

	// 3) A cached toolchain from tools/install-si.sh.
	spec, err := parseGoToolchainSpec(filepath.Join(root, "tools", "si", "go.mod"))
	if err == nil {
		if p, ok := resolveCachedGo(root, spec); ok {
			return p, nil
		}
	}

	// 4) Bootstrap a user-local toolchain (same cache dir as installer).
	if err := ensureGoToolchainCached(root); err != nil {
		return "", fmt.Errorf("go executable not found: go (bootstrap failed: %w)", err)
	}
	if err == nil {
		spec, _ = parseGoToolchainSpec(filepath.Join(root, "tools", "si", "go.mod"))
		if p, ok := resolveCachedGo(root, spec); ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("go executable not found: go")
}

func ensureGoToolchainCached(root string) error {
	goMod := filepath.Join(root, "tools", "si", "go.mod")
	spec, err := parseGoToolchainSpec(goMod)
	if err != nil {
		return err
	}
	want := strings.TrimSpace(spec.ToolchainVersion)
	if want == "" {
		want = strings.TrimSpace(spec.MinVersion)
	}
	if want == "" {
		return fmt.Errorf("go version not found in %s", filepath.Clean(goMod))
	}

	goPath, err := goToolchainPath(want)
	if err != nil {
		return err
	}
	if info, err := os.Stat(goPath); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return nil
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goos != "linux" && goos != "darwin" {
		return fmt.Errorf("unsupported OS for Go bootstrap: %s", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return fmt.Errorf("unsupported arch for Go bootstrap: %s", goarch)
	}

	tgz := fmt.Sprintf("go%s.%s-%s.tar.gz", want, goos, goarch)
	url := "https://dl.google.com/go/" + tgz
	shaURL := url + ".sha256"

	// Download to temp dir.
	infof("downloading Go toolchain %s for si self build...", want)
	base, err := goCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "si-go-bootstrap-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	tgzPath := filepath.Join(work, tgz)
	if err := httpDownloadToFile(url, tgzPath); err != nil {
		return err
	}
	expected, err := httpDownloadText(shaURL)
	if err != nil {
		return err
	}
	expected = strings.TrimSpace(strings.Fields(expected)[0])
	if expected == "" {
		return fmt.Errorf("failed to parse sha256 from %s", shaURL)
	}
	actual, err := fileSHA256Hex(tgzPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("sha256 mismatch for %s (expected %s, got %s)", tgz, expected, actual)
	}

	// Extract "go/" safely then move into place.
	destDir := filepath.Dir(filepath.Dir(goPath)) // .../go<ver>
	_ = os.RemoveAll(destDir)

	extractRoot := filepath.Join(work, "extract")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		return err
	}
	if err := extractGoTarGz(tgzPath, extractRoot); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(extractRoot, "go", "bin", "go")); err != nil {
		return fmt.Errorf("go bootstrap: extracted toolchain missing bin/go")
	}
	if err := os.Rename(filepath.Join(extractRoot, "go"), destDir); err != nil {
		return err
	}
	if _, err := os.Stat(goPath); err != nil {
		return fmt.Errorf("go bootstrap: installed toolchain missing %s", goPath)
	}
	return nil
}

func httpDownloadText(url string) (string, error) {
	body, err := httpGet(url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func httpDownloadToFile(url string, outPath string) error {
	body, err := httpGet(url)
	if err != nil {
		return err
	}
	defer body.Close()

	f, err := os.Create(filepath.Clean(outPath))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, body); err != nil {
		return err
	}
	return f.Close()
}

func httpGet(url string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "si-go-bootstrap")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download failed (%d) for %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractGoTarGz(tgzPath string, dest string) error {
	f, err := os.Open(filepath.Clean(tgzPath))
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		name := strings.TrimSpace(hdr.Name)
		if name == "" {
			continue
		}
		// Expect everything under "go/" in the archive.
		if !strings.HasPrefix(name, "go/") && name != "go" {
			continue
		}

		clean := filepath.Clean(name)
		if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
			return fmt.Errorf("go bootstrap: unsafe tar path: %q", name)
		}
		// Force relative join below dest.
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
		outPath := filepath.Join(dest, clean)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			// Default safe file perms; we only need executable bits to persist for bin/.
			if mode == 0 {
				mode = 0o600
			}
			tmp := outPath + ".tmp"
			w, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, tr); err != nil {
				_ = w.Close()
				_ = os.Remove(tmp)
				return err
			}
			if err := w.Close(); err != nil {
				_ = os.Remove(tmp)
				return err
			}
			if err := os.Rename(tmp, outPath); err != nil {
				_ = os.Remove(tmp)
				return err
			}
		default:
			// Ignore symlinks and other types.
		}
	}
}
