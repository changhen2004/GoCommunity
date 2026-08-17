package article

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"resource_community_go/internal/cachekey"
	"resource_community_go/internal/social"
)

type Repo struct {
	db      *gorm.DB
	redisDB *redis.Client
}

type articleAuthor struct {
	ID       uint
	Username string
}

func NewRepo(db *gorm.DB, redisDB *redis.Client) *Repo {
	return &Repo{db: db, redisDB: redisDB}
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *Repo) Create(article *Article) error {
	return r.db.Create(article).Error
}

func (r *Repo) List(query ListArticlesQuery) ([]Article, error) {
	var articles []Article

	db := r.db.Model(&Article{}).Where("status = ?", "published")
	if query.Keyword != "" {
		db = db.Where("title LIKE ?", "%"+query.Keyword+"%")
	}
	if query.Tag != "" {
		db = r.applyTagFilter(db, query.Tag)
	}

	switch query.Sort {
	case "hot":
		db = db.Order("like_count DESC").Order("view_count DESC").Order("created_at DESC")
	default:
		db = db.Order("created_at DESC")
	}
	//分页查询文章列表：计算 SQL 分页里的“跳过多少条”
	offset := (query.Page - 1) * query.PageSize
	if err := db.Offset(offset).Limit(query.PageSize).Find(&articles).Error; err != nil {
		return nil, err
	}
	return articles, nil
}

func (r *Repo) ListFollowing(ctx context.Context, query FollowingFeedQuery) ([]Article, error) {
	ctx = normalizeContext(ctx)
	if query.FollowerID == 0 {
		return []Article{}, nil
	}
	if query.PageSize < 1 {
		query.PageSize = 10
	}

	followingSubQuery := r.db.WithContext(ctx).
		Model(&social.Follow{}).
		Select("author_id").
		Where("follower_id = ?", query.FollowerID)

	db := r.db.WithContext(ctx).
		Model(&Article{}).
		Where("author_id IN (?)", followingSubQuery).
		Where("status = ?", "published").
		Order("created_at DESC").
		Order("id DESC")

	if !query.BeforeCreatedAt.IsZero() && query.BeforeID > 0 {
		db = db.Where(
			"(created_at < ?) OR (created_at = ? AND id < ?)",
			query.BeforeCreatedAt,
			query.BeforeCreatedAt,
			query.BeforeID,
		)
	}

	var articles []Article
	if err := db.Limit(query.PageSize).Find(&articles).Error; err != nil {
		return nil, err
	}
	return articles, nil
}

func (r *Repo) FindByID(id string) (*Article, error) {
	var article Article
	if err := r.db.Where("id = ?", id).First(&article).Error; err != nil {
		return nil, err
	}
	return &article, nil
}

func (r *Repo) FindPublishedByID(id string) (*Article, error) {
	var article Article
	if err := r.db.Where("id = ? AND status = ?", id, "published").First(&article).Error; err != nil {
		return nil, err
	}
	return &article, nil
}

