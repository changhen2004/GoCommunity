package comment

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
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

func (h *Handler) CreateComment(ctx *gin.Context) {
	var req CreateCommentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		writeError(ctx, http.StatusBadRequest, 10001, err.Error(), "INVALID_REQUEST")
		return
	}
	if err := ValidateCreateCommentRequest(req); err != nil {
		writeError(ctx, http.StatusBadRequest, 10001, err.Error(), "INVALID_REQUEST")
		return
	}

	resp, err := h.service.Create(ctx, ctx.Param("id"), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrArticleNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Article not found", "ARTICLE_NOT_FOUND")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}

	writeSuccess(ctx, http.StatusCreated, resp)
}

func (h *Handler) GetComments(ctx *gin.Context) {
	resp, err := h.service.List(ctx.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, ErrArticleNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Article not found", "ARTICLE_NOT_FOUND")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}

	writeSuccess(ctx, http.StatusOK, resp)
}

func (h *Handler) DeleteComment(ctx *gin.Context) {
	resp, err := h.service.Delete(ctx.Param("id"), ctx.GetUint("userID"))
	if err != nil {
		switch {
		case errors.Is(err, ErrCommentNotFound):
			writeError(ctx, http.StatusNotFound, 10003, "Comment not found", "COMMENT_NOT_FOUND")
		case errors.Is(err, ErrForbidden):
			writeError(ctx, http.StatusForbidden, 10007, err.Error(), "FORBIDDEN")
		default:
			writeError(ctx, http.StatusInternalServerError, 10005, err.Error(), "INTERNAL_ERROR")
		}
		return
	}

	writeSuccess(ctx, http.StatusOK, resp)
}
