package main

import (
	"context"
	"log"
	"resource_community_go/config"
	"resource_community_go/internal/app"
	"resource_community_go/internal/asyncjob"
	"resource_community_go/utils"
	"strings"
	"time"
)

func main() {
	cfg, err := config.LoadConfig("./config")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := config.InitDB(cfg)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}

	redisClient, err := config.InitRedis(context.Background(), cfg)
	if err != nil {
		log.Fatalf("init redis: %v", err)
	}
	rabbitConn, err := config.InitRabbitMQ(cfg)
	if err != nil {
		log.Fatalf("init rabbitmq: %v", err)
	}
	defer rabbitConn.Close()

	publisher, err := asyncjob.NewRabbitPublisher(
		rabbitConn,
		cfg.RabbitMQ.Exchange,
		cfg.RabbitMQ.Queue,
	)
	if err != nil {
		log.Fatalf("init async publisher: %v", err)
	}
	defer publisher.Close()

	if err := config.Migrate(db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	utils.SetJWTSecret(cfg.Auth.JWTSecret)

	r := app.SetUpRouter(app.Dependencies{
		DB:                   db,
		RedisDB:              redisClient,
		UploadDir:            cfg.Storage.UploadDir,
		EnablePprof:          cfg.Observability.EnablePprof,
		AsyncPublisher:       publisher,
		SlowRequestThreshold: time.Duration(cfg.Observability.SlowRequestThresholdM) * time.Millisecond,
	})
	trustedProxies := strings.FieldsFunc(cfg.App.TrustedProxies, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if err := r.SetTrustedProxies(trustedProxies); err != nil {
		log.Fatalf("set trusted proxies: %v", err)
	}

	port := cfg.App.Port
	if port == "" {
		port = "3000"
	}

	r.Run(":" + port)
}
