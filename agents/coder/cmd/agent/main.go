package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
)

// simple health server so container stays up and can be probed.
func main() {
    logger := log.New(os.Stdout, "coder-agent ", log.LstdFlags|log.LUTC)
    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })

    srv := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    go func() {
        logger.Println("starting coder agent listener on :8080")
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Fatalf("listen: %v", err)
        }
    }()

    stop := make(chan os.Signal, 1)
    signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
    <-stop

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        logger.Printf("shutdown error: %v", err)
    }
    logger.Println("coder agent stopped")
}
