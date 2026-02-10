package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAWSE2E_AuthStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "AWS4-HMAC-SHA256") {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, "Action=GetUser") {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = w.Write([]byte(`<GetUserResponse><GetUserResult><User><UserName>user-cli</UserName></User></GetUserResult><ResponseMetadata><RequestId>req-123</RequestId></ResponseMetadata></GetUserResponse>`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "auth", "status", "--base-url", server.URL, "--region", "us-east-1", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status": "ready"`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestAWSE2E_CreateUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, "Action=CreateUser") {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, "UserName=user-cli") {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, "Path=%2Fsystem%2F") {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = w.Write([]byte(`<CreateUserResponse><CreateUserResult><User><UserName>user-cli</UserName></User></CreateUserResult><ResponseMetadata><RequestId>req-234</RequestId></ResponseMetadata></CreateUserResponse>`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "iam", "user", "create", "--name", "user-cli", "--path", "/system/", "--base-url", server.URL, "--region", "us-east-1", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestAWSE2E_AttachUserPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, "Action=AttachUserPolicy") {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, "UserName=user-cli") {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, "PolicyArn=arn%3Aaws%3Aiam%3A%3Aaws%3Apolicy%2FAdministratorAccess") {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = w.Write([]byte(`<AttachUserPolicyResponse><ResponseMetadata><RequestId>req-345</RequestId></ResponseMetadata></AttachUserPolicyResponse>`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "iam", "user", "attach-policy", "--user", "user-cli", "--policy-arn", "arn:aws:iam::aws:policy/AdministratorAccess", "--base-url", server.URL, "--region", "us-east-1", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestAWSE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "aws", "doctor", "--public", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
