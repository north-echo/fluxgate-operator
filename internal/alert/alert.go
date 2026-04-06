package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// slackMessage is the payload sent to a Slack incoming webhook.
type slackMessage struct {
	Channel string `json:"channel,omitempty"`
	Text    string `json:"text"`
}

// SendSlack sends a message to a Slack incoming webhook.
func SendSlack(webhookURL, channel, message string) error {
	payload := slackMessage{
		Channel: channel,
		Text:    message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	resp, err := httpClient.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("posting to slack webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// SendWebhook sends a JSON payload to a generic webhook URL.
func SendWebhook(url string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("posting to webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
