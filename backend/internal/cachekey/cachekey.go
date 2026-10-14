package cachekey

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ArticleListPrefix      = "articles:list:"
	ArticleDetailPrefix    = "articles:detail:"
	ArticleHotPrefix       = "articles:hot:cache:"
	ArticleFollowingPrefix = "articles:following:"
	ArticleHotZSetKey      = "articles:hot:zset"
	PointsSummaryPrefix    = "points:summary:"
	CacheNullValue         = "__NULL__"
)

// 分布式锁前缀独立于详情缓存前缀：避免被按前缀删除详情缓存的逻辑（SCAN + DEL）
// 误删到锁 key。锁 key 与缓存的“失效即删”生命周期不同，必须隔离。
const ArticleDetailLockPrefix = "locks:article:detail:"

const (
	ArticleListTTL       = 5 * time.Minute
	ArticleDetailTTL     = 10 * time.Minute
	ArticleDetailNullTTL = 30 * time.Second
	ArticleDetailLockTTL = 30 * time.Second
	ArticleHotTTL        = 3 * time.Minute
	ArticleFollowingTTL  = 45 * time.Second
	PointsSummaryTTL     = 2 * time.Minute
)

func ArticleListKey(page, pageSize int, sort, keyword, tag string) string {
	return fmt.Sprintf(
		"%spage=%d:size=%d:sort=%s:keyword=%s:tag=%s",
		ArticleListPrefix,
		page,
		pageSize,
		sort,
		keyword,
		tag,
	)
}

func ArticleDetailKey(articleID string) string {
	return ArticleDetailPrefix + articleID
}

// ArticleDetailLockKey 热点详情跨实例回源合并用的分布式锁 key
func ArticleDetailLockKey(articleID string) string {
	return ArticleDetailLockPrefix + articleID
}

func ArticleHotKey(limit int) string {
	return fmt.Sprintf("%slimit=%d", ArticleHotPrefix, limit)
}

func ArticleFollowingPrefixForUser(userID uint) string {
	return fmt.Sprintf("%suser=%d:", ArticleFollowingPrefix, userID)
}

func ArticleFollowingKey(userID uint, pageSize int, beforeCreatedAt string, beforeID uint) string {
	return fmt.Sprintf(
		"%ssize=%d:beforeCreatedAt=%s:beforeID=%d",
		ArticleFollowingPrefixForUser(userID),
		pageSize,
		beforeCreatedAt,
		beforeID,
	)
}

func PointsSummaryKey(userID uint) string {
	return fmt.Sprintf("%s%d", PointsSummaryPrefix, userID)
}

func JitterTTL(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}

	maxJitter := base / 5
	if maxJitter <= 0 {
		return base
	}

	return base + time.Duration(rand.Int63n(int64(maxJitter)+1))
}

func DeleteByPrefix(ctx context.Context, redisDB *redis.Client, prefix string) {
	if redisDB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// COUNT reduces round trips in a large keyspace; invalidation still scans the keyspace.
	iter := redisDB.Scan(ctx, 0, prefix+"*", 1000).Iterator()
	keys := make([]string, 0)
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if len(keys) > 0 {
		_ = redisDB.Del(ctx, keys...).Err()
	}
}

func DeleteKeys(ctx context.Context, redisDB *redis.Client, keys ...string) {
	if redisDB == nil || len(keys) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = redisDB.Del(ctx, keys...).Err()
}
