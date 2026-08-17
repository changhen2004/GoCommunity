package favorite

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"resource_community_go/internal/cachekey"
)

type Repo struct {
	db      *gorm.DB
	redisDB *redis.Client
}

func NewRepo(db *gorm.DB, redisDB *redis.Client) *Repo {
	return &Repo{db: db, redisDB: redisDB}
}

func (r *Repo) ArticleExists(articleID uint) (bool, error) {
	var count int64
	if err := r.db.Table("articles").Where("id = ? AND status = ?", articleID, "published").Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repo) HasFavorite(articleID, userID uint) (bool, error) {
	var count int64
	if err := r.db.Model(&Favorite{}).
		Where("article_id = ? AND user_id = ?", articleID, userID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repo) Create(articleID, userID uint) (uint, error) {
	var count uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		favorite := Favorite{
			ArticleID: articleID,
			UserID:    userID,
		}
		if err := tx.Create(&favorite).Error; err != nil {
			return err
		}

		if err := tx.Table("articles").
			Where("id = ?", articleID).
			UpdateColumn("favorite_count", gorm.Expr("favorite_count + ?", 1)).
			Error; err != nil {
			return err
		}

		return tx.Table("articles").Select("favorite_count").Where("id = ?", articleID).Scan(&count).Error
	})
	return count, err
}

func (r *Repo) Delete(articleID, userID uint) (uint, error) {
	var count uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("article_id = ? AND user_id = ?", articleID, userID).Delete(&Favorite{}).Error; err != nil {
			return err
		}
		if err := tx.Table("articles").
			Where("id = ?", articleID).
			UpdateColumn("favorite_count", gorm.Expr("favorite_count - ?", 1)).
			Error; err != nil {
			return err
		}

		return tx.Table("articles").Select("favorite_count").Where("id = ?", articleID).Scan(&count).Error
	})
	return count, err
}

func (r *Repo) ListByUserID(userID uint) ([]FavoriteArticleResponse, error) {
	rows := make([]struct {
		ArticleID      uint
		Title          string
		Preview        string
		Tags           string
		FavoriteCount  uint
		IsFree         bool
		RequiredPoints uint
		AuthorID       uint      `gorm:"column:author_id"`
		AuthorUsername string    `gorm:"column:author_username"`
		FavoritedAt    time.Time `gorm:"column:favorited_at"`
	}, 0)

	err := r.db.Table("favorites").
		Select(
			"articles.id AS article_id, articles.title, articles.preview, articles.tags, articles.favorite_count, articles.is_free, articles.required_points, "+
				"users.id AS author_id, users.username AS author_username, favorites.created_at AS favorited_at",
		).
		Joins("INNER JOIN articles ON articles.id = favorites.article_id").
		Joins("LEFT JOIN users ON users.id = articles.author_id").
		Where("favorites.user_id = ?", userID).
		Order("favorites.created_at DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	responses := make([]FavoriteArticleResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, FavoriteArticleResponse{
			ArticleID:      row.ArticleID,
			Title:          row.Title,
			Preview:        row.Preview,
			Tags:           splitTags(row.Tags),
			FavoriteCount:  row.FavoriteCount,
			IsFree:         row.IsFree,
			RequiredPoints: row.RequiredPoints,
			Author: FavoriteArticleAuthorResponse{
				ID:       row.AuthorID,
				Username: row.AuthorUsername,
			},
			FavoritedAt: row.FavoritedAt,
		})
	}
	return responses, nil
}

func splitTags(tags string) []string {
	if strings.TrimSpace(tags) == "" {
		return []string{}
	}
	return strings.Split(tags, ",")
}

func (r *Repo) InvalidateArticleCaches(ctx context.Context, articleID uint) {
	cachekey.DeleteByPrefix(ctx, r.redisDB, cachekey.ArticleListPrefix)
	cachekey.DeleteByPrefix(ctx, r.redisDB, cachekey.ArticleHotPrefix)
	cachekey.DeleteKeys(ctx, r.redisDB, cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articleID), 10)))
}
