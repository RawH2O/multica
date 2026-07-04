package notificationpush

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	barkDefaultBaseURL = "https://api.day.app"
	barkTimeout        = 5 * time.Second
)

type barkPayload struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Group     string `json:"group,omitempty"`
}

// SendBarkForInbox sends a best-effort Bark push for member inbox items when
// deployment-level Bark configuration is present.
func SendBarkForInbox(ctx context.Context, recipientType, title, body string) {
	if recipientType != "member" {
		return
	}
	keys := barkDeviceKeysFromEnv()
	if len(keys) == 0 {
		return
	}
	baseURL := barkBaseURLFromEnv()
	if baseURL == "" {
		return
	}
	if strings.TrimSpace(title) == "" {
		title = "Multica"
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), barkTimeout)
		defer cancel()
		for _, key := range keys {
			if err := postBark(ctx, http.DefaultClient, baseURL, key, title, body); err != nil {
				slog.Warn("bark push failed", "error", err)
			}
		}
	}()
}

func postBark(ctx context.Context, client *http.Client, baseURL, key, title, body string) error {
	payload, err := json.Marshal(barkPayload{
		DeviceKey: key,
		Title:     title,
		Body:      body,
		Group:     "Multica",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/push", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &barkStatusError{status: resp.Status}
	}
	return nil
}

type barkStatusError struct {
	status string
}

func (e *barkStatusError) Error() string {
	return "bark push returned " + e.status
}

func barkDeviceKeysFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("MULTICA_BARK_DEVICE_KEYS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MULTICA_BARK_DEVICE_KEY"))
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := strings.TrimSpace(part)
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func barkBaseURLFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("MULTICA_BARK_BASE_URL"))
	if raw == "" {
		raw = barkDefaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		slog.Warn("invalid MULTICA_BARK_BASE_URL; bark push disabled")
		return ""
	}
	return strings.TrimRight(raw, "/")
}
