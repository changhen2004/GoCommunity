package article

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"

	"resource_community_go/internal/cachekey"
)

// 核心验证：多副本部署时，进程内 singleflight 各自为政，
// 只有 Redis 分布式锁能把“谁回源 MySQL”收口到全局唯一。
func TestServiceGetDetailCoalescesAcrossInstancesWithRedisLock(t *testing.T) {
	db, redisDB := newArticleDetailCacheTestStore(t)
	article := createArticleDetailCacheArticle(t, db)

	// 模拟两个部署实例：各自的 Service 持有独立的内存 singleflight 状态，
	// 共享同一个 DB 与 Redis —— 跨实例合并只能依赖 Redis 锁。
	instanceA := NewService(NewRepo(db, redisDB), nil, nil)
	instanceB := NewService(NewRepo(db, redisDB), nil, nil)

	var articleQueries atomic.Int64
	if err := db.Callback().Query().Before("gorm:query").Register(
		"article_detail_distributed_query_counter",
		func(tx *gorm.DB) {
			if tx.Statement.Table == "articles" {
				articleQueries.Add(1)
			}
		},
	); err != nil {
		t.Fatalf("register query counter: %v", err)
	}

	articleID := strconv.FormatUint(uint64(article.ID), 10)

	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			svc := instanceA
			if seq%2 == 1 {
				svc = instanceB
			}
			_, err := svc.GetDetail(articleID, 0)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatalf("expected detail response, got %v", err)
		}
	}

	if got := articleQueries.Load(); got != 1 {
		t.Fatalf("expected exactly one DB query across two instances via Redis lock, got %d", got)
	}

	// 所有请求结束后锁应已被释放（幂等 Unlock 生效，无残留）
	if n, err := redisDB.Exists(context.Background(), cachekey.ArticleDetailLockKey(articleID)).Result(); err != nil || n != 0 {
		t.Fatalf("expected lock released after fill, exists=%d err=%v", n, err)
	}

	// 缓存已重建，后续请求直接命中，不再触碰 DB
	before := articleQueries.Load()
	if _, err := instanceA.GetDetail(articleID, 0); err != nil {
		t.Fatalf("cached detail: %v", err)
	}
	if after := articleQueries.Load(); after != before {
		t.Fatalf("expected cache hit not to touch DB, queries %d -> %d", before, after)
	}
}