func (r *Repo) FindByContentImageURL(url string) (*Article, error) {
	var articles []Article
	if err := r.db.Where("content_images LIKE ?", "%"+url+"%").Find(&articles).Error; err != nil {
		return nil, err
	}
	for _, article := range articles {
		for _, contentImage := range splitContentImages(article.ContentImages) {
			if contentImage == url {
				return &article, nil
			}
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *Repo) FindAuthorByID(authorID uint) (ArticleAuthorResponse, error) {
	if authorID == 0 {
		return ArticleAuthorResponse{}, nil
	}

	var author articleAuthor
	if err := r.db.Table("users").Select("id, username").Where("id = ?", authorID).Take(&author).Error; err != nil {
		return ArticleAuthorResponse{}, err
	}

	return ArticleAuthorResponse{
		ID:       author.ID,
		Username: author.Username,
	}, nil
}

func (r *Repo) HasUnlocked(articleID, userID uint) (bool, error) {
	if articleID == 0 || userID == 0 {
		return false, nil
	}

	var count int64
	if err := r.db.Model(&ArticleUnlock{}).
		Where("article_id = ? AND user_id = ?", articleID, userID).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *Repo) DeleteArticlesCacheByPrefix(ctx context.Context, prefix string) {
	cachekey.DeleteByPrefix(ctx, r.redisDB, prefix)
}

func (r *Repo) GetArticlesCache(ctx context.Context, key string) (string, error) {
	if r.redisDB == nil {
		return "", redis.Nil
	}
	ctx = normalizeContext(ctx)
	return r.redisDB.Get(ctx, key).Result()
}

func (r *Repo) SetArticlesCache(ctx context.Context, key, value string, ttl time.Duration) {
	if r.redisDB == nil {
		return
	}
	ctx = normalizeContext(ctx)
	_ = r.redisDB.Set(ctx, key, value, ttl).Err()
}

func (r *Repo) DeleteArticleCacheKeys(ctx context.Context, keys ...string) {
	ctx = normalizeContext(ctx)
	cachekey.DeleteKeys(ctx, r.redisDB, keys...)
}

func (r *Repo) Like(ctx context.Context, articleID, userID uint) (int, bool, error) {
	ctx = normalizeContext(ctx)

	var likes int64
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ArticleLike{
			ArticleID: articleID,
			UserID:    userID,
		})
		if result.Error != nil {
			return result.Error
		}
		changed = result.RowsAffected > 0

		if err := tx.Model(&ArticleLike{}).Where("article_id = ?", articleID).Count(&likes).Error; err != nil {
			return err
		}
		return tx.Model(&Article{}).
			Where("id = ?", articleID).
			UpdateColumn("like_count", likes).
			Error
	})
	if err != nil {
		return 0, false, err
	}

	if err := r.syncLikeCountCache(ctx, articleID, int(likes), boolToDelta(changed)); err != nil {
		return 0, false, err
	}
	return int(likes), changed, nil
}

func (r *Repo) Unlike(ctx context.Context, articleID, userID uint) (int, bool, error) {
	ctx = normalizeContext(ctx)

	var likes int64
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("article_id = ? AND user_id = ?", articleID, userID).Delete(&ArticleLike{})
		if result.Error != nil {
			return result.Error
		}
		changed = result.RowsAffected > 0

		if err := tx.Model(&ArticleLike{}).Where("article_id = ?", articleID).Count(&likes).Error; err != nil {
			return err
		}
		return tx.Model(&Article{}).
			Where("id = ?", articleID).
			UpdateColumn("like_count", likes).
			Error
	})
	if err != nil {
		return 0, false, err
	}

	if err := r.syncLikeCountCache(ctx, articleID, int(likes), -boolToDelta(changed)); err != nil {
		return 0, false, err
	}
	return int(likes), changed, nil
}

func (r *Repo) GetLikeCount(ctx context.Context, articleID string) (int, error) {
	parsedArticleID, err := strconv.ParseUint(articleID, 10, 64)
	if err != nil {
		return 0, err
	}

	var likes int64
	if err := r.db.WithContext(normalizeContext(ctx)).
		Model(&ArticleLike{}).
		Where("article_id = ?", uint(parsedArticleID)).
		Count(&likes).Error; err != nil {
		return 0, err
	}
	if r.redisDB != nil {
		_ = r.setLikeCountCache(ctx, articleID, int(likes))
	}
	return int(likes), nil
}

