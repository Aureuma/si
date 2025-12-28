package main

import (
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "log"
  "net/http"
  "net/url"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"

  "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
  ManagerURL     string
  RegistryPath   string
  SecretsDir     string
  ApproverToken  string
  AllowPlaintext bool
  Timeout        time.Duration
}

type Registry struct {
  Version int           `json:"version"`
  Secrets []SecretEntry `json:"secrets"`
}

type SecretEntry struct {
  Name         string   `json:"name"`
  File         string   `json:"file"`
  Format       string   `json:"format"`
  Key          string   `json:"key,omitempty"`
  Keys         []string `json:"keys,omitempty"`
  AllowAnyKey  bool     `json:"allow_any_key,omitempty"`
  Description  string   `json:"description,omitempty"`
}

type AccessRequest struct {
  ID         int    `json:"id"`
  Requester  string `json:"requester"`
  Department string `json:"department"`
  Resource   string `json:"resource"`
  Action     string `json:"action"`
  Reason     string `json:"reason"`
  Status     string `json:"status"`
  ResolvedBy string `json:"resolved_by"`
  Notes      string `json:"notes"`
}

type Server struct {
  cfg    Config
  reg    Registry
  client *http.Client
  logger *log.Logger
}

type RequestSecretInput struct {
  Secret     string `json:"secret"`
  Key        string `json:"key"`
  Reason     string `json:"reason"`
  Requester  string `json:"requester"`
  Department string `json:"department,omitempty"`
}

type RequestSecretOutput struct {
  RequestID int    `json:"request_id"`
  Status    string `json:"status"`
  Resource  string `json:"resource"`
  Message   string `json:"message"`
}

type CheckRequestInput struct {
  RequestID int `json:"request_id"`
}

type CheckRequestOutput struct {
  RequestID int    `json:"request_id"`
  Status    string `json:"status"`
  Resource  string `json:"resource"`
  Requester string `json:"requester"`
  Reason    string `json:"reason"`
  ResolvedBy string `json:"resolved_by"`
  Notes     string `json:"notes"`
}

type ResolveRequestInput struct {
  RequestID int    `json:"request_id"`
  Status    string `json:"status"`
  Notes     string `json:"notes,omitempty"`
  Token     string `json:"token"`
}

type ResolveRequestOutput struct {
  RequestID int    `json:"request_id"`
  Status    string `json:"status"`
  Resource  string `json:"resource"`
}

type RevealSecretInput struct {
  RequestID int    `json:"request_id"`
  Token     string `json:"token"`
}

type RevealSecretOutput struct {
  Secret string `json:"secret"`
  Key    string `json:"key"`
  Value  string `json:"value"`
}

type ListSecretsInput struct{}

type SecretSummary struct {
  Name        string   `json:"name"`
  Description string   `json:"description"`
  Keys        []string `json:"keys"`
  AnyKey      bool     `json:"any_key"`
}

type ListSecretsOutput struct {
  Secrets []SecretSummary `json:"secrets"`
}

type ListRequestsInput struct {
  Status string `json:"status,omitempty"`
  Token  string `json:"token"`
}

type ListRequestsOutput struct {
  Requests []AccessRequest `json:"requests"`
}

