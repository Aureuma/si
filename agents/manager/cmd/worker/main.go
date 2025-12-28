package main

import (
	"log"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"silexa/agents/manager/internal/beam"
	"silexa/agents/manager/internal/state"
)

func main() {
	logger := log.New(os.Stdout, "manager-worker ", log.LstdFlags|log.LUTC)
	addr := env("TEMPORAL_ADDRESS", "localhost:7233")
	namespace := env("TEMPORAL_NAMESPACE", "default")
	taskQueue := env("TEMPORAL_TASK_QUEUE", state.TaskQueue)

	c, err := client.Dial(client.Options{
		HostPort:  addr,
		Namespace: namespace,
	})
	if err != nil {
		logger.Fatalf("temporal client: %v", err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(state.StateWorkflow)
	w.RegisterWorkflow(beam.BeamWorkflow)

	activities, err := beam.NewActivities(beam.ActivityConfig{
		Temporal:       c,
		TelegramURL:    os.Getenv("TELEGRAM_NOTIFY_URL"),
		TelegramChatID: os.Getenv("TELEGRAM_CHAT_ID"),
	})
	if err != nil {
		logger.Fatalf("beam activities init: %v", err)
	}
	w.RegisterActivity(activities)

	logger.Printf("worker started (task queue: %s)", taskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Fatalf("worker error: %v", err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
