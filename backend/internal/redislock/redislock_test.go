package redislock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestLocker(t *testing.T) (*miniredis.Miniredis, *redis.Client, *Locker) {
	t.Helper()

	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return server, rdb, NewLocker(rdb)
}

const testKey = "test:lock"

// 基本互斥 + 释放后他人可再次获取
func TestAcquireLockAndRelease(t *testing.T) {
	_, rdb, locker := newTestLocker(t)
	ctx := context.Background()

	lock, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// SET 进去的 value 必须等于 Lock.Value（token 一致性）
	stored, getErr := rdb.Get(ctx, testKey).Result()
	if getErr != nil {
		t.Fatalf("get stored token: %v", getErr)
	}
	if stored != lock.Value {
		t.Fatalf("stored token mismatch: got %q want %q", stored, lock.Value)
	}

	// 已被持有：第二次获取必须失败
	if _, err := locker.AcquireLock(ctx, testKey, 30*time.Second); !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("expected ErrLockNotAcquired, got %v", err)
	}

	if err := lock.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	// 释放后 key 应消失，且可重新获取
	if n, err := rdb.Exists(ctx, testKey).Result(); err != nil || n != 0 {
		t.Fatalf("expected lock key removed after unlock, exists=%d err=%v", n, err)
	}
	if _, err := locker.AcquireLock(ctx, testKey, 30*time.Second); err != nil {
		t.Fatalf("re-acquire after unlock: %v", err)
	}
}

// 非持有者（错误 token）删不掉锁 —— Lua 比对语义
func TestReleaseOnlyByOwner(t *testing.T) {
	_, rdb, locker := newTestLocker(t)
	ctx := context.Background()

	lock, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// 模拟“锁的 value 已被别人更新”（如本锁过期后被新持有者覆盖）
	intruderToken := "someone-else-token"
	if err := rdb.Set(ctx, testKey, intruderToken, 30*time.Second).Err(); err != nil {
		t.Fatalf("simulate new holder: %v", err)
	}

	// 原持有者释放：比对失败，不得删除新持有者的锁
	if err := lock.Unlock(); err != nil {
		t.Fatalf("unlock by stale owner should be a no-op, got err=%v", err)
	}
	stored, _ := rdb.Get(ctx, testKey).Result()
	if stored != intruderToken {
		t.Fatalf("stale owner deleted someone else's lock: got %q", stored)
	}

	// 真正的新持有者能正常释放
	if err := rdb.Eval(ctx, releaseScript, []string{testKey}, intruderToken).Err(); err != nil {
		t.Fatalf("new holder release: %v", err)
	}
	if n, _ := rdb.Exists(ctx, testKey).Result(); n != 0 {
		t.Fatalf("expected lock removed by real owner, exists=%d", n)
	}
}

// Unlock 幂等：重复释放不 panic、不报错、不误删（sync.Once + Lua 双重保证）
func TestUnlockIsIdempotent(t *testing.T) {
	_, _, locker := newTestLocker(t)
	ctx := context.Background()

	lock, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatalf("first unlock: %v", err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatalf("second unlock should be a no-op, got %v", err)
	}
}

// TTL 兜底：持有者“死亡”（看门狗被关闭）后，锁到期自动释放，不会死锁
func TestLockExpiresWithoutUnlock(t *testing.T) {
	server, _, locker := newTestLocker(t)
	ctx := context.Background()

	lock, err := locker.AcquireLock(ctx, testKey, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// 模拟持有者进程崩溃：停掉看门狗且不调用 Unlock
	close(lock.stopRenew)

	// miniredis 不按真实时钟主动过期 key，用 FastForward 推进虚拟时钟触发 TTL 释放
	server.FastForward(300 * time.Millisecond)

	// 锁应已自动过期，他人可正常获取
	if _, err := locker.AcquireLock(ctx, testKey, 30*time.Second); err != nil {
		t.Fatalf("expected lock released by TTL, got %v", err)
	}
}

// 看门狗：业务执行超过 TTL 时锁仍被续期保活
func TestWatchdogKeepsLockAlive(t *testing.T) {
	_, rdb, locker := newTestLocker(t)
	ctx := context.Background()

	ttl := 500 * time.Millisecond
	lock, err := locker.AcquireLock(ctx, testKey, ttl)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = lock.Unlock() }()

	// 业务运行 800ms > 初始 TTL：若无看门狗锁早已过期
	time.Sleep(800 * time.Millisecond)

	n, err := rdb.Exists(ctx, testKey).Result()
	if err != nil || n != 1 {
		t.Fatalf("expected lock kept alive by watchdog, exists=%d err=%v", n, err)
	}
	pttl, err := rdb.PTTL(ctx, testKey).Result()
	if err != nil || pttl <= 0 {
		t.Fatalf("expected positive remaining TTL after watchdog renewals, pttl=%v err=%v", pttl, err)
	}
}

// 自旋重试：持有者释放后，等待者最终能抢到锁
func TestAcquireWithRetrySucceedsAfterRelease(t *testing.T) {
	_, _, locker := newTestLocker(t)
	ctx := context.Background()

	holder, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	type result struct {
		lock *Lock
		err  error
	}
	done := make(chan result, 1)
	go func() {
		lock, err := locker.AcquireWithRetry(ctx, testKey, 30*time.Second, 3*time.Second)
		done <- result{lock: lock, err: err}
	}()

	time.Sleep(150 * time.Millisecond) // 让等待者先进入自旋
	if err := holder.Unlock(); err != nil {
		t.Fatalf("holder unlock: %v", err)
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("retry acquire should succeed after release, got %v", res.err)
		}
		if err := res.lock.Unlock(); err != nil {
			t.Fatalf("cleanup unlock: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("retry acquire did not return in time")
	}
}

// 自旋重试超时：锁一直被占用且超过 maxWait → ErrLockTimeout
func TestAcquireWithRetryTimesOut(t *testing.T) {
	_, _, locker := newTestLocker(t)
	ctx := context.Background()

	holder, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	defer func() { _ = holder.Unlock() }()

	start := time.Now()
	lock, err := locker.AcquireWithRetry(ctx, testKey, 30*time.Second, 150*time.Millisecond)
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("expected ErrLockTimeout, got %v", err)
	}
	if lock != nil {
		t.Fatalf("expected nil lock on timeout")
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("timeout returned too early: %v", elapsed)
	}
}

// 并发互斥：同一时刻多协程抢一把锁，只有一个胜者
func TestConcurrentAcquireSingleWinner(t *testing.T) {
	_, _, locker := newTestLocker(t)
	ctx := context.Background()

	const goroutines = 20
	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		mu        sync.Mutex
		winners   []*Lock
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			lock, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
			if err == nil {
				successes.Add(1)
				mu.Lock()
				winners = append(winners, lock)
				mu.Unlock()
			} else if !errors.Is(err, ErrLockNotAcquired) {
				t.Errorf("unexpected acquire error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("expected exactly one winner, got %d", got)
	}

	// 释放后进入第二轮：应再次只有一个胜者
	mu.Lock()
	for _, w := range winners {
		_ = w.Unlock()
	}
	winners = winners[:0]
	mu.Unlock()
	successes.Store(0)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			lock, err := locker.AcquireLock(ctx, testKey, 30*time.Second)
			if err == nil {
				successes.Add(1)
				mu.Lock()
				winners = append(winners, lock)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("expected exactly one winner in round two, got %d", got)
	}
	mu.Lock()
	for _, w := range winners {
		_ = w.Unlock()
	}
	mu.Unlock()
}
