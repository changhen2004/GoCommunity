package cachekey

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestDeleteHotCachePreservesRankingAndCoordinationKeys(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	cache := ArticleHotKey(10)
	preserved := []string{ArticleDetailKey("1"), ArticleDetailLockKey("1"), "asyncjob:processed:job-1"}
	for _, key := range append(preserved, cache) {
		if err := client.Set(ctx, key, "value", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.ZAdd(ctx, ArticleHotZSetKey, redis.Z{Score: 100, Member: "1"}).Err(); err != nil {
		t.Fatal(err)
	}

	DeleteByPrefix(ctx, client, ArticleHotPrefix)
	if count, err := client.Exists(ctx, cache).Result(); err != nil || count != 0 {
		t.Fatalf("hot response cache was not invalidated: exists=%d err=%v", count, err)
	}
	if score, err := client.ZScore(ctx, ArticleHotZSetKey, "1").Result(); err != nil || score != 100 {
		t.Fatalf("ranking data changed during cache invalidation: score=%v err=%v", score, err)
	}
	for _, key := range preserved {
		if value, err := client.Get(ctx, key).Result(); err != nil || value != "value" {
			t.Fatalf("unrelated key %q changed: value=%q err=%v", key, value, err)
		}
	}
}
