package token

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

type ctxKey string

const (
	ctxPayloadKey ctxKey = "payload"
)

func WithPayload(ctx context.Context, info Info) context.Context {
	if ctx == nil {
		ctx = context.TODO()
	}

	return context.WithValue(ctx, ctxPayloadKey, info)
}

func PayloadFromCtx(ctx context.Context) (Info, error) {
	if ctx == nil {
		return Info{}, fmt.Errorf("ctx is nil")
	}

	val := ctx.Value(ctxPayloadKey)
	if val == nil {
		return Info{}, fmt.Errorf("ctx meta info not found")
	}

	info, ok := val.(Info)
	if !ok {
		return Info{}, fmt.Errorf("ctx meta info damaged")
	}

	return info, nil
}

func GetUIDFromCtx(ctx context.Context) string {
	payload, err := PayloadFromCtx(ctx)
	if err != nil {
		zap.L().Warn("GetUIDFromCtx ", zap.Error(err))
		return ""
	}

	return payload.UID
}

func GetNameFromCtx(ctx context.Context) string {
	payload, err := PayloadFromCtx(ctx)
	if err != nil {
		zap.L().Warn("GetNameFromCtx parsing token error", zap.Error(err))
		return ""
	}
	return payload.Name
}
func GetUserNameFromCtx(ctx context.Context) string {
	payload, err := PayloadFromCtx(ctx)
	if err != nil {
		zap.L().Warn("GetNameFromCtx parsing token error", zap.Error(err))
		return ""
	}
	return payload.Username
}

func PrimaryFromCtx(ctx context.Context) bool {
	payload, err := PayloadFromCtx(ctx)
	if err != nil {
		zap.L().Warn("PrimaryFromCtx parsing token error", zap.Error(err))
		return false
	}

	return payload.Primary
}
