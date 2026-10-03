package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/go-resty/resty/v2"
)

func init() {
	Register(storage.NotifyTelegram, func(raw string) (Notifier, error) { return newTelegram(raw) })
}

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type telegram struct {
	cfg  telegramConfig
	http *resty.Client
}

func newTelegram(raw string) (*telegram, error) {
	var cfg telegramConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return nil, errors.New("telegram bot_token and chat_id are required")
	}
	return &telegram{cfg: cfg, http: resty.New()}, nil
}

func (t *telegram) Type() storage.NotificationChannelType { return storage.NotifyTelegram }

func (t *telegram) SetProxy(proxyURL string) {
	if proxyURL != "" {
		t.http.SetProxy(proxyURL)
	}
}

func (t *telegram) Send(ctx context.Context, msg Message) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.cfg.BotToken)
	text := fmt.Sprintf("*%s*\n%s", msg.Subject, msg.Body)
	resp, err := t.http.R().
		SetContext(ctx).
		SetBody(map[string]any{
			"chat_id":    t.cfg.ChatID,
			"text":       text,
			"parse_mode": "Markdown",
		}).
		Post(url)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return errors.New("telegram returned " + resp.Status())
	}
	return nil
}