func main() {
  logger := log.New(os.Stdout, "credentials-mcp ", log.LstdFlags|log.LUTC)
  cfg := loadConfig()
  reg, err := loadRegistry(cfg.RegistryPath)
  if err != nil {
    logger.Fatalf("registry load: %v", err)
  }

  srv := &Server{
    cfg:    cfg,
    reg:    reg,
    client: &http.Client{Timeout: cfg.Timeout},
    logger: logger,
  }

  impl := &mcp.Implementation{
    Name:    "silexa-credentials",
    Title:   "Silexa Credentials Broker",
    Version: "0.1.0",
  }
  server := mcp.NewServer(impl, &mcp.ServerOptions{HasTools: true})

  mcp.AddTool(server, &mcp.Tool{
    Name:        "credentials.request_secret",
    Description: "Request access to a secret with justification. Creates an access request and returns a request id.",
  }, srv.requestSecret)

  mcp.AddTool(server, &mcp.Tool{
    Name:        "credentials.check_request",
    Description: "Check the status of an access request by id.",
  }, srv.checkRequest)

  mcp.AddTool(server, &mcp.Tool{
    Name:        "credentials.list_secrets",
    Description: "List available secret entries (names and allowed keys only).",
  }, srv.listSecrets)

  mcp.AddTool(server, &mcp.Tool{
    Name:        "credentials.list_requests",
    Description: "List access requests (requires approval token).",
  }, srv.listRequests)

  mcp.AddTool(server, &mcp.Tool{
    Name:        "credentials.resolve_request",
    Description: "Approve or deny an access request (requires approval token).",
  }, srv.resolveRequest)

  mcp.AddTool(server, &mcp.Tool{
    Name:        "credentials.reveal_secret",
    Description: "Decrypt and return the requested secret value (requires approval token).",
  }, srv.revealSecret)

  handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
    return server
  }, &mcp.StreamableHTTPOptions{JSONResponse: true})

  mux := http.NewServeMux()
  mux.Handle("/mcp", handler)
  mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write([]byte("ok"))
  })

  addr := envOr("ADDR", ":8091")
  logger.Printf("listening on %s", addr)
  if err := http.ListenAndServe(addr, mux); err != nil {
    logger.Fatalf("serve: %v", err)
  }
}

func loadConfig() Config {
  timeout := 10 * time.Second
  if raw := strings.TrimSpace(os.Getenv("REQUEST_TIMEOUT")); raw != "" {
    if v, err := time.ParseDuration(raw); err == nil {
      timeout = v
    }
  }
  return Config{
    ManagerURL:     envOr("MANAGER_URL", "http://silexa-manager:9090"),
    RegistryPath:   envOr("CREDENTIALS_REGISTRY", "/configs/credentials-registry.json"),
    SecretsDir:     envOr("SECRETS_DIR", "/credentials/secrets"),
    ApproverToken:  strings.TrimSpace(os.Getenv("CREDENTIALS_APPROVER_TOKEN")),
    AllowPlaintext: strings.TrimSpace(os.Getenv("CREDENTIALS_ALLOW_PLAINTEXT")) == "true",
    Timeout:        timeout,
  }
}

func loadRegistry(path string) (Registry, error) {
  data, err := os.ReadFile(path)
  if err != nil {
    return Registry{}, err
  }
  var reg Registry
  if err := json.Unmarshal(data, &reg); err != nil {
    return Registry{}, err
  }
  if reg.Version == 0 {
    reg.Version = 1
  }
  return reg, nil
}

func (s *Server) requestSecret(ctx context.Context, _ *mcp.CallToolRequest, in RequestSecretInput) (*mcp.CallToolResult, RequestSecretOutput, error) {
  in.Secret = strings.TrimSpace(in.Secret)
  in.Key = strings.TrimSpace(in.Key)
  in.Reason = strings.TrimSpace(in.Reason)
  in.Requester = strings.TrimSpace(in.Requester)
  if in.Secret == "" || in.Key == "" || in.Reason == "" || in.Requester == "" {
    return nil, RequestSecretOutput{}, errors.New("secret, key, reason, and requester are required")
  }
  entry, err := s.findSecret(in.Secret)
  if err != nil {
    return nil, RequestSecretOutput{}, err
  }
  if err := validateSecretKey(entry, in.Key); err != nil {
    return nil, RequestSecretOutput{}, err
  }

  resource := fmt.Sprintf("secret:%s/%s", entry.Name, in.Key)
  payload := map[string]string{
    "requester":  in.Requester,
    "department": in.Department,
    "resource":   resource,
    "action":     "read",
    "reason":     in.Reason,
  }
  reqBody, _ := json.Marshal(payload)
  req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ManagerURL+"/access-requests", bytes.NewReader(reqBody))
  if err != nil {
    return nil, RequestSecretOutput{}, err
  }
  req.Header.Set("Content-Type", "application/json")
  resp, err := s.client.Do(req)
  if err != nil {
    return nil, RequestSecretOutput{}, err
  }
  defer resp.Body.Close()
  body, _ := io.ReadAll(resp.Body)
  if resp.StatusCode >= 300 {
    return nil, RequestSecretOutput{}, fmt.Errorf("manager error: %s", strings.TrimSpace(string(body)))
  }
  var ar AccessRequest
  if err := json.Unmarshal(body, &ar); err != nil {
    return nil, RequestSecretOutput{}, err
  }
  return nil, RequestSecretOutput{
    RequestID: ar.ID,
    Status:    ar.Status,
    Resource:  ar.Resource,
    Message:   "request recorded; credentials dyad must approve before reveal",
  }, nil
}

