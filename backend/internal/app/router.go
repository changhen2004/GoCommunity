package app

import (
	"net/http"
	"path"
	"path/filepath"
	internalArticle "resource_community_go/internal/article"
	"resource_community_go/internal/asyncjob"
	internalAuth "resource_community_go/internal/auth"
	internalComment "resource_community_go/internal/comment"
	internalFavorite "resource_community_go/internal/favorite"
	internalMedia "resource_community_go/internal/media"
	internalPoints "resource_community_go/internal/points"
	internalSocial "resource_community_go/internal/social"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Dependencies struct {
	DB                   *gorm.DB
	RedisDB              *redis.Client
	UploadDir            string
	EnablePprof          bool
	AsyncPublisher       asyncjob.Publisher
	SlowRequestThreshold time.Duration
}

func SetUpRouter(deps Dependencies) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(MetricsMiddleware())
	r.Use(ObservabilityMiddleware(deps.SlowRequestThreshold))
	r.GET("/healthz", func(ctx *gin.Context) {
		ctx.JSON(200, gin.H{
			"status": "ok",
		})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if deps.EnablePprof {
		pprof.Register(r)
	}

	authService := internalAuth.NewService(
		internalAuth.NewRepo(deps.DB, deps.RedisDB),
	)
	authHandler := internalAuth.NewHandler(
		authService,
	)
	pointsService := internalPoints.NewService(
		internalPoints.NewRepo(deps.DB, deps.RedisDB),
	)
	pointsHandler := internalPoints.NewHandler(pointsService)
	articleService := internalArticle.NewService(
		internalArticle.NewRepo(deps.DB, deps.RedisDB),
		deps.AsyncPublisher,
		pointsService,
	)
	articleHandler := internalArticle.NewHandler(
		articleService,
	)
	socialHandler := internalSocial.NewHandler(
		internalSocial.NewService(
			internalSocial.NewRepo(deps.DB, deps.RedisDB),
			internalAuth.NewRepo(deps.DB, deps.RedisDB),
		),
	)
	commentHandler := internalComment.NewHandler(
		internalComment.NewService(
			internalComment.NewRepo(deps.DB),
			articleService,
			deps.AsyncPublisher,
			pointsService,
		),
	)
	favoriteHandler := internalFavorite.NewHandler(
		internalFavorite.NewService(
			internalFavorite.NewRepo(deps.DB, deps.RedisDB),
			articleService,
			deps.AsyncPublisher,
		),
	)
	mediaHandler := internalMedia.NewHandler(
		internalMedia.NewService(deps.UploadDir),
	)

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"PUT", "PATCH", "GET", "POST", "DELETE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	auth := r.Group("/api/auth")
	{
		auth.POST("/login", RateLimitMiddleware(deps.RedisDB, loginRateLimitRule), authHandler.Login)
		auth.POST("/register", RateLimitMiddleware(deps.RedisDB, registerRateLimitRule), authHandler.Register)
		auth.POST("/refresh", authHandler.Refresh)
	}

	publicAPI := r.Group("/api")
	{
		publicAPI.GET("/articles", articleHandler.GetArticles)
		publicAPI.GET("/articles/hot", articleHandler.GetHotArticles)
		publicAPI.GET("/articles/:id", OptionalAuthMiddleware(authService), articleHandler.GetArticleByID)
		publicAPI.GET("/articles/:id/like", articleHandler.GetArticleLikes)
		publicAPI.GET("/articles/:id/comments", commentHandler.GetComments)
	}

	protectedAPI := r.Group("/api")
	protectedAPI.Use(AuthMiddleware(authService))
	{
		protectedAPI.POST("/auth/logout", authHandler.Logout)
		protectedAPI.POST("/articles", RateLimitMiddleware(deps.RedisDB, publishRateLimitRule), articleHandler.CreateArticle)
		protectedAPI.POST("/articles/:id/like", articleHandler.LikeArticle)
		protectedAPI.POST("/articles/:id/unlock", pointsHandler.UnlockArticle)
		protectedAPI.POST("/articles/:id/comments", RateLimitMiddleware(deps.RedisDB, commentRateLimitRule), commentHandler.CreateComment)
		protectedAPI.DELETE("/comments/:id", commentHandler.DeleteComment)
		protectedAPI.POST("/articles/:id/favorite", favoriteHandler.CreateFavorite)
		protectedAPI.DELETE("/articles/:id/favorite", favoriteHandler.DeleteFavorite)
		protectedAPI.GET("/me/following/articles", articleHandler.GetFollowingArticles)
		protectedAPI.POST("/authors/:id/follow", socialHandler.FollowAuthor)
		protectedAPI.DELETE("/authors/:id/follow", socialHandler.UnfollowAuthor)
		protectedAPI.POST("/uploads/cover", mediaHandler.UploadCover)
		protectedAPI.POST("/uploads/content-images", mediaHandler.UploadContentImages)
		protectedAPI.GET("/me/favorites", favoriteHandler.ListMyFavorites)
		protectedAPI.GET("/me/points", pointsHandler.GetMyPoints)
		protectedAPI.GET("/me/points/records", pointsHandler.GetMyPointsRecords)
		protectedAPI.POST("/me/check-in", RateLimitMiddleware(deps.RedisDB, checkInRateLimitRule), pointsHandler.CheckIn)
		protectedAPI.POST("/me/points/redeem", pointsHandler.RedeemPrivilege)
	}
	publicAPI.GET("/authors/:id/social-status", OptionalAuthMiddleware(authService), socialHandler.GetAuthorSocialStatus)
	r.Static("/uploads/covers", filepath.Join(deps.UploadDir, "covers"))
	r.GET("/uploads/content/*filepath", OptionalAuthMiddleware(authService), func(ctx *gin.Context) {
		serveProtectedContentImage(ctx, articleService, deps.UploadDir)
	})
	return r
}

func serveProtectedContentImage(ctx *gin.Context, articleService *internalArticle.Service, uploadDir string) {
	cleanPath := path.Clean("/" + ctx.Param("filepath"))
	if cleanPath == "/" {
		ctx.Status(http.StatusNotFound)
		return
	}

	publicURL := "/uploads/content" + cleanPath
	allowed, err := articleService.CanAccessContentImage(publicURL, ctx.GetUint("userID"))
	if err != nil || !allowed {
		ctx.Status(http.StatusNotFound)
		return
	}

	localPath := filepath.Join(uploadDir, "content", filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
	ctx.File(localPath)
}
