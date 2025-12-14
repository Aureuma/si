package internal

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "time"
)

type Monitor struct {
    ActorContainer string
    ManagerURL     string
    Logger         *log.Logger
    lastTimestamp  time.Time
    httpClient     *http.Client
}

func NewMonitor(actor, manager string, logger *log.Logger) (*Monitor, error) {
    dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
        return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
    }
    transport := &http.Transport{
        DialContext: dial,
    }
    return &Monitor{
        ActorContainer: actor,
        ManagerURL:     manager,
        Logger:         logger,
        lastTimestamp:  time.Now().Add(-30 * time.Second),
        httpClient:     &http.Client{Transport: transport, Timeout: 10 * time.Second},
    }, nil
}

// Poll actor logs and mirror them to stdout for visibility and potential future parsing.
func (m *Monitor) StreamOnce(ctx context.Context) error {
    since := m.lastTimestamp.Unix()
    endpoint := fmt.Sprintf("http://unix/containers/%s/logs?stdout=1&stderr=1&tail=100&since=%d", url.PathEscape(m.ActorContainer), since)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
    if err != nil {
        return err
    }
    resp, err := m.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    data, err := io.ReadAll(resp.Body)
    if err != nil {
        return err
    }
    if len(data) > 0 {
        m.Logger.Printf("[%s logs]\n%s", m.ActorContainer, string(data))
        m.lastTimestamp = time.Now()
    }
    return nil
}

func (m *Monitor) Heartbeat(ctx context.Context) error {
    body, _ := json.Marshal(map[string]string{
        "actor":   m.ActorContainer,
        "critic":  hostname(),
        "status":  "ok",
        "message": "heartbeat",
    })
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/heartbeat", bytes.NewReader(body))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    io.Copy(io.Discard, resp.Body)
    return nil
}

func hostname() string {
    h, err := os.Hostname()
    if err != nil {
        return "unknown"
    }
    return h
}
