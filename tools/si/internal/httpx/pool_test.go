package httpx

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func resetPoolStateForTest() func() {
	transportOnce = sync.Once{}
	transport = nil
	clientsMu.Lock()
	clients = map[time.Duration]*http.Client{}
	clientsMu.Unlock()
	return func() {
		transportOnce = sync.Once{}
		transport = nil
		clientsMu.Lock()
		clients = map[time.Duration]*http.Client{}
		clientsMu.Unlock()
	}
}

func TestSharedClientReuseByTimeout(t *testing.T) {
	defer resetPoolStateForTest()()
	a := SharedClient(10 * time.Second)
	b := SharedClient(10 * time.Second)
	if a != b {
		t.Fatalf("expected same client pointer for same timeout")
	}
	c := SharedClient(20 * time.Second)
	if c == a {
		t.Fatalf("expected different client pointer for different timeout")
	}
}

func TestSharedClientDefaultsForNonPositiveTimeout(t *testing.T) {
	defer resetPoolStateForTest()()
	a := SharedClient(0)
	b := SharedClient(-1 * time.Second)
	if a != b {
		t.Fatalf("expected default timeout clients to reuse the same pointer")
	}
	if a.Timeout != 30*time.Second {
		t.Fatalf("unexpected default timeout: %s", a.Timeout)
	}
}

func TestSharedTransportSingleton(t *testing.T) {
	defer resetPoolStateForTest()()
	a := sharedTransport()
	b := sharedTransport()
	if a != b {
		t.Fatalf("expected same transport pointer")
	}
	if !a.ForceAttemptHTTP2 {
		t.Fatalf("expected ForceAttemptHTTP2=true")
	}
}

func TestSharedClientConcurrentAccess(t *testing.T) {
	defer resetPoolStateForTest()()
	const workers = 32
	ch := make(chan *http.Client, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch <- SharedClient(15 * time.Second)
		}()
	}
	wg.Wait()
	close(ch)

	var first *http.Client
	for item := range ch {
		if first == nil {
			first = item
			continue
		}
		if item != first {
			t.Fatalf("expected all concurrent calls to return same pointer")
		}
	}
}
