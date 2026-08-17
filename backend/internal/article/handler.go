package article

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type responseEnvelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type errorData struct {
	ErrorCode string `json:"errorCode"`
}

func writeSuccess(ctx *gin.Context, status int, data interface{}) {
	ctx.JSON(status, responseEnvelope{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

func writeError(ctx *gin.Context, status int, code int, message, errorCode string) {
	ctx.JSON(status, responseEnvelope{
		Code:    code,
		Message: message,
		Data: errorData{
			ErrorCode: errorCode,
		},
	})
}

func (h *Handler) CreateArticle(ctx *gin.Context) {
	var req CreateArticleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		writeError(ctx, http.StatusBadRequest, 10001, err.Error(), "INVALID_REQUEST")
		return
	}
	if err := ValidateCreateArticleRequest(req); err != nil {
		switch {
		case errors.Is(err, ErrTooManyContentImages):
			writeError(ctx, http.StatusBadRequest, 10013, err.Error(), "TOO_MANY_FILES")
		default:
			writeError(ctx, http.StatusBadRequest, 10001, err.Error(), "INVALID_REQUEST")
		}
		return
	}

	resp, err := h.service.Create(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrTooManyContentImages):
			writeError(ctx, http.StatusBadRequest, 10013, err.Error(), "TOO_MANY_FILES")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}
	writeSuccess(ctx, http.StatusCreated, resp)
}

func (h *Handler) GetArticles(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "10"))

	query := NewListArticlesQuery(
		page,
		pageSize,
		ctx.Query("sort"),
		ctx.Query("keyword"),
		ctx.Query("tag"),
	)

	resp, err := h.service.List(ctx, query)
	if err != nil {
		writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) GetHotArticles(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	resp, err := h.service.ListHot(ctx, limit)
	if err != nil {
		writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) GetFollowingArticles(ctx *gin.Context) {
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "10"))
	beforeID, _ := strconv.ParseUint(ctx.DefaultQuery("beforeId", "0"), 10, 64)
	resp, err := h.service.ListFollowing(ctx.Request.Context(), NewFollowingFeedQuery(
		userIDFromGinContext(ctx),
		pageSize,
		ctx.Query("beforeCreatedAt"),
		uint(beforeID),
	))
	if err != nil {
		writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) GetArticleByID(ctx *gin.Context) {
	resp, err := h.service.GetDetail(ctx.Param("id"), userIDFromGinContext(ctx))
	if err != nil {
		switch {
		case errors.Is(err, ErrArticleNotFound), errors.Is(err, gorm.ErrRecordNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Article not found", "ARTICLE_NOT_FOUND")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) LikeArticle(ctx *gin.Context) {
	resp, err := h.service.Like(ctx, ctx.Param("id"), ctx.GetUint("userID"))
	if err != nil {
		switch {
		case errors.Is(err, ErrArticleNotFound), errors.Is(err, gorm.ErrRecordNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Article not found", "ARTICLE_NOT_FOUND")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) UnlikeArticle(ctx *gin.Context) {
	resp, err := h.service.Unlike(ctx, ctx.Param("id"), ctx.GetUint("userID"))
	if err != nil {
		switch {
		case errors.Is(err, ErrArticleNotFound), errors.Is(err, gorm.ErrRecordNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Article not found", "ARTICLE_NOT_FOUND")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) GetArticleLikes(ctx *gin.Context) {
	resp, err := h.service.GetLikes(ctx, ctx.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, ErrArticleNotFound), errors.Is(err, gorm.ErrRecordNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Article not found", "ARTICLE_NOT_FOUND")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}
	writeSuccess(ctx, http.StatusOK, resp)
}

func userIDFromGinContext(ctx *gin.Context) uint {
	value, exists := ctx.Get("userID")
	if !exists {
		return 0
	}
	userID, ok := value.(uint)
	if !ok {
		return 0
	}
	return userID
}
