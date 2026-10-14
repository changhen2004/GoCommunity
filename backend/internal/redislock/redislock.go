package redislock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	mrand "math/rand/v2"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrLockNotAcquired = errors.New("redislock: lock is already held")
	ErrLockTimeout     = errors.New("redislock: acquire lock timed out")
)

type Locker struct {
	rdb *redis.Client
}

type Lock struct {
	Value      string        // token：标识持有者，SET 进去、释放/续期时比对
	key        string        // 锁在 Redis 里的 key
	locker     *Locker       // 回指，执行 Lua 释放/续期
	ttl        time.Duration // 锁的过期时长（续期间隔 = ttl/3）
	stopRenew  chan struct{} // 关闭它 = 通知看门狗退出
	unlockOnce sync.Once     // Unlock 幂等
}

// releaseScript 原子地“比对 token 后删除”，防止把别人的锁删掉
const releaseScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`

// renewScript 原子地“比对 token 后续期”，续期统一用毫秒（PEXPIRE），与 SetNX(ttl) 粒度一致
const renewScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("pexpire", KEYS[1], ARGV[2])
else
	return 0
end
`

// NewLocker 返回锁管理器；rdb 为 nil 时返回 nil，由调用方决定降级策略
func NewLocker(rdb *redis.Client) *Locker {
	if rdb == nil {
		return nil
	}
	return &Locker{rdb: rdb}
}

// randomValue 生成锁持有者的唯一标识（必须每次加锁都不同）
func randomValue() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// AcquireOnce 单次尝试加锁：token 在此生成并返回，
// 保证 SET 进 Redis 的 value 与返回的 Lock.Value 是同一个。
// 加锁成功即自动启动看门狗。
func (l *Locker) AcquireOnce(ctx context.Context, key string, ttl time.Duration) (*Lock, bool, error) {
	value := randomValue()
	// SET key token NX PX ttl —— 一条命令原子完成“互斥 + 防死锁过期”
	ok, err := l.rdb.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}

	lock := &Lock{
		Value:     value,
		key:       key,
		locker:    l,
		ttl:       ttl,
		stopRenew: make(chan struct{}),
	}
	lock.startWatchdog()
	return lock, true, nil
}

// AcquireLock 单次抢锁：锁已被他人持有时返回 ErrLockNotAcquired
func (l *Locker) AcquireLock(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
	lock, ok, err := l.AcquireOnce(ctx, key, ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}
	return lock, nil
}

// AcquireWithRetry 带随机退避自旋抢锁，直到 maxWait 超时返回 ErrLockTimeout；
// 等待期间响应 ctx 取消，不会空转。
func (l *Locker) AcquireWithRetry(ctx context.Context, key string, ttl, maxWait time.Duration) (*Lock, error) {
	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	for {
		lock, ok, err := l.AcquireOnce(ctx, key, ttl)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errors.Join(ErrLockTimeout, ctx.Err())
			}
			return nil, err
		}
		if ok {
			return lock, nil
		}

		// 短随机退避避免惊群，同时减少缓存已经回填后的额外等待。
		timer := time.NewTimer(5*time.Millisecond + time.Duration(mrand.IntN(10))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(ErrLockTimeout, ctx.Err())
		case <-timer.C:
		}
	}
}

// startWatchdog：持有期间每 ttl/3 自动续期一次（Redisson lockWatchdogTimeout/3）；
// 内部一律用 Background()，锁的续期不被请求 ctx 取消绑架。
func (lk *Lock) startWatchdog() {
	go func() {
		ticker := time.NewTicker(lk.ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				scriptOK, err := lk.locker.rdb.Eval(
					context.Background(),
					renewScript,
					[]string{lk.key},
					lk.Value,
					lk.ttl.Milliseconds(), // ARGV[2]
				).Int64()
				if err != nil {
					log.Printf("redislock: renew lock %q failed: %v", lk.key, err)
					continue
				}
				if scriptOK == 0 {
					// 锁已不在自己手中（如过期后被他人抢走），续期无意义，退出
					return
				}
			case <-lk.stopRenew:
				return
			}
		}
	}()
}

// Unlock 幂等释放：先停看门狗，再原子地“比对 token 后删除”。
// 释放同样用 Background()，确保调用方 ctx 已取消时锁也能被删掉。
func (lk *Lock) Unlock() error {
	var err error
	lk.unlockOnce.Do(func() {
		close(lk.stopRenew) // 1. 先停看门狗
		// 2. 再执行 Lua 原子释放
		err = lk.locker.rdb.Eval(context.Background(), releaseScript, []string{lk.key}, lk.Value).Err()
	})
	return err
}
