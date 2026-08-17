package points

import (
	"context"
	"errors"
	"fmt"
	"time"

	internalAuth "resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct {
	db      *gorm.DB
	redisDB *redis.Client
}

type articleRecord struct {
	ID             uint
	AuthorID       uint
	Status         string
	IsFree         bool
	RequiredPoints uint
}

type articleUnlockRecord struct {
	gorm.Model
	ArticleID uint `gorm:"not null;index:idx_article_unlocks_article_user,unique"`
	UserID    uint `gorm:"not null;index:idx_article_unlocks_article_user,unique"`
}

func (articleUnlockRecord) TableName() string {
	return "article_unlocks"
}

func NewRepo(db *gorm.DB, redisDB *redis.Client) *Repo {
	return &Repo{db: db, redisDB: redisDB}
}

var errPointOperationExists = errors.New("point operation already exists")

func createPointOperation(tx *gorm.DB, operation PointOperation) error {
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&operation)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errPointOperationExists
	}
	return nil
}

func getPointOperation(db *gorm.DB, userID uint, operationKey string) (*PointOperation, error) {
	var operation PointOperation
	if err := db.Where("user_id = ? AND operation_key = ?", userID, operationKey).Take(&operation).Error; err != nil {
		return nil, err
	}
	return &operation, nil
}

func getUserBalance(tx *gorm.DB, userID uint) (uint, error) {
	var user internalAuth.User
	if err := tx.Select("points").First(&user, userID).Error; err != nil {
		return 0, err
	}
	return user.Points, nil
}

func classifyInsufficientPoints(tx *gorm.DB, userID uint) error {
	var user internalAuth.User
	if err := tx.Select("id").First(&user, userID).Error; err != nil {
		return err
	}
	return ErrInsufficientPoints
}

func deductPoints(tx *gorm.DB, userID, amount uint) (uint, error) {
	result := tx.Model(&internalAuth.User{}).
		Where("id = ? AND points >= ?", userID, amount).
		UpdateColumn("points", gorm.Expr("points - ?", amount))
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, classifyInsufficientPoints(tx, userID)
	}
	return getUserBalance(tx, userID)
}

func (r *Repo) GetUserByID(userID uint) (*internalAuth.User, error) {
	var user internalAuth.User
	if err := r.db.First(&user, userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repo) GetSummaryCache(ctx context.Context, key string) (string, error) {
	if r.redisDB == nil {
		return "", redis.Nil
	}
	return r.redisDB.Get(ctx, key).Result()
}

func (r *Repo) SetSummaryCache(ctx context.Context, key, value string) {
	if r.redisDB == nil {
		return
	}
	_ = r.redisDB.Set(ctx, key, value, cachekey.PointsSummaryTTL).Err()
}

func (r *Repo) DeleteSummaryCache(ctx context.Context, userID uint) {
	cachekey.DeleteKeys(ctx, r.redisDB, cachekey.PointsSummaryKey(userID))
}

func (r *Repo) ListPrivileges(userID uint) ([]UserPrivilegeResponse, error) {
	privileges := make([]UserPrivilege, 0)
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&privileges).Error; err != nil {
		return nil, err
	}

	resp := make([]UserPrivilegeResponse, 0, len(privileges))
	for _, privilege := range privileges {
		resp = append(resp, UserPrivilegeResponse{
			PrivilegeKey: privilege.PrivilegeKey,
			Cost:         privilege.Cost,
			RedeemedAt:   privilege.CreatedAt,
		})
	}
	return resp, nil
}

func (r *Repo) ListRecords(userID uint) ([]PointsRecordResponse, error) {
	records := make([]PointLedger, 0)
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, err
	}

	resp := make([]PointsRecordResponse, 0, len(records))
	for _, record := range records {
		resp = append(resp, PointsRecordResponse{
			ID:            record.ID,
			Change:        record.Change,
			BalanceAfter:  record.BalanceAfter,
			Direction:     record.Direction,
			Source:        record.Source,
			ReferenceType: record.ReferenceType,
			ReferenceID:   record.ReferenceID,
			Description:   record.Description,
			CreatedAt:     record.CreatedAt,
		})
	}
	return resp, nil
}

