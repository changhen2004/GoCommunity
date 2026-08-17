package comment

import (
	"context"
	"errors"
	"strconv"

	"gorm.io/gorm"
	internalArticle "resource_community_go/internal/article"
	"resource_community_go/internal/asyncjob"
	internalPoints "resource_community_go/internal/points"
)

type Service struct {
	repo           *Repo
	articleService *internalArticle.Service
	publisher      asyncjob.Publisher
	pointsService  *internalPoints.Service
}

func NewService(repo *Repo, articleService *internalArticle.Service, publisher asyncjob.Publisher, pointsService *internalPoints.Service) *Service {
	if publisher == nil {
		publisher = asyncjob.NoopPublisher{}
	}
	return &Service{repo: repo, articleService: articleService, publisher: publisher, pointsService: pointsService}
}

func (s *Service) Create(ctx context.Context, articleID string, req CreateCommentRequest) (CommentResponse, error) {
	parsedArticleID, err := strconv.ParseUint(articleID, 10, 64)
	if err != nil {
		return CommentResponse{}, ErrArticleNotFound
	}

	exists, err := s.repo.ArticleExists(uint(parsedArticleID))
	if err != nil {
		return CommentResponse{}, err
	}
	if !exists {
		return CommentResponse{}, ErrArticleNotFound
	}

	comment := &Comment{
		ArticleID: uint(parsedArticleID),
		UserID:    userIDFromContext(ctx),
		Content:   req.Content,
	}
	if err := s.repo.Create(comment); err != nil {
		return CommentResponse{}, err
	}
	if err := s.publisher.Publish(ctx, asyncjob.Job{
		Type: asyncjob.TypeCommentCreated,
		Payload: map[string]uint{
			"userID":    comment.UserID,
			"articleID": comment.ArticleID,
			"commentID": comment.ID,
		},
	}); err != nil {
		if s.pointsService != nil {
			if err := s.pointsService.AwardQualityInteraction(comment.UserID, comment.ID); err != nil {
				return CommentResponse{}, err
			}
		}
		if s.articleService != nil {
			if err := s.articleService.RecordCommentHeat(ctx, comment.ArticleID); err != nil {
				return CommentResponse{}, err
			}
		}
	}

	author := CommentAuthorResponse{
		ID:       comment.UserID,
		Username: usernameFromContext(ctx),
	}
	if author.Username == "" {
		author, err = s.repo.FindAuthorByID(comment.UserID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return CommentResponse{}, err
		}
	}

	return CommentResponse{
		ID:        comment.ID,
		ArticleID: comment.ArticleID,
		UserID:    comment.UserID,
		Content:   comment.Content,
		Author:    author,
		CreatedAt: comment.CreatedAt,
		UpdatedAt: comment.UpdatedAt,
	}, nil
}

func (s *Service) List(articleID string) ([]CommentResponse, error) {
	parsedArticleID, err := strconv.ParseUint(articleID, 10, 64)
	if err != nil {
		return nil, ErrArticleNotFound
	}

	exists, err := s.repo.ArticleExists(uint(parsedArticleID))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrArticleNotFound
	}

	return s.repo.ListByArticleID(uint(parsedArticleID))
}

func (s *Service) Delete(commentID string, userID uint) (DeleteCommentResponse, error) {
	parsedCommentID, err := strconv.ParseUint(commentID, 10, 64)
	if err != nil {
		return DeleteCommentResponse{}, ErrCommentNotFound
	}

	comment, err := s.repo.FindByID(uint(parsedCommentID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DeleteCommentResponse{}, ErrCommentNotFound
		}
		return DeleteCommentResponse{}, err
	}
	if comment.UserID != userID {
		return DeleteCommentResponse{}, ErrForbidden
	}
	if err := s.repo.Delete(comment); err != nil {
		return DeleteCommentResponse{}, err
	}
	if err := s.publisher.Publish(context.Background(), asyncjob.Job{
		Type: asyncjob.TypeCommentDeleted,
		Payload: map[string]uint{
			"articleID": comment.ArticleID,
		},
	}); err != nil {
		if s.articleService != nil {
			if err := s.articleService.RevertCommentHeat(context.Background(), comment.ArticleID); err != nil {
				return DeleteCommentResponse{}, err
			}
		}
	}

	return DeleteCommentResponse{Message: "comment deleted successfully"}, nil
}

func userIDFromContext(ctx context.Context) uint {
	if ctx == nil {
		return 0
	}

	userID, ok := ctx.Value("userID").(uint)
	if !ok {
		return 0
	}
	return userID
}

func usernameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	username, ok := ctx.Value("username").(string)
	if !ok {
		return ""
	}
	return username
}
