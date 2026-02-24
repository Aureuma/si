package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

const defaultSunGoogleLoginURL = "https://aureuma.ai/sun/auth/cli/start"

var sunBrowserAuthOpenURLFn = func(url string) {
	if cmdTemplate := envSunLoginOpenCmd(); cmdTemplate != "" {
		cmdLine := expandLoginOpenCommand(cmdTemplate, url, codexProfile{})
		if !strings.Contains(cmdTemplate, "{url}") {
			cmdLine = strings.TrimSpace(cmdLine + " " + shellSingleQuote(url))
		}
		if err := runShellCommand(cmdLine); err != nil {
			warnf("open login url failed: %v", err)
		}
		return
	}
	openLoginURL(url, codexProfile{}, "", "")
}

type sunBrowserAuthResult struct {
	Token    string
	BaseURL  string
	Account  string
	AutoSync bool
}

func runSunBrowserAuthFlow(loginURL string, timeout time.Duration, openBrowser bool) (sunBrowserAuthResult, error) {
	loginURL = strings.TrimSpace(loginURL)
	if loginURL == "" {
		loginURL = defaultSunGoogleLoginURL
	}
	startURL, err := neturl.Parse(loginURL)
	if err != nil {
		return sunBrowserAuthResult{}, fmt.Errorf("invalid login url: %w", err)
	}
	if startURL.Scheme != "https" && startURL.Scheme != "http" {
		return sunBrowserAuthResult{}, fmt.Errorf("login url must be http(s)")
	}

	state, err := randomHex(24)
	if err != nil {
		return sunBrowserAuthResult{}, fmt.Errorf("generate state: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return sunBrowserAuthResult{}, fmt.Errorf("start callback listener: %w", err)
	}
	defer listener.Close()

	host, portRaw, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return sunBrowserAuthResult{}, fmt.Errorf("parse callback listener address: %w", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(portRaw))
	if err != nil || port <= 0 {
		return sunBrowserAuthResult{}, fmt.Errorf("invalid callback listener port: %q", portRaw)
	}
	callbackURL := (&neturl.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/callback",
	}).String()

	authURL := *startURL
	query := authURL.Query()
	query.Set("cb", callbackURL)
	query.Set("state", state)
	authURL.RawQuery = query.Encode()

	resultCh := make(chan sunBrowserAuthResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		gotState := strings.TrimSpace(r.URL.Query().Get("state"))
		if gotState != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("browser auth state mismatch"):
			default:
			}
			return
		}
		if flowErr := strings.TrimSpace(r.URL.Query().Get("error")); flowErr != "" {
			escaped := html.EscapeString(flowErr)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><html><body><h1>Sign-in failed</h1><p>" + escaped + "</p></body></html>"))
			select {
			case errCh <- fmt.Errorf("browser auth failed: %s", flowErr):
			default:
			}
			return
		}
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		baseURL := strings.TrimSpace(r.URL.Query().Get("url"))
		account := strings.TrimSpace(r.URL.Query().Get("account"))
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("browser auth callback missing token"):
			default:
			}
			return
		}
		autoSync := isTruthyFlagValue(r.URL.Query().Get("auto_sync"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body><h1>Sign-in complete</h1><p>You can return to the terminal.</p></body></html>"))
		select {
		case resultCh <- sunBrowserAuthResult{
			Token:    token,
			BaseURL:  baseURL,
			Account:  account,
			AutoSync: autoSync,
		}:
		default:
		}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			select {
			case errCh <- serveErr:
			default:
			}
		}
	}()

	if openBrowser {
		sunBrowserAuthOpenURLFn(authURL.String())
	}
	infof("complete Google sign-in in your browser:")
	fmt.Println(authURL.String())

	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var result sunBrowserAuthResult
	select {
	case result = <-resultCh:
	case authErr := <-errCh:
		_ = server.Close()
		<-done
		return sunBrowserAuthResult{}, authErr
	case <-timer.C:
		_ = server.Close()
		<-done
		return sunBrowserAuthResult{}, fmt.Errorf("timed out waiting for browser callback")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	<-done
	return result, nil
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("size must be > 0")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
