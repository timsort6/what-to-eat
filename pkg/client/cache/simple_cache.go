package cache

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrNoSuchKey = errors.New("no such key")

type simpleObject struct {
	value       string
	neverExpire bool
	expiredAt   time.Time
}

// SimpleCache implements cache.Interface use memory objects, it should be used only for testing
type simpleCache struct {
	store sync.Map
}

func NewSimpleCache() Interface {
	return &simpleCache{store: sync.Map{}}
}

func (s *simpleCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	// There is a little difference between go regexp and redis key pattern
	// In redis, * means any character, while in go . means match everything.
	pattern = strings.Replace(pattern, "*", ".", -1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	var keys []string
	s.store.Range(func(key, value interface{}) bool {
		if re.MatchString(key.(string)) {
			keys = append(keys, key.(string))
		}
		return true
	})

	return keys, nil
}

func (s *simpleCache) Set(ctx context.Context, key string, value string, duration time.Duration) error {
	sobject := simpleObject{
		value:       value,
		neverExpire: false,
		expiredAt:   time.Now().Add(duration),
	}

	if duration == NeverExpire {
		sobject.neverExpire = true
	}

	s.store.Store(key, &sobject)
	return nil
}

func (s *simpleCache) Del(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		s.store.Delete(key)
	}
	return nil
}

func (s *simpleCache) Get(ctx context.Context, key string) (string, error) {
	if sobject, ok := s.store.Load(key); ok {
		obj := sobject.(*simpleObject)
		if obj.neverExpire || time.Now().Before(obj.expiredAt) {
			return obj.value, nil
		}

		s.store.Delete(key)
	}

	return "", ErrNoSuchKey
}

func (s *simpleCache) Exists(ctx context.Context, keys ...string) (bool, error) {
	for _, key := range keys {
		if _, ok := s.store.Load(key); !ok {
			return false, nil
		}
	}

	return true, nil
}

func (s *simpleCache) Expire(ctx context.Context, key string, duration time.Duration) error {
	value, err := s.Get(ctx, key)
	if err != nil {
		return err
	}

	sobject := simpleObject{
		value:       value,
		neverExpire: false,
		expiredAt:   time.Now().Add(duration),
	}

	if duration == NeverExpire {
		sobject.neverExpire = true
	}

	s.store.Store(key, &sobject)
	return nil
}
