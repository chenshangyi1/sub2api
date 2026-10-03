package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/go-resty/resty/v2"
)

func init() {
	Register(storage.NotifyFeishu, func(raw string) (Notifier, error) { return newFeishu(raw) })
}

type feishuConfig struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret,omitempty"`
}

type feishu struct {
	cfg  feishuConfig
	http *resty.Client
}

func newFeishu(raw string) (*feishu, error) {
	var cfg feishuConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	if cfg.WebhookURL == "" {
		return nil, errors.New("feishu webhook_url is required")
	}
	return &feishu{cfg: cfg, http: resty.New()}, nil
}

func (f *feishu) Type() storage.NotificationChannelType { return storage.NotifyFeishu }

func (f *feishu) SetProxy(proxyURL string) {
	if proxyURL != "" {
		f.http.SetProxy(proxyURL)
	}
}

func (f *feishu) Send(ctx context.Context, msg Message) error {
	body := map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": msg.Subject + "\n" + msg.Body,
		},
	}
	if f.cfg.Secret != "" {
		ts := time.Now().Unix()
		stringToSign := strconv.FormatInt(ts, 10) + "\n" + f.cfg.Secret
		mac := hmac.New(sha256.New, []byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		body["timestamp"] = strconv.FormatInt(ts, 10)
		body["sign"] = sign
	}
	resp, err := f.http.R().
		SetContext(ctx).
		SetBody(body).
		Post(f.cfg.WebhookURL)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return errors.New("feishu returned " + resp.Status())
	}
	return nil
}
