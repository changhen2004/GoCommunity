package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"resource_community_go/config"
	internalArticle "resource_community_go/internal/article"
	internalAuth "resource_community_go/internal/auth"
	internalComment "resource_community_go/internal/comment"
	"resource_community_go/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupRouterTestEnv(t *testing.T) Dependencies {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	if err := config.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = redisClient.Close()
	})

	return Dependencies{
		DB:      db,
		RedisDB: redisClient,
	}
}

func seedRouterData(t *testing.T, deps Dependencies) internalArticle.Article {
	t.Helper()

	user := internalAuth.User{Username: "alice", Password: "secret123", Points: 100}
	if err := deps.DB.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	article := internalArticle.Article{
		AuthorID:  user.ID,
		Title:     "Daily Exchange Update",
		Content:   "Full article body",
		Preview:   "Preview content",
		Status:    "published",
		ViewCount: 12,
		LikeCount: 3,
	}
	if err := deps.DB.Create(&article).Error; err != nil {
		t.Fatalf("seed article: %v", err)
	}

	if err := deps.RedisDB.Set(context.Background(), "article:1:like", 3, 0).Err(); err != nil {
		t.Fatalf("seed like count: %v", err)
	}

	return article
}

func TestPublicContentRoutesAccessibleWithoutAuth(t *testing.T) {
	deps := setupRouterTestEnv(t)
	article := seedRouterData(t, deps)

	r := SetUpRouter(deps)

	tests := []struct {
		name string
		path string
	}{
		{name: "list articles", path: "/api/articles"},
		{name: "article detail", path: "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10)},
		{name: "article likes", path: "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/like"},
		{name: "article comments", path: "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/comments"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("expected status 200 for %s, got %d", tt.path, resp.Code)
			}
		})
	}
}

func TestHealthzAccessibleWithoutAuth(t *testing.T) {
	deps := setupRouterTestEnv(t)
	r := SetUpRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /healthz, got %d", resp.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal healthz response: %v", err)
	}

	if payload["status"] != "ok" {
		t.Fatalf("expected healthz status ok, got %q", payload["status"])
	}
}

func TestExchangeRoutesNotRegistered(t *testing.T) {
	deps := setupRouterTestEnv(t)
	r := SetUpRouter(deps)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "list exchange rates", method: http.MethodGet, path: "/api/exchangeRates"},
		{name: "create exchange rate", method: http.MethodPost, path: "/api/exchangeRates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected status 404 for %s %s, got %d", tt.method, tt.path, resp.Code)
			}
		})
	}
}

func TestPprofRouteDisabledByDefault(t *testing.T) {
	deps := setupRouterTestEnv(t)
	r := SetUpRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 when pprof disabled, got %d", resp.Code)
	}
}

func TestPprofRouteEnabled(t *testing.T) {
	deps := setupRouterTestEnv(t)
	deps.EnablePprof = true
	r := SetUpRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 when pprof enabled, got %d", resp.Code)
	}
}

func TestProtectedPublishAndLikeRoutesRequireAuth(t *testing.T) {
	deps := setupRouterTestEnv(t)
	article := seedRouterData(t, deps)
	comment := internalComment.Comment{
		ArticleID: article.ID,
		UserID:    1,
		Content:   "seed comment",
	}
	if err := deps.DB.Create(&comment).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	r := SetUpRouter(deps)

	tests := []struct {
		name   string
		path   string
		body   any
		want   int
		method string
	}{
		{
			name:   "create article",
			path:   "/api/articles",
			method: http.MethodPost,
			body: map[string]any{
				"title":   "Protected article",
				"content": "Protected content",
				"preview": "Protected preview",
			},
			want: http.StatusUnauthorized,
		},
		{
			name:   "like article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/like",
			method: http.MethodPost,
			body:   map[string]any{},
			want:   http.StatusUnauthorized,
		},
		{
			name:   "create comment",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/comments",
			method: http.MethodPost,
			body: map[string]any{
				"content": "new comment",
			},
			want: http.StatusUnauthorized,
		},
		{
			name:   "delete comment",
			path:   "/api/comments/" + strconv.FormatUint(uint64(comment.ID), 10),
			method: http.MethodDelete,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "favorite article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/favorite",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "upload cover",
			path:   "/api/uploads/cover",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "unlock article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/unlock",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "my favorites",
			path:   "/api/me/favorites",
			method: http.MethodGet,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "my points",
			path:   "/api/me/points",
			method: http.MethodGet,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "points records",
			path:   "/api/me/points/records",
			method: http.MethodGet,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "daily check-in",
			path:   "/api/me/check-in",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
		{
			name:   "redeem privilege",
			path:   "/api/me/points/redeem",
			method: http.MethodPost,
			body: map[string]any{
				"privilegeKey": "feature_article",
			},
			want: http.StatusUnauthorized,
		},
		{
			name:   "logout",
			path:   "/api/auth/logout",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if resp.Code != tt.want {
				t.Fatalf("expected status %d for %s, got %d", tt.want, tt.path, resp.Code)
			}
		})
	}
}

