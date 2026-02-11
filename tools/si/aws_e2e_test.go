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

func TestAWSE2E_BedrockFoundationModelList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/foundation-models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); !strings.Contains(got, "/bedrock/") {
			t.Fatalf("expected bedrock service in auth header: %q", got)
		}
		_, _ = w.Write([]byte(`{"modelSummaries":[{"modelId":"anthropic.claude-3-haiku-20240307-v1:0"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "bedrock", "foundation-model", "list", "--base-url", server.URL, "--region", "us-east-1", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestAWSE2E_BedrockRuntimeInvoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/anthropic.claude-3-haiku/invoke" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); !strings.Contains(got, "/bedrock-runtime/") {
			t.Fatalf("expected bedrock-runtime in auth header: %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, `"inputText":"hello world"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = w.Write([]byte(`{"outputText":"hello from model"}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "bedrock", "runtime", "invoke", "--model-id", "anthropic.claude-3-haiku", "--prompt", "hello world", "--base-url", server.URL, "--region", "us-east-1", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestAWSE2E_BedrockBatchJobCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model-invocation-job" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); !strings.Contains(got, "/bedrock/") {
			t.Fatalf("expected bedrock service in auth header: %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, `"jobName":"batch-job-1"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, `"modelId":"anthropic.claude-3-haiku"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, `"s3Uri":"s3://bucket/in/input.jsonl"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, `"s3Uri":"s3://bucket/out/"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = w.Write([]byte(`{"jobArn":"arn:aws:bedrock:us-east-1:123:model-invocation-job/job-1"}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "bedrock", "job", "create",
		"--name", "batch-job-1",
		"--role-arn", "arn:aws:iam::123456789012:role/BedrockBatchRole",
		"--model-id", "anthropic.claude-3-haiku",
		"--input-s3-uri", "s3://bucket/in/input.jsonl",
		"--output-s3-uri", "s3://bucket/out/",
		"--base-url", server.URL,
		"--region", "us-east-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestAWSE2E_BedrockAgentRuntimeRetrieveAndGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieveAndGenerate" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); !strings.Contains(got, "/bedrock-agent-runtime/") {
			t.Fatalf("expected bedrock-agent-runtime in auth header: %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, `"knowledgeBaseId":"kb-123"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		if !strings.Contains(body, `"text":"where is the runbook?"`) {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = w.Write([]byte(`{"output":{"text":"runbook is in docs/runbook.md"}}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA123456789EXAMPLE",
		"AWS_SECRET_ACCESS_KEY": "secret-key-value",
	}, "aws", "bedrock", "agent-runtime", "retrieve-and-generate",
		"--knowledge-base-id", "kb-123",
		"--query", "where is the runbook?",
		"--base-url", server.URL,
		"--region", "us-east-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
