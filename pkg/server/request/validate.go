package request

import (
	"context"
	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	en_translations "github.com/go-playground/validator/v10/translations/en"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"
	"go.uber.org/zap"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"food-agent/pkg/i18n"
	"food-agent/pkg/server/errutil"
)

var (
	validate *i18nValidator
)

func ValidateStruct(ctx context.Context, s interface{}) error {
	return validate.ValidateStruct(ctx, s)
}

func init() {
	validate = &i18nValidator{va: validator.New(validator.WithRequiredStructEnabled())}

	// 注册翻译
	uni := ut.New(zh.New(), en.New())
	zhTranslate, _ := uni.GetTranslator("zh")
	enTranslate, _ := uni.GetTranslator("en")
	_ = zh_translations.RegisterDefaultTranslations(validate.va, zhTranslate)
	_ = en_translations.RegisterDefaultTranslations(validate.va, enTranslate)
	validate.translate = uni

	// 自定义tag注册
	registerCustomAll(validate)
}

type i18nValidator struct {
	once      sync.Once
	va        *validator.Validate
	translate *ut.UniversalTranslator
}

func (v *i18nValidator) ValidateStruct(ctx context.Context, s interface{}) error {
	if err := v.va.Struct(s); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			lang := i18n.LanguageFromCtx(ctx)
			trans, _ := v.translate.GetTranslator(lang)
			t := removeStructName(validationErrors.Translate(trans))
			return errutil.NewError(http.StatusBadRequest, t)
		}
		return err
	}

	return nil
}

func removeStructName(fields map[string]string) string {
	result := make([]string, 0)
	for _, errmsg := range fields {
		result = append(result, errmsg)
	}
	return strings.Join(result, "\n")
}

// registerAll 注册自定义tag校验和翻译
func registerCustomAll(valid *i18nValidator) {
	zhTranslator, _ := valid.translate.GetTranslator("zh")
	enTranslate, _ := valid.translate.GetTranslator("en")

	_ = valid.va.RegisterValidation(tagTenantUsername, username)
	registerTranslation(valid.va, tagTenantUsername, zhTranslator, "{0}只能包含字母和数字以及-_")
	registerTranslation(valid.va, tagTenantUsername, enTranslate, "{0} can only contain alphanumeric characters and -_")

	_ = valid.va.RegisterValidation(tagPassword, password)
	registerTranslation(valid.va, tagPassword, zhTranslator, "{0}只能包含字母和数字以及~!@$%%^&*.")
	registerTranslation(valid.va, tagPassword, enTranslate, "{0} can only contain alphanumeric characters and ~!@$%%^&*.")

	_ = valid.va.RegisterValidation(tagExportName, exportName)
	registerTranslation(valid.va, tagExportName, zhTranslator, "{0}只能包含字母和数字以及-_")
	registerTranslation(valid.va, tagExportName, enTranslate, "{0} can only contain alphanumeric characters and -_")

	// 注册错误翻译
	registerTranslation(valid.va, tagIpBlock, zhTranslator, "{0}必须是一个有效的IPv4地址或是一个包含IPv4地址的有效无类别域间路由(CIDR)")
	registerTranslation(valid.va, tagIpBlock, enTranslate, "{0} must be a valid IPv4 address or contain a valid CIDR notation for an IPv4 address")
}

func registerTranslation(va *validator.Validate, tag string, trans ut.Translator, text string) {
	err := va.RegisterTranslation(tag, trans, func(ut ut.Translator) error {
		return ut.Add(tag, text, true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		t, _ := ut.T(tag, fe.Field(), fe.Param())
		return t
	})

	if err != nil {
		zap.L().Error("register translation error", zap.Error(err))
	}
}

const (
	tagTenantUsername = "_tenant_username"
	tagPassword       = "_password"
	tagExportName     = "_export_name"
	tagIpBlock        = "ipv4|cidrv4"
)

var (
	// 只能包含字母和数字以及-_
	usernameRegex   = regexp.MustCompile("^[a-zA-Z0-9_-]+$")
	passwordRegex   = regexp.MustCompile("^[a-zA-Z0-9~!@$%^&*.]+$")
	exportNameRegex = regexp.MustCompile("^[\u4e00-\u9fa5a-zA-Z0-9_-]+$")
)

func username(fl validator.FieldLevel) bool {
	return usernameRegex.MatchString(fl.Field().String())
}

func password(fl validator.FieldLevel) bool {
	return passwordRegex.MatchString(fl.Field().String())
}

func exportName(fl validator.FieldLevel) bool {
	return exportNameRegex.MatchString(fl.Field().String())
}
