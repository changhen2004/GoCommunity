package worker

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisIdempotencyStoreSkipsDuplicateJob(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	store := NewRedisIdempotencyStore(client)
	ctx := context.Background()
	jobID := "job-123"

	result, err := store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin first job: %v", err)
	}
	if result != BeginAcquired {
		t.Fatalf("expected first begin to acquire job, got %d", result)
	}

	result, err = store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin duplicate job: %v", err)
	}
	if result != BeginBusy {
		t.Fatalf("expected processing duplicate to be busy, got %d", result)
	}
	if ttl := mr.TTL(store.prefix + jobID); ttl != idempotencyProcessingTTL {
		t.Fatalf("expected processing TTL %s, got %s", idempotencyProcessingTTL, ttl)
	}
	if other, err := store.Begin(ctx, "another-job"); err != nil || other != BeginAcquired {
		t.Fatalf("expected a different job to acquire independently, got %d, err=%v", other, err)
	}

	if err := store.Complete(ctx, jobID); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	result, err = store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin completed duplicate job: %v", err)
	}
	if result != BeginDone {
		t.Fatalf("expected completed duplicate to be done, got %d", result)
	}
	if ttl := mr.TTL(store.prefix + jobID); ttl != idempotencyDoneTTL {
		t.Fatalf("expected done TTL %s, got %s", idempotencyDoneTTL, ttl)
	}
}

func TestRedisIdempotencyStoreReleaseAllowsRetry(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	store := NewRedisIdempotencyStore(client)
	ctx := context.Background()
	jobID := "job-retry"

	result, err := store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin job: %v", err)
	}
	if result != BeginAcquired {
		t.Fatalf("expected first begin to acquire job, got %d", result)
	}

	if err := store.Release(ctx, jobID); err != nil {
		t.Fatalf("release job: %v", err)
	}

	result, err = store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin released job: %v", err)
	}
	if result != BeginAcquired {
		t.Fatalf("expected released job to acquire again, got %d", result)
	}
}
