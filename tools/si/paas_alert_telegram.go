package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const paasTelegramAPIBaseEnvKey = "SI_PAAS_TELEGRAM_API_BASE"

type paasTelegramSendResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

func sendPaasTelegramMessage(cfg paasTelegramNotifierConfig, message string) error {
	token := strings.TrimSpace(cfg.BotToken)
	chatID := strings.TrimSpace(cfg.ChatID)
	text := strings.TrimSpace(message)
	if token == "" || chatID == "" {
		return fmt.Errorf("telegram notifier is not configured; run `si paas alert setup-telegram` first")
	}
	if text == "" {
		return fmt.Errorf("telegram message is required")
	}
	baseURL := strings.TrimSpace(os.Getenv(paasTelegramAPIBaseEnvKey))
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/bot" + token + "/sendMessage"
	payload := url.Values{}
	payload.Set("chat_id", chatID)
	payload.Set("text", text)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(endpoint, payload)
	if err != nil {
		return fmt.Errorf("send telegram alert: %w", err)
	}
	defer resp.Body.Close()

	var body paasTelegramSendResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(body.Description)
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("telegram API error: %s", msg)
	}
	if !body.OK && strings.TrimSpace(body.Description) != "" {
		return fmt.Errorf("telegram API error: %s", strings.TrimSpace(body.Description))
	}
	return nil
}
