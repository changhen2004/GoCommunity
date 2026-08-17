package config

import (
	"strings"
	"testing"

	"resource_community_go/internal/article"
	"resource_community_go/internal/auth"
	"resource_community_go/internal/comment"
	"resource_community_go/internal/favorite"
	"resource_community_go/internal/points"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	return db
}

func TestMigrateEnforcesUniqueUsername(t *testing.T) {
	db := openTestDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	first := auth.User{Username: "alice", Password: "secret123"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first user: %v", err)
	}

	second := auth.User{Username: "alice", Password: "secret456"}
	err := db.Create(&second).Error
	if err == nil {
		t.Fatal("expected duplicate username insert to fail")
	}

	if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("expected unique constraint error, got: %v", err)
	}
}

func TestMigrateAddsArticleOwnershipAndStatsFields(t *testing.T) {
	db := openTestDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	if !db.Migrator().HasColumn(&article.Article{}, "author_id") {
		t.Fatal("expected author_id column to exist")
	}
	if !db.Migrator().HasColumn(&auth.User{}, "points") {
		t.Fatal("expected user points column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "status") {
		t.Fatal("expected status column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "cover_url") {
		t.Fatal("expected cover_url column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "content_images") {
		t.Fatal("expected content_images column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "tags") {
		t.Fatal("expected tags column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "view_count") {
		t.Fatal("expected view_count column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "like_count") {
		t.Fatal("expected like_count column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "favorite_count") {
		t.Fatal("expected favorite_count column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "is_free") {
		t.Fatal("expected is_free column to exist")
	}
	if !db.Migrator().HasColumn(&article.Article{}, "required_points") {
		t.Fatal("expected required_points column to exist")
	}

	if !db.Migrator().HasIndex(&article.Article{}, "idx_articles_author_id") {
		t.Fatal("expected author_id index to exist")
	}
	if !db.Migrator().HasIndex(&article.Article{}, "idx_articles_status") {
		t.Fatal("expected status index to exist")
	}
	if !db.Migrator().HasIndex(&article.Article{}, "idx_articles_tags") {
		t.Fatal("expected tags index to exist")
	}
	if !db.Migrator().HasIndex(&article.Article{}, "idx_articles_is_free") {
		t.Fatal("expected is_free index to exist")
	}
	if !db.Migrator().HasTable(&article.ArticleUnlock{}) {
		t.Fatal("expected article_unlocks table to exist")
	}
	if !db.Migrator().HasTable(&article.ArticleLike{}) {
		t.Fatal("expected article_likes table to exist")
	}
	if !db.Migrator().HasIndex(&article.ArticleLike{}, "idx_article_likes_article_user") {
		t.Fatal("expected article_likes article/user unique index to exist")
	}
	if !db.Migrator().HasTable(&comment.Comment{}) {
		t.Fatal("expected comments table to exist")
	}
	if !db.Migrator().HasTable(&favorite.Favorite{}) {
		t.Fatal("expected favorites table to exist")
	}
	if !db.Migrator().HasTable(&points.PointLedger{}) {
		t.Fatal("expected point_ledgers table to exist")
	}
	if !db.Migrator().HasTable(&points.UserCheckIn{}) {
		t.Fatal("expected user_check_ins table to exist")
	}
	if !db.Migrator().HasTable(&points.UserPrivilege{}) {
		t.Fatal("expected user_privileges table to exist")
	}

	createdArticle := article.Article{
		AuthorID:       99,
		Title:          "Daily Exchange Update",
		Content:        "Full article body",
		Preview:        "Preview text",
		CoverURL:       "/uploads/covers/demo.png",
		ContentImages:  "/uploads/content/body-1.png,/uploads/content/body-2.png",
		Tags:           "go,backend",
		IsFree:         false,
		RequiredPoints: 12,
	}
	if err := db.Create(&createdArticle).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	var saved article.Article
	if err := db.First(&saved, createdArticle.ID).Error; err != nil {
		t.Fatalf("reload article: %v", err)
	}

	if saved.AuthorID != 99 {
		t.Fatalf("expected author id 99, got %d", saved.AuthorID)
	}
	if saved.Status != "draft" {
		t.Fatalf("expected default status draft, got %q", saved.Status)
	}
	if saved.Tags != "go,backend" {
		t.Fatalf("expected tags go,backend, got %q", saved.Tags)
	}
	if saved.CoverURL != "/uploads/covers/demo.png" {
		t.Fatalf("expected cover url to persist, got %q", saved.CoverURL)
	}
	if saved.ContentImages != "/uploads/content/body-1.png,/uploads/content/body-2.png" {
		t.Fatalf("expected content images to persist, got %q", saved.ContentImages)
	}
	if saved.ViewCount != 0 {
		t.Fatalf("expected default view count 0, got %d", saved.ViewCount)
	}
	if saved.LikeCount != 0 {
		t.Fatalf("expected default like count 0, got %d", saved.LikeCount)
	}
	if saved.FavoriteCount != 0 {
		t.Fatalf("expected default favorite count 0, got %d", saved.FavoriteCount)
	}
	if saved.IsFree != false {
		t.Fatalf("expected isFree false, got %v", saved.IsFree)
	}
	if saved.RequiredPoints != 12 {
		t.Fatalf("expected required points 12, got %d", saved.RequiredPoints)
	}
}
