// Package storage 提供 GORM 仓储与领域模型持久化。
package storage

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sqliteDriver "github.com/glebarez/sqlite"
	mysqlDriver "gorm.io/driver/mysql"
	postgresDriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DBDriver string

const (
	DBDriverSQLite   DBDriver = "sqlite"
	DBDriverMySQL    DBDriver = "mysql"
	DBDriverPostgres DBDriver = "postgres"
)

type DBConfig struct {
	Driver       DBDriver
	URL          string
	Path         string
	Host         string
	Port         int
	User         string
	Password     string
	Name         string
	MaxOpenConns int
	MaxIdleConns int
}

func (c DBConfig) SQLitePath() string {
	if strings.TrimSpace(c.Path) != "" {
		return c.Path
	}
	return "./data/upstream-ops.db"
}

func (c DBConfig) MySQLDSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.Name,
	)
}

// EffectiveDriver gives DATABASE_URL precedence so existing PostgreSQL
// deployments do not need to duplicate the connection settings.
func (c DBConfig) EffectiveDriver() DBDriver {
	if strings.TrimSpace(c.URL) != "" {
		return DBDriverPostgres
	}
	driver := DBDriver(strings.ToLower(strings.TrimSpace(string(c.Driver))))
	if driver == "postgresql" {
		return DBDriverPostgres
	}
	if driver == "" {
		return DBDriverSQLite
	}
	return driver
}

// PostgresDSN accepts the legacy panel's Prisma-style DATABASE_URL. Prisma's
// schema query parameter is translated to libpq/pgx's search_path parameter.
func (c DBConfig) PostgresDSN() (string, error) {
	raw := strings.TrimSpace(c.URL)
	if raw == "" {
		port := c.Port
		if port == 0 {
			port = 5432
		}
		u := &url.URL{
			Scheme: "postgresql",
			User:   url.UserPassword(c.User, c.Password),
			Host:   net.JoinHostPort(c.Host, strconv.Itoa(port)),
			Path:   "/" + c.Name,
		}
		return u.String(), nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("DATABASE_URL must use postgres or postgresql scheme")
	}
	if u.Host == "" || strings.TrimPrefix(u.Path, "/") == "" {
		return "", fmt.Errorf("DATABASE_URL must include host and database name")
	}
	query := u.Query()
	if schema := strings.TrimSpace(query.Get("schema")); schema != "" {
		if query.Get("search_path") == "" {
			query.Set("search_path", schema)
		}
		query.Del("schema")
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

// newGormLogger 关掉 GORM 默认 logger 对 ErrRecordNotFound 的告警噪音。
//
// 业务代码（如 Rates.Upsert）显式处理了"找不到就插入"，这种情况下 GORM 默认仍会
// 把 record not found 当 Warn 打出来，造成日志看起来满是错误其实没问题。
// IgnoreRecordNotFoundError = true 可以静默这类预期内的"未找到"。
func newGormLogger() logger.Interface {
	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)
}

func Open(cfg DBConfig) (*gorm.DB, error) {
	dialector, driver, err := dialectorForConfig(cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	switch driver {
	case DBDriverSQLite:
		if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			return nil, fmt.Errorf("set sqlite journal mode: %w", err)
		}
		if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
			return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	default:
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}

	return db, nil
}

func dialectorForConfig(cfg DBConfig) (gorm.Dialector, DBDriver, error) {
	driver := cfg.EffectiveDriver()
	switch driver {
	case DBDriverSQLite:
		path := cfg.SQLitePath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, "", fmt.Errorf("create sqlite dir: %w", err)
		}
		return sqliteDriver.Open(path), driver, nil
	case DBDriverMySQL:
		return mysqlDriver.Open(cfg.MySQLDSN()), driver, nil
	case DBDriverPostgres:
		dsn, err := cfg.PostgresDSN()
		if err != nil {
			return nil, "", err
		}
		return postgresDriver.Open(dsn), driver, nil
	default:
		return nil, "", fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}
}

// AutoMigrate 启动时自动同步表结构。
func AutoMigrate(db *gorm.DB) error {
	if err := dropDeletedAtColumns(db); err != nil {
		return err
	}
	// 仅 AutoMigrate 当前模型（网关未发布，不做「密钥→组」等历史数据迁移）
	if err := db.AutoMigrate(
		&AdminUser{},
		&Channel{},
		&AuthSession{},
		&CaptchaConfig{},
		&RateSnapshot{},
		&RateChangeLog{},
		&UpstreamAnnouncement{},
		&BalanceSnapshot{},
		&CostSnapshot{},
		&NotificationChannel{},
		&NotificationLog{},
		&NotificationCooldown{},
		&MonitorLog{},
		&UpstreamSyncTarget{},
		&UpstreamSyncTargetGroup{},
		&UpstreamSyncGroup{},
		&UpstreamSyncAccount{},
		&UpstreamSyncManagedAccount{},
		&UpstreamSyncLog{},
		&GatewayGroup{},
		&GatewayKey{},
		&GatewayRoute{},
		&GatewayProvider{},
		&GatewayUsageLog{},
		&ModelPriceOverride{},
	); err != nil {
		return err
	}
	// Existing legacy schemas own this table definition. Fresh databases still
	// need it so canonical targets can satisfy extension-table foreign keys.
	if !db.Migrator().HasTable(&Connection{}) {
		return db.AutoMigrate(&Connection{})
	}
	return nil
}

func dropDeletedAtColumns(db *gorm.DB) error {
	targets := []struct {
		table string
		model any
	}{
		{table: "channels", model: &Channel{}},
		{table: "captcha_configs", model: &CaptchaConfig{}},
		{table: "notification_channels", model: &NotificationChannel{}},
	}

	for _, target := range targets {
		if !db.Migrator().HasTable(target.model) {
			continue
		}
		hasColumn, err := tableHasColumn(db, target.table, "deleted_at")
		if err != nil {
			return fmt.Errorf("inspect %s.deleted_at: %w", target.table, err)
		}
		if !hasColumn {
			continue
		}
		if err := db.Exec("DELETE FROM " + target.table + " WHERE deleted_at IS NOT NULL").Error; err != nil {
			return fmt.Errorf("delete soft-deleted rows from %s: %w", target.table, err)
		}
		if db.Migrator().HasIndex(target.model, "idx_"+target.table+"_deleted_at") {
			if err := db.Migrator().DropIndex(target.model, "idx_"+target.table+"_deleted_at"); err != nil {
				return fmt.Errorf("drop %s deleted_at index: %w", target.table, err)
			}
		}
		if err := db.Migrator().DropColumn(target.model, "deleted_at"); err != nil {
			return fmt.Errorf("drop %s.deleted_at: %w", target.table, err)
		}
		hasColumn, err = tableHasColumn(db, target.table, "deleted_at")
		if err != nil {
			return fmt.Errorf("inspect %s.deleted_at after drop: %w", target.table, err)
		}
		if hasColumn && db.Dialector.Name() == "sqlite" {
			if err := db.Exec("ALTER TABLE " + target.table + " DROP COLUMN deleted_at").Error; err != nil {
				return fmt.Errorf("drop sqlite %s.deleted_at: %w", target.table, err)
			}
		}
	}
	return nil
}

func tableHasColumn(db *gorm.DB, table, column string) (bool, error) {
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false, err
	}
	for _, c := range columns {
		if strings.EqualFold(c.Name(), column) {
			return true, nil
		}
	}
	return false, nil
}
