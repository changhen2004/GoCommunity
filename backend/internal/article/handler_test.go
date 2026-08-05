package article

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	internalAuth "resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"
	internalPoints "resource_community_go/internal/points"
	"resource_community_go/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupArticleTestHandler(t *testing.T, withRedis bool) (*Handler, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&internalAuth.User{},
		&Article{},
		&ArticleUnlock{},
		&internalPoints.PointLedger{},
		&internalPoints.UserCheckIn{},
		&internalPoints.UserPrivilege{},
	); err != nil {
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

	pointsService := internalPoints.NewService(internalPoints.NewRepo(db, redisClient))
	return NewHandler(NewService(NewRepo(db, redisClient), nil, pointsService)), db
}

func TestCreateArticleUsesModuleRequestAndEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, false)

	author := internalAuth.User{Username: "author007", Password: "secret123", Points: 0}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles", func(ctx *gin.Context) {
		ctx.Set("userID", author.ID)
		ctx.Set("username", author.Username)
		handler.CreateArticle(ctx)
	})

	payload := map[string]any{
		"title":         "Daily Exchange Update",
		"content":       "Full content",
		"preview":       "Preview content",
		"coverUrl":      "/uploads/covers/cover.png",
		"contentImages": []string{"/uploads/content/body-1.png", "/uploads/content/body-2.png"},
		"tags":          []string{"forex", "daily"},
		"status":        "published",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/articles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if code, ok := envelope["code"].(float64); !ok || code != 0 {
		t.Fatalf("expected code 0, got %#v", envelope["code"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", envelope["data"])
	}
	if _, ok := data["id"]; !ok {
		t.Fatal("expected article id in response data")
	}

	var saved Article
	if err := db.First(&saved).Error; err != nil {
		t.Fatalf("load saved article: %v", err)
	}
	if saved.Title != "Daily Exchange Update" {
		t.Fatalf("unexpected saved title: %s", saved.Title)
	}
	if saved.Tags != "forex,daily" {
		t.Fatalf("unexpected saved tags: %s", saved.Tags)
	}
	if saved.CoverURL != "/uploads/covers/cover.png" {
		t.Fatalf("unexpected saved cover url: %s", saved.CoverURL)
	}
	if saved.ContentImages != "/uploads/content/body-1.png,/uploads/content/body-2.png" {
		t.Fatalf("unexpected saved content images: %s", saved.ContentImages)
	}
	if saved.AuthorID != author.ID {
		t.Fatalf("expected author id %d, got %d", author.ID, saved.AuthorID)
	}
}

func TestCreateArticleSupportsPaidAccessRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, false)

	author := internalAuth.User{Username: "paid-author", Password: "secret123", Points: 0}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles", func(ctx *gin.Context) {
		ctx.Set("userID", author.ID)
		ctx.Set("username", author.Username)
		handler.CreateArticle(ctx)
	})

	payload := map[string]any{
		"title":          "Paid Resource",
		"content":        "Premium content",
		"preview":        "Premium preview",
		"tags":           []string{"premium", "resource"},
		"isFree":         false,
		"requiredPoints": 18,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/articles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.Code)
	}

	var saved Article
	if err := db.Last(&saved).Error; err != nil {
		t.Fatalf("load saved article: %v", err)
	}
	if saved.IsFree {
		t.Fatal("expected article to require points")
	}
	if saved.RequiredPoints != 18 {
		t.Fatalf("expected requiredPoints 18, got %d", saved.RequiredPoints)
	}
	if saved.AuthorID != author.ID {
		t.Fatalf("expected author id %d, got %d", author.ID, saved.AuthorID)
	}
}

func TestCreateArticleRejectsInvalidContentAndPointsRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, false)

	author := internalAuth.User{Username: "validator01", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles", func(ctx *gin.Context) {
		ctx.Set("userID", author.ID)
		ctx.Set("username", author.Username)
		handler.CreateArticle(ctx)
	})

	tests := []map[string]any{
		{
			"title":   "Too Long Content",
			"content": strings.Repeat("a", 20001),
			"preview": "Preview",
		},
		{
			"title":          "Invalid Points",
			"content":        "Body",
			"preview":        "Preview",
			"isFree":         false,
			"requiredPoints": 0,
		},
		{
			"title":          "Excess Points",
			"content":        "Body",
			"preview":        "Preview",
			"requiredPoints": 10001,
		},
	}

	for _, payload := range tests {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/articles", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid payload %#v to return 400, got %d", payload, resp.Code)
		}
	}
}