func (s *Server) checkRequest(ctx context.Context, _ *mcp.CallToolRequest, in CheckRequestInput) (*mcp.CallToolResult, CheckRequestOutput, error) {
  if in.RequestID <= 0 {
    return nil, CheckRequestOutput{}, errors.New("request_id required")
  }
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.ManagerURL+"/access-requests", nil)
  if err != nil {
    return nil, CheckRequestOutput{}, err
  }
  resp, err := s.client.Do(req)
  if err != nil {
    return nil, CheckRequestOutput{}, err
  }
  defer resp.Body.Close()
  var list []AccessRequest
  if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
    return nil, CheckRequestOutput{}, err
  }
  for _, ar := range list {
    if ar.ID == in.RequestID {
      return nil, CheckRequestOutput{
        RequestID: ar.ID,
        Status:    ar.Status,
        Resource:  ar.Resource,
        Requester: ar.Requester,
        Reason:    ar.Reason,
        ResolvedBy: ar.ResolvedBy,
        Notes:     ar.Notes,
      }, nil
    }
  }
  return nil, CheckRequestOutput{}, fmt.Errorf("request %d not found", in.RequestID)
}

func (s *Server) listSecrets(ctx context.Context, _ *mcp.CallToolRequest, _ ListSecretsInput) (*mcp.CallToolResult, ListSecretsOutput, error) {
  _ = ctx
  out := ListSecretsOutput{}
  for _, entry := range s.reg.Secrets {
    summary := SecretSummary{
      Name:        entry.Name,
      Description: entry.Description,
      Keys:        append([]string(nil), entry.Keys...),
      AnyKey:      entry.AllowAnyKey || entry.Key != "",
    }
    if entry.Key != "" {
      summary.Keys = append(summary.Keys, entry.Key)
    }
    out.Secrets = append(out.Secrets, summary)
  }
  return nil, out, nil
}

func (s *Server) listRequests(ctx context.Context, _ *mcp.CallToolRequest, in ListRequestsInput) (*mcp.CallToolResult, ListRequestsOutput, error) {
  if err := s.requireToken(in.Token); err != nil {
    return nil, ListRequestsOutput{}, err
  }
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.ManagerURL+"/access-requests", nil)
  if err != nil {
    return nil, ListRequestsOutput{}, err
  }
  resp, err := s.client.Do(req)
  if err != nil {
    return nil, ListRequestsOutput{}, err
  }
  defer resp.Body.Close()
  var list []AccessRequest
  if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
    return nil, ListRequestsOutput{}, err
  }
  status := strings.ToLower(strings.TrimSpace(in.Status))
  out := ListRequestsOutput{}
  for _, item := range list {
    if !strings.HasPrefix(item.Resource, "secret:") {
      continue
    }
    if status != "" && strings.ToLower(item.Status) != status {
      continue
    }
    out.Requests = append(out.Requests, item)
  }
  return nil, out, nil
}

func (s *Server) resolveRequest(ctx context.Context, _ *mcp.CallToolRequest, in ResolveRequestInput) (*mcp.CallToolResult, ResolveRequestOutput, error) {
  if err := s.requireToken(in.Token); err != nil {
    return nil, ResolveRequestOutput{}, err
  }
  if in.RequestID <= 0 {
    return nil, ResolveRequestOutput{}, errors.New("request_id required")
  }
  status := strings.ToLower(strings.TrimSpace(in.Status))
  if status != "approved" && status != "denied" {
    return nil, ResolveRequestOutput{}, errors.New("status must be approved or denied")
  }
  notes := strings.TrimSpace(in.Notes)
  url := fmt.Sprintf("%s/access-requests/resolve?id=%d&status=%s&by=%s&notes=%s",
    s.cfg.ManagerURL,
    in.RequestID,
    status,
    url.QueryEscape("silexa-credentials"),
    url.QueryEscape(notes),
  )
  req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
  if err != nil {
    return nil, ResolveRequestOutput{}, err
  }
  resp, err := s.client.Do(req)
  if err != nil {
    return nil, ResolveRequestOutput{}, err
  }
  defer resp.Body.Close()
  if resp.StatusCode >= 300 {
    body, _ := io.ReadAll(resp.Body)
    return nil, ResolveRequestOutput{}, fmt.Errorf("resolve failed: %s", strings.TrimSpace(string(body)))
  }
  var out AccessRequest
  if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
    return nil, ResolveRequestOutput{}, err
  }
  return nil, ResolveRequestOutput{RequestID: out.ID, Status: out.Status, Resource: out.Resource}, nil
}

