package comment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	internalArticle "resource_community_go/internal/article"
	internalAuth "resource_community_go/internal/auth"
	internalPoints "resource_community_go/internal/points"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupCommentTestHandler(t *testing.T, withRedis bool) (*Handler, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&internalAuth.User{}, &internalArticle.Article{}, &Comment{}, &internalPoints.PointLedger{}); err != nil {
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
	articleService := internalArticle.NewService(internalArticle.NewRepo(db, redisClient), nil, pointsService)
	return NewHandler(NewService(NewRepo(db), articleService, nil, pointsService)), db
}

func TestCommentLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupCommentTestHandler(t, false)

	author := internalAuth.User{Username: "alice", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	article := internalArticle.Article{
		AuthorID: author.ID,
		Title:    "Go Community",
		Content:  "Body",
		Preview:  "Preview",
		IsFree:   true,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	t.Run("create and list comments", func(t *testing.T) {
		router := gin.New()
		router.POST("/api/articles/:id/comments", func(ctx *gin.Context) {
			ctx.Set("userID", author.ID)
			ctx.Set("username", author.Username)
			handler.CreateComment(ctx)
		})
		router.GET("/api/articles/:id/comments", handler.GetComments)

		payload, _ := json.Marshal(map[string]any{"content": "first comment"})
		req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/comments", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d", resp.Code)
		}

		listReq := httptest.NewRequest(http.MethodGet, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/comments", nil)
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
			t.Fatalf("expected 1 comment, got %d", len(items))
		}
		comment := items[0].(map[string]any)
		if comment["content"] != "first comment" {
			t.Fatalf("unexpected comment content: %v", comment["content"])
		}
		authorData := comment["author"].(map[string]any)
		if authorData["username"] != "alice" {
			t.Fatalf("unexpected author username: %v", authorData["username"])
		}

		var savedArticle internalArticle.Article
		if err := db.First(&savedArticle, article.ID).Error; err != nil {
			t.Fatalf("reload article: %v", err)
		}
		if savedArticle.CommentCount != 1 {
			t.Fatalf("expected article comment count 1, got %d", savedArticle.CommentCount)
		}
	})

	t.Run("delete comment checks owner", func(t *testing.T) {
		commentRecord := Comment{
			ArticleID: article.ID,
			UserID:    author.ID,
			Content:   "to be deleted",
		}
		if err := db.Create(&commentRecord).Error; err != nil {
			t.Fatalf("seed comment: %v", err)
		}

		router := gin.New()
		router.DELETE("/api/comments/:id", func(ctx *gin.Context) {
			ctx.Set("userID", author.ID+1)
			handler.DeleteComment(ctx)
		})

		req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+strconv.FormatUint(uint64(commentRecord.ID), 10), nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d", resp.Code)
		}

		router = gin.New()
		router.DELETE("/api/comments/:id", func(ctx *gin.Context) {
			ctx.Set("userID", author.ID)
			handler.DeleteComment(ctx)
		})

		successReq := httptest.NewRequest(http.MethodDelete, "/api/comments/"+strconv.FormatUint(uint64(commentRecord.ID), 10), nil)
		successResp := httptest.NewRecorder()
		router.ServeHTTP(successResp, successReq)
		if successResp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", successResp.Code)
		}

		var savedArticle internalArticle.Article
		if err := db.First(&savedArticle, article.ID).Error; err != nil {
			t.Fatalf("reload article: %v", err)
		}
		if savedArticle.CommentCount != 0 {
			t.Fatalf("expected article comment count 0 after delete, got %d", savedArticle.CommentCount)
		}
	})
}

func TestCreateCommentRejectsTooLongContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupCommentTestHandler(t, false)

	author := internalAuth.User{Username: "validator", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	article := internalArticle.Article{
		AuthorID: author.ID,
		Title:    "Go Community",
		Content:  "Body",
		Preview:  "Preview",
		IsFree:   true,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles/:id/comments", func(ctx *gin.Context) {
		ctx.Set("userID", author.ID)
		ctx.Set("username", author.Username)
		handler.CreateComment(ctx)
	})

	payload, _ := json.Marshal(map[string]any{"content": strings.Repeat("a", 1001)})
	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/comments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestCreateCommentUpdatesHotRanking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupCommentTestHandler(t, true)

	author := internalAuth.User{Username: "hot-comment-author", Password: "secret123"}
	if err := db.Create(&author).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	article := internalArticle.Article{
		AuthorID: author.ID,
		Title:    "Hot Article",
		Content:  "Body",
		Preview:  "Preview",
		IsFree:   true,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	if err := handler.service.articleService.RecordFavoriteHeat(nil, article.ID, true); err != nil {
		t.Fatalf("seed hot ranking: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles/:id/comments", func(ctx *gin.Context) {
		ctx.Set("userID", author.ID)
		ctx.Set("username", author.Username)
		handler.CreateComment(ctx)
	})

	payload, _ := json.Marshal(map[string]any{"content": "boost hot score"})
	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/comments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.Code)
	}

	ids, err := handler.service.articleService.ListHot(req.Context(), 1)
	if err != nil {
		t.Fatalf("load hot articles: %v", err)
	}
	if len(ids) != 1 || ids[0].ID != article.ID {
		t.Fatalf("expected article %d to appear in hot list, got %#v", article.ID, ids)
	}
}