func TestGetArticlesReturnsEnvelopeWithoutModelFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, false)

	if err := db.Create(&Article{
		AuthorID:      1,
		Title:         "Article One",
		Content:       "Body",
		Preview:       "Preview",
		CoverURL:      "/uploads/covers/list.png",
		ContentImages: "/uploads/content/list-1.png",
		Status:        "published",
		ViewCount:     10,
		LikeCount:     3,
		CommentCount:  2,
	}).Error; err != nil {
		t.Fatalf("seed article: %v", err)
	}

	router := gin.New()
	router.GET("/api/articles", handler.GetArticles)

	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	rawData, ok := envelope["data"].([]any)
	if !ok {
		t.Fatalf("expected data array, got %#v", envelope["data"])
	}
	entry := rawData[0].(map[string]any)
	if _, ok := entry["authorId"]; !ok {
		t.Fatal("expected authorId field in article list response")
	}
	if entry["coverUrl"] != "/uploads/covers/list.png" {
		t.Fatalf("expected coverUrl in list response, got %#v", entry["coverUrl"])
	}
	if images, ok := entry["contentImages"].([]any); !ok || len(images) != 1 {
		t.Fatalf("expected contentImages field in list response, got %#v", entry["contentImages"])
	}
	if entry["commentCount"] != float64(2) {
		t.Fatalf("expected commentCount in list response, got %#v", entry["commentCount"])
	}
	if _, ok := entry["CreatedAt"]; ok {
		t.Fatal("did not expect CreatedAt field in article list response")
	}
	if tags, ok := entry["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("expected tags field in article list response, got %#v", entry["tags"])
	}
}

func TestGetArticlesSupportsPaginationSortSearchAndTagFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, false)

	now := time.Now()
	seedArticles := []Article{
		{
			Model:     gorm.Model{CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
			AuthorID:  1,
			Title:     "Go Backend Guide",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "go,backend",
			Status:    "published",
			ViewCount: 10,
			LikeCount: 3,
		},
		{
			Model:     gorm.Model{CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
			AuthorID:  2,
			Title:     "Gin Routing Deep Dive",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "go,gin",
			Status:    "published",
			ViewCount: 25,
			LikeCount: 8,
		},
		{
			Model:     gorm.Model{CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
			AuthorID:  3,
			Title:     "Vue Community Design",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "frontend,vue",
			Status:    "published",
			ViewCount: 6,
			LikeCount: 1,
		},
	}
	if err := db.Create(&seedArticles).Error; err != nil {
		t.Fatalf("seed articles: %v", err)
	}

	router := gin.New()
	router.GET("/api/articles", handler.GetArticles)

	t.Run("pagination with latest sort", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles?page=2&pageSize=1&sort=latest", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		items := envelope["data"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 article, got %d", len(items))
		}
		entry := items[0].(map[string]any)
		if entry["title"] != "Gin Routing Deep Dive" {
			t.Fatalf("expected second latest article, got %v", entry["title"])
		}
	})

	t.Run("hot sort", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles?sort=hot&pageSize=3", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		items := envelope["data"].([]any)
		first := items[0].(map[string]any)
		if first["title"] != "Gin Routing Deep Dive" {
			t.Fatalf("expected hottest article first, got %v", first["title"])
		}
	})

	t.Run("keyword search", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles?keyword=Go", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		items := envelope["data"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 search result, got %d", len(items))
		}
		entry := items[0].(map[string]any)
		if entry["title"] != "Go Backend Guide" {
			t.Fatalf("expected Go Backend Guide, got %v", entry["title"])
		}
	})

	t.Run("tag filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles?tag=gin", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		items := envelope["data"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 tag result, got %d", len(items))
		}
		entry := items[0].(map[string]any)
		if entry["title"] != "Gin Routing Deep Dive" {
			t.Fatalf("expected gin-tagged article, got %v", entry["title"])
		}
	})
}

