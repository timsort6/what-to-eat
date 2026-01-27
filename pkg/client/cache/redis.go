package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

type Client struct {
	client *redis.Client
}

func NewRedisClient(option *Options, stopCh <-chan struct{}) (Interface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if stopCh == nil {
		zap.L().Fatal("no stop channel passed, redis connections will leak.")
	}

	r := &Client{}
	redisOptions := &redis.Options{
		Addr:     option.Host,
		Password: option.Password,
		DB:       option.DB,
	}

	r.client = redis.NewClient(redisOptions)

	if err := r.client.Ping(ctx).Err(); err != nil {
		r.client.Close()
		return nil, err
	}

	// close redis in case of connection leak
	go func() {
		<-stopCh
		if err := r.client.Close(); err != nil {
			zap.L().Error("", zap.Error(err))
		}
	}()

	return r, nil
}

func NewFailoverRedisClient(option *Options, stopCh <-chan struct{}) (Interface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if stopCh == nil {
		zap.L().Fatal("no stop channel passed, redis connections will leak.")
	}

	var r Client
	redisOptions := &redis.FailoverOptions{
		MasterName:    "cspm-master",
		SentinelAddrs: strings.Split(option.Host, ","),
		Password:      option.Password,
		DB:            option.DB,
	}

	r.client = redis.NewFailoverClient(redisOptions)
	if err := r.client.Ping(ctx).Err(); err != nil {
		r.client.Close()
		zap.L().Error("connect redis err", zap.Error(err))
		return nil, fmt.Errorf("connect redis err:%v", err)
	}

	// close redis in case of connection leak
	if stopCh != nil {
		go func() {
			<-stopCh
			if err := r.client.Close(); err != nil {
				zap.L().Error("", zap.Error(err))
			}
		}()
	}

	return &r, nil
}

func (r *Client) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *Client) Keys(ctx context.Context, pattern string) ([]string, error) {
	return r.client.Keys(ctx, pattern).Result()
}

func (r *Client) Set(ctx context.Context, key string, value string, duration time.Duration) error {
	return r.client.Set(ctx, key, value, duration).Err()
}

func (r *Client) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

func (r *Client) Exists(ctx context.Context, keys ...string) (bool, error) {
	existedKeys, err := r.client.Exists(ctx, keys...).Result()
	if err != nil {
		return false, err
	}

	return len(keys) == int(existedKeys), nil
}

func (r *Client) Expire(ctx context.Context, key string, duration time.Duration) error {
	return r.client.Expire(ctx, key, duration).Err()
}
