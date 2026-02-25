package token

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-cmp/cmp"
)

func TestTokenVerifyWithoutCacheValidate(t *testing.T) {
	issuer := NewJWTTokenManager([]byte("fake"), jwt.SigningMethodHS256)

	admin := Info{
		Username: "admin",
	}

	tokenString, err := issuer.IssueTo(admin, 0)

	if err != nil {
		t.Fatal(err)
	}

	got, err := issuer.Verify(tokenString)

	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(got, admin); diff != "" {
		t.Errorf("token validate failed: %s", diff)

	}
}