func TestGetArticleDetailShowsAccessRulesForGuestAndUnlockedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, true)

	author := internalAuth.User{Username: "author001", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	reader := internalAuth.User{Username: "reader001", Password: "secret123"}
	if err := db.Create(&reader).Error; err != nil {
		t.Fatalf("create reader: %v", err)
	}

	articleRecord := Article{
		AuthorID:       author.ID,
		Title:          "Premium Go Guide",
		Content:        "Full premium body",
		Preview:        "Premium preview",
		CoverURL:       "/uploads/covers/premium.png",
		ContentImages:  "/uploads/content/premium-1.png,/uploads/content/premium-2.png",
		Tags:           "go,premium",
		Status:         "published",
		ViewCount:      128,
		LikeCount:      15,
		CommentCount:   6,
		FavoriteCount:  9,
		IsFree:         false,
		RequiredPoints: 20,
	}
	if err := db.Create(&articleRecord).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	unlock := ArticleUnlock{ArticleID: articleRecord.ID, UserID: reader.ID}
	if err := db.Create(&unlock).Error; err != nil {
		t.Fatalf("create unlock: %v", err)
	}

	router := gin.New()
	router.GET("/api/articles/:id", handler.GetArticleByID)

	t.Run("guest sees locked paid article", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/articles/"+toID(articleRecord.ID), nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		data := envelope["data"].(map[string]any)
		if data["content"] != "Full premium body" {
			t.Fatalf("expected content in detail response, got %v", data["content"])
		}
		if data["coverUrl"] != "/uploads/covers/premium.png" {
			t.Fatalf("expected cover url in detail response, got %v", data["coverUrl"])
		}
		if images, ok := data["contentImages"].([]any); !ok || len(images) != 2 {
			t.Fatalf("expected 2 content images in detail response, got %#v", data["contentImages"])
		}
		authorData := data["author"].(map[string]any)
		if authorData["username"] != "author001" {
			t.Fatalf("expected author username author001, got %v", authorData["username"])
		}
		stats := data["stats"].(map[string]any)
		if stats["likeCount"] != float64(15) || stats["commentCount"] != float64(6) || stats["favoriteCount"] != float64(9) || stats["viewCount"] != float64(128) {
			t.Fatalf("unexpected stats payload: %#v", stats)
		}
		if data["isFree"] != false {
			t.Fatalf("expected paid article, got %v", data["isFree"])
		}
		if data["requiredPoints"] != float64(20) {
			t.Fatalf("expected requiredPoints 20, got %v", data["requiredPoints"])
		}
		if data["isUnlocked"] != false {
			t.Fatalf("expected guest to be locked, got %v", data["isUnlocked"])
		}
	})

	t.Run("unlocked user sees unlocked status", func(t *testing.T) {
		token, err := utils.GenerateJWT(reader.ID, reader.Username)
		if err != nil {
			t.Fatalf("generate jwt: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/articles/"+toID(articleRecord.ID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}

		data := envelope["data"].(map[string]any)
		if data["isUnlocked"] != true {
			t.Fatalf("expected unlocked user to see isUnlocked=true, got %v", data["isUnlocked"])
		}
	})

	t.Run("repeated detail access does not change unlocked state", func(t *testing.T) {
		token, err := utils.GenerateJWT(reader.ID, reader.Username)
		if err != nil {
			t.Fatalf("generate jwt: %v", err)
		}

		req1 := httptest.NewRequest(http.MethodGet, "/api/articles/"+toID(articleRecord.ID), nil)
		req1.Header.Set("Authorization", "Bearer "+token)
		resp1 := httptest.NewRecorder()
		router.ServeHTTP(resp1, req1)

		req2 := httptest.NewRequest(http.MethodGet, "/api/articles/"+toID(articleRecord.ID), nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2 := httptest.NewRecorder()
		router.ServeHTTP(resp2, req2)

		if resp1.Code != http.StatusOK || resp2.Code != http.StatusOK {
			t.Fatalf("expected repeated access to remain 200, got %d and %d", resp1.Code, resp2.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(resp2.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		data := envelope["data"].(map[string]any)
		if data["isUnlocked"] != true {
			t.Fatalf("expected repeated access to stay unlocked, got %v", data["isUnlocked"])
		}
	})
}

func TestGetHotArticlesUsesRedisZSetRanking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, true)

	now := time.Now()
	articles := []Article{
		{
			Model:     gorm.Model{CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
			AuthorID:  1,
			Title:     "Low Heat",
			Content:   "Body",
			Preview:   "Preview",
			Status:    "published",
			ViewCount: 20,
		},
		{
			Model:     gorm.Model{CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
			AuthorID:  1,
			Title:     "High Heat",
			Content:   "Body",
			Preview:   "Preview",
			Status:    "published",
			ViewCount: 5,
		},
	}
	if err := db.Create(&articles).Error; err != nil {
		t.Fatalf("seed articles: %v", err)
	}

	ctx := context.Background()
	if err := handler.service.repo.SetInitialHotScore(ctx, articles[0].ID, 10); err != nil {
		t.Fatalf("seed low heat: %v", err)
	}
	if err := handler.service.repo.SetInitialHotScore(ctx, articles[1].ID, 100); err != nil {
		t.Fatalf("seed high heat: %v", err)
	}

	router := gin.New()
	router.GET("/api/articles/hot", handler.GetHotArticles)

	req := httptest.NewRequest(http.MethodGet, "/api/articles/hot?limit=2", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	items := envelope["data"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["title"] != "High Heat" {
		t.Fatalf("expected zset top article first, got %v", first["title"])
	}
}

func TestGetArticleLikesUsesUnifiedResponseEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := setupArticleTestHandler(t, true)

	repo := handler.service.repo
	if err := repo.redisDB.Set(context.Background(), "article:1:like", 7, 0).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	router := gin.New()
	router.GET("/api/articles/:id/like", handler.GetArticleLikes)

	req := httptest.NewRequest(http.MethodGet, "/api/articles/1/like", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data := envelope["data"].(map[string]any)
	if likes, ok := data["likes"].(float64); !ok || likes != 7 {
		t.Fatalf("expected likes 7, got %#v", data["likes"])
	}
}

func TestArticleCachesPopulateAndInvalidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupArticleTestHandler(t, true)

	author := internalAuth.User{Username: "cache-author", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create author: %v", err)
	}

	seedArticles := []Article{
		{
			AuthorID:  author.ID,
			Title:     "Cache Warmup",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "go,cache",
			Status:    "published",
			ViewCount: 20,
			LikeCount: 4,
		},
		{
			AuthorID:  author.ID,
			Title:     "Unrelated Detail Cache",
			Content:   "Body",
			Preview:   "Preview",
			Tags:      "go,test",
			Status:    "published",
			ViewCount: 8,
			LikeCount: 1,
		},
	}
	if err := db.Create(&seedArticles).Error; err != nil {
		t.Fatalf("seed articles: %v", err)
	}

	router := gin.New()
	router.GET("/api/articles", handler.GetArticles)
	router.GET("/api/articles/hot", handler.GetHotArticles)
	router.GET("/api/articles/:id", handler.GetArticleByID)
	router.POST("/api/articles/:id/like", handler.LikeArticle)

	listKey := cachekey.ArticleListKey(1, 10, "hot", "", "")
	hotKey := cachekey.ArticleHotKey(2)
	firstDetailKey := cachekey.ArticleDetailKey(toID(seedArticles[0].ID))
	secondDetailKey := cachekey.ArticleDetailKey(toID(seedArticles[1].ID))

	listReq := httptest.NewRequest(http.MethodGet, "/api/articles?sort=hot", nil)
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResp.Code)
	}

	hotReq := httptest.NewRequest(http.MethodGet, "/api/articles/hot?limit=2", nil)
	hotResp := httptest.NewRecorder()
	router.ServeHTTP(hotResp, hotReq)
	if hotResp.Code != http.StatusOK {
		t.Fatalf("expected hot status 200, got %d", hotResp.Code)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/articles/"+toID(seedArticles[0].ID), nil)
	detailResp := httptest.NewRecorder()
	router.ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("expected detail status 200, got %d", detailResp.Code)
	}

	secondDetailReq := httptest.NewRequest(http.MethodGet, "/api/articles/"+toID(seedArticles[1].ID), nil)
	secondDetailResp := httptest.NewRecorder()
	router.ServeHTTP(secondDetailResp, secondDetailReq)
	if secondDetailResp.Code != http.StatusOK {
		t.Fatalf("expected second detail status 200, got %d", secondDetailResp.Code)
	}

	repo := handler.service.repo
	if exists, err := repo.redisDB.Exists(context.Background(), listKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected list cache key %s, exists=%d err=%v", listKey, exists, err)
	}
	if exists, err := repo.redisDB.Exists(context.Background(), hotKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected hot cache key %s, exists=%d err=%v", hotKey, exists, err)
	}
	if exists, err := repo.redisDB.Exists(context.Background(), firstDetailKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected detail cache key %s, exists=%d err=%v", firstDetailKey, exists, err)
	}
	if exists, err := repo.redisDB.Exists(context.Background(), secondDetailKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected detail cache key %s, exists=%d err=%v", secondDetailKey, exists, err)
	}

	likeReq := httptest.NewRequest(http.MethodPost, "/api/articles/"+toID(seedArticles[0].ID)+"/like", nil)
	likeResp := httptest.NewRecorder()
	router.ServeHTTP(likeResp, likeReq)
	if likeResp.Code != http.StatusOK {
		t.Fatalf("expected like status 200, got %d", likeResp.Code)
	}

	if exists, err := repo.redisDB.Exists(context.Background(), listKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected list cache invalidated, exists=%d err=%v", exists, err)
	}
	if exists, err := repo.redisDB.Exists(context.Background(), hotKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected hot cache invalidated, exists=%d err=%v", exists, err)
	}
	if exists, err := repo.redisDB.Exists(context.Background(), firstDetailKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected liked article detail cache invalidated, exists=%d err=%v", exists, err)
	}
	if exists, err := repo.redisDB.Exists(context.Background(), secondDetailKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected unrelated detail cache preserved, exists=%d err=%v", exists, err)
	}
}

func toID(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
