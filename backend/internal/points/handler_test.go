package points

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	internalAuth "resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type testArticle struct {
	gorm.Model
	AuthorID       uint
	Title          string
	Content        string
	Preview        string
	Status         string
	IsFree         bool
	RequiredPoints uint
}

func (testArticle) TableName() string {
	return "articles"
}

type testArticleUnlock struct {
	gorm.Model
	ArticleID uint
	UserID    uint
}

func (testArticleUnlock) TableName() string {
	return "article_unlocks"
}

func setupPointsTestHandler(t *testing.T, withRedis bool) (*Handler, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&internalAuth.User{},
		&testArticle{},
		&testArticleUnlock{},
		&PointLedger{},
		&UserCheckIn{},
		&UserPrivilege{},
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

	return NewHandler(NewService(NewRepo(db, redisClient))), db
}

func TestPointsFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupPointsTestHandler(t, false)

	user := internalAuth.User{Username: "points_user", Password: "secret123", Points: 40}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	article := testArticle{
		AuthorID:       99,
		Title:          "Paid Resource",
		Content:        "Body",
		Preview:        "Preview",
		Status:         "published",
		IsFree:         false,
		RequiredPoints: 20,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	router := gin.New()
	router.GET("/api/me/points", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.GetMyPoints(ctx)
	})
	router.GET("/api/me/points/records", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.GetMyPointsRecords(ctx)
	})
	router.POST("/api/me/check-in", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.CheckIn(ctx)
	})
	router.POST("/api/articles/:id/unlock", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.UnlockArticle(ctx)
	})
	router.POST("/api/me/points/redeem", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.RedeemPrivilege(ctx)
	})

	t.Run("check-in and summary", func(t *testing.T) {
		checkInReq := httptest.NewRequest(http.MethodPost, "/api/me/check-in", nil)
		checkInResp := httptest.NewRecorder()
		router.ServeHTTP(checkInResp, checkInReq)
		if checkInResp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", checkInResp.Code)
		}

		summaryReq := httptest.NewRequest(http.MethodGet, "/api/me/points", nil)
		summaryResp := httptest.NewRecorder()
		router.ServeHTTP(summaryResp, summaryReq)
		if summaryResp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", summaryResp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(summaryResp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal summary: %v", err)
		}
		data := envelope["data"].(map[string]any)
		if data["balance"] != float64(45) {
			t.Fatalf("expected balance 45, got %v", data["balance"])
		}

		repeatReq := httptest.NewRequest(http.MethodPost, "/api/me/check-in", nil)
		repeatResp := httptest.NewRecorder()
		router.ServeHTTP(repeatResp, repeatReq)
		if repeatResp.Code != http.StatusConflict {
			t.Fatalf("expected repeat check-in status 409, got %d", repeatResp.Code)
		}

		var userAfterRepeatCheckIn internalAuth.User
		if err := db.First(&userAfterRepeatCheckIn, user.ID).Error; err != nil {
			t.Fatalf("reload user after repeat check-in: %v", err)
		}
		if userAfterRepeatCheckIn.Points != 45 {
			t.Fatalf("expected repeat check-in to keep balance 45, got %d", userAfterRepeatCheckIn.Points)
		}

		var checkInCount int64
		if err := db.Model(&UserCheckIn{}).Where("user_id = ?", user.ID).Count(&checkInCount).Error; err != nil {
			t.Fatalf("count check-ins: %v", err)
		}
		if checkInCount != 1 {
			t.Fatalf("expected exactly 1 check-in record, got %d", checkInCount)
		}

		var ledgerCount int64
		if err := db.Model(&PointLedger{}).Where("user_id = ? AND source = ?", user.ID, "daily_check_in").Count(&ledgerCount).Error; err != nil {
			t.Fatalf("count point ledgers: %v", err)
		}
		if ledgerCount != 1 {
			t.Fatalf("expected exactly 1 daily check-in ledger, got %d", ledgerCount)
		}
	})

	t.Run("unlock article and records", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/unlock", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.Code)
		}

		var userAfterFirstUnlock internalAuth.User
		if err := db.First(&userAfterFirstUnlock, user.ID).Error; err != nil {
			t.Fatalf("reload user after unlock: %v", err)
		}
		if userAfterFirstUnlock.Points != 25 {
			t.Fatalf("expected balance 25 after unlock, got %d", userAfterFirstUnlock.Points)
		}

		repeatReq := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(article.ID), 10)+"/unlock", nil)
		repeatResp := httptest.NewRecorder()
		router.ServeHTTP(repeatResp, repeatReq)
		if repeatResp.Code != http.StatusConflict {
			t.Fatalf("expected repeat unlock status 409, got %d", repeatResp.Code)
		}

		var userAfterRepeat internalAuth.User
		if err := db.First(&userAfterRepeat, user.ID).Error; err != nil {
			t.Fatalf("reload user after repeat unlock: %v", err)
		}
		if userAfterRepeat.Points != 25 {
			t.Fatalf("expected repeat unlock to keep balance 25, got %d", userAfterRepeat.Points)
		}

		var unlockCount int64
		if err := db.Table("article_unlocks").Where("article_id = ? AND user_id = ?", article.ID, user.ID).Count(&unlockCount).Error; err != nil {
			t.Fatalf("count unlock records: %v", err)
		}
		if unlockCount != 1 {
			t.Fatalf("expected exactly 1 unlock record, got %d", unlockCount)
		}

		recordsReq := httptest.NewRequest(http.MethodGet, "/api/me/points/records", nil)
		recordsResp := httptest.NewRecorder()
		router.ServeHTTP(recordsResp, recordsReq)
		if recordsResp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", recordsResp.Code)
		}

		var envelope map[string]any
		if err := json.Unmarshal(recordsResp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("unmarshal records: %v", err)
		}
		records := envelope["data"].([]any)
		if len(records) < 2 {
			t.Fatalf("expected at least 2 records, got %d", len(records))
		}
	})

	t.Run("redeem privilege", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"privilegeKey": "feature_article"})
		req := httptest.NewRequest(http.MethodPost, "/api/me/points/redeem", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 due to insufficient points, got %d", resp.Code)
		}

		if err := db.Model(&internalAuth.User{}).Where("id = ?", user.ID).Update("points", 80).Error; err != nil {
			t.Fatalf("top up points: %v", err)
		}

		successReq := httptest.NewRequest(http.MethodPost, "/api/me/points/redeem", bytes.NewReader(payload))
		successReq.Header.Set("Content-Type", "application/json")
		successResp := httptest.NewRecorder()
		router.ServeHTTP(successResp, successReq)
		if successResp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", successResp.Code)
		}
	})
}

func TestDraftArticleCannotBeUnlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupPointsTestHandler(t, false)

	user := internalAuth.User{Username: "draft_unlock_user", Password: "secret123", Points: 40}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	draft := testArticle{
		AuthorID:       99,
		Title:          "Draft Paid Resource",
		Content:        "Body",
		Preview:        "Preview",
		Status:         "draft",
		IsFree:         false,
		RequiredPoints: 20,
	}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	router := gin.New()
	router.POST("/api/articles/:id/unlock", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.UnlockArticle(ctx)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+strconv.FormatUint(uint64(draft.ID), 10)+"/unlock", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected draft unlock to return 404, got %d", resp.Code)
	}

	var reloaded internalAuth.User
	if err := db.First(&reloaded, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.Points != 40 {
		t.Fatalf("expected draft unlock to keep balance 40, got %d", reloaded.Points)
	}

	var unlockCount int64
	if err := db.Table("article_unlocks").Where("article_id = ? AND user_id = ?", draft.ID, user.ID).Count(&unlockCount).Error; err != nil {
		t.Fatalf("count unlock records: %v", err)
	}
	if unlockCount != 0 {
		t.Fatalf("expected no draft unlock record, got %d", unlockCount)
	}
}

func TestPointsSummaryCacheHitAndInvalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := setupPointsTestHandler(t, true)

	user := internalAuth.User{Username: "summary_user", Password: "secret123", Points: 40}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	router := gin.New()
	router.GET("/api/me/points", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.GetMyPoints(ctx)
	})
	router.POST("/api/me/check-in", func(ctx *gin.Context) {
		ctx.Set("userID", user.ID)
		handler.CheckIn(ctx)
	})

	cacheKey := cachekey.PointsSummaryKey(user.ID)
	repo := handler.service.repo
	ctx := context.Background()

	firstReq := httptest.NewRequest(http.MethodGet, "/api/me/points", nil)
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("expected summary status 200, got %d", firstResp.Code)
	}
	if exists, err := repo.redisDB.Exists(ctx, cacheKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected summary cache key %s, exists=%d err=%v", cacheKey, exists, err)
	}

	if err := db.Model(&internalAuth.User{}).Where("id = ?", user.ID).Update("points", 99).Error; err != nil {
		t.Fatalf("update points directly: %v", err)
	}

	cachedReq := httptest.NewRequest(http.MethodGet, "/api/me/points", nil)
	cachedResp := httptest.NewRecorder()
	router.ServeHTTP(cachedResp, cachedReq)
	if cachedResp.Code != http.StatusOK {
		t.Fatalf("expected cached summary status 200, got %d", cachedResp.Code)
	}

	var cachedEnvelope map[string]any
	if err := json.Unmarshal(cachedResp.Body.Bytes(), &cachedEnvelope); err != nil {
		t.Fatalf("unmarshal cached summary: %v", err)
	}
	cachedData := cachedEnvelope["data"].(map[string]any)
	if cachedData["balance"] != float64(40) {
		t.Fatalf("expected cached balance 40, got %v", cachedData["balance"])
	}

	checkInReq := httptest.NewRequest(http.MethodPost, "/api/me/check-in", nil)
	checkInResp := httptest.NewRecorder()
	router.ServeHTTP(checkInResp, checkInReq)
	if checkInResp.Code != http.StatusOK {
		t.Fatalf("expected check-in status 200, got %d", checkInResp.Code)
	}
	if exists, err := repo.redisDB.Exists(ctx, cacheKey).Result(); err != nil || exists != 0 {
		t.Fatalf("expected summary cache invalidated after check-in, exists=%d err=%v", exists, err)
	}

	refreshedReq := httptest.NewRequest(http.MethodGet, "/api/me/points", nil)
	refreshedResp := httptest.NewRecorder()
	router.ServeHTTP(refreshedResp, refreshedReq)
	if refreshedResp.Code != http.StatusOK {
		t.Fatalf("expected refreshed summary status 200, got %d", refreshedResp.Code)
	}

	var refreshedEnvelope map[string]any
	if err := json.Unmarshal(refreshedResp.Body.Bytes(), &refreshedEnvelope); err != nil {
		t.Fatalf("unmarshal refreshed summary: %v", err)
	}
	refreshedData := refreshedEnvelope["data"].(map[string]any)
	if refreshedData["balance"] != float64(104) {
		t.Fatalf("expected refreshed balance 104, got %v", refreshedData["balance"])
	}
	if exists, err := repo.redisDB.Exists(ctx, cacheKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expected summary cache rebuilt, exists=%d err=%v", exists, err)
	}
}