func (s *Server) revealSecret(ctx context.Context, _ *mcp.CallToolRequest, in RevealSecretInput) (*mcp.CallToolResult, RevealSecretOutput, error) {
  if err := s.requireToken(in.Token); err != nil {
    return nil, RevealSecretOutput{}, err
  }
  if in.RequestID <= 0 {
    return nil, RevealSecretOutput{}, errors.New("request_id required")
  }
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.ManagerURL+"/access-requests", nil)
  if err != nil {
    return nil, RevealSecretOutput{}, err
  }
  resp, err := s.client.Do(req)
  if err != nil {
    return nil, RevealSecretOutput{}, err
  }
  defer resp.Body.Close()
  var list []AccessRequest
  if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
    return nil, RevealSecretOutput{}, err
  }
  var target *AccessRequest
  for i := range list {
    if list[i].ID == in.RequestID {
      target = &list[i]
      break
    }
  }
  if target == nil {
    return nil, RevealSecretOutput{}, fmt.Errorf("request %d not found", in.RequestID)
  }
  if strings.ToLower(target.Status) != "approved" {
    return nil, RevealSecretOutput{}, fmt.Errorf("request %d not approved", in.RequestID)
  }
  secretName, key, err := parseResource(target.Resource)
  if err != nil {
    return nil, RevealSecretOutput{}, err
  }
  entry, err := s.findSecret(secretName)
  if err != nil {
    return nil, RevealSecretOutput{}, err
  }
  if err := validateSecretKey(entry, key); err != nil {
    return nil, RevealSecretOutput{}, err
  }

  value, err := s.decryptValue(ctx, entry, key)
  if err != nil {
    return nil, RevealSecretOutput{}, err
  }
  return nil, RevealSecretOutput{Secret: entry.Name, Key: key, Value: value}, nil
}

func (s *Server) decryptValue(ctx context.Context, entry SecretEntry, key string) (string, error) {
  path, err := s.resolveSecretPath(entry.File)
  if err != nil {
    return "", err
  }
  encrypted := isEncryptedPath(path)
  if !encrypted && !s.cfg.AllowPlaintext {
    return "", fmt.Errorf("plaintext secret blocked: %s", path)
  }

  raw := ""
  if encrypted {
    raw, err = s.decryptSops(ctx, path)
    if err != nil {
      return "", err
    }
  } else {
    data, err := os.ReadFile(path)
    if err != nil {
      return "", err
    }
    raw = string(data)
  }

  format := strings.ToLower(strings.TrimSpace(entry.Format))
  if format == "" {
    format = "env"
  }
  switch format {
  case "env":
    envMap := parseEnv(raw)
    if val, ok := envMap[key]; ok {
      return val, nil
    }
    return "", fmt.Errorf("key %s not found in %s", key, entry.File)
  case "json":
    var obj map[string]any
    if err := json.Unmarshal([]byte(raw), &obj); err != nil {
      return "", err
    }
    if val, ok := obj[key]; ok {
      switch v := val.(type) {
      case string:
        return v, nil
      default:
        data, _ := json.Marshal(v)
        return string(data), nil
      }
    }
    return "", fmt.Errorf("key %s not found in %s", key, entry.File)
  default:
    return "", fmt.Errorf("unsupported format: %s", entry.Format)
  }
}

