# 积分并发幂等实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 将积分余额变更改为数据库原子更新，并为签到、奖励、解锁和兑换增加业务幂等，确保并发下余额和流水一致。

**架构：** 新增 `point_operations` 记录业务幂等键及首次成功结果；`point_ledgers.operation_key` 作为新流水的唯一业务标识并允许历史数据为空。每个积分操作在一个事务内先占用幂等键，再写业务记录、条件更新用户余额、写流水并保存最终余额；重复请求读取已提交操作结果。

**技术栈：** Go、GORM、SQLite 集成测试、现有 `internal/points` 服务/仓库结构。

---

### 任务 1：增加幂等实体与迁移覆盖

**文件：**
- 修改：`internal/points/entity.go`
- 修改：`config/migrate.go`
- 修改：`config/migrate_test.go`

- [ ] **步骤 1：扩展实体定义**

在 `internal/points/entity.go` 增加：

```go
type PointOperation struct {
	gorm.Model
	UserID        uint   `gorm:"not null;index:idx_point_operations_user_key,unique"`
	OperationKey  string `gorm:"size:128;not null;index:idx_point_operations_user_key,unique"`
	Change        int    `gorm:"not null"`
	BalanceAfter  uint   `gorm:"not null"`
}

func (PointOperation) TableName() string {
	return "point_operations"
}
```

为 `PointLedger` 增加可为空的唯一操作键，保持已有历史流水可迁移：

```go
OperationKey *string `gorm:"size:128;uniqueIndex"`
```

- [ ] **步骤 2：先写迁移失败测试**

在 `config/migrate_test.go` 增加断言：

```go
if !db.Migrator().HasTable(&points.PointOperation{}) {
	t.Fatal("expected point_operations table to exist")
}
if !db.Migrator().HasColumn(&points.PointLedger{}, "operation_key") {
	t.Fatal("expected point_ledgers operation_key column to exist")
}
```

- [ ] **步骤 3：运行迁移测试确认失败**

运行：

```bash
go test ./config -run TestMigrate -count=1
```

预期：失败，提示 `point_operations` 表或 `operation_key` 列不存在。

- [ ] **步骤 4：注册 AutoMigrate 并运行测试**

在 `config/migrate.go` 的积分模型列表中加入 `&points.PointOperation{}`，然后运行：

```bash
gofmt -w internal/points/entity.go config/migrate.go config/migrate_test.go
go test ./config -run TestMigrate -count=1
```

预期：迁移测试通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/points/entity.go config/migrate.go config/migrate_test.go
git commit -m "feat: add point operation idempotency model"
```

### 任务 2：增加事务幂等与余额更新基础函数

**文件：**
- 修改：`internal/points/errors.go`
- 修改：`internal/points/repo.go`
- 测试：`internal/points/repo_concurrency_test.go`

- [ ] **步骤 1：编写失败测试验证重复操作只返回一次结果**

新增共享 SQLite 测试数据库和基础测试，直接调用仓库，验证相同操作键执行两次时：

```go
firstBalance, firstErr := repo.AwardPointsWithKey(user.ID, 10, "publish_resource", "article", articleID, "publish", "publish_resource:1:2")
secondBalance, secondErr := repo.AwardPointsWithKey(user.ID, 10, "publish_resource", "article", articleID, "publish", "publish_resource:1:2")

if firstErr != nil || secondErr != nil {
	t.Fatalf("expected idempotent success, got %v and %v", firstErr, secondErr)
}
if firstBalance != secondBalance {
	t.Fatalf("expected repeated operation to return same balance, got %d and %d", firstBalance, secondBalance)
}
```

同时断言用户余额只增加 10，`PointOperation` 和 `PointLedger` 各只有一条记录。

- [ ] **步骤 2：运行测试确认 API 尚不存在**

运行：

```bash
go test ./internal/points -run TestAwardPointsIdempotent -count=1
```

预期：编译失败，提示仓库幂等方法不存在。

- [ ] **步骤 3：实现幂等占用和余额更新辅助逻辑**

在 `repo.go` 增加内部错误和辅助函数，使用 GORM `clause.OnConflict{DoNothing: true}`：

```go
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
```

增加按用户和操作键读取 `PointOperation` 的方法，并在事务外将 `errPointOperationExists` 转换为首次操作的 `BalanceAfter`。余额变更只能使用：

```go
result := tx.Model(&internalAuth.User{}).
	Where("id = ?", userID).
	UpdateColumn("points", gorm.Expr("points + ?", amount))
