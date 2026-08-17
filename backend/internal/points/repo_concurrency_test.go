package points

import (
	"testing"

	internalAuth "resource_community_go/internal/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupPointsRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&internalAuth.User{}, &PointLedger{}, &PointOperation{}, &UserCheckIn{}, &UserPrivilege{}); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}

	return db
}

func TestAwardPointsIdempotent(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	repo := NewRepo(db, nil)
	user := internalAuth.User{Username: "award_idempotent_user", Password: "secret123", Points: 20}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	operationKey := "publish_resource:1:2"
	firstBalance, firstErr := repo.AwardPointsWithKey(user.ID, 10, "publish_resource", "article", 2, "publish resource reward", operationKey)
	secondBalance, secondErr := repo.AwardPointsWithKey(user.ID, 10, "publish_resource", "article", 2, "publish resource reward", operationKey)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("expected idempotent success, got %v and %v", firstErr, secondErr)
	}
	if firstBalance != secondBalance {
		t.Fatalf("expected repeated operation to return same balance, got %d and %d", firstBalance, secondBalance)
	}

	var saved internalAuth.User
	if err := db.First(&saved, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if saved.Points != 30 {
		t.Fatalf("expected balance to increase once to 30, got %d", saved.Points)
	}

	var operationCount int64
	if err := db.Model(&PointOperation{}).Where("user_id = ? AND operation_key = ?", user.ID, operationKey).Count(&operationCount).Error; err != nil {
		t.Fatalf("count point operations: %v", err)
	}
	if operationCount != 1 {
		t.Fatalf("expected exactly 1 point operation, got %d", operationCount)
	}

	var ledgerCount int64
	if err := db.Model(&PointLedger{}).Where("user_id = ? AND operation_key = ?", user.ID, operationKey).Count(&ledgerCount).Error; err != nil {
		t.Fatalf("count point ledgers: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("expected exactly 1 point ledger, got %d", ledgerCount)
	}
}
