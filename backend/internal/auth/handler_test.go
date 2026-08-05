package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupAuthTestHandler(t *testing.T, withRedis bool) *Handler {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
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

	return NewHandler(NewService(NewRepo(db, redisClient)))
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, false)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)

	payload := map[string]string{"username": "alice", "password": "secret123"}
	body, _ := json.Marshal(payload)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)

	if firstResp.Code != http.StatusOK {
		t.Fatalf("expected first registration status 200, got %d", firstResp.Code)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp := httptest.NewRecorder()
	router.ServeHTTP(secondResp, secondReq)

	if secondResp.Code != http.StatusConflict {
		t.Fatalf("expected duplicate registration status 409, got %d", secondResp.Code)
	}
}

func TestRegisterReturnsEnvelopeWithoutModelFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, false)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)

	payload := map[string]string{"username": "bob", "password": "secret123"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
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
	if _, ok := data["access_token"]; !ok {
		t.Fatal("expected access_token in response data")
	}
	if _, ok := data["refresh_token"]; !ok {
		t.Fatal("expected refresh_token in response data")
	}
	if _, ok := envelope["ID"]; ok {
		t.Fatal("did not expect gorm ID field in response")
	}
}

func TestRegisterDuplicateUsernameUsesUnifiedErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, false)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)

	payload := map[string]string{"username": "alice", "password": "secret123"}
	body, _ := json.Marshal(payload)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)

	secondReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp := httptest.NewRecorder()
	router.ServeHTTP(secondResp, secondReq)

	var envelope map[string]any
	if err := json.Unmarshal(secondResp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := envelope["data"]; !ok {
		t.Fatal("expected data field in error response")
	}
	if _, ok := envelope["error"]; ok {
		t.Fatal("did not expect legacy error field in response")
	}
}

func TestLoginReturnsAccessAndRefreshTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, false)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)
	router.POST("/api/auth/login", handler.Login)

	registerBody, _ := json.Marshal(map[string]string{"username": "carol", "password": "secret123"})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d", registerResp.Code)
	}

	loginBody, _ := json.Marshal(map[string]string{"username": "carol", "password": "secret123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp := httptest.NewRecorder()
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d", loginResp.Code)
	}

	var envelope map[string]any
	if err := json.Unmarshal(loginResp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	data := envelope["data"].(map[string]any)
	accessToken, ok := data["access_token"].(string)
	if !ok || accessToken == "" {
		t.Fatalf("expected access_token, got %#v", data["access_token"])
	}
	refreshToken, ok := data["refresh_token"].(string)
	if !ok || refreshToken == "" {
		t.Fatalf("expected refresh_token, got %#v", data["refresh_token"])
	}
	if accessToken == refreshToken {
		t.Fatal("expected access_token and refresh_token to differ")
	}
}

func TestRefreshReturnsNewTokenPair(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, true)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)
	router.POST("/api/auth/refresh", handler.Refresh)

	registerBody, _ := json.Marshal(map[string]string{"username": "dave", "password": "secret123"})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d", registerResp.Code)
	}

	var registerEnvelope map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerEnvelope); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	registerData := registerEnvelope["data"].(map[string]any)
	oldAccessToken := registerData["access_token"].(string)
	oldRefreshToken := registerData["refresh_token"].(string)

	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": oldRefreshToken})
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(refreshBody))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshResp := httptest.NewRecorder()
	router.ServeHTTP(refreshResp, refreshReq)
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("expected refresh status 200, got %d", refreshResp.Code)
	}

	var refreshEnvelope map[string]any
	if err := json.Unmarshal(refreshResp.Body.Bytes(), &refreshEnvelope); err != nil {
		t.Fatalf("unmarshal refresh response: %v", err)
	}
	refreshData := refreshEnvelope["data"].(map[string]any)
	newAccessToken := refreshData["access_token"].(string)
	newRefreshToken := refreshData["refresh_token"].(string)
	if newAccessToken == "" || newRefreshToken == "" {
		t.Fatalf("expected refreshed tokens, got %#v", refreshData)
	}
	if newAccessToken == oldAccessToken && newRefreshToken == oldRefreshToken {
		t.Fatal("expected refresh to issue a new token pair")
	}
}

func TestRefreshRejectsAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, true)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)
	router.POST("/api/auth/refresh", handler.Refresh)

	registerBody, _ := json.Marshal(map[string]string{"username": "erin", "password": "secret123"})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d", registerResp.Code)
	}

	var registerEnvelope map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerEnvelope); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	registerData := registerEnvelope["data"].(map[string]any)
	accessToken := registerData["access_token"].(string)

	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": accessToken})
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(refreshBody))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshResp := httptest.NewRecorder()
	router.ServeHTTP(refreshResp, refreshReq)
	if refreshResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected refresh with access token to return 401, got %d", refreshResp.Code)
	}
}

func TestRegisterRejectsWeakPasswordAndLongUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, false)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)

	tests := []map[string]string{
		{
			"username": strings.Repeat("a", 21),
			"password": "secret123",
		},
		{
			"username": "validname",
			"password": "abcdefgh",
		},
	}

	for _, payload := range tests {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid payload %#v to return 400, got %d", payload, resp.Code)
		}
	}
}

func TestLogoutInvalidatesExistingTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := setupAuthTestHandler(t, true)

	router := gin.New()
	router.POST("/api/auth/register", handler.Register)
	router.POST("/api/auth/refresh", handler.Refresh)
	router.POST("/api/auth/logout", func(ctx *gin.Context) {
		ctx.Set("userID", uint(1))
		handler.Logout(ctx)
	})

	registerBody, _ := json.Marshal(map[string]string{"username": "frank", "password": "secret123"})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("expected register status 200, got %d", registerResp.Code)
	}

	var registerEnvelope map[string]any
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerEnvelope); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	registerData := registerEnvelope["data"].(map[string]any)
	refreshToken := registerData["refresh_token"].(string)

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutResp := httptest.NewRecorder()
	router.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusOK {
		t.Fatalf("expected logout status 200, got %d", logoutResp.Code)
	}

	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(refreshBody))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshResp := httptest.NewRecorder()
	router.ServeHTTP(refreshResp, refreshReq)
	if refreshResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected logged-out refresh token to return 401, got %d", refreshResp.Code)
	}
}
