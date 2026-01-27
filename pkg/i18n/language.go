package i18n

import "context"

type ctxKey string

const ctxLanguageKey ctxKey = "language"

// WithLanguage 将语言偏好写入 context（例如 "zh" / "en"）。
func WithLanguage(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.TODO()
	}
	return context.WithValue(ctx, ctxLanguageKey, lang)
}

// LanguageFromCtx 从 context 读取语言偏好，默认返回 "zh"。
func LanguageFromCtx(ctx context.Context) string {
	if ctx == nil {
		return "zh"
	}
	if v, ok := ctx.Value(ctxLanguageKey).(string); ok && v != "" {
		return v
	}
	return "zh"
}

