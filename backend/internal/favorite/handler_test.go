package favorite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	internalArticle "resource_community_go/internal/article"
	internalAuth "resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupFavoriteTestHandler(t *testing.T, withRedis bool) (*Handler, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&internalAuth.User{}, &internalArticle.Article{}, &Favorite{}); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}

	var redisClient *redis.Client
	if withRedis {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("start miniredis: %v", err)
		}
		t.Cleanup(mr.Close)
		redisClient = redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = redisClient.Close() })
	}

	articleService := internalArticle.NewService(internalArticle.NewRepo(db, redisClient), nil, nil)
	return NewHandler(NewService(NewRepo(db, redisClient), articleService, nil)), db
}

func TestFavoriteLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupFavoriteTestHandler(t, false)

	author := internalAuth.User{Username: "author", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}
	user := internalAuth.User{Username: "reader", Password: "secret123"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create reader: %v", err)
	}

	article := internalArticle.Article{
		AuthorID:       author.ID,
		Title:          "Premium Resource",
		Content:        "Body",
		Preview:        "Preview",
		Tags:           "go,resource",
		Status:         "published",
		IsFree:         false,
		RequiredPoints: 10,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles/:id/favorite", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.CreateFavorite(ctx)
	})
	router.DELETE("/api/articles/:id/favorite", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.DeleteFavorite(ctx)
	})
	router.GET("/api/me/favorites", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.ListMyFavorites(ctx)
	})

	createReq := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/favorite", nil)
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", createResp.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/me/favorites", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listResp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	items := envelope["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 favorite, got %d", len(items))
	}
	entry := items[0].(map[string]any)
	if entry["title"] != "Premium Resource" {
		t.Fatalf("unexpected favorite title: %v", entry["title"])
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/favorite", nil)
	deleteResp := httptest.NewRecorder()
	router.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", deleteResp.Code)
	}

	var saved internalArticle.Article
	if err := db.First(&saved, article.ID).Error; err != nil {
		t.Fatalf("reload article: %v", err)
	}
	if saved.FavoriteCount != 0 {
		t.Fatalf("expected favorite count 0, got %d", saved.FavoriteCount)
	}
}

func TestDraftArticleCannotBeFavorited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupFavoriteTestHandler(t, false)

	author := internalAuth.User{Username: "draft-favorite-author", Password: "secret123"}
	user := internalAuth.User{Username: "draft-favorite-reader", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create reader: %v", err)
	}

	draft := internalArticle.Article{
		AuthorID: author.ID,
		Title:    "Draft Favorite Target",
		Content:  "Body",
		Preview:  "Preview",
		Status:   "draft",
		IsFree:   true,
	}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles/:id/favorite", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.CreateFavorite(ctx)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(draft.ID), 10)+"/favorite", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected draft favorite to return 404, got %d", resp.Code)
	}

	var reloaded internalArticle.Article
	if err := db.First(&reloaded, draft.ID).Error; err != nil {
		t.Fatalf("reload draft: %v", err)
	}
	if reloaded.FavoriteCount != 0 {
		t.Fatalf("expected draft favorite count to remain 0, got %d", reloaded.FavoriteCount)
	}
}

func TestFavoriteInvalidatesArticleCaches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupFavoriteTestHandler(t, true)

	author := internalAuth.User{Username: "author-cache", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}
	user := internalAuth.User{Username: "reader-cache", Password: "secret123"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create reader: %v", err)
	}

	articles := []internalArticle.Article{
		{
			AuthorID:  author.ID,
			Title:     "Favorite Cache Target",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "go,cache",
			Status:    "published",
			ViewCount: 12,
			LikeCount: 3,
		},
		{
			AuthorID:  author.ID,
			Title:     "Unrelated Favorite Cache",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "go,detail",
			Status:    "published",
			ViewCount: 4,
			LikeCount: 1,
		},
	}
	if err := db.Create(&articles).Error; err != nil {
		t.Fatalf("seed articles: %v", err)
	}

	repo := handler.service.repo
	ctx := context.Background()
	listKey := cachekey.ArticleListKey(1, 10, "latest", "", "")
	hotKey := cachekey.ArticleHotKey(10)
	targetDetailKey := cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articles[0].ID), 10))
	unrelatedDetailKey := cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articles[1].ID), 10))

	if err := repo.redisDB.Set(ctx, listKey, "list-cache", 0).Err(); err != nil {
		t.Fatalf("seed list cache: %v", err)
	}
	if err := repo.redisDB.Set(ctx, hotKey, "hot-cache", 0).Err(); err != nil {
		t.Fatalf("seed hot cache: %v", err)
	}
	if err := repo.redisDB.Set(ctx, targetDetailKey, "detail-cache", 0).Err(); err != nil {
		t.Fatalf("seed target detail cache: %v", err)
	}
	if err := repo.redisDB.Set(ctx, unrelatedDetailKey, "other-detail-cache", 0).Err(); err != nil {
		t.Fatalf("seed unrelated detail cache: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles/:id/favorite", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.CreateFavorite(ctx)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(articles[0].ID), 10)+"/favorite", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.Code)
	}

	if exists, err := repo.redisDB.Exists(ctx, listKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected list cache invalidated, exists=%d err=%v", exists, err)
	}
	if exists, err := repo.redisDB.Exists(ctx, hotKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected hot cache invalidated, exists=%d err=%v", exists, err)
	}
	if exists, err := repo.redisDB.Exists(ctx, targetDetailKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected target detail cache invalidated, exists=%d err=%v", exists, err)
	}
	if exists, err := repo.redisDB.Exists(ctx, unrelatedDetailKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected unrelated detail cache preserved, exists=%d err=%v", exists, err)
	}
}
