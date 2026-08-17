package points

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalAuth "resource_community_go/internal/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type concurrentTestArticle struct {
	gorm.Model
	AuthorID       uint
	Title          string
	Content        string
	Preview        string
	Status         string
	IsFree         bool
	RequiredPoints uint
}

func (concurrentTestArticle) TableName() string {
	return "articles"
}

type concurrentTestArticleUnlock struct {
	gorm.Model
	ArticleID uint `gorm:"not null;index:idx_concurrent_article_unlocks_article_user,unique"`
	UserID    uint `gorm:"not null;index:idx_concurrent_article_unlocks_article_user,unique"`
}

func (concurrentTestArticleUnlock) TableName() string {
	return "article_unlocks"
}

func setupPointsRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbName := fmt.Sprintf("points_test_%d", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&internalAuth.User{},
		&PointLedger{},
		&PointOperation{},
		&UserCheckIn{},
		&UserPrivilege{},
		&concurrentTestArticle{},
		&concurrentTestArticleUnlock{},
	); err != nil {
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

func TestUnlockArticleConcurrent(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "unlock_concurrent_user", Password: "secret123", Points: 50}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	article := concurrentTestArticle{
		AuthorID:       user.ID + 1,
		Title:          "Concurrent Paid Article",
		Content:        "Body",
		Preview:        "Preview",
		Status:         "published",
		RequiredPoints: 10,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	results := runConcurrentAwardCalls(t, 100, func() (uint, error) {
		response, err := service.UnlockArticle(user.ID, fmt.Sprintf("%d", article.ID))
		if err != nil {
			return 0, err
		}
		return response.Balance, nil
	})
	assertConcurrentAwardResults(t, results, 40)

	var saved internalAuth.User
	if err := db.First(&saved, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if saved.Points != 40 {
		t.Fatalf("expected balance to decrease once to 40, got %d", saved.Points)
	}

	var unlockCount int64
	if err := db.Model(&concurrentTestArticleUnlock{}).
		Where("article_id = ? AND user_id = ?", article.ID, user.ID).
		Count(&unlockCount).Error; err != nil {
		t.Fatalf("count article unlocks: %v", err)
	}
	if unlockCount != 1 {
		t.Fatalf("expected exactly 1 article unlock, got %d", unlockCount)
	}

	operationKey := fmt.Sprintf("unlock_paid_resource:%d:%d", user.ID, article.ID)
	assertSinglePointOperationAndLedger(t, db, user.ID, operationKey)
}

func TestRedeemPrivilegeConcurrent(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "redeem_concurrent_user", Password: "secret123", Points: 80}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	results := runConcurrentAwardCalls(t, 100, func() (uint, error) {
		response, err := service.RedeemPrivilege(user.ID, RedeemPrivilegeRequest{PrivilegeKey: "feature_article"})
		if err != nil {
			return 0, err
		}
		return response.Balance, nil
	})
	assertConcurrentAwardResults(t, results, 50)

	var saved internalAuth.User
	if err := db.First(&saved, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if saved.Points != 50 {
		t.Fatalf("expected balance to decrease once to 50, got %d", saved.Points)
	}

	var privilegeCount int64
	if err := db.Model(&UserPrivilege{}).
		Where("user_id = ? AND privilege_key = ?", user.ID, "feature_article").
		Count(&privilegeCount).Error; err != nil {
		t.Fatalf("count privileges: %v", err)
	}
	if privilegeCount != 1 {
		t.Fatalf("expected exactly 1 privilege, got %d", privilegeCount)
	}

	operationKey := fmt.Sprintf("redeem_privilege:%d:%s", user.ID, "feature_article")
	assertSinglePointOperationAndLedger(t, db, user.ID, operationKey)
}

func assertSinglePointOperationAndLedger(t *testing.T, db *gorm.DB, userID uint, operationKey string) {
	t.Helper()

	var operationCount int64
	if err := db.Model(&PointOperation{}).
		Where("user_id = ? AND operation_key = ?", userID, operationKey).
		Count(&operationCount).Error; err != nil {
		t.Fatalf("count point operations: %v", err)
	}
	if operationCount != 1 {
		t.Fatalf("expected exactly 1 point operation, got %d", operationCount)
	}

	var ledgerCount int64
	if err := db.Model(&PointLedger{}).
		Where("user_id = ? AND operation_key = ?", userID, operationKey).
		Count(&ledgerCount).Error; err != nil {
		t.Fatalf("count point ledgers: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("expected exactly 1 point ledger, got %d", ledgerCount)
	}
}

func TestInsufficientUnlockRollsBackAllRecords(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "insufficient_unlock_user", Password: "secret123", Points: 5}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	article := concurrentTestArticle{
		AuthorID:       user.ID + 1,
		Title:          "Too Expensive Article",
		Content:        "Body",
		Preview:        "Preview",
		Status:         "published",
		RequiredPoints: 10,
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}

	_, err := service.UnlockArticle(user.ID, fmt.Sprintf("%d", article.ID))
	if !errors.Is(err, ErrInsufficientPoints) {
		t.Fatalf("expected insufficient points, got %v", err)
	}

	assertUserPoints(t, db, user.ID, 5)

	var unlockCount, operationCount, ledgerCount int64
	if err := db.Model(&concurrentTestArticleUnlock{}).Where("article_id = ? AND user_id = ?", article.ID, user.ID).Count(&unlockCount).Error; err != nil {
		t.Fatalf("count unlocks: %v", err)
	}
	if err := db.Model(&PointOperation{}).Where("user_id = ?", user.ID).Count(&operationCount).Error; err != nil {
		t.Fatalf("count operations: %v", err)
	}
	if err := db.Model(&PointLedger{}).Where("user_id = ?", user.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatalf("count ledgers: %v", err)
	}
	if unlockCount != 0 || operationCount != 0 || ledgerCount != 0 {
		t.Fatalf("expected insufficient unlock to leave no records, got unlocks=%d operations=%d ledgers=%d", unlockCount, operationCount, ledgerCount)
	}
}

func TestConcurrentUniqueUnlocksDoNotOverdraw(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	user := internalAuth.User{Username: "overdraw_unlock_user", Password: "secret123", Points: 50}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	articles := make([]concurrentTestArticle, 100)
	for i := range articles {
		articles[i] = concurrentTestArticle{
			AuthorID:       user.ID + 1,
			Title:          fmt.Sprintf("Unique Paid Article %d", i),
			Content:        "Body",
			Preview:        "Preview",
			Status:         "published",
			RequiredPoints: 10,
		}
		if err := db.Create(&articles[i]).Error; err != nil {
			t.Fatalf("create article %d: %v", i, err)
		}
	}

	var next atomic.Uint64
	results := runConcurrentErrorCalls(t, len(articles), func() error {
		index := int(next.Add(1) - 1)
		_, err := service.UnlockArticle(user.ID, fmt.Sprintf("%d", articles[index].ID))
		return err
	})

	successCount := 0
	for i, err := range results {
		if err == nil {
			successCount++
			continue
		}
		if !errors.Is(err, ErrInsufficientPoints) {
			t.Fatalf("goroutine %d returned unexpected error: %v", i, err)
		}
	}
	if successCount != 5 {
		t.Fatalf("expected exactly 5 successful 10-point unlocks, got %d", successCount)
	}

	assertUserPoints(t, db, user.ID, uint(50-successCount*10))

	var totalExpense int64
	if err := db.Model(&PointLedger{}).
		Where("user_id = ? AND direction = ?", user.ID, "expense").
		Select("COALESCE(SUM(change), 0)").
		Scan(&totalExpense).Error; err != nil {
		t.Fatalf("sum expenses: %v", err)
	}
	if totalExpense < -50 {
		t.Fatalf("total expense exceeds initial balance: %d", totalExpense)
	}
}

func assertUserPoints(t *testing.T, db *gorm.DB, userID, expected uint) {
	t.Helper()

	var user internalAuth.User
	if err := db.First(&user, userID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.Points != expected {
		t.Fatalf("expected user points %d, got %d", expected, user.Points)
	}
}

func TestPointsConcurrencyLedgerInvariant(t *testing.T) {
	db := setupPointsRepoTestDB(t)
	service := NewService(NewRepo(db, nil))
	const initialPoints uint = 1000
	user := internalAuth.User{Username: "ledger_invariant_user", Password: "secret123", Points: initialPoints}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	articles := make([]concurrentTestArticle, 34)
	for i := range articles {
		articles[i] = concurrentTestArticle{
			AuthorID:       user.ID + 1,
			Title:          fmt.Sprintf("Invariant Paid Article %d", i),
			Content:        "Body",
			Preview:        "Preview",
			Status:         "published",
			RequiredPoints: 10,
		}
		if err := db.Create(&articles[i]).Error; err != nil {
			t.Fatalf("create article %d: %v", i, err)
		}
	}

	calls := make([]func() error, 0, 100)
	calls = append(calls, func() error {
		_, err := service.CheckIn(user.ID)
		return err
	})
	for i := 0; i < 32; i++ {
		articleID := uint(i + 1)
		calls = append(calls, func() error {
			return service.AwardPublishResource(user.ID, articleID)
		})
	}
	for i := 0; i < 32; i++ {
		commentID := uint(i + 1)
		calls = append(calls, func() error {
			return service.AwardQualityInteraction(user.ID, commentID)
		})
	}
	for i := range articles {
		articleID := articles[i].ID
		calls = append(calls, func() error {
			_, err := service.UnlockArticle(user.ID, fmt.Sprintf("%d", articleID))
			return err
		})
	}
	calls = append(calls, func() error {
		_, err := service.RedeemPrivilege(user.ID, RedeemPrivilegeRequest{PrivilegeKey: "feature_article"})
		return err
	})

	if len(calls) != 100 {
		t.Fatalf("expected 100 concurrent calls, got %d", len(calls))
	}

	var next atomic.Uint64
	results := runConcurrentErrorCalls(t, len(calls), func() error {
		index := int(next.Add(1) - 1)
		return calls[index]()
	})
	for i, err := range results {
		if err != nil {
			t.Fatalf("goroutine %d returned error: %v", i, err)
		}
	}

	assertLedgerMatchesBalance(t, db, user.ID, initialPoints)

	var operationCount int64
	if err := db.Model(&PointOperation{}).Where("user_id = ?", user.ID).Count(&operationCount).Error; err != nil {
		t.Fatalf("count point operations: %v", err)
	}
	if operationCount != int64(len(calls)) {
		t.Fatalf("expected %d point operations, got %d", len(calls), operationCount)
	}

	var ledgerCount int64
	if err := db.Model(&PointLedger{}).Where("user_id = ?", user.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatalf("count point ledgers: %v", err)
	}
	if ledgerCount != int64(len(calls)) {
		t.Fatalf("expected %d point ledgers, got %d", len(calls), ledgerCount)
	}
}

func assertLedgerMatchesBalance(t *testing.T, db *gorm.DB, userID, initial uint) {
	t.Helper()

	var user internalAuth.User
	if err := db.First(&user, userID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}

	var total int64
	if err := db.Model(&PointLedger{}).
		Where("user_id = ?", userID).
		Select("COALESCE(SUM(change), 0)").
		Scan(&total).Error; err != nil {
		t.Fatalf("sum point ledger changes: %v", err)
	}
	if int64(initial)+total != int64(user.Points) {
		t.Fatalf("balance %d != initial %d + ledger total %d", user.Points, initial, total)
	}
}
