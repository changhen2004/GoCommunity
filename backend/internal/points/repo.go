package points

import (
	"context"
	"time"

	internalAuth "resource_community_go/internal/auth"
	"resource_community_go/internal/cachekey"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
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
	ArticleID uint `gorm:"not null"`
	UserID    uint `gorm:"not null"`
}

func (articleUnlockRecord) TableName() string {
	return "article_unlocks"
}

func NewRepo(db *gorm.DB, redisDB *redis.Client) *Repo {
	return &Repo{db: db, redisDB: redisDB}
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

func (r *Repo) AwardPoints(userID uint, amount uint, source, referenceType string, referenceID uint, description string) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var user internalAuth.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		balance = user.Points + amount
		if err := tx.Model(&internalAuth.User{}).
			Where("id = ?", userID).
			Update("points", balance).Error; err != nil {
			return err
		}

		return tx.Create(&PointLedger{
			UserID:        userID,
			Change:        int(amount),
			BalanceAfter:  balance,
			Direction:     "income",
			Source:        source,
			ReferenceType: referenceType,
			ReferenceID:   referenceID,
			Description:   description,
		}).Error
	})
	r.DeleteSummaryCache(context.Background(), userID)
	return balance, err
}

func (r *Repo) CreateCheckInAndAward(userID uint, date string, amount uint, description string) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var user internalAuth.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		checkIn := UserCheckIn{
			UserID:      userID,
			CheckInDate: date,
		}
		if err := tx.Create(&checkIn).Error; err != nil {
			return err
		}

		balance = user.Points + amount
		if err := tx.Model(&internalAuth.User{}).
			Where("id = ?", userID).
			Update("points", balance).Error; err != nil {
			return err
		}

		return tx.Create(&PointLedger{
			UserID:        userID,
			Change:        int(amount),
			BalanceAfter:  balance,
			Direction:     "income",
			Source:        "daily_check_in",
			ReferenceType: "check_in",
			Description:   description,
		}).Error
	})
	r.DeleteSummaryCache(context.Background(), userID)
	return balance, err
}

func (r *Repo) UnlockArticle(userID, articleID uint, requiredPoints uint) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var user internalAuth.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		if user.Points < requiredPoints {
			return ErrInsufficientPoints
		}

		unlock := articleUnlockRecord{
			ArticleID: articleID,
			UserID:    userID,
		}
		if err := tx.Create(&unlock).Error; err != nil {
			return err
		}

		balance = user.Points - requiredPoints
		if err := tx.Model(&internalAuth.User{}).
			Where("id = ?", userID).
			Update("points", balance).Error; err != nil {
			return err
		}

		return tx.Create(&PointLedger{
			UserID:        userID,
			Change:        -int(requiredPoints),
			BalanceAfter:  balance,
			Direction:     "expense",
			Source:        "unlock_paid_resource",
			ReferenceType: "article",
			ReferenceID:   articleID,
			Description:   "unlock paid resource",
		}).Error
	})
	r.DeleteSummaryCache(context.Background(), userID)
	return balance, err
}

func (r *Repo) RedeemPrivilege(userID uint, privilegeKey string, cost uint) (uint, error) {
	var balance uint
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var user internalAuth.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		if user.Points < cost {
			return ErrInsufficientPoints
		}

		if err := tx.Create(&UserPrivilege{
			UserID:       userID,
			PrivilegeKey: privilegeKey,
			Cost:         cost,
		}).Error; err != nil {
			return err
		}

		balance = user.Points - cost
		if err := tx.Model(&internalAuth.User{}).
			Where("id = ?", userID).
			Update("points", balance).Error; err != nil {
			return err
		}

		return tx.Create(&PointLedger{
			UserID:        userID,
			Change:        -int(cost),
			BalanceAfter:  balance,
			Direction:     "expense",
			Source:        "redeem_privilege",
			ReferenceType: "privilege",
			Description:   "redeem privilege " + privilegeKey,
		}).Error
	})
	r.DeleteSummaryCache(context.Background(), userID)
	return balance, err
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