func (r *Repo) syncLikeCountCache(ctx context.Context, articleID uint, count int, delta int) error {
	if r.redisDB == nil {
		return nil
	}

	script := redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if tonumber(ARGV[1]) == 0 then
	redis.call("SET", KEYS[1], ARGV[2])
	return tonumber(ARGV[2])
end
if current then
	local next = redis.call("INCRBY", KEYS[1], ARGV[1])
	if tonumber(next) < 0 then
		redis.call("SET", KEYS[1], 0)
		return 0
	end
	return next
end
redis.call("SET", KEYS[1], ARGV[2])
return tonumber(ARGV[2])
`)
	_, err := script.Run(
		ctx,
		r.redisDB,
		[]string{articleLikeKey(strconv.FormatUint(uint64(articleID), 10))},
		delta,
		count,
	).Result()
	return err
}

func (r *Repo) setLikeCountCache(ctx context.Context, articleID string, count int) error {
	if r.redisDB == nil {
		return nil
	}
	return r.redisDB.Set(normalizeContext(ctx), articleLikeKey(articleID), count, 0).Err()
}

func articleLikeKey(articleID string) string {
	return "article:" + articleID + ":like"
}

func boolToDelta(changed bool) int {
	if changed {
		return 1
	}
	return 0
}

func (r *Repo) IncrementView(ctx context.Context, articleID string) (*Article, error) {
	if err := r.db.Model(&Article{}).
		Where("id = ?", articleID).
		UpdateColumn("view_count", gorm.Expr("view_count + ?", 1)).
		Error; err != nil {
		return nil, err
	}

	return r.FindByID(articleID)
}

func (r *Repo) AddHotScore(ctx context.Context, articleID uint, delta float64) error {
	if r.redisDB == nil || articleID == 0 || delta == 0 {
		return nil
	}

	ctx = normalizeContext(ctx)
	return r.redisDB.ZIncrBy(ctx, cachekey.ArticleHotZSetKey, delta, strconv.FormatUint(uint64(articleID), 10)).Err()
}

func (r *Repo) SetInitialHotScore(ctx context.Context, articleID uint, score float64) error {
	if r.redisDB == nil || articleID == 0 {
		return nil
	}

	ctx = normalizeContext(ctx)
	return r.redisDB.ZAdd(ctx, cachekey.ArticleHotZSetKey, redis.Z{
		Score:  score,
		Member: strconv.FormatUint(uint64(articleID), 10),
	}).Err()
}

func (r *Repo) GetHotArticleIDs(ctx context.Context, limit int64) ([]uint, error) {
	if r.redisDB == nil || limit <= 0 {
		return nil, nil
	}

	ctx = normalizeContext(ctx)
	members, err := r.redisDB.ZRevRange(ctx, cachekey.ArticleHotZSetKey, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}

	ids := make([]uint, 0, len(members))
	for _, member := range members {
		parsed, parseErr := strconv.ParseUint(member, 10, 64)
		if parseErr != nil {
			continue
		}
		ids = append(ids, uint(parsed))
	}

	return ids, nil
}

func (r *Repo) ListByIDs(ids []uint) ([]Article, error) {
	if len(ids) == 0 {
		return []Article{}, nil
	}

	var articles []Article
	if err := r.db.Where("id IN ? AND status = ?", ids, "published").Find(&articles).Error; err != nil {
		return nil, err
	}

	ordered := make(map[uint]Article, len(articles))
	for _, article := range articles {
		ordered[article.ID] = article
	}

	result := make([]Article, 0, len(ids))
	for _, id := range ids {
		article, ok := ordered[id]
		if !ok {
			continue
		}
		result = append(result, article)
	}

	return result, nil
}

func (r *Repo) SeedHotRanking(ctx context.Context, limit int) error {
	if r.redisDB == nil {
		return nil
	}

	ctx = normalizeContext(ctx)
	var count int64
	count, err := r.redisDB.ZCard(ctx, cachekey.ArticleHotZSetKey).Result()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	if limit < 1 {
		limit = 100
	}

	var articles []Article
	if err := r.db.Where("status = ?", "published").
		Order("like_count DESC").
		Order("favorite_count DESC").
		Order("view_count DESC").
		Order("created_at DESC").
		Limit(limit).
		Find(&articles).Error; err != nil {
		return err
	}

	members := make([]redis.Z, 0, len(articles))
	for _, article := range articles {
		score := initialHotScore(article.CreatedAt) +
			float64(article.ViewCount)*hotScoreView +
			float64(article.LikeCount)*hotScoreLike +
			float64(article.CommentCount)*hotScoreComment +
			float64(article.FavoriteCount)*hotScoreFavorite
		members = append(members, redis.Z{
			Score:  score,
			Member: fmt.Sprintf("%d", article.ID),
		})
	}

	if len(members) == 0 {
		return nil
	}

	return r.redisDB.ZAdd(ctx, cachekey.ArticleHotZSetKey, members...).Err()
}

func (r *Repo) applyTagFilter(db *gorm.DB, tag string) *gorm.DB {
	normalizedTag := strings.ToLower(strings.TrimSpace(tag))
	if normalizedTag == "" {
		return db
	}

	switch r.db.Dialector.Name() {
	case "mysql":
		return db.Where("FIND_IN_SET(?, LOWER(tags)) > 0", normalizedTag)
	default:
		return db.Where("instr(',' || lower(tags) || ',', ',' || ? || ',') > 0", normalizedTag)
	}
}