func TestAuthenticatedPublishAndLikeRoutesAllowed(t *testing.T) {
	deps := setupRouterTestEnv(t)
	article := seedRouterData(t, deps)
	comment := internalComment.Comment{
		ArticleID: article.ID,
		UserID:    1,
		Content:   "seed comment",
	}
	if err := deps.DB.Create(&comment).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	r := SetUpRouter(deps)

	token, err := utils.GenerateJWT(1, "alice")
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	tests := []struct {
		name   string
		path   string
		body   any
		want   int
		method string
	}{
		{
			name:   "create article",
			path:   "/api/articles",
			method: http.MethodPost,
			body: map[string]any{
				"title":   "Protected article",
				"content": "Protected content",
				"preview": "Protected preview",
			},
			want: http.StatusCreated,
		},
		{
			name:   "like article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/like",
			method: http.MethodPost,
			body:   map[string]any{},
			want:   http.StatusOK,
		},
		{
			name:   "create comment",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/comments",
			method: http.MethodPost,
			body: map[string]any{
				"content": "Protected comment",
			},
			want: http.StatusCreated,
		},
		{
			name:   "favorite article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/favorite",
			method: http.MethodPost,
			body:   map[string]any{},
			want:   http.StatusCreated,
		},
		{
			name:   "unlock article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/unlock",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusOK,
		},
		{
			name:   "my favorites",
			path:   "/api/me/favorites",
			method: http.MethodGet,
			body:   nil,
			want:   http.StatusOK,
		},
		{
			name:   "my points",
			path:   "/api/me/points",
			method: http.MethodGet,
			body:   nil,
			want:   http.StatusOK,
		},
		{
			name:   "points records",
			path:   "/api/me/points/records",
			method: http.MethodGet,
			body:   nil,
			want:   http.StatusOK,
		},
		{
			name:   "daily check-in",
			path:   "/api/me/check-in",
			method: http.MethodPost,
			body:   nil,
			want:   http.StatusOK,
		},
		{
			name:   "redeem privilege",
			path:   "/api/me/points/redeem",
			method: http.MethodPost,
			body: map[string]any{
				"privilegeKey": "feature_article",
			},
			want: http.StatusOK,
		},
		{
			name:   "unfavorite article",
			path:   "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/favorite",
			method: http.MethodDelete,
			body:   nil,
			want:   http.StatusOK,
		},
		{
			name:   "delete comment",
			path:   "/api/comments/" + strconv.FormatUint(uint64(comment.ID), 10),
			method: http.MethodDelete,
			body:   nil,
			want:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if resp.Code != tt.want {
				t.Fatalf("expected status %d for %s, got %d", tt.want, tt.path, resp.Code)
			}
		})
	}
}

