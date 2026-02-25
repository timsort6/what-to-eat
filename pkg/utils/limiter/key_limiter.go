package limiter

import (
	"golang.org/x/time/rate"
	"k8s.io/client-go/tools/cache"
	"time"
)

const (
	defaultKeyLimiterCacheExpireAt = time.Minute * 3
)

type KeyLimiter struct {
	limit rate.Limit
	burst int
	cache cache.Store
}

type limiterWrapper struct {
	key string
	lmt *rate.Limiter
}

func NewKeyLimiter(r rate.Limit, b int, cacheExpireDuration ...time.Duration) *KeyLimiter {
	duration := defaultKeyLimiterCacheExpireAt
	if len(cacheExpireDuration) > 0 {
		duration = cacheExpireDuration[0]
	}

	return &KeyLimiter{
		limit: r,
		burst: b,
		cache: cache.NewTTLStore(func(obj interface{}) (string, error) { return obj.(*limiterWrapper).key, nil }, duration),
	}
}

func (l *KeyLimiter) AllowKey(key string) bool {
	item, found, _ := l.cache.GetByKey(key)
	if !found {
		// create limiter
		item = &limiterWrapper{key: key, lmt: rate.NewLimiter(l.limit, l.burst)}
		_ = l.cache.Add(item)
	}

	return item.(*limiterWrapper).lmt.Allow()
}
