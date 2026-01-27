package cache

import (
	"context"
	"time"
)

// NeverExpire represents a never expired time
var NeverExpire = time.Duration(0)

type Interface interface {
	// Keys retrieves all keys match the given pattern
	Keys(ctx context.Context, pattern string) ([]string, error)

	// Get retrieves the value of the given key, return error if key doesn't exist
	Get(ctx context.Context, key string) (string, error)

	// Set sets the value and living duration of the given key, zero duration means never expire
	Set(ctx context.Context, key string, value string, duration time.Duration) error

	// Del deletes the given key, no error returned if the key doesn't exists
	Del(ctx context.Context, keys ...string) error

	// Exists checks the existence of a give key
	Exists(ctx context.Context, keys ...string) (bool, error)

	// Expire updates object's expiration time, return err if key doesn't exist
	Expire(ctx context.Context, key string, duration time.Duration) error
}
