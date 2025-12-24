package main

import (
	"embed"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	logger := log.New(os.Stdout, "dashboard ", log.LstdFlags|log.LUTC)
	managerURL := env("MANAGER_URL", "http://manager:9090")
	binDir := env("BIN_DIR", "/workspace/silexa/bin")
	addr := env("ADDR", ":8087")

	r := chi.NewRouter()
	r.Use(corsMiddleware)

	r.Get("/api/dyad-tasks", func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(managerURL + "/dyad-tasks")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	r.Post("/api/spawn", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Name        string `json:"name"`
			Role        string `json:"role"`
			Department  string `json:"department"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.Name) == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if payload.Role == "" {
			payload.Role = payload.Name
		}
		if payload.Department == "" {
			payload.Department = payload.Role
		}
		script := filepath.Join(binDir, "spawn-dyad.sh")
		cmd := exec.Command(script, payload.Name, payload.Role, payload.Department)
		out, err := cmd.CombinedOutput()
		if err != nil {
			http.Error(w, err.Error()+"\n"+string(out), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"output": string(out),
		})
	})

	// Static UI
	r.Handle("/*", http.FileServer(http.FS(staticFS)))

	logger.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

