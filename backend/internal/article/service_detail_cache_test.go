package article

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"resource_community_go/internal/asyncjob"
	"resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"
)

type recordingArticleDetailPublisher struct {
	mu   sync.Mutex
	jobs []asyncjob.Job
}

func (p *recordingArticleDetailPublisher) Publish(_ context.Context, job asyncjob.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = append(p.jobs, job)
	return nil
}

func (p *recordingArticleDetailPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = nil
}

func (p *recordingArticleDetailPublisher) count(jobType asyncjob.Type) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, job := range p.jobs {
		if job.Type == jobType {
			count++
		}
	}
	return count
}

func newArticleDetailCacheTestStore(t *testing.T) (*gorm.DB, *redis.Client) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&auth.User{}, &Article{}, &ArticleUnlock{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	redisServer := miniredis.RunT(t)
	redisDB := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisDB.Close() })

	return db, redisDB
}

func createArticleDetailCacheArticle(t *testing.T, db *gorm.DB) Article {
	t.Helper()

	author := auth.User{Username: "cache-author", Password: "hashed-password"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	article := Article{
		AuthorID: author.ID,
		Title:    "Cache Governance",
		Content:  "content",
		Preview:  "preview",
		Status:   "published",
		IsFree:   true,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}
	return article
}

func TestServiceGetDetailCachesMissingArticleSentinel(t *testing.T) {
	db, redisDB := newArticleDetailCacheTestStore(t)
	service := NewService(NewRepo(db, redisDB), nil, nil)

	_, err := service.GetDetail("404", 0)
	if !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("expected ErrArticleNotFound, got %v", err)
	}

	cached, cacheErr := redisDB.Get(context.Background(), cachekey.ArticleDetailKey("404")).Result()
	if cacheErr != nil {
		t.Fatalf("expected missing article sentinel cache, got %v", cacheErr)
	}
	if cached != cachekey.CacheNullValue {
		t.Fatalf("expected null sentinel, got %q", cached)
	}
}

func TestJitterTTLStaysWithinExpectedRange(t *testing.T) {
	base := 10 * time.Minute
	for i := 0; i < 100; i++ {
		ttl := cachekey.JitterTTL(base)
		if ttl < 10*time.Minute || ttl > 12*time.Minute {
			t.Fatalf("expected jittered ttl between 10m and 12m, got %s", ttl)
		}
	}
}

func TestServiceGetDetailCoalescesConcurrentCacheMisses(t *testing.T) {
	db, redisDB := newArticleDetailCacheTestStore(t)
	article := createArticleDetailCacheArticle(t, db)
	publisher := &recordingArticleDetailPublisher{}
	service := NewService(NewRepo(db, redisDB), publisher, nil)

	var articleQueries atomic.Int64
	if err := db.Callback().Query().Before("gorm:query").Register("article_detail_query_counter", func(tx *gorm.DB) {
		if tx.Statement.Table == "articles" {
			articleQueries.Add(1)
		}
	}); err != nil {
		t.Fatalf("register query counter: %v", err)
	}

	articleID := strconv.FormatUint(uint64(article.ID), 10)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.GetDetail(articleID, 0)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatalf("expected detail response, got %v", err)
		}
	}

	if got := articleQueries.Load(); got != 1 {
		t.Fatalf("expected one article DB query after coalescing cache miss, got %d", got)
	}
	if got := publisher.count(asyncjob.TypeArticleViewed); got != 16 {
		t.Fatalf("expected every detail request to publish a view event, got %d", got)
	}

	cached, err := redisDB.Get(context.Background(), cachekey.ArticleDetailKey(articleID)).Result()
	if err != nil || cached == cachekey.CacheNullValue {
		t.Fatalf("expected article detail cache, got cached=%q err=%v", cached, err)
	}
}

func TestServiceGetDetailPublishesViewEventOnCacheHit(t *testing.T) {
	db, redisDB := newArticleDetailCacheTestStore(t)
	article := createArticleDetailCacheArticle(t, db)
	publisher := &recordingArticleDetailPublisher{}
	service := NewService(NewRepo(db, redisDB), publisher, nil)

	articleID := strconv.FormatUint(uint64(article.ID), 10)
	if _, err := service.GetDetail(articleID, 0); err != nil {
		t.Fatalf("warm detail cache: %v", err)
	}
	publisher.reset()

	if _, err := service.GetDetail(articleID, 0); err != nil {
		t.Fatalf("get cached detail: %v", err)
	}

	if got := publisher.count(asyncjob.TypeArticleViewed); got != 1 {
		t.Fatalf("expected cached detail request to publish one view event, got %d", got)
	}
}

func TestServiceGetDetailDoesNotRecordDraftViews(t *testing.T) {
	db, redisDB := newArticleDetailCacheTestStore(t)
	author := auth.User{Username: "draft-author", Password: "hashed-password"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}
	draft := Article{
		AuthorID: author.ID,
		Title:    "Draft",
		Content:  "draft content",
		Preview:  "draft preview",
		Status:   "draft",
		IsFree:   true,
	}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	publisher := &recordingArticleDetailPublisher{}
	service := NewService(NewRepo(db, redisDB), publisher, nil)

	articleID := strconv.FormatUint(uint64(draft.ID), 10)
	resp, err := service.GetDetail(articleID, author.ID)
	if err != nil {
		t.Fatalf("get author draft detail: %v", err)
	}
	if resp.Content != "draft content" {
		t.Fatalf("expected author to receive draft content, got %q", resp.Content)
	}
	if got := publisher.count(asyncjob.TypeArticleViewed); got != 0 {
		t.Fatalf("expected draft detail not to publish view event, got %d", got)
	}

	var reloaded Article
	if err := db.First(&reloaded, draft.ID).Error; err != nil {
		t.Fatalf("reload draft: %v", err)
	}
	if reloaded.ViewCount != 0 {
		t.Fatalf("expected draft view count to remain 0, got %d", reloaded.ViewCount)
	}
}
