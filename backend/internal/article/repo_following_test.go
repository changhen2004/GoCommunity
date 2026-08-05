package article

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"resource_community_go/internal/auth"
	"resource_community_go/internal/social"
)

func newFollowingFeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&auth.User{}, &Article{}, &social.Follow{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func createFollowingFeedUser(t *testing.T, db *gorm.DB, username string) auth.User {
	t.Helper()

	user := auth.User{Username: username, Password: "hashed-password"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user
}

func createFollowingFeedArticle(t *testing.T, db *gorm.DB, authorID uint, title string, createdAt time.Time) Article {
	t.Helper()

	return createFollowingFeedArticleWithStatus(t, db, authorID, title, createdAt, "published")
}

func createFollowingFeedArticleWithStatus(t *testing.T, db *gorm.DB, authorID uint, title string, createdAt time.Time, status string) Article {
	t.Helper()

	article := Article{
		Model:    gorm.Model{CreatedAt: createdAt, UpdatedAt: createdAt},
		AuthorID: authorID,
		Title:    title,
		Content:  "content",
		Preview:  "preview",
		Status:   status,
		IsFree:   true,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article %s: %v", title, err)
	}
	return article
}

func TestRepoListFollowingOnlyReturnsFollowedAuthorsInCursorOrder(t *testing.T) {
	db := newFollowingFeedTestDB(t)
	viewer := createFollowingFeedUser(t, db, "viewer")
	followedAuthor := createFollowingFeedUser(t, db, "followed")
	otherAuthor := createFollowingFeedUser(t, db, "other")

	if err := db.Create(&social.Follow{FollowerID: viewer.ID, AuthorID: followedAuthor.ID}).Error; err != nil {
		t.Fatalf("create follow: %v", err)
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	oldFollowed := createFollowingFeedArticle(t, db, followedAuthor.ID, "old-followed", now.Add(-2*time.Hour))
	newFollowed := createFollowingFeedArticle(t, db, followedAuthor.ID, "new-followed", now.Add(-1*time.Hour))
	createFollowingFeedArticle(t, db, otherAuthor.ID, "other-newer", now)

	repo := NewRepo(db, nil)
	firstPage, err := repo.ListFollowing(context.Background(), NewFollowingFeedQuery(viewer.ID, 1, "", 0))
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("expected first page size 1, got %d", len(firstPage))
	}
	if firstPage[0].ID != newFollowed.ID {
		t.Fatalf("expected newest followed article %d, got %d", newFollowed.ID, firstPage[0].ID)
	}

	secondPage, err := repo.ListFollowing(
		context.Background(),
		NewFollowingFeedQuery(viewer.ID, 10, firstPage[0].CreatedAt.Format(time.RFC3339Nano), firstPage[0].ID),
	)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("expected second page size 1, got %d", len(secondPage))
	}
	if secondPage[0].ID != oldFollowed.ID {
		t.Fatalf("expected old followed article %d, got %d", oldFollowed.ID, secondPage[0].ID)
	}
}

func TestRepoListFollowingReturnsEmptyForViewerWithoutFollows(t *testing.T) {
	db := newFollowingFeedTestDB(t)
	viewer := createFollowingFeedUser(t, db, "viewer")
	author := createFollowingFeedUser(t, db, "author")
	createFollowingFeedArticle(t, db, author.ID, "article", time.Now().UTC())

	repo := NewRepo(db, nil)
	articles, err := repo.ListFollowing(context.Background(), NewFollowingFeedQuery(viewer.ID, 10, "", 0))
	if err != nil {
		t.Fatalf("list following: %v", err)
	}
	if len(articles) != 0 {
		t.Fatalf("expected empty following feed, got %d articles", len(articles))
	}
}

func TestRepoListFollowingOnlyReturnsPublishedArticles(t *testing.T) {
	db := newFollowingFeedTestDB(t)
	viewer := createFollowingFeedUser(t, db, "viewer")
	author := createFollowingFeedUser(t, db, "author")

	if err := db.Create(&social.Follow{FollowerID: viewer.ID, AuthorID: author.ID}).Error; err != nil {
		t.Fatalf("create follow: %v", err)
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	createFollowingFeedArticleWithStatus(t, db, author.ID, "draft", now, "draft")
	published := createFollowingFeedArticleWithStatus(t, db, author.ID, "published", now.Add(-time.Minute), "published")
	createFollowingFeedArticleWithStatus(t, db, author.ID, "archived", now.Add(-2*time.Minute), "archived")

	repo := NewRepo(db, nil)
	articles, err := repo.ListFollowing(context.Background(), NewFollowingFeedQuery(viewer.ID, 10, "", 0))
	if err != nil {
		t.Fatalf("list following: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected only one published article, got %d", len(articles))
	}
	if articles[0].ID != published.ID {
		t.Fatalf("expected published article %d, got %d", published.ID, articles[0].ID)
	}
}