func (s *Server) decryptSops(ctx context.Context, path string) (string, error) {
  cmd := exec.CommandContext(ctx, "sops", "-d", path)
  cmd.Env = os.Environ()
  output, err := cmd.Output()
  if err == nil {
    return string(output), nil
  }
  var stderr []byte
  if ee := new(exec.ExitError); errors.As(err, &ee) {
    stderr = ee.Stderr
  }
  msg := strings.TrimSpace(string(stderr))
  if msg == "" {
    msg = err.Error()
  }
  return "", fmt.Errorf("sops decrypt failed: %s", msg)
}

func (s *Server) resolveSecretPath(file string) (string, error) {
  file = strings.TrimSpace(file)
  if file == "" {
    return "", errors.New("secret file not set")
  }
  secretsDir := filepath.Clean(s.cfg.SecretsDir)
  if !filepath.IsAbs(secretsDir) {
    return "", fmt.Errorf("secrets dir must be absolute: %s", secretsDir)
  }
  clean := filepath.Clean(file)
  if !filepath.IsAbs(clean) {
    clean = filepath.Join(secretsDir, clean)
  }
  clean = filepath.Clean(clean)
  if !strings.HasPrefix(clean, secretsDir+string(os.PathSeparator)) && clean != secretsDir {
    return "", fmt.Errorf("secret path outside secrets dir: %s", clean)
  }
  if _, err := os.Stat(clean); err != nil {
    return "", err
  }
  return clean, nil
}

func (s *Server) findSecret(name string) (SecretEntry, error) {
  for _, entry := range s.reg.Secrets {
    if entry.Name == name {
      return entry, nil
    }
  }
  return SecretEntry{}, fmt.Errorf("secret %s not registered", name)
}

func validateSecretKey(entry SecretEntry, key string) error {
  if entry.Key != "" {
    if entry.Key != key {
      return fmt.Errorf("key %s not allowed for %s", key, entry.Name)
    }
    return nil
  }
  if len(entry.Keys) > 0 {
    for _, allowed := range entry.Keys {
      if allowed == key {
        return nil
      }
    }
    return fmt.Errorf("key %s not allowed for %s", key, entry.Name)
  }
  if entry.AllowAnyKey {
    return nil
  }
  return fmt.Errorf("key %s not allowed for %s", key, entry.Name)
}

func parseEnv(content string) map[string]string {
  out := map[string]string{}
  for _, line := range strings.Split(content, "\n") {
    line = strings.TrimSpace(line)
    if line == "" || strings.HasPrefix(line, "#") {
      continue
    }
    if strings.HasPrefix(line, "export ") {
      line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
    }
    parts := strings.SplitN(line, "=", 2)
    if len(parts) != 2 {
      continue
    }
    key := strings.TrimSpace(parts[0])
    val := strings.TrimSpace(parts[1])
    val = strings.Trim(val, "\"'")
    if key != "" {
      out[key] = val
    }
  }
  return out
}

func parseResource(resource string) (string, string, error) {
  if !strings.HasPrefix(resource, "secret:") {
    return "", "", fmt.Errorf("invalid resource: %s", resource)
  }
  raw := strings.TrimPrefix(resource, "secret:")
  parts := strings.SplitN(raw, "/", 2)
  if len(parts) != 2 {
    return "", "", fmt.Errorf("invalid secret resource: %s", resource)
  }
  name := strings.TrimSpace(parts[0])
  key := strings.TrimSpace(parts[1])
  if name == "" || key == "" {
    return "", "", fmt.Errorf("invalid secret resource: %s", resource)
  }
  return name, key, nil
}

func (s *Server) requireToken(token string) error {
  token = strings.TrimSpace(token)
  if token == "" {
    return errors.New("approval token required")
  }
  if s.cfg.ApproverToken == "" {
    return errors.New("approver token not configured")
  }
  if token != s.cfg.ApproverToken {
    return errors.New("invalid approval token")
  }
  return nil
}

func isEncryptedPath(path string) bool {
  base := filepath.Base(path)
  if strings.Contains(base, ".sops.") {
    return true
  }
  return strings.HasSuffix(base, ".sops")
}

func envOr(key, def string) string {
  if v := strings.TrimSpace(os.Getenv(key)); v != "" {
    return v
  }
  return def
}
