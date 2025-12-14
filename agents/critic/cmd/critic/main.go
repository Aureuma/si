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
    logger := log.New(os.Stdout, "critic ", log.LstdFlags|log.LUTC)

    mon, err := internal.NewMonitor(actor, manager, logger)
    if err != nil {
        logger.Fatalf("init monitor: %v", err)
    }

    ctx := context.Background()
    tickLogs := time.NewTicker(5 * time.Second)
    tickBeat := time.NewTicker(30 * time.Second)
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
