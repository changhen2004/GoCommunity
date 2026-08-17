package article

import "gorm.io/gorm"

type Article struct {
	gorm.Model
	AuthorID       uint   `gorm:"index"`
	Title          string `binding:"required,max=200" gorm:"size:200;not null;index"`
	Content        string `binding:"required" gorm:"type:text;not null"`
	Preview        string `binding:"required,max=500" gorm:"size:500;not null"`
	CoverURL       string `gorm:"size:500;not null;default:''"`
	ContentImages  string `gorm:"type:text;not null"`
	Tags           string `gorm:"size:500;not null;default:'';index"`
	Status         string `binding:"omitempty,oneof=draft published archived" gorm:"size:20;not null;default:draft;index"`
	ViewCount      uint   `gorm:"not null;default:0"`
	LikeCount      uint   `gorm:"not null;default:0"`
	CommentCount   uint   `gorm:"not null;default:0"`
	FavoriteCount  uint   `gorm:"not null;default:0"`
	IsFree         bool   `gorm:"not null;index"`
	RequiredPoints uint   `gorm:"not null;default:0"`
}

type ArticleUnlock struct {
	gorm.Model
	ArticleID uint `gorm:"not null;index:idx_article_unlocks_article_user,unique"`
	UserID    uint `gorm:"not null;index:idx_article_unlocks_article_user,unique"`
}

type ArticleLike struct {
	ID        uint `gorm:"primarykey"`
	ArticleID uint `gorm:"not null;index:idx_article_likes_article_user,unique"`
	UserID    uint `gorm:"not null;index:idx_article_likes_article_user,unique"`
}
