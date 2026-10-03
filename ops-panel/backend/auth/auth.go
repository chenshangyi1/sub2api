// Package auth 提供单管理员登录：账号密码写在 config 里，登录后下发 HMAC-SHA256 签名的短 token。
//
// Token 格式："<base64url(payload)>.<base64url(hmac)>"，payload 是 {"sub":"<user>","exp":<unix>}。
// 服务端无状态，AppSecret 不变的情况下重启 token 仍有效。
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CredentialStore validates credentials and confirms that a token subject is
// still active. Verify calls SubjectExists for every authenticated request.
type CredentialStore interface {
	Authenticate(ctx context.Context, username, password string) (subject string, ok bool, err error)
	SubjectExists(ctx context.Context, subject string) (bool, error)
}

type staticCredentialStore struct {
	username string
	password string
}

func newStaticCredentialStore(username, password string) (*staticCredentialStore, error) {
	if username == "" || password == "" {
		return nil, errors.New("auth username / password are required")
	}
	return &staticCredentialStore{username: username, password: password}, nil
}

func (s *staticCredentialStore) Authenticate(_ context.Context, username, password string) (string, bool, error) {
	valid := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1 &&
		subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
	return s.username, valid, nil
}

func (s *staticCredentialStore) SubjectExists(_ context.Context, subject string) (bool, error) {
	return subtle.ConstantTimeCompare([]byte(subject), []byte(s.username)) == 1, nil
}

func (s *staticCredentialStore) DefaultSubject() string { return s.username }

// Service signs tokens while delegating credential lifecycle to a store.
type Service struct {
	credentials CredentialStore
	secret      []byte
	tokenTTL    time.Duration
}

// New 构造 Service。secret 推荐 32 字节以上；若为空报错。
// 调用方应在 secret 为空时回退到 APP_SECRET。
func New(username, password, secret string, ttl time.Duration) (*Service, error) {
	store, err := newStaticCredentialStore(username, password)
	if err != nil {
		return nil, err
	}
	return NewWithCredentialStore(store, secret, ttl)
}

// NewWithCredentialStore constructs a service backed by a database or another
// credential provider.
func NewWithCredentialStore(credentials CredentialStore, secret string, ttl time.Duration) (*Service, error) {
	if credentials == nil {
		return nil, errors.New("auth credential store is nil")
	}
	if secret == "" {
		return nil, errors.New("auth token secret is empty")
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Service{
		credentials: credentials,
		secret:      []byte(secret),
		tokenTTL:    ttl,
	}, nil
}

// claims 是签发到 token payload 里的最小必要字段。
type claims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

// Login 校验账号密码，返回新的 token 与过期时间。
func (s *Service) Login(username, password string) (string, time.Time, error) {
	return s.LoginContext(context.Background(), username, password)
}

func (s *Service) LoginContext(ctx context.Context, username, password string) (string, time.Time, error) {
	subject, ok, err := s.credentials.Authenticate(ctx, username, password)
	if err != nil {
		return "", time.Time{}, err
	}
	if !ok || subject == "" {
		return "", time.Time{}, errors.New("invalid username or password")
	}
	expiresAt := time.Now().Add(s.tokenTTL)
	c := claims{Sub: subject, Exp: expiresAt.Unix()}
	tok, err := s.sign(c)
	if err != nil {
		return "", time.Time{}, err
	}
	return tok, expiresAt, nil
}

// Verify 校验 token 合法性并返回 subject。
func (s *Service) Verify(token string) (string, error) {
	return s.VerifyContext(context.Background(), token)
}

func (s *Service) VerifyContext(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", errors.New("malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode sig: %w", err)
	}
	expectedSig := s.mac(payload)
	if subtle.ConstantTimeCompare(sig, expectedSig) != 1 {
		return "", errors.New("bad signature")
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return "", fmt.Errorf("decode claims: %w", err)
	}
	if time.Now().Unix() > c.Exp {
		return "", errors.New("token expired")
	}
	if c.Sub == "" {
		return "", errors.New("unknown subject")
	}
	exists, err := s.credentials.SubjectExists(ctx, c.Sub)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.New("unknown subject")
	}
	return c.Sub, nil
}

func (s *Service) sign(c claims) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sig := s.mac(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Service) mac(payload []byte) []byte {
	m := hmac.New(sha256.New, s.secret)
	m.Write(payload)
	return m.Sum(nil)
}

// Username remains available for static-store callers. Dynamic stores return
// an empty value because the authenticated token subject is authoritative.
func (s *Service) Username() string {
	if provider, ok := s.credentials.(interface{ DefaultSubject() string }); ok {
		return provider.DefaultSubject()
	}
	return ""
}

func (s *Service) CredentialStore() CredentialStore { return s.credentials }

// TokenTTL 返回登录 token 的有效期。
func (s *Service) TokenTTL() time.Duration { return s.tokenTTL }

// Middleware 校验 Authorization 头。不通过返回 401。
//
// 路径白名单（不需要鉴权）：
//   - "/healthz"
//   - "/api/version"
//   - "/api/auth/login"
func (s *Service) Middleware() gin.HandlerFunc {
	whitelist := map[string]struct{}{
		"/healthz":        {},
		"/api/version":    {},
		"/api/auth/login": {},
	}
	return func(c *gin.Context) {
		if _, ok := whitelist[c.FullPath()]; ok {
			c.Next()
			return
		}
		if _, ok := whitelist[c.Request.URL.Path]; ok {
			c.Next()
			return
		}
		token := extractToken(c.Request)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		sub, err := s.VerifyContext(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set("authSubject", sub)
		c.Next()
	}
}

func extractToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if c, err := r.Cookie("uh_token"); err == nil {
		return c.Value
	}
	return ""
}
