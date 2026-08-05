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

	ok, err := store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin first job: %v", err)
	}
	if !ok {
		t.Fatal("expected first begin to acquire idempotency lock")
	}

	ok, err = store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin duplicate job: %v", err)
	}
	if ok {
		t.Fatal("expected duplicate begin to be rejected")
	}

	if err := store.Complete(ctx, jobID); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	ok, err = store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin completed duplicate job: %v", err)
	}
	if ok {
		t.Fatal("expected completed duplicate job to remain rejected")
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

	ok, err := store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin job: %v", err)
	}
	if !ok {
		t.Fatal("expected first begin to acquire idempotency lock")
	}

	if err := store.Release(ctx, jobID); err != nil {
		t.Fatalf("release job: %v", err)
	}

	ok, err = store.Begin(ctx, jobID)
	if err != nil {
		t.Fatalf("begin released job: %v", err)
	}
	if !ok {
		t.Fatal("expected released processing lock to allow retry")
	}
}