```

随后校验 `result.RowsAffected == 1`，读取同一事务中的用户余额，创建操作流水并更新 `PointOperation.BalanceAfter`。

- [ ] **步骤 4：运行单测确认基础收入路径通过**

运行：

```bash
gofmt -w internal/points/errors.go internal/points/repo.go internal/points/repo_concurrency_test.go
go test ./internal/points -run TestAwardPointsIdempotent -count=1
```

预期：测试通过，重复调用不增加第二条流水。

- [ ] **步骤 5：Commit**

```bash
git add internal/points/errors.go internal/points/repo.go internal/points/repo_concurrency_test.go
git commit -m "feat: make point awards idempotent"
```

### 任务 3：重构签到和奖励服务调用

**文件：**
- 修改：`internal/points/service.go`
- 修改：`internal/points/repo.go`
- 测试：`internal/points/repo_concurrency_test.go`

- [ ] **步骤 1：编写签到和奖励并发失败测试**

使用 `start` barrier 启动 100 个 goroutine：

```go
for i := 0; i < 100; i++ {
	go func() {
		<-start
		_, err := service.CheckIn(user.ID)
		errCh <- err
	}()
}
close(start)
```

测试发布奖励和高质量互动奖励时使用 100 个相同业务事件，断言：

- 每个事件只有一次积分变化。
- 所有请求都成功或按幂等成功返回。
- 用户最终余额等于初始余额加唯一事件奖励总和。

- [ ] **步骤 2：运行测试确认当前实现存在丢更新/重复业务失败**

运行：

```bash
go test ./internal/points -run 'TestConcurrent(CheckIn|Awards)' -count=1
```

预期：当前实现可能出现 SQLite 锁错误、重复签到错误或余额/流水断言失败；该测试用于固定并发行为。

- [ ] **步骤 3：为每个服务操作生成稳定幂等键**

在 `service.go` 使用统一格式生成键：

```go
operationKey := fmt.Sprintf("check_in:%d:%s", userID, date)
```

奖励分别使用 `publish_resource:{userID}:{articleID}` 和 `quality_interaction:{userID}:{commentID}`，并调用仓库的幂等收入方法。签到不再依赖 `HasCheckedInOn` 的预检查决定是否成功；让事务内的 `UserCheckIn` 唯一约束和操作键决定结果。

- [ ] **步骤 4：将签到业务记录纳入同一幂等事务**

调整 `CreateCheckInAndAward`：事务内先创建操作记录，再创建 `UserCheckIn`，执行原子加分，写流水并保存余额。重复操作读取已有操作结果并返回首次余额。

- [ ] **步骤 5：运行并发测试**

运行：

```bash
gofmt -w internal/points/repo.go internal/points/service.go internal/points/repo_concurrency_test.go
go test ./internal/points -run 'TestConcurrent(CheckIn|Awards)' -count=1 -race
```

预期：通过，无数据竞争；唯一业务事件只产生一次流水。

- [ ] **步骤 6：Commit**

```bash
git add internal/points/repo.go internal/points/service.go internal/points/repo_concurrency_test.go
git commit -m "fix: make check-in and point awards concurrency-safe"
```

### 任务 4：重构解锁和兑换支出路径

**文件：**
- 修改：`internal/points/repo.go`
- 修改：`internal/points/service.go`
- 修改：`internal/points/errors.go`
- 测试：`internal/points/repo_concurrency_test.go`
- 测试：`internal/points/handler_test.go`

- [ ] **步骤 1：编写失败测试验证并发扣减不超额**

创建余额为 50 的用户，使用 100 个 goroutine 并发执行每次 10 分的唯一消费操作，断言：

```go
if user.Points < 0 {
	t.Fatalf("points must not be negative: %d", user.Points)
}
if totalExpense > 50 {
	t.Fatalf("total expense exceeds initial balance: %d", totalExpense)
}
```

同一文章解锁和同一特权兑换分别使用 100 个相同操作键，断言只扣除一次且所有重复请求返回首次余额。

- [ ] **步骤 2：运行测试确认当前实现失败**

运行：

```bash
go test ./internal/points -run 'TestConcurrent(Unlock|Redeem|Spending)' -count=1
```

预期：当前读余额后写回逻辑导致余额/流水断言失败，或重复业务请求返回已解锁/已兑换错误。

- [ ] **步骤 3：实现条件扣减**

在解锁和兑换事务中使用：

```go
result := tx.Model(&internalAuth.User{}).
	Where("id = ? AND points >= ?", userID, cost).
	UpdateColumn("points", gorm.Expr("points - ?", cost))