func TestLogoutRevokesExistingAccessToken(t *testing.T) {
	deps := setupRouterTestEnv(t)
	r := SetUpRouter(deps)

	registerBody, _ := json.Marshal(map[string]string{
		"username": "logout-user",
		"password": "secret123",
	})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	r.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d", registerResp.Code)
	}

	var registerEnvelope map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerEnvelope); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	registerData := registerEnvelope["data"].(map[string]any)
	accessToken := registerData["access_token"].(string)

	beforeLogoutReq := httptest.NewRequest(http.MethodGet, "/api/me/points", nil)
	beforeLogoutReq.Header.Set("Authorization", "Bearer "+accessToken)
	beforeLogoutResp := httptest.NewRecorder()
	r.ServeHTTP(beforeLogoutResp, beforeLogoutReq)
	if beforeLogoutResp.Code != http.StatusOK {
		t.Fatalf("expected protected route before logout to return 200, got %d", beforeLogoutResp.Code)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+accessToken)
	logoutResp := httptest.NewRecorder()
	r.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusOK {
		t.Fatalf("expected logout status 200, got %d", logoutResp.Code)
	}

	afterLogoutReq := httptest.NewRequest(http.MethodGet, "/api/me/points", nil)
	afterLogoutReq.Header.Set("Authorization", "Bearer "+accessToken)
	afterLogoutResp := httptest.NewRecorder()
	r.ServeHTTP(afterLogoutResp, afterLogoutReq)
	if afterLogoutResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected protected route after logout to return 401, got %d", afterLogoutResp.Code)
	}
}

