package article

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"resource_community_go/internal/asyncjob"
	"resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"
	"resource_community_go/internal/social"
)

func TestServiceCreateInvalidatesFollowingFeedCache(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&auth.User{}, &Article{}, &social.Follow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	author := auth.User{Username: "author", Password: "hashed-password"}
	follower := auth.User{Username: "follower", Password: "hashed-password"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}
	if err := db.Create(&follower).Error; err != nil {
		t.Fatalf("create follower: %v", err)
	}
	if err := db.Create(&social.Follow{FollowerID: follower.ID, AuthorID: author.ID}).Error; err != nil {
		t.Fatalf("create follow: %v", err)
	}

	redisServer := miniredis.RunT(t)
	redisDB := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisDB.Close() })

	ctx := context.WithValue(context.Background(), "userID", author.ID)
	cacheKey := cachekey.ArticleFollowingKey(follower.ID, 10, "", 0)
	if err := redisDB.Set(ctx, cacheKey, "cached", 0).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	service := NewService(NewRepo(db, redisDB), nil, nil)
	if _, err := service.Create(ctx, CreateArticleRequest{
		Title:   "new article",
		Content: "content",
		Preview: "preview",
		Status:  "published",
	}); err != nil {
		t.Fatalf("create article: %v", err)
	}

	if exists := redisDB.Exists(context.Background(), cacheKey).Val(); exists != 0 {
		t.Fatalf("expected following feed cache to be invalidated after publish")
	}
}

func TestServiceCreateDoesNotPublishArticlePublishedEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&auth.User{}, &Article{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	author := auth.User{Username: "author", Password: "hashed-password"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	publisher := &recordingArticleDetailPublisher{}
	service := NewService(NewRepo(db, nil), publisher, nil)
	ctx := context.WithValue(context.Background(), "userID", author.ID)

	if _, err := service.Create(ctx, CreateArticleRequest{
		Title:   "draft",
		Content: "draft content",
		Preview: "draft preview",
		Status:  "draft",
	}); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if got := publisher.count(asyncjob.TypeArticlePublished); got != 0 {
		t.Fatalf("expected draft create not to publish article.published, got %d", got)
	}

	if _, err := service.Create(ctx, CreateArticleRequest{
		Title:   "published",
		Content: "published content",
		Preview: "published preview",
		Status:  "published",
	}); err != nil {
		t.Fatalf("create published: %v", err)
	}
	if got := publisher.count(asyncjob.TypeArticlePublished); got != 0 {
		t.Fatalf("expected published create not to publish article.published, got %d", got)
	}
}
