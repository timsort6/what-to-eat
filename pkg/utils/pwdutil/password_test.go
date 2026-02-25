package pwdutil

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCheckPasswordRating(t *testing.T) {
	allow := CheckPasswordRating("12345678A", Strong)
	assert.Equal(t, true, allow)
}
