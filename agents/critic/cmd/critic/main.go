package main

import (
    "context"
    "log"
    "os"
    "time"

    "silexa/agents/critic/internal"
)

func main() {
    actor := envOr("ACTOR_CONTAINER", "silexa-actor-web")
    manager := envOr("MANAGER_URL", "http://manager:9090")
    logInterval := durationEnv("CRITIC_LOG_INTERVAL", 5*time.Second)
    beatInterval := durationEnv("CRITIC_BEAT_INTERVAL", 30*time.Second)
    logger := log.New(os.Stdout, "critic ", log.LstdFlags|log.LUTC)

    mon, err := internal.NewMonitor(actor, manager, logger)
    if err != nil {
        logger.Fatalf("init monitor: %v", err)
    }

    ctx := context.Background()
    tickLogs := time.NewTicker(logInterval)
    tickBeat := time.NewTicker(beatInterval)
    defer tickLogs.Stop()
    defer tickBeat.Stop()

    logger.Printf("monitoring actor %s", actor)
    for {
        select {
        case <-tickLogs.C:
            if err := mon.StreamOnce(ctx); err != nil {
                logger.Printf("stream error: %v", err)
            }
        case <-tickBeat.C:
            if err := mon.Heartbeat(ctx); err != nil {
                logger.Printf("heartbeat error: %v", err)
            }
        }
    }
}

func envOr(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

func durationEnv(key string, def time.Duration) time.Duration {
    if v := os.Getenv(key); v != "" {
        if d, err := time.ParseDuration(v); err == nil && d > 0 {
            return d
        }
    }
    return def
}
