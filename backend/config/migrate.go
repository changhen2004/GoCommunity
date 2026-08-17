package config

import (
	"resource_community_go/internal/article"
	"resource_community_go/internal/auth"
	"resource_community_go/internal/comment"
	"resource_community_go/internal/favorite"
	"resource_community_go/internal/points"
	"resource_community_go/internal/social"

	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&auth.User{},
		&article.Article{},
		&article.ArticleUnlock{},
		&article.ArticleLike{},
		&comment.Comment{},
		&favorite.Favorite{},
		&points.PointLedger{},
		&points.UserCheckIn{},
		&points.UserPrivilege{},
		&social.Follow{},
	)
}
