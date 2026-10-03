package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/go-resty/resty/v2"
)

func init() {
	Register(storage.NotifyServerChan3, func(raw string) (Notifier, error) { return newServerChan3(raw) })
}

type serverChan3Config struct {
	UID     string `json:"uid"`
	SendKey string `json:"sendkey"`
}

type serverChan3 struct {
	cfg  serverChan3Config
	http *resty.Client
}

func newServerChan3(raw string) (*serverChan3, error) {
	var cfg serverChan3Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	cfg.UID = strings.TrimSpace(cfg.UID)
	cfg.SendKey = strings.TrimSpace(cfg.SendKey)
	if cfg.UID == "" {
		return nil, errors.New("serverchan3 uid is required")
	}
	if cfg.SendKey == "" {
		return nil, errors.New("serverchan3 sendkey is required")
	}
	return &serverChan3{cfg: cfg, http: resty.New()}, nil
}

func (s *serverChan3) Type() storage.NotificationChannelType { return storage.NotifyServerChan3 }

func (s *serverChan3) SetProxy(proxyURL string) {
	if proxyURL != "" {
		s.http.SetProxy(proxyURL)
	}
}

func (s *serverChan3) Send(ctx context.Context, msg Message) error {
	endpoint := fmt.Sprintf("https://%s.push.ft07.com/send/%s.send", s.cfg.UID, url.PathEscape(s.cfg.SendKey))
	resp, err := s.http.R().
		SetContext(ctx).
		SetFormData(map[string]string{
			"title": msg.Subject,
			"desp":  msg.Body,
		}).
		Post(endpoint)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return errors.New("serverchan3 returned " + resp.Status())
	}

	var result struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err == nil && result.Code != nil && *result.Code != 0 {
		reason := result.Message
		if reason == "" {
			reason = result.Msg
		}
		if reason == "" {
			reason = "code " + fmt.Sprint(*result.Code)
		}
		return errors.New("serverchan3 returned " + reason)
	}
	return nil
}
