package limiter

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultLoginLimiterExpireAt = time.Hour * 4
)

type failStatus struct {
	count     int64
	createdAt time.Time
}

func (s *failStatus) Incr() {
	atomic.AddInt64(&s.count, 1)
}

func (s *failStatus) Count() int64 {
	return atomic.LoadInt64(&s.count)
}

func newFailStatus() *failStatus {
	return &failStatus{
		count:     0,
		createdAt: time.Now(),
	}
}

type LoginLimiter struct {
	counts         *sync.Map
	expireDuration time.Duration
}

func NewLoginLimiter(expireDuration ...time.Duration) *LoginLimiter {
	duration := defaultLoginLimiterExpireAt
	if len(expireDuration) > 0 {
		duration = expireDuration[0]
	}

	return &LoginLimiter{
		expireDuration: duration,
		counts:         new(sync.Map),
	}
}

func (l *LoginLimiter) getOrCreateStatus(key string) *failStatus {
	status, _ := l.counts.LoadOrStore(key, newFailStatus())
	fs, ok := status.(*failStatus)
	if !ok {
		return newFailStatus()
	}

	if time.Since(fs.createdAt) > l.expireDuration {
		l.Clean(key)
		return newFailStatus()
	}

	return fs
}

func (l *LoginLimiter) IsLimit(key string, threshold int64) bool {
	return l.getOrCreateStatus(key).Count() >= threshold
}

func (l *LoginLimiter) LoginFailToReachLimit(key string, threshold int64) bool {
	status := l.getOrCreateStatus(key)
	status.Incr()
	return status.Count() >= threshold
}

func (l *LoginLimiter) Clean(key string) {
	l.counts.Delete(key)
}
