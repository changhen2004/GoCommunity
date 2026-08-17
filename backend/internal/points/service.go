package points

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
	"resource_community_go/internal/cachekey"
)

const (
	DailyCheckInAward       uint = 5
	PublishResourceAward    uint = 10
	QualityInteractionAward uint = 2
)

var privilegeCosts = map[string]uint{
	"feature_article":  30,
	"priority_publish": 50,
}

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetSummary(userID uint) (PointsSummaryResponse, error) {
	cacheKey := cachekey.PointsSummaryKey(userID)
	cached, err := s.repo.GetSummaryCache(context.Background(), cacheKey)
	if err == nil {
		var response PointsSummaryResponse
		if unmarshalErr := json.Unmarshal([]byte(cached), &response); unmarshalErr == nil {
			return response, nil
		}
	}

	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PointsSummaryResponse{}, ErrUserNotFound
		}
		return PointsSummaryResponse{}, err
	}

	privileges, err := s.repo.ListPrivileges(userID)
	if err != nil {
		return PointsSummaryResponse{}, err
	}

	response := PointsSummaryResponse{
		Balance:    user.Points,
		Privileges: privileges,
	}
	if payload, marshalErr := json.Marshal(response); marshalErr == nil {
		s.repo.SetSummaryCache(context.Background(), cacheKey, string(payload))
	}
	return response, nil
}

func (s *Service) ListRecords(userID uint) ([]PointsRecordResponse, error) {
	return s.repo.ListRecords(userID)
}

func (s *Service) CheckIn(userID uint) (CheckInResponse, error) {
	date := todayString(time.Now())
	operationKey := fmt.Sprintf("check_in:%d:%s", userID, date)
	balance, err := s.repo.CreateCheckInAndAwardWithKey(userID, date, DailyCheckInAward, "daily check-in reward", operationKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CheckInResponse{}, ErrUserNotFound
		}
		return CheckInResponse{}, err
	}

	return CheckInResponse{
		Message:       "check-in successful",
		AwardedPoints: DailyCheckInAward,
		Balance:       balance,
	}, nil
}

func (s *Service) AwardPublishResource(userID, articleID uint) error {
	if userID == 0 || articleID == 0 {
		return nil
	}
	operationKey := fmt.Sprintf("publish_resource:%d:%d", userID, articleID)
	_, err := s.repo.AwardPointsWithKey(userID, PublishResourceAward, "publish_resource", "article", articleID, "publish resource reward", operationKey)
	return err
}

func (s *Service) AwardQualityInteraction(userID, commentID uint) error {
	if userID == 0 || commentID == 0 {
		return nil
	}
	operationKey := fmt.Sprintf("quality_interaction:%d:%d", userID, commentID)
	_, err := s.repo.AwardPointsWithKey(userID, QualityInteractionAward, "quality_interaction", "comment", commentID, "quality interaction reward", operationKey)
	return err
}

func (s *Service) UnlockArticle(userID uint, articleID string) (UnlockArticleResponse, error) {
	parsedArticleID, err := strconv.ParseUint(articleID, 10, 64)
	if err != nil {
		return UnlockArticleResponse{}, ErrArticleNotFound
	}

	article, err := s.repo.FindArticleByID(uint(parsedArticleID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return UnlockArticleResponse{}, ErrArticleNotFound
		}
		return UnlockArticleResponse{}, err
	}

	if article.IsFree || article.RequiredPoints == 0 || article.AuthorID == userID {
		user, getUserErr := s.repo.GetUserByID(userID)
		if getUserErr != nil {
			return UnlockArticleResponse{}, getUserErr
		}
		return UnlockArticleResponse{
			Message:        "article already accessible",
			ArticleID:      article.ID,
			DeductedPoints: 0,
			Balance:        user.Points,
		}, nil
	}

	unlocked, err := s.repo.HasArticleUnlock(article.ID, userID)
	if err != nil {
		return UnlockArticleResponse{}, err
	}
	if unlocked {
		return UnlockArticleResponse{}, ErrAlreadyUnlocked
	}

	balance, err := s.repo.UnlockArticle(userID, article.ID, article.RequiredPoints)
	if err != nil {
		return UnlockArticleResponse{}, err
	}

	return UnlockArticleResponse{
		Message:        "article unlocked successfully",
		ArticleID:      article.ID,
		DeductedPoints: article.RequiredPoints,
		Balance:        balance,
	}, nil
}

func (s *Service) RedeemPrivilege(userID uint, req RedeemPrivilegeRequest) (RedeemPrivilegeResponse, error) {
	cost, ok := privilegeCosts[req.PrivilegeKey]
	if !ok {
		return RedeemPrivilegeResponse{}, ErrPrivilegeNotFound
	}

	hasPrivilege, err := s.repo.HasPrivilege(userID, req.PrivilegeKey)
	if err != nil {
		return RedeemPrivilegeResponse{}, err
	}
	if hasPrivilege {
		return RedeemPrivilegeResponse{}, ErrPrivilegeAlreadyRedeemed
	}

	balance, err := s.repo.RedeemPrivilege(userID, req.PrivilegeKey, cost)
	if err != nil {
		return RedeemPrivilegeResponse{}, err
	}

	return RedeemPrivilegeResponse{
		Message:      "privilege redeemed successfully",
		PrivilegeKey: req.PrivilegeKey,
		Cost:         cost,
		Balance:      balance,
	}, nil
}
