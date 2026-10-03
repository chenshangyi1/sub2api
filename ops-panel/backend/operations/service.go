package operations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

var (
	ErrNotFound = errors.New("operations resource not found")
	ErrInvalid  = errors.New("invalid operations request")
)

type Service struct {
	db                    *gorm.DB
	mainDB                *gorm.DB
	cipher                *crypto.Cipher
	admin                 *sub2api.AdminClient
	targetReferenceLoader targetReferenceLoader
	http                  *http.Client
	log                   *slog.Logger
	now                   func() time.Time
}

func New(db, mainDB *gorm.DB, cipher *crypto.Cipher, log *slog.Logger) (*Service, error) {
	if db == nil || cipher == nil {
		return nil, errors.New("operations database and cipher are required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		db:     db,
		mainDB: mainDB,
		cipher: cipher,
		admin:  sub2api.NewAdminClient(),
		http:   &http.Client{Timeout: 15 * time.Second},
		log:    log,
		now:    time.Now,
	}, nil
}

func EnsureSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("operations database is nil")
	}
	return ensureExtensionSchema(db)
}

func OpenMainDatabase(rawURL string) (*gorm.DB, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil
	}
	return storage.Open(storage.DBConfig{URL: rawURL, MaxOpenConns: 5, MaxIdleConns: 2})
}

func (s *Service) target(ctx context.Context, id uint) (*storage.UpstreamSyncTarget, sub2api.AdminTarget, error) {
	if id == 0 {
		return nil, sub2api.AdminTarget{}, fmt.Errorf("%w: target id is required", ErrInvalid)
	}
	var target storage.UpstreamSyncTarget
	err := s.db.WithContext(ctx).First(&target, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, sub2api.AdminTarget{}, fmt.Errorf("%w: target %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, sub2api.AdminTarget{}, err
	}
	key, err := s.cipher.Decrypt(target.AdminAPIKeyCipher)
	if err != nil {
		return nil, sub2api.AdminTarget{}, fmt.Errorf("decrypt target administrator key: %w", err)
	}
	if strings.TrimSpace(key) == "" {
		return nil, sub2api.AdminTarget{}, fmt.Errorf("%w: target %d has no administrator key", ErrInvalid, id)
	}
	return &target, sub2api.AdminTarget{BaseURL: target.BaseURL, APIKey: key}, nil
}

func (s *Service) validateTarget(ctx context.Context, id uint) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&storage.UpstreamSyncTarget{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: target %d", ErrNotFound, id)
	}
	return nil
}

func (s *Service) ListTargets(ctx context.Context) ([]storage.UpstreamSyncTarget, error) {
	var targets []storage.UpstreamSyncTarget
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&targets).Error; err != nil {
		return nil, err
	}
	return targets, nil
}

func (s *Service) recordAction(ctx context.Context, action, target, message string, success bool) {
	row := ActionLog{Action: action, Target: target, Message: message, Success: success, CreatedAt: s.now().UTC()}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		s.log.Warn("record operations action failed", "action", action, "target", target, "err", err)
	}
}

func ErrorStatus(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func containsFold(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(strings.TrimSpace(needle)))
}
