package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	internalAuth "resource_community_go/internal/auth"
	"resource_community_go/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestAuthMiddlewareRequiresBearerScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authService := newTestAuthService(t)

	router := gin.New()
	router.Use(AuthMiddleware(authService))
	router.GET("/protected", func(ctx *gin.Context) {
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "plain-token")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if code, ok := envelope["code"].(float64); !ok || code != 10002 {
		t.Fatalf("expected code 10002, got %#v", envelope["code"])
	}
}

func TestAuthMiddlewareSetsUserIDAndUsernameInContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authService := newTestAuthService(t)
	token, err := utils.GenerateAccessToken(42, "alice", 0)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	router := gin.New()
	router.Use(AuthMiddleware(authService))
	router.GET("/protected", func(ctx *gin.Context) {
		userID, ok := ctx.Get("userID")
		if !ok {
			t.Fatal("expected userID in context")
		}
		username, ok := ctx.Get("username")
		if !ok {
			t.Fatal("expected username in context")
		}

		ctx.JSON(http.StatusOK, gin.H{
			"userID":   userID,
			"username": username,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if userID, ok := body["userID"].(float64); !ok || userID != 42 {
		t.Fatalf("expected userID 42, got %#v", body["userID"])
	}
	if username, ok := body["username"].(string); !ok || username != "alice" {
		t.Fatalf("expected username alice, got %#v", body["username"])
	}
}

func TestAuthMiddlewareRejectsRefreshToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authService := newTestAuthService(t)
	token, err := utils.GenerateRefreshToken(42, "alice", 0)
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	router := gin.New()
	router.Use(AuthMiddleware(authService))
	router.GET("/protected", func(ctx *gin.Context) {
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestAuthMiddlewareRejectsRevokedAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authService := newTestAuthService(t)
	token, err := utils.GenerateAccessToken(42, "alice", 0)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}
	if err := authService.Logout(42); err != nil {
		t.Fatalf("logout user: %v", err)
	}

	router := gin.New()
	router.Use(AuthMiddleware(authService))
	router.GET("/protected", func(ctx *gin.Context) {
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func newTestAuthService(t *testing.T) *internalAuth.Service {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&internalAuth.User{}); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	return internalAuth.NewService(internalAuth.NewRepo(db, redisClient))
}
