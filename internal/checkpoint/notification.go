package checkpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// NotificationEvent carries information about something worth notifying on.
type NotificationEvent struct {
	Tier    int    `json:"tier"` // 1=passive, 2=system, 3=external
	Title   string `json:"title"`
	Message string `json:"message"`
}

// Notifier sends notifications about workspace events.
type Notifier interface {
	Notify(event NotificationEvent) error
}

// SystemNotifier sends notifications through the OS notification system.
// On macOS it uses osascript; on Linux it would use notify-send.
type SystemNotifier struct{}

// NewSystemNotifier creates a SystemNotifier.
func NewSystemNotifier() *SystemNotifier {
	return &SystemNotifier{}
}

// Notify sends a system-level notification. Only fires for tier >= 2.
func (n *SystemNotifier) Notify(event NotificationEvent) error {
	if event.Tier < 2 {
		return nil // passive events are not dispatched
	}

	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(
			`display notification %q with title %q`,
			event.Message, event.Title,
		)
		cmd := exec.Command("osascript", "-e", script)
		return cmd.Run()
	case "linux":
		cmd := exec.Command("notify-send", event.Title, event.Message)
		return cmd.Run()
	default:
		// Terminal bell fallback.
		fmt.Print("\a")
		return nil
	}
}

// WebhookNotifier sends notifications as HTTP POST requests with a JSON body.
type WebhookNotifier struct {
	URL     string
	Timeout time.Duration
	client  *http.Client
}

// NewWebhookNotifier creates a WebhookNotifier targeting the given URL.
func NewWebhookNotifier(url string) *WebhookNotifier {
	timeout := 10 * time.Second
	return &WebhookNotifier{
		URL:     url,
		Timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Notify posts the event as JSON to the configured webhook URL.
// Only fires for tier >= 3.
func (n *WebhookNotifier) Notify(event NotificationEvent) error {
	if event.Tier < 3 {
		return nil // only external-tier events trigger webhooks
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	resp, err := n.client.Post(n.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook POST to %s: %w", n.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
