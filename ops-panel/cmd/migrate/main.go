package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/migration"
	"github.com/bejix/upstream-ops/backend/storage"
)

type commandError struct {
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	action := flags.String("action", "preflight", "preflight, migrate, verify, or rollback")
	configPath := flags.String("config", "", "path to config.yaml")
	version := flags.String("version", migration.VersionV007LegacyImport, "migration version")
	requirePostgres := flags.Bool("require-postgres", true, "require PostgreSQL dialect")
	timeout := flags.Duration("timeout", 10*time.Minute, "command timeout")
	if err := flags.Parse(args); err != nil {
		writeError(*action, err, nil)
		return 2
	}

	requestedAction := strings.ToLower(strings.TrimSpace(*action))
	switch requestedAction {
	case "preflight", "migrate", "verify", "rollback":
	default:
		writeError(requestedAction, fmt.Errorf("unsupported action %q", requestedAction), nil)
		return 2
	}

	cfg, _, err := config.LoadWithPath(*configPath)
	if err != nil {
		writeError(requestedAction, fmt.Errorf("load config: %w", err), nil)
		return 1
	}
	secrets := []string{
		os.Getenv("DATABASE_URL"),
		os.Getenv("ENCRYPTION_KEY"),
		cfg.Security.AppSecret,
		cfg.Database.Password,
	}

	db, err := storage.Open(cfg.Database.ToStorageConfig())
	if err != nil {
		writeError(requestedAction, fmt.Errorf("open database: %w", err), secrets)
		return 1
	}
	if sqlDB, dbErr := db.DB(); dbErr == nil {
		defer sqlDB.Close()
	}

	runner, err := migration.NewRunner(db, migration.RunnerOptions{
		Version:             *version,
		LegacyEncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		AppSecret:           cfg.Security.AppSecret,
		RequirePostgres:     *requirePostgres,
	})
	if err != nil {
		writeError(requestedAction, err, secrets)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	var result any
	switch requestedAction {
	case "preflight":
		result, err = runner.Preflight(ctx)
	case "migrate":
		result, err = runner.Migrate(ctx)
	case "verify":
		result, err = runner.Verify(ctx)
	case "rollback":
		result, err = runner.Rollback(ctx)
	}
	if err != nil {
		writeError(requestedAction, err, secrets)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		writeError(requestedAction, fmt.Errorf("encode result: %w", err), secrets)
		return 1
	}
	return 0
}

func writeError(action string, err error, secrets []string) {
	message := "unknown error"
	if err != nil {
		message = err.Error()
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	_ = json.NewEncoder(os.Stderr).Encode(commandError{Action: action, OK: false, Error: message})
}
