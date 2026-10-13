package worker

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	idempotencyProcessingTTL = 10 * time.Minute
	idempotencyDoneTTL       = 24 * time.Hour
)

type BeginResult int

const (
	BeginAcquired BeginResult = iota
	BeginBusy
	BeginDone
)

type IdempotencyStore interface {
	Begin(ctx context.Context, jobID string) (BeginResult, error)
	Complete(ctx context.Context, jobID string) error
	Release(ctx context.Context, jobID string) error
}

type NoopIdempotencyStore struct{}

func (NoopIdempotencyStore) Begin(context.Context, string) (BeginResult, error) {
	return BeginAcquired, nil
}

func (NoopIdempotencyStore) Complete(context.Context, string) error {
	return nil
}

func (NoopIdempotencyStore) Release(context.Context, string) error {
	return nil
}

type RedisIdempotencyStore struct {
	client *redis.Client
	prefix string
}

func NewRedisIdempotencyStore(client *redis.Client) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{
		client: client,
		prefix: "asyncjob:processed:",
	}
}

var beginJobScript = redis.NewScript(`
local value = redis.call("GET", KEYS[1])
if value == "done" then
    return 2
end
if value then
    return 1
end
redis.call("SET", KEYS[1], "processing", "PX", ARGV[1])
return 0
`)

func (s *RedisIdempotencyStore) Begin(ctx context.Context, jobID string) (BeginResult, error) {
	if s == nil || s.client == nil || jobID == "" {
		return BeginAcquired, nil
	}

	result, err := beginJobScript.Run(
		ctx,
		s.client,
		[]string{s.prefix + jobID},
		idempotencyProcessingTTL.Milliseconds(),
	).Int()
	if err != nil {
		return BeginBusy, err
	}
	return BeginResult(result), nil
}

func (s *RedisIdempotencyStore) Complete(ctx context.Context, jobID string) error {
	if s == nil || s.client == nil || jobID == "" {
		return nil
	}

	return s.client.Set(ctx, s.prefix+jobID, "done", idempotencyDoneTTL).Err()
}

func (s *RedisIdempotencyStore) Release(ctx context.Context, jobID string) error {
	if s == nil || s.client == nil || jobID == "" {
		return nil
	}

	return s.client.Del(ctx, s.prefix+jobID).Err()
}
