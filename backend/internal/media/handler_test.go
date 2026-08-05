package media

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func pngFixture() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
		0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func TestUploadCoverAndContentImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	handler := NewHandler(NewService("uploads"))
	router := gin.New()
	router.POST("/api/uploads/cover", handler.UploadCover)
	router.POST("/api/uploads/content-images", handler.UploadContentImages)

	t.Run("upload cover returns url", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "cover.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(pngFixture()); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/uploads/cover", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d", resp.Code)
		}
		if !strings.Contains(resp.Body.String(), "/uploads/covers/") {
			t.Fatalf("expected cover url in response, got %s", resp.Body.String())
		}
	})

	t.Run("upload cover with absolute upload dir returns public url", func(t *testing.T) {
		absoluteUploadDir := filepath.Join(tempDir, "absolute-uploads")
		handler := NewHandler(NewService(absoluteUploadDir))
		router := gin.New()
		router.POST("/api/uploads/cover", handler.UploadCover)

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "cover.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(pngFixture()); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/uploads/cover", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d", resp.Code)
		}
		if !strings.Contains(resp.Body.String(), `"/uploads/covers/`) {
			t.Fatalf("expected public cover url in response, got %s", resp.Body.String())
		}
		if strings.Contains(resp.Body.String(), absoluteUploadDir) {
			t.Fatalf("expected response not to expose upload directory, got %s", resp.Body.String())
		}
	})

	t.Run("upload too many content images rejected", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		for i := 0; i < ContentImageMaxCount+1; i++ {
			part, err := writer.CreateFormFile("files", filepath.Base("body.png"))
			if err != nil {
				t.Fatalf("create form file: %v", err)
			}
			if _, err := part.Write(pngFixture()); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
		}
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/uploads/content-images", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", resp.Code)
		}
	})

	t.Run("upload unsupported cover rejected", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "cover.txt")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write([]byte("plain text file")); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/uploads/cover", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", resp.Code)
		}
	})

	t.Run("upload oversized content image rejected", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("files", "large.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		oversized := append(pngFixture(), bytes.Repeat([]byte{0x01}, int(ContentImageMaxSizeBytes))...)
		if _, err := part.Write(oversized); err != nil {
			t.Fatalf("write oversized fixture: %v", err)
		}
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/uploads/content-images", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", resp.Code)
		}
	})
}
