package request

import (
	"context"
	"testing"

	"food-agent/pkg/i18n"
)

func TestValidateStruct(t *testing.T) {
	type A struct {
		P1 string `validate:"gte=3"`
		P2 int64  `validate:"required"`
	}

	a := A{}

	// zh
	ctx := context.Background()
	err := ValidateStruct(ctx, a)
	t.Log(err)

	// en
	ctx = i18n.WithLanguage(ctx, "en")
	err = ValidateStruct(ctx, a)
	t.Log(err)
}
