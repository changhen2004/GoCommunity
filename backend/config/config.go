package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	App struct {
		Name string
		Port string
	}
	Observability struct {
		EnablePprof           bool
		SlowRequestThresholdM int
	}
	Auth struct {
		JWTSecret string `mapstructure:"jwt_secret"`
	}
	Redis struct {
		Addr     string
		Password string
		DB       int
	}
	RabbitMQ struct {
		URL      string
		Exchange string
		Queue    string
	}
	Database struct {
		DSN string
	}
	Storage struct {
		UploadDir string
	}
}

func LoadConfig(configDir string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)
	v.SetEnvPrefix("RESOURCE_COMMUNITY_GO")
	v.AutomaticEnv()
	v.SetDefault("observability.enable_pprof", false)
	v.SetDefault("observability.slow_request_threshold_ms", 500)
	v.BindEnv("app.port", "RESOURCE_COMMUNITY_GO_APP_PORT")
	v.BindEnv("observability.enable_pprof", "RESOURCE_COMMUNITY_GO_ENABLE_PPROF")
	v.BindEnv("observability.slow_request_threshold_ms", "RESOURCE_COMMUNITY_GO_SLOW_REQUEST_THRESHOLD_MS")
	v.BindEnv("auth.jwt_secret", "RESOURCE_COMMUNITY_GO_JWT_SECRET")
	v.BindEnv("database.dsn", "RESOURCE_COMMUNITY_GO_DATABASE_DSN")
	v.BindEnv("redis.addr", "RESOURCE_COMMUNITY_GO_REDIS_ADDR")
	v.BindEnv("redis.password", "RESOURCE_COMMUNITY_GO_REDIS_PASSWORD")
	v.BindEnv("redis.db", "RESOURCE_COMMUNITY_GO_REDIS_DB")
	v.BindEnv("rabbitmq.url", "RESOURCE_COMMUNITY_GO_RABBITMQ_URL")
	v.BindEnv("rabbitmq.exchange", "RESOURCE_COMMUNITY_GO_RABBITMQ_EXCHANGE")
	v.BindEnv("rabbitmq.queue", "RESOURCE_COMMUNITY_GO_RABBITMQ_QUEUE")
	v.BindEnv("storage.upload_dir", "RESOURCE_COMMUNITY_GO_UPLOAD_DIR")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.App.Port == "" {
		cfg.App.Port = "3000"
	}
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = "secret"
	}
	if cfg.RabbitMQ.URL == "" {
		cfg.RabbitMQ.URL = "amqp://guest:guest@localhost:5672/"
	}
	if cfg.RabbitMQ.Exchange == "" {
		cfg.RabbitMQ.Exchange = "resource_community_go.async"
	}
	if cfg.RabbitMQ.Queue == "" {
		cfg.RabbitMQ.Queue = "resource_community_go.async.jobs"
	}
	if cfg.Storage.UploadDir == "" {
		cfg.Storage.UploadDir = "uploads"
	}

	return cfg, nil
}
