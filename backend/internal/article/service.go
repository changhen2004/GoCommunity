package article

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"resource_community_go/internal/asyncjob"
	"resource_community_go/internal/cachekey"
	internalMedia "resource_community_go/internal/media"
	internalPoints "resource_community_go/internal/points"

	"gorm.io/gorm"
)

type articleDetailCachePayload struct {
	ID             uint                  `json:"id"`
	Title          string                `json:"title"`
	Content        string                `json:"content"`
	Preview        string                `json:"preview"`
	CoverURL       string                `json:"coverUrl"`
	ContentImages  []string              `json:"contentImages"`
	Tags           []string              `json:"tags"`
	Status         string                `json:"status"`
	Author         ArticleAuthorResponse `json:"author"`
	Stats          ArticleStatsResponse  `json:"stats"`
	IsFree         bool                  `json:"isFree"`
	RequiredPoints uint                  `json:"requiredPoints"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}

type Service struct {
	repo          *Repo
	publisher     asyncjob.Publisher
	pointsService *internalPoints.Service
	cacheFillMu   sync.Mutex
	cacheFills    map[string]*articleDetailCacheFillCall
}

type articleDetailCacheFillCall struct {
	done    chan struct{}
	payload articleDetailCachePayload
	err     error
}

func NewService(repo *Repo, publisher asyncjob.Publisher, pointsService *internalPoints.Service) *Service {
	if publisher == nil {
		publisher = asyncjob.NoopPublisher{}
	}
	return &Service{
		repo:          repo,
		publisher:     publisher,
		pointsService: pointsService,
		cacheFills:    make(map[string]*articleDetailCacheFillCall),
	}
}

func (s *Service) Create(ctx context.Context, req CreateArticleRequest) (ArticleResponse, error) {
	isFree := true
	if req.IsFree != nil {
		isFree = *req.IsFree
	}
	if req.RequiredPoints > 0 {
		isFree = false
	}

	article := &Article{
		AuthorID:       userIDFromContext(ctx),
		Title:          req.Title,
		Content:        req.Content,
		Preview:        req.Preview,
		CoverURL:       req.CoverURL,
		ContentImages:  joinContentImages(req.ContentImages),
		Tags:           joinTags(req.Tags),
		Status:         req.Status,
		IsFree:         isFree,
		RequiredPoints: req.RequiredPoints,
	}
	if article.Status == "" {
		article.Status = "draft"
	}
	if len(normalizeContentImages(req.ContentImages)) > internalMedia.ContentImageMaxCount {
		return ArticleResponse{}, ErrTooManyContentImages
	}

	if err := s.repo.Create(article); err != nil {
		return ArticleResponse{}, err
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleFollowingPrefix)

	return toArticleResponse(*article), nil
}

func (s *Service) List(ctx context.Context, query ListArticlesQuery) ([]ArticleResponse, error) {
	cacheKey := cachekey.ArticleListKey(query.Page, query.PageSize, query.Sort, query.Keyword, query.Tag)
	cached, err := s.repo.GetArticlesCache(ctx, cacheKey)
	if err == nil {
		var responses []ArticleResponse
		if unmarshalErr := json.Unmarshal([]byte(cached), &responses); unmarshalErr == nil {
			return responses, nil
		}
	}

	articles, err := s.repo.List(query)
	if err != nil {
		return nil, err
	}

	responses := toArticleResponses(articles)
	if payload, marshalErr := json.Marshal(responses); marshalErr == nil {
		s.repo.SetArticlesCache(ctx, cacheKey, string(payload), cachekey.ArticleListTTL)
	}
	return responses, nil
}

func (s *Service) ListFollowing(ctx context.Context, query FollowingFeedQuery) (FollowingFeedResponse, error) {
	cacheKey := cachekey.ArticleFollowingKey(
		query.FollowerID,
		query.PageSize,
		query.CacheBeforeCreatedAt(),
		query.BeforeID,
	)
	cached, err := s.repo.GetArticlesCache(ctx, cacheKey)
	if err == nil {
		var response FollowingFeedResponse
		if unmarshalErr := json.Unmarshal([]byte(cached), &response); unmarshalErr == nil {
			return response, nil
		}
	}

	repoQuery := query
	repoQuery.PageSize = query.PageSize + 1
	articles, err := s.repo.ListFollowing(ctx, repoQuery)
	if err != nil {
		return FollowingFeedResponse{}, err
	}

	hasMore := len(articles) > query.PageSize
	if hasMore {
		articles = articles[:query.PageSize]
	}

	response := FollowingFeedResponse{
		Items:   toArticleResponses(articles),
		HasMore: hasMore,
	}
	if hasMore && len(articles) > 0 {
		last := articles[len(articles)-1]
		response.NextCursor = &FollowingFeedCursorResponse{
			BeforeCreatedAt: last.CreatedAt.Format(time.RFC3339Nano),
			BeforeID:        last.ID,
		}
	}
	if payload, marshalErr := json.Marshal(response); marshalErr == nil {
		s.repo.SetArticlesCache(ctx, cacheKey, string(payload), cachekey.ArticleFollowingTTL)
	}
	return response, nil
}

func (s *Service) FindByID(id string) (ArticleResponse, error) {
	article, err := s.repo.FindByID(id)
	if err != nil {
		return ArticleResponse{}, err
	}

	return toArticleResponse(*article), nil
}

func (s *Service) CanAccessContentImage(url string, currentUserID uint) (bool, error) {
	article, err := s.repo.FindByContentImageURL(url)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrArticleNotFound
		}
		return false, err
	}
	if article.Status == "draft" {
		return article.AuthorID == currentUserID, nil
	}
	if article.Status != "published" {
		return false, nil
	}
	return s.resolveUnlockStatus(*article, currentUserID)
}

func (s *Service) GetDetail(id string, currentUserID uint) (ArticleDetailResponse, error) {
	ctx := context.Background()

	cacheKey := cachekey.ArticleDetailKey(id)
	if payload, hit, err := s.getArticleDetailCachePayload(ctx, cacheKey); hit || err != nil {
		if err != nil {
			return ArticleDetailResponse{}, err
		}
		return s.articleDetailPayloadToResponse(ctx, payload, currentUserID)
	}

	payload, err := s.doArticleDetailCacheFill(cacheKey, func() (articleDetailCachePayload, error) {
		if payload, hit, err := s.getArticleDetailCachePayload(ctx, cacheKey); hit || err != nil {
			return payload, err
		}
		return s.loadArticleDetailCachePayload(ctx, id, cacheKey)
	})
	if err != nil {
		return ArticleDetailResponse{}, err
	}
	return s.articleDetailPayloadToResponse(ctx, payload, currentUserID)
}

func (s *Service) getArticleDetailCachePayload(ctx context.Context, cacheKey string) (articleDetailCachePayload, bool, error) {
	cached, err := s.repo.GetArticlesCache(ctx, cacheKey)
	if err != nil {
		return articleDetailCachePayload{}, false, nil
	}

	if cached == cachekey.CacheNullValue {
		return articleDetailCachePayload{}, true, ErrArticleNotFound
	}

	var payload articleDetailCachePayload
	if unmarshalErr := json.Unmarshal([]byte(cached), &payload); unmarshalErr != nil {
		return articleDetailCachePayload{}, false, nil
	}
	return payload, true, nil
}

func (s *Service) loadArticleDetailCachePayload(ctx context.Context, id, cacheKey string) (articleDetailCachePayload, error) {
	article, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.repo.SetArticlesCache(ctx, cacheKey, cachekey.CacheNullValue, cachekey.JitterTTL(cachekey.ArticleDetailNullTTL))
			return articleDetailCachePayload{}, ErrArticleNotFound
		}
		return articleDetailCachePayload{}, err
	}
	author, err := s.repo.FindAuthorByID(article.AuthorID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			author = ArticleAuthorResponse{}
		} else {
			return articleDetailCachePayload{}, err
		}
	}

	response := toArticleDetailResponse(*article, author, false)
	payload := newArticleDetailCachePayload(response)
	if marshaled, marshalErr := json.Marshal(payload); marshalErr == nil {
		s.repo.SetArticlesCache(ctx, cacheKey, string(marshaled), cachekey.JitterTTL(cachekey.ArticleDetailTTL))
	}
	return payload, nil
}

func (s *Service) publishArticleView(ctx context.Context, articleID uint) (bool, error) {
	if articleID == 0 {
		return false, nil
	}

	if err := s.publisher.Publish(ctx, asyncjob.Job{
		Type: asyncjob.TypeArticleViewed,
		Payload: map[string]uint{
			"articleID": articleID,
		},
	}); err != nil {
		if err := s.RecordView(ctx, articleID); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) articleDetailPayloadToResponse(ctx context.Context, payload articleDetailCachePayload, currentUserID uint) (ArticleDetailResponse, error) {
	if payload.Status == "draft" {
		if payload.Author.ID != currentUserID {
			return ArticleDetailResponse{}, ErrArticleNotFound
		}
		return payload.toResponse(true), nil
	}
	if recordedSynchronously, recordErr := s.publishArticleView(ctx, payload.ID); recordErr != nil {
		return ArticleDetailResponse{}, recordErr
	} else if recordedSynchronously {
		payload.Stats.ViewCount++
	}

	isUnlocked, err := s.resolveUnlockStatus(Article{
		Model:          gorm.Model{ID: payload.ID},
		AuthorID:       payload.Author.ID,
		IsFree:         payload.IsFree,
		RequiredPoints: payload.RequiredPoints,
	}, currentUserID)
	if err != nil {
		return ArticleDetailResponse{}, err
	}
	return payload.toResponse(isUnlocked), nil
}

func (s *Service) doArticleDetailCacheFill(cacheKey string, fill func() (articleDetailCachePayload, error)) (articleDetailCachePayload, error) {
	s.cacheFillMu.Lock()
	if s.cacheFills == nil {
		s.cacheFills = make(map[string]*articleDetailCacheFillCall)
	}
	if call, ok := s.cacheFills[cacheKey]; ok {
		s.cacheFillMu.Unlock()
		<-call.done
		return call.payload, call.err
	}

	call := &articleDetailCacheFillCall{done: make(chan struct{})}
	s.cacheFills[cacheKey] = call
	s.cacheFillMu.Unlock()

	call.payload, call.err = fill()
	close(call.done)

	s.cacheFillMu.Lock()
	delete(s.cacheFills, cacheKey)
	s.cacheFillMu.Unlock()

	return call.payload, call.err
}

func (s *Service) Like(ctx context.Context, articleID string, userID uint) (LikeActionResponse, error) {
	parsedArticleID, err := strconv.ParseUint(articleID, 10, 64)
	if err != nil {
		return LikeActionResponse{}, ErrArticleNotFound
	}
	if _, err := s.repo.FindPublishedByID(articleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LikeActionResponse{}, ErrArticleNotFound
		}
		return LikeActionResponse{}, err
	}
	likes, changed, err := s.repo.Like(ctx, uint(parsedArticleID), userID)
	if err != nil {
		return LikeActionResponse{}, err
	}
	if changed {
		if err := s.repo.AddHotScore(ctx, uint(parsedArticleID), hotScoreLike); err != nil {
			return LikeActionResponse{}, err
		}
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(articleID))

	return LikeActionResponse{
		Message: "article liked successfully",
		Likes:   likes,
	}, nil
}

func (s *Service) Unlike(ctx context.Context, articleID string, userID uint) (LikeActionResponse, error) {
	parsedArticleID, err := strconv.ParseUint(articleID, 10, 64)
	if err != nil {
		return LikeActionResponse{}, ErrArticleNotFound
	}
	if _, err := s.repo.FindPublishedByID(articleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LikeActionResponse{}, ErrArticleNotFound
		}
		return LikeActionResponse{}, err
	}
	likes, changed, err := s.repo.Unlike(ctx, uint(parsedArticleID), userID)
	if err != nil {
		return LikeActionResponse{}, err
	}
	if changed {
		if err := s.repo.AddHotScore(ctx, uint(parsedArticleID), -hotScoreLike); err != nil {
			return LikeActionResponse{}, err
		}
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(articleID))

	return LikeActionResponse{
		Message: "article unliked successfully",
		Likes:   likes,
	}, nil
}

func (s *Service) GetLikes(ctx context.Context, articleID string) (LikeResponse, error) {
	if _, err := s.repo.FindPublishedByID(articleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LikeResponse{}, ErrArticleNotFound
		}
		return LikeResponse{}, err
	}
	likes, err := s.repo.GetLikeCount(ctx, articleID)
	if err != nil {
		return LikeResponse{}, err
	}
	return LikeResponse{Likes: likes}, nil
}

func (s *Service) ListHot(ctx context.Context, limit int) ([]ArticleResponse, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	cacheKey := cachekey.ArticleHotKey(limit)
	cached, err := s.repo.GetArticlesCache(ctx, cacheKey)
	if err == nil {
		var responses []ArticleResponse
		if unmarshalErr := json.Unmarshal([]byte(cached), &responses); unmarshalErr == nil {
			return responses, nil
		}
	}

	if err := s.repo.SeedHotRanking(ctx, 200); err != nil {
		return nil, err
	}

	hotIDs, err := s.repo.GetHotArticleIDs(ctx, int64(limit))
	if err != nil {
		return nil, err
	}

	var articles []Article
	if len(hotIDs) > 0 {
		articles, err = s.repo.ListByIDs(hotIDs)
		if err != nil {
			return nil, err
		}
	} else {
		articles, err = s.repo.List(NewListArticlesQuery(1, limit, "hot", "", ""))
		if err != nil {
			return nil, err
		}
	}
	responses := toArticleResponses(articles)
	if payload, marshalErr := json.Marshal(responses); marshalErr == nil {
		s.repo.SetArticlesCache(ctx, cacheKey, string(payload), cachekey.ArticleHotTTL)
	}
	return responses, nil
}

func (s *Service) SetInitialHeat(ctx context.Context, articleID uint) error {
	article, err := s.repo.FindByID(strconv.FormatUint(uint64(articleID), 10))
	if err != nil {
		return err
	}
	return s.repo.SetInitialHotScore(ctx, article.ID, initialHotScore(article.CreatedAt))
}

func (s *Service) RecordView(ctx context.Context, articleID uint) error {
	article, err := s.repo.IncrementView(ctx, strconv.FormatUint(uint64(articleID), 10))
	if err != nil {
		return err
	}
	if err := s.repo.AddHotScore(ctx, article.ID, hotScoreView); err != nil {
		return err
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articleID), 10)))
	return nil
}

func (s *Service) ApplyLike(ctx context.Context, articleID uint) error {
	if err := s.repo.AddHotScore(ctx, articleID, hotScoreLike); err != nil {
		return err
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articleID), 10)))
	return nil
}

func (s *Service) RecordCommentHeat(ctx context.Context, articleID uint) error {
	if err := s.repo.AddHotScore(ctx, articleID, hotScoreComment); err != nil {
		return err
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articleID), 10)))
	return nil
}

func (s *Service) RevertCommentHeat(ctx context.Context, articleID uint) error {
	if err := s.repo.AddHotScore(ctx, articleID, -hotScoreComment); err != nil {
		return err
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articleID), 10)))
	return nil
}

func (s *Service) RecordFavoriteHeat(ctx context.Context, articleID uint, increase bool) error {
	delta := float64(-hotScoreFavorite)
	if increase {
		delta = hotScoreFavorite
	}

	if err := s.repo.AddHotScore(ctx, articleID, delta); err != nil {
		return err
	}
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleHotPrefix)
	s.repo.DeleteArticlesCacheByPrefix(ctx, cachekey.ArticleListPrefix)
	s.repo.DeleteArticleCacheKeys(ctx, cachekey.ArticleDetailKey(strconv.FormatUint(uint64(articleID), 10)))
	return nil
}

func (s *Service) resolveUnlockStatus(article Article, currentUserID uint) (bool, error) {
	if article.IsFree || article.RequiredPoints == 0 {
		return true, nil
	}
	if currentUserID == 0 {
		return false, nil
	}
	if article.AuthorID == currentUserID {
		return true, nil
	}
	return s.repo.HasUnlocked(article.ID, currentUserID)
}

func userIDFromContext(ctx context.Context) uint {
	if ctx == nil {
		return 0
	}

	value := ctx.Value("userID")
	userID, ok := value.(uint)
	if !ok {
		return 0
	}
	return userID
}

func newArticleDetailCachePayload(detail ArticleDetailResponse) articleDetailCachePayload {
	return articleDetailCachePayload{
		ID:             detail.ID,
		Title:          detail.Title,
		Content:        detail.Content,
		Preview:        detail.Preview,
		CoverURL:       detail.CoverURL,
		ContentImages:  detail.ContentImages,
		Tags:           detail.Tags,
		Status:         detail.Status,
		Author:         detail.Author,
		Stats:          detail.Stats,
		IsFree:         detail.IsFree,
		RequiredPoints: detail.RequiredPoints,
		CreatedAt:      detail.CreatedAt,
		UpdatedAt:      detail.UpdatedAt,
	}
}

func (p articleDetailCachePayload) toResponse(isUnlocked bool) ArticleDetailResponse {
	content := p.Content
	contentImages := p.ContentImages
	if !isUnlocked {
		content = ""
		contentImages = []string{}
	}
	return ArticleDetailResponse{
		ID:             p.ID,
		Title:          p.Title,
		Content:        content,
		Preview:        p.Preview,
		CoverURL:       p.CoverURL,
		ContentImages:  contentImages,
		Tags:           p.Tags,
		Status:         p.Status,
		Author:         p.Author,
		Stats:          p.Stats,
		IsFree:         p.IsFree,
		RequiredPoints: p.RequiredPoints,
		IsUnlocked:     isUnlocked,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}
