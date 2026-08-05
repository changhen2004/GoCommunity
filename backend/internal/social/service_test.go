package social

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"
)

func newSocialTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&auth.User{}, &Follow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func createSocialTestUser(t *testing.T, db *gorm.DB, username string) auth.User {
	t.Helper()

	user := auth.User{Username: username, Password: "hashed-password"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user
}

func TestServiceFollowIsIdempotentAndInvalidatesFollowingFeedCache(t *testing.T) {
	db := newSocialTestDB(t)
	viewer := createSocialTestUser(t, db, "viewer")
	author := createSocialTestUser(t, db, "author")

	redisServer := miniredis.RunT(t)
	redisDB := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisDB.Close() })

	ctx := context.Background()
	cacheKey := cachekey.ArticleFollowingKey(viewer.ID, 10, "", 0)
	if err := redisDB.Set(ctx, cacheKey, "cached", 0).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	service := NewService(NewRepo(db, redisDB), auth.NewRepo(db, nil))
	for i := 0; i < 2; i++ {
		if err := service.Follow(ctx, viewer.ID, author.ID); err != nil {
			t.Fatalf("follow attempt %d: %v", i+1, err)
		}
	}

	var count int64
	if err := db.Model(&Follow{}).
		Where("follower_id = ? AND author_id = ?", viewer.ID, author.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one follow row, got %d", count)
	}
	if exists := redisDB.Exists(ctx, cacheKey).Val(); exists != 0 {
		t.Fatalf("expected following feed cache to be invalidated")
	}
}

func TestServiceRejectsFollowingSelf(t *testing.T) {
	db := newSocialTestDB(t)
	viewer := createSocialTestUser(t, db, "viewer")

	service := NewService(NewRepo(db, nil), auth.NewRepo(db, nil))
	if err := service.Follow(context.Background(), viewer.ID, viewer.ID); err == nil {
		t.Fatal("expected following self to fail")
	}
}

func TestServiceCountsAndStatusReflectFollowState(t *testing.T) {
	db := newSocialTestDB(t)
	viewer := createSocialTestUser(t, db, "viewer")
	author := createSocialTestUser(t, db, "author")

	service := NewService(NewRepo(db, nil), auth.NewRepo(db, nil))
	if err := service.Follow(context.Background(), viewer.ID, author.ID); err != nil {
		t.Fatalf("follow: %v", err)
	}

	status, err := service.GetAuthorSocialStatus(context.Background(), viewer.ID, author.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.IsFollowing {
		t.Fatalf("expected viewer to follow author")
	}
	if status.FollowerCount != 1 {
		t.Fatalf("expected author follower count 1, got %d", status.FollowerCount)
	}
	if status.FollowingCount != 0 {
		t.Fatalf("expected author following count 0, got %d", status.FollowingCount)
	}

	if err := service.Unfollow(context.Background(), viewer.ID, author.ID); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	status, err = service.GetAuthorSocialStatus(context.Background(), viewer.ID, author.ID)
	if err != nil {
		t.Fatalf("status after unfollow: %v", err)
	}
	if status.IsFollowing {
		t.Fatalf("expected viewer not to follow author")
	}
	if status.FollowerCount != 0 {
		t.Fatalf("expected author follower count 0, got %d", status.FollowerCount)
	}
}
