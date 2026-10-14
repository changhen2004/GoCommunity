package main

import (
	"context"
	"log"
	"resource_community_go/config"
	"resource_community_go/internal/article"
	"resource_community_go/internal/points"
	internalWorker "resource_community_go/internal/worker"
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
	defer redisClient.Close()

	idempotencyRedis := redisClient
	if cfg.Worker.IdempotencyRedisDB != nil && *cfg.Worker.IdempotencyRedisDB != cfg.Redis.DB {
		idempotencyConfig := *cfg
		idempotencyConfig.Redis.DB = *cfg.Worker.IdempotencyRedisDB
		idempotencyRedis, err = config.InitRedis(context.Background(), &idempotencyConfig)
		if err != nil {
			log.Fatalf("init idempotency redis: %v", err)
		}
		defer idempotencyRedis.Close()
	}

	rabbitConn, err := config.InitRabbitMQ(cfg)
	if err != nil {
		log.Fatalf("init rabbitmq: %v", err)
	}
	defer rabbitConn.Close()

	pointsService := points.NewService(points.NewRepo(db, redisClient))
	articleService := article.NewService(article.NewRepo(db, redisClient), nil, pointsService)
	processor := internalWorker.NewProcessor(articleService, pointsService)
	idemStore := internalWorker.NewRedisIdempotencyStore(idempotencyRedis)

	worker, err := internalWorker.NewRunner(rabbitConn, cfg.RabbitMQ.Exchange, cfg.RabbitMQ.Queue, processor, idemStore)
	if err != nil {
		log.Fatalf("init async worker: %v", err)
	}
	defer worker.Close()

	if err := worker.Run(context.Background()); err != nil {
		log.Fatalf("run async worker: %v", err)
	}
}
