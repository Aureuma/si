package main

import (
    "fmt"
    "net/http"
    "os"
)

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })
    mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintln(w, "Hello from sample-go-service")
    })
    port := env("PORT", "8080")
    http.ListenAndServe(":"+port, mux)
}

func env(k, def string) string {
    if v := os.Getenv(k); v != "" {
        return v
    }
    return def
}