func (r *Repo) HasCheckedInOn(userID uint, date string) (bool, error) {
	var count int64
	if err := r.db.Model(&UserCheckIn{}).
		Where("user_id = ? AND check_in_date = ?", userID, date).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repo) AwardPointsWithKey(userID uint, amount uint, source, referenceType string, referenceID uint, description, operationKey string) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := createPointOperation(tx, PointOperation{
			UserID:       userID,
			OperationKey: operationKey,
			Change:       int(amount),
		})
		if errors.Is(err, errPointOperationExists) {
			operation, getErr := getPointOperation(tx, userID, operationKey)
			if getErr != nil {
				return getErr
			}
			balance = operation.BalanceAfter
			return nil
		}
		if err != nil {
			return err
		}

		result := tx.Model(&internalAuth.User{}).
			Where("id = ?", userID).
			UpdateColumn("points", gorm.Expr("points + ?", amount))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}

		updatedBalance, err := getUserBalance(tx, userID)
		if err != nil {
			return err
		}
		balance = updatedBalance

		if err := tx.Create(&PointLedger{
			UserID:        userID,
			OperationKey:  &operationKey,
			Change:        int(amount),
			BalanceAfter:  balance,
			Direction:     "income",
			Source:        source,
			ReferenceType: referenceType,
			ReferenceID:   referenceID,
			Description:   description,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&PointOperation{}).
			Where("user_id = ? AND operation_key = ?", userID, operationKey).
			Update("balance_after", balance).Error
	})
	if err == nil {
		r.DeleteSummaryCache(context.Background(), userID)
	}
	return balance, err
}

func (r *Repo) CreateCheckInAndAwardWithKey(userID uint, date string, amount uint, description, operationKey string) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := createPointOperation(tx, PointOperation{
			UserID:       userID,
			OperationKey: operationKey,
			Change:       int(amount),
		})
		if errors.Is(err, errPointOperationExists) {
			operation, getErr := getPointOperation(tx, userID, operationKey)
			if getErr != nil {
				return getErr
			}
			balance = operation.BalanceAfter
			return nil
		}
		if err != nil {
			return err
		}

		if err := tx.Create(&UserCheckIn{
			UserID:      userID,
			CheckInDate: date,
		}).Error; err != nil {
			return err
		}

		result := tx.Model(&internalAuth.User{}).
			Where("id = ?", userID).
			UpdateColumn("points", gorm.Expr("points + ?", amount))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}

		updatedBalance, err := getUserBalance(tx, userID)
		if err != nil {
			return err
		}
		balance = updatedBalance

		if err := tx.Create(&PointLedger{
			UserID:        userID,
			OperationKey:  &operationKey,
			Change:        int(amount),
			BalanceAfter:  balance,
			Direction:     "income",
			Source:        "daily_check_in",
			ReferenceType: "check_in",
			Description:   description,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&PointOperation{}).
			Where("user_id = ? AND operation_key = ?", userID, operationKey).
			Update("balance_after", balance).Error
	})
	if err == nil {
		r.DeleteSummaryCache(context.Background(), userID)
	}
	return balance, err
}

func (r *Repo) AwardPoints(userID uint, amount uint, source, referenceType string, referenceID uint, description string) (uint, error) {
	operationKey := fmt.Sprintf("%s:%d:%s:%d", source, userID, referenceType, referenceID)
	return r.AwardPointsWithKey(userID, amount, source, referenceType, referenceID, description, operationKey)
}

func (r *Repo) CreateCheckInAndAward(userID uint, date string, amount uint, description string) (uint, error) {
	operationKey := fmt.Sprintf("check_in:%d:%s", userID, date)
	return r.CreateCheckInAndAwardWithKey(userID, date, amount, description, operationKey)
}

