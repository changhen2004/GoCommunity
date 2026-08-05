package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := []byte(`app:
  name: "ExchangeApp"
  port: "3000"
database:
  dsn: "user:password@tcp(127.0.0.1:3306)/db_name?charset=utf8mb4&parseTime=True&loc=Local"
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
storage:
  upload_dir: "uploads"
`)

	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.App.Name != "ExchangeApp" {
		t.Fatalf("unexpected app name: %s", cfg.App.Name)
	}

	if cfg.App.Port != "3000" {
		t.Fatalf("unexpected app port: %s", cfg.App.Port)
	}

	if cfg.Database.DSN == "" {
		t.Fatal("expected database dsn to be loaded")
	}

	if cfg.Redis.Addr != "localhost:6379" {
		t.Fatalf("unexpected redis addr: %s", cfg.Redis.Addr)
	}

	if cfg.Auth.JWTSecret != "secret" {
		t.Fatalf("unexpected jwt secret: %s", cfg.Auth.JWTSecret)
	}

	if cfg.Storage.UploadDir != "uploads" {
		t.Fatalf("unexpected upload dir: %s", cfg.Storage.UploadDir)
	}
}

func TestLoadConfigWithEnvOverride(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := []byte(`app:
  name: "ExchangeApp"
  port: "3000"
database:
  dsn: "user:password@tcp(127.0.0.1:3306)/db_name?charset=utf8mb4&parseTime=True&loc=Local"
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
storage:
  upload_dir: "uploads"
`)

	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESOURCE_COMMUNITY_GO_APP_PORT", "8080")
	t.Setenv("RESOURCE_COMMUNITY_GO_DATABASE_DSN", "root:root@tcp(mysql:3306)/resource_community_go?charset=utf8mb4&parseTime=True&loc=Local")
	t.Setenv("RESOURCE_COMMUNITY_GO_REDIS_ADDR", "redis:6379")
	t.Setenv("RESOURCE_COMMUNITY_GO_REDIS_DB", "1")

	cfg, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.App.Port != "8080" {
		t.Fatalf("expected app port override, got: %s", cfg.App.Port)
	}

	if cfg.Database.DSN != "root:root@tcp(mysql:3306)/resource_community_go?charset=utf8mb4&parseTime=True&loc=Local" {
		t.Fatalf("expected database dsn override, got: %s", cfg.Database.DSN)
	}

	if cfg.Redis.Addr != "redis:6379" {
		t.Fatalf("expected redis addr override, got: %s", cfg.Redis.Addr)
	}

	if cfg.Redis.DB != 1 {
		t.Fatalf("expected redis db override, got: %d", cfg.Redis.DB)
	}

	if cfg.Auth.JWTSecret != "secret" {
		t.Fatalf("expected jwt secret default, got: %s", cfg.Auth.JWTSecret)
	}

	if cfg.Storage.UploadDir != "uploads" {
		t.Fatalf("expected upload dir default, got: %s", cfg.Storage.UploadDir)
	}
}
