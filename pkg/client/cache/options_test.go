package cache

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestNewRedisOptions(t *testing.T) {
	// Test default options
	opts := NewRedisOptions()
	assert.Equal(t, "", opts.Host)
	assert.Equal(t, "", opts.Password)
	assert.Equal(t, 0, opts.DB)
}

func TestOptions_Validate(t *testing.T) {
	// Test valid options
	opts := NewRedisOptions()
	opts.Host = "localhost:6379"
	opts.Password = "password"
	opts.DB = 1

	errs := opts.Validate()
	assert.Empty(t, errs)

	// Test invalid DB (DB > 15)
	opts.DB = 16
	errs = opts.Validate()
	assert.NotEmpty(t, errs)
	assert.Equal(t, "invalid redis db", errs[0].Error())

	// Test invalid DB (DB < 0)
	opts.DB = -1
	errs = opts.Validate()
	assert.NotEmpty(t, errs)
	assert.Equal(t, "invalid redis db", errs[0].Error())
}

func TestOptions_AddFlags(t *testing.T) {
	opts := NewRedisOptions()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)

	// Test flag parsing
	err := fs.Parse([]string{
		"--redis-host=localhost:6379",
		"--redis-password=password",
		"--redis-db=1",
	})
	assert.NoError(t, err)

	assert.Equal(t, "localhost:6379", opts.Host)
	assert.Equal(t, "password", opts.Password)
	assert.Equal(t, 1, opts.DB)
}

func TestOptions_LoadEnv(t *testing.T) {
	// Set environment variables
	t.Setenv("FOOD_AGENT_REDIS_HOST", "localhost:6379")
	t.Setenv("FOOD_AGENT_REDIS_PASSWORD", "password")
	t.Setenv("FOOD_AGENT_REDIS_DB", "1")

	// Create options and load environment variables
	opts := NewRedisOptions()
	errs := opts.LoadEnv("FOOD_AGENT", nil, "")
	assert.Empty(t, errs)

	assert.Equal(t, "localhost:6379", opts.Host)
	assert.Equal(t, "password", opts.Password)
	assert.Equal(t, 1, opts.DB)
}
