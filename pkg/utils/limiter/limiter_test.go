package limiter

import (
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"testing"
	"time"
)

func TestNewKeyLimiter(t *testing.T) {
	zap.ReplaceGlobals(zap.NewExample())

	limiter := NewKeyLimiter(rate.Every(time.Second), 3)
	for i := 0; i < 3; i++ {
		t.Log(limiter.AllowKey("key1"))
	}

	assert.Equal(t, false, limiter.AllowKey("key1")) // false
	assert.Equal(t, true, limiter.AllowKey("key2"))  // true

	time.Sleep(time.Second)
	assert.Equal(t, true, limiter.AllowKey("key1"))
}

func TestNewLoginLimiter(t *testing.T) {
	zap.ReplaceGlobals(zap.NewExample())

	limiter := NewLoginLimiter(time.Second)
	threshold := 5

	for i := 0; i < threshold-1; i++ {
		assert.Equal(t, false, limiter.LoginFailToReachLimit("key1", int64(threshold)))
	}

	assert.Equal(t, true, limiter.LoginFailToReachLimit("key1", int64(threshold)))

	assert.Equal(t, true, limiter.IsLimit("key1", int64(threshold)))

	time.Sleep(time.Second)
	assert.Equal(t, false, limiter.IsLimit("key1", int64(threshold)))

}