func TestRateLimitProtectsSensitiveRoutes(t *testing.T) {
	t.Run("register limited by ip", func(t *testing.T) {
		restore := applyTestRateLimitRules(
			rateLimitRule{Name: registerRateLimitRule.Name, Limit: 2, Window: registerRateLimitRule.Window, Scope: registerRateLimitRule.Scope},
			loginRateLimitRule,
			publishRateLimitRule,
			commentRateLimitRule,
			checkInRateLimitRule,
		)
		defer restore()

		deps := setupRouterTestEnv(t)
		r := SetUpRouter(deps)

		for i := 1; i <= 3; i++ {
			body, _ := json.Marshal(map[string]string{
				"username": "register-user-" + strconv.Itoa(i),
				"password": "secret123",
			})
			req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "203.0.113.10:1234"
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if i < 3 && resp.Code != http.StatusOK {
				t.Fatalf("expected register request %d to succeed, got %d", i, resp.Code)
			}
			if i == 3 && resp.Code != http.StatusTooManyRequests {
				t.Fatalf("expected third register request to return 429, got %d", resp.Code)
			}
		}
	})

	t.Run("login limited by ip", func(t *testing.T) {
		restore := applyTestRateLimitRules(
			registerRateLimitRule,
			rateLimitRule{Name: loginRateLimitRule.Name, Limit: 2, Window: loginRateLimitRule.Window, Scope: loginRateLimitRule.Scope},
			publishRateLimitRule,
			commentRateLimitRule,
			checkInRateLimitRule,
		)
		defer restore()

		deps := setupRouterTestEnv(t)
		hashedPassword, err := utils.HashPassword("secret123")
		if err != nil {
			t.Fatalf("hash password: %v", err)
		}
		if err := deps.DB.Create(&internalAuth.User{
			Username: "login-user",
			Password: hashedPassword,
			Points:   0,
		}).Error; err != nil {
			t.Fatalf("seed login user: %v", err)
		}

		r := SetUpRouter(deps)
		body, _ := json.Marshal(map[string]string{
			"username": "login-user",
			"password": "secret123",
		})

		for i := 1; i <= 3; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "203.0.113.20:4567"
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if i < 3 && resp.Code != http.StatusOK {
				t.Fatalf("expected login request %d to succeed, got %d", i, resp.Code)
			}
			if i == 3 && resp.Code != http.StatusTooManyRequests {
				t.Fatalf("expected third login request to return 429, got %d", resp.Code)
			}
		}
	})

	t.Run("publish limited by user", func(t *testing.T) {
		restore := applyTestRateLimitRules(
			registerRateLimitRule,
			loginRateLimitRule,
			rateLimitRule{Name: publishRateLimitRule.Name, Limit: 1, Window: publishRateLimitRule.Window, Scope: publishRateLimitRule.Scope},
			commentRateLimitRule,
			checkInRateLimitRule,
		)
		defer restore()

		deps := setupRouterTestEnv(t)
		_ = seedRouterData(t, deps)
		r := SetUpRouter(deps)

		token, err := utils.GenerateAccessToken(1, "alice", 0)
		if err != nil {
			t.Fatalf("generate access token: %v", err)
		}

		body, _ := json.Marshal(map[string]any{
			"title":   "Limited publish",
			"content": "Body",
			"preview": "Preview",
		})

		for i := 1; i <= 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/articles", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if i == 1 && resp.Code != http.StatusCreated {
				t.Fatalf("expected first publish to succeed, got %d", resp.Code)
			}
			if i == 2 && resp.Code != http.StatusTooManyRequests {
				t.Fatalf("expected second publish to return 429, got %d", resp.Code)
			}
		}
	})

	t.Run("comment limited by user", func(t *testing.T) {
		restore := applyTestRateLimitRules(
			registerRateLimitRule,
			loginRateLimitRule,
			publishRateLimitRule,
			rateLimitRule{Name: commentRateLimitRule.Name, Limit: 1, Window: commentRateLimitRule.Window, Scope: commentRateLimitRule.Scope},
			checkInRateLimitRule,
		)
		defer restore()

		deps := setupRouterTestEnv(t)
		article := seedRouterData(t, deps)
		r := SetUpRouter(deps)

		token, err := utils.GenerateAccessToken(1, "alice", 0)
		if err != nil {
			t.Fatalf("generate access token: %v", err)
		}

		body, _ := json.Marshal(map[string]any{"content": "rate limited comment"})
		path := "/api/articles/" + strconv.FormatUint(uint64(article.ID), 10) + "/comments"

		for i := 1; i <= 2; i++ {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if i == 1 && resp.Code != http.StatusCreated {
				t.Fatalf("expected first comment to succeed, got %d", resp.Code)
			}
			if i == 2 && resp.Code != http.StatusTooManyRequests {
				t.Fatalf("expected second comment to return 429, got %d", resp.Code)
			}
		}
	})

	t.Run("check-in limited by user", func(t *testing.T) {
		restore := applyTestRateLimitRules(
			registerRateLimitRule,
			loginRateLimitRule,
			publishRateLimitRule,
			commentRateLimitRule,
			rateLimitRule{Name: checkInRateLimitRule.Name, Limit: 1, Window: checkInRateLimitRule.Window, Scope: checkInRateLimitRule.Scope},
		)
		defer restore()

		deps := setupRouterTestEnv(t)
		_ = seedRouterData(t, deps)
		r := SetUpRouter(deps)

		token, err := utils.GenerateAccessToken(1, "alice", 0)
		if err != nil {
			t.Fatalf("generate access token: %v", err)
		}

		for i := 1; i <= 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/me/check-in", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if i == 1 && resp.Code != http.StatusOK {
				t.Fatalf("expected first check-in to succeed, got %d", resp.Code)
			}
			if i == 2 && resp.Code != http.StatusTooManyRequests {
				t.Fatalf("expected second check-in to return 429, got %d", resp.Code)
			}
		}
	})
}

func applyTestRateLimitRules(registerRule, loginRule, publishRule, commentRule, checkInRule rateLimitRule) func() {
	previousRegister := registerRateLimitRule
	previousLogin := loginRateLimitRule
	previousPublish := publishRateLimitRule
	previousComment := commentRateLimitRule
	previousCheckIn := checkInRateLimitRule

	registerRateLimitRule = registerRule
	loginRateLimitRule = loginRule
	publishRateLimitRule = publishRule
	commentRateLimitRule = commentRule
	checkInRateLimitRule = checkInRule

	return func() {
		registerRateLimitRule = previousRegister
		loginRateLimitRule = previousLogin
		publishRateLimitRule = previousPublish
		commentRateLimitRule = previousComment
		checkInRateLimitRule = previousCheckIn
	}
}
