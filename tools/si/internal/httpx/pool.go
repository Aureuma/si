package httpx

import (
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	transportOnce sync.Once
	transport     *http.Transport
	clientsMu     sync.Mutex
	clients       = map[time.Duration]*http.Client{}
)

func SharedClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if client, ok := clients[timeout]; ok {
		return client
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: sharedTransport(),
	}
	clients[timeout] = client
	return client
}

func sharedTransport() *http.Transport {
	transportOnce.Do(func() {
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   64,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	})
	return transport
}
