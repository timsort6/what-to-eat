package token

import (
	"context"
	"fmt"
	"time"

	"food-agent/pkg/client/cache"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

const (
	DefaultIssuerName    = "async"
	DefaultCacheDuration = 24 * time.Hour
)

type Claims struct {
	Info
	// Currently, we are not using any field in jwt.StandardClaims
	jwt.RegisteredClaims
}

type jwtToken struct {
	name       string
	signKey    any
	verifyKey  any
	signMethod jwt.SigningMethod

	cacheClient   cache.Interface
	cacheDuration time.Duration
	duration      bool
}

func (jt *jwtToken) GetTokenFromCtx(ctx context.Context) (string, error) {
	// Assuming the token is stored in the context with a key "token"
	token, ok := ctx.Value("token").(string)
	if !ok {
		return "", fmt.Errorf("token not found in context")
	}
	return token, nil
}
func (jt *jwtToken) Verify(tokenString string) (Info, error) {
	clm := Claims{}
	// verify token signature and expiration time
	_, err := jwt.ParseWithClaims(tokenString, &clm, jt.keyFunc)
	if err != nil {
		zap.L().Info("", zap.Error(err))
		return clm.Info, err
	}

	if jt.duration {
		saveToken, err := jt.cacheClient.Get(context.Background(), "token:"+clm.Info.UID)
		if err != nil {
			return clm.Info, fmt.Errorf("cache not found %w", err)
		}

		if saveToken != tokenString {
			return clm.Info, fmt.Errorf("token not match %s\n%s", saveToken, tokenString)
		}

		// renew token
		if err = jt.cacheClient.Set(context.Background(), "token:"+clm.Info.UID, tokenString, jt.cacheDuration); err != nil {
			return clm.Info, fmt.Errorf("cache renew error %w", err)
		}
	}

	return clm.Info, nil
}

func (jt *jwtToken) IssueTo(info Info, expiresIn time.Duration) (string, error) {
	issueAt := jwt.NewNumericDate(time.Now())
	notBefore := issueAt
	clm := &Claims{
		Info: info,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  issueAt,
			Issuer:    jt.name,
			NotBefore: notBefore,
		},
	}

	if expiresIn > 0 {
		clm.ExpiresAt = jwt.NewNumericDate(clm.IssuedAt.Add(expiresIn))
	}

	token := jwt.NewWithClaims(jt.signMethod, clm)

	tokenString, err := token.SignedString(jt.signKey)
	if err != nil {
		return "", fmt.Errorf("sign token error %w", err)
	}

	if jt.duration {
		if err = jt.cacheClient.Set(context.Background(), "token:"+info.UID, tokenString, jt.cacheDuration); err != nil {
			return "", fmt.Errorf("cache set error %w", err)
		}
	}

	return tokenString, nil
}

func (jt *jwtToken) keyFunc(t *jwt.Token) (any, error) {
	if jt.verifyKey != nil {
		return jt.verifyKey, nil
	} else {
		return jt.signKey, nil
	}
}

type Option func(o *jwtToken)

func SetIssuer(name string) Option {
	return func(jt *jwtToken) {
		jt.name = name
	}
}

func SetVerifyKey(verifyKey []byte) Option {
	return func(jt *jwtToken) {
		jt.verifyKey = verifyKey
	}
}

func SetDuration(cacheClient cache.Interface, duration time.Duration) Option {
	return func(jt *jwtToken) {
		jt.duration = true
		jt.cacheClient = cacheClient
		jt.cacheDuration = duration
	}
}

func NewJWTTokenManager(signKey []byte, signMethod jwt.SigningMethod, options ...Option) Manager {
	jt := &jwtToken{
		name:       DefaultIssuerName,
		signKey:    signKey,
		signMethod: signMethod,
	}

	for _, opt := range options {
		opt(jt)
	}

	return jt
}