func (r *Repo) UnlockArticleWithKey(userID, articleID, requiredPoints uint, operationKey string) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := createPointOperation(tx, PointOperation{
			UserID:       userID,
			OperationKey: operationKey,
			Change:       -int(requiredPoints),
		})
		if errors.Is(err, errPointOperationExists) {
			operation, getErr := getPointOperation(tx, userID, operationKey)
			if getErr != nil {
				return getErr
			}
			balance = operation.BalanceAfter
			return nil
		}
		if err != nil {
			return err
		}

		unlock := articleUnlockRecord{
			ArticleID: articleID,
			UserID:    userID,
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&unlock)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrAlreadyUnlocked
		}

		balance, err = deductPoints(tx, userID, requiredPoints)
		if err != nil {
			return err
		}

		if err := tx.Create(&PointLedger{
			UserID:        userID,
			OperationKey:  &operationKey,
			Change:        -int(requiredPoints),
			BalanceAfter:  balance,
			Direction:     "expense",
			Source:        "unlock_paid_resource",
			ReferenceType: "article",
			ReferenceID:   articleID,
			Description:   "unlock paid resource",
		}).Error; err != nil {
			return err
		}

		return tx.Model(&PointOperation{}).
			Where("user_id = ? AND operation_key = ?", userID, operationKey).
			Update("balance_after", balance).Error
	})
	if err == nil {
		r.DeleteSummaryCache(context.Background(), userID)
	}
	return balance, err
}

func (r *Repo) UnlockArticle(userID, articleID uint, requiredPoints uint) (uint, error) {
	operationKey := fmt.Sprintf("unlock_paid_resource:%d:%d", userID, articleID)
	return r.UnlockArticleWithKey(userID, articleID, requiredPoints, operationKey)
}

func (r *Repo) RedeemPrivilegeWithKey(userID uint, privilegeKey string, cost uint, operationKey string) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		err := createPointOperation(tx, PointOperation{
			UserID:       userID,
			OperationKey: operationKey,
			Change:       -int(cost),
		})
		if errors.Is(err, errPointOperationExists) {
			operation, getErr := getPointOperation(tx, userID, operationKey)
			if getErr != nil {
				return getErr
			}
			balance = operation.BalanceAfter
			return nil
		}
		if err != nil {
			return err
		}

		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&UserPrivilege{
			UserID:       userID,
			PrivilegeKey: privilegeKey,
			Cost:         cost,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPrivilegeAlreadyRedeemed
		}

		balance, err = deductPoints(tx, userID, cost)
		if err != nil {
			return err
		}

		if err := tx.Create(&PointLedger{
			UserID:        userID,
			OperationKey:  &operationKey,
			Change:        -int(cost),
			BalanceAfter:  balance,
			Direction:     "expense",
			Source:        "redeem_privilege",
			ReferenceType: "privilege",
			Description:   "redeem privilege " + privilegeKey,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&PointOperation{}).
			Where("user_id = ? AND operation_key = ?", userID, operationKey).
			Update("balance_after", balance).Error
	})
	if err == nil {
		r.DeleteSummaryCache(context.Background(), userID)
	}
	return balance, err
}

func (r *Repo) RedeemPrivilege(userID uint, privilegeKey string, cost uint) (uint, error) {
	operationKey := fmt.Sprintf("redeem_privilege:%d:%s", userID, privilegeKey)
	return r.RedeemPrivilegeWithKey(userID, privilegeKey, cost, operationKey)
}

func (r *Repo) FindArticleByID(articleID uint) (*articleRecord, error) {
	var article articleRecord
	if err := r.db.Table("articles").
		Select("id, author_id, status, is_free, required_points").
		Where("id = ? AND status = ?", articleID, "published").
		Take(&article).Error; err != nil {
		return nil, err
	}
	return &article, nil
}

func (r *Repo) HasArticleUnlock(articleID, userID uint) (bool, error) {
	var count int64
	if err := r.db.Table("article_unlocks").
		Where("article_id = ? AND user_id = ?", articleID, userID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repo) HasPrivilege(userID uint, privilegeKey string) (bool, error) {
	var count int64
	if err := r.db.Model(&UserPrivilege{}).
		Where("user_id = ? AND privilege_key = ?", userID, privilegeKey).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func todayString(now time.Time) string {
	return now.Format("2006-01-02")
}
