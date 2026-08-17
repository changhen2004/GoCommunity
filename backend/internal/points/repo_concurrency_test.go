package points

import (
	"fmt"
	"sync"
	"testing"
	"time"

	internalAuth "resource_community_go/internal/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupPointsRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbName := fmt.Sprintf("points_test_%d", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{})
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

	operationKey := fmt.Sprintf("publish_resource:%d:%d", user.ID, 2)
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

type concurrentAwardResult struct {
	balance uint
	err     error
}

func runConcurrentAwardCalls(t *testing.T, n int, fn func() (uint, error)) []concurrentAwardResult {
	t.Helper()

	results := make([]concurrentAwardResult, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			balance, err := fn()
			results[idx] = concurrentAwardResult{balance: balance, err: err}
		}(i)
	}

	close(start)
	wg.Wait()
	return results
}

func runConcurrentErrorCalls(t *testing.T, n int, fn func() error) []error {
	t.Helper()

	results := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx] = fn()
		}(i)
	}

	close(start)
	wg.Wait()
	return results
}

func assertConcurrentAwardResults(t *testing.T, results []concurrentAwardResult, expectedBalance uint) {
	t.Helper()

	for i, result := range results {
		if result.err != nil {
			t.Fatalf("goroutine %d returned error: %v", i, result.err)
		}
		if result.balance != expectedBalance {
			t.Fatalf("goroutine %d returned balance %d, expected %d", i, result.balance, expectedBalance)
		}
	}
}

func TestCreateCheckInAndAwardWithKeyConcurrent(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "checkin_concurrent_user", Password: "secret123", Points: 20}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	results := runConcurrentAwardCalls(t, 100, func() (uint, error) {
		resp, err := service.CheckIn(user.ID)
		if err != nil {
			return 0, err
		}
		return resp.Balance, nil
	})
	assertConcurrentAwardResults(t, results, 25)

	var saved internalAuth.User
	if err := db.First(&saved, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if saved.Points != 25 {
		t.Fatalf("expected balance to increase once to 25, got %d", saved.Points)
	}

	date := todayString(time.Now())
	var checkInCount int64
	if err := db.Model(&UserCheckIn{}).Where("user_id = ? AND check_in_date = ?", user.ID, date).Count(&checkInCount).Error; err != nil {
		t.Fatalf("count check-ins: %v", err)
	}
	if checkInCount != 1 {
		t.Fatalf("expected exactly 1 check-in record, got %d", checkInCount)
	}

	operationKey := fmt.Sprintf("check_in:%d:%s", user.ID, date)
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

func TestAwardPointsWithKeyConcurrentPublishResource(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "publish_concurrent_user", Password: "secret123", Points: 20}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	operationKey := "publish_resource:1:2"
	results := runConcurrentErrorCalls(t, 100, func() error {
		return service.AwardPublishResource(user.ID, 2)
	})
	for i, err := range results {
		if err != nil {
			t.Fatalf("goroutine %d returned error: %v", i, err)
		}
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

func TestAwardPointsWithKeyConcurrentQualityInteraction(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "interaction_concurrent_user", Password: "secret123", Points: 20}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	operationKey := fmt.Sprintf("quality_interaction:%d:%d", user.ID, 3)
	results := runConcurrentErrorCalls(t, 100, func() error {
		return service.AwardQualityInteraction(user.ID, 3)
	})
	for i, err := range results {
		if err != nil {
			t.Fatalf("goroutine %d returned error: %v", i, err)
		}
	}

	var saved internalAuth.User
	if err := db.First(&saved, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if saved.Points != 22 {
		t.Fatalf("expected balance to increase once to 22, got %d", saved.Points)
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