if result.Error != nil {
	return result.Error
}
if result.RowsAffected == 0 {
	return classifyInsufficientPoints(tx, userID)
}
```

`classifyInsufficientPoints` 查询用户是否存在，分别返回用户不存在或 `ErrInsufficientPoints`。不得再用 Go 中读取的 `user.Points - cost` 更新余额。

- [ ] **步骤 4：调整解锁和兑换幂等顺序**

事务内先占用操作键，再创建 `article_unlocks` 或 `user_privileges`，条件扣减用户积分，写入带 `operation_key` 的流水并保存 `balance_after`。服务层的预检查保留资源存在性判断，但重复成功路径由操作记录返回原结果；历史业务记录唯一冲突必须回滚，不得扣款。

- [ ] **步骤 5：运行支出并发和已有 handler 测试**

运行：

```bash
gofmt -w internal/points/repo.go internal/points/service.go internal/points/errors.go internal/points/repo_concurrency_test.go internal/points/handler_test.go
go test ./internal/points -run 'TestConcurrent(Unlock|Redeem|Spending)|TestPoints' -count=1 -race
```

预期：并发消费不超过余额，重复请求按幂等成功；既有 HTTP 错误码和正常响应测试保持通过。

- [ ] **步骤 6：Commit**

```bash
git add internal/points/repo.go internal/points/service.go internal/points/errors.go internal/points/repo_concurrency_test.go internal/points/handler_test.go
git commit -m "fix: make point spending atomic and idempotent"
```

### 任务 5：完成流水一致性集成测试

**文件：**
- 测试：`internal/points/repo_concurrency_test.go`
- 修改：`internal/points/handler_test.go`（仅在需要补充迁移模型时）

- [ ] **步骤 1：增加全链路并发场景**

同一个用户同时执行签到、发布奖励、高质量互动、解锁和兑换。使用固定初始余额与固定业务事件集合，等待全部 goroutine 完成后读取用户、操作表和流水表。

- [ ] **步骤 2：增加最终不变量断言**

实现测试辅助函数：

```go
func assertLedgerMatchesBalance(t *testing.T, db *gorm.DB, userID uint, initial uint) {
	var user internalAuth.User
	var total int64
	if err := db.First(&user, userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&PointLedger{}).
		Where("user_id = ?", userID).
		Select("COALESCE(SUM(change), 0)").
		Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if int64(initial)+total != int64(user.Points) {
		t.Fatalf("balance %d != initial %d + ledger total %d", user.Points, initial, total)
	}
}
```

同时断言每个非空 `operation_key` 只有一条流水，所有成功支出都有对应业务记录。

- [ ] **步骤 3：运行稳定性验证**

运行：

```bash
go test ./internal/points -run TestPointsConcurrency -count=20
go test ./internal/points -run TestPointsConcurrency -count=1 -race
```

预期：20 次重复运行全部通过，竞态检测无报告。

- [ ] **步骤 4：Commit**

```bash
git add internal/points/repo_concurrency_test.go internal/points/handler_test.go
git commit -m "test: verify point ledger balance invariant"
```

### 任务 6：全量验证

**文件：**
- 无新增文件。

- [ ] **步骤 1：格式和静态检查**

运行：

```bash
gofmt -w internal/points config/migrate.go config/migrate_test.go
git diff --check
```

预期：命令成功且无 diff 检查错误。

- [ ] **步骤 2：运行积分和迁移测试**

运行：

```bash
go test ./config ./internal/points -count=1 -race
```

预期：退出码为 0。

- [ ] **步骤 3：运行后端全量测试**

运行：

```bash
go test ./... -count=1
```

预期：退出码为 0，所有包测试通过。

- [ ] **步骤 4：检查最终差异**

运行：

```bash
git status --short
git log --oneline -8
```

确认没有未预期文件，且每个实现阶段提交都存在。
