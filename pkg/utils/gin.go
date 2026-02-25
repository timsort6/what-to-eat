package utils

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func GetInt64SliceFromQuery(ctx *gin.Context, key string) []int64 {
	values := make([]int64, 0)
	split := ctx.QueryArray(key)
	for i := range split {
		parseInt, err := strconv.ParseInt(split[i], 10, 64)
		if err != nil || parseInt <= 0 {
			continue
		}
		values = append(values, parseInt)
	}

	return DuplicateInt64Slice(values)
}

func GetStringSliceFromQuery(ctx *gin.Context, key string) []string {
	statusStr := ctx.QueryArray(key)
	ans := make([]string, 0)
	for i := range statusStr {
		if strings.TrimSpace(statusStr[i]) != "" {
			ans = append(ans, strings.TrimSpace(statusStr[i]))
		}
	}

	return DuplicateStringSlice(ans)
}

// 从gin的query中取值后解析成int64,如果没有传或解析出错，都是返回0
func GetInt64FromQuery(ctx *gin.Context, key string) int64 {
	var value int64
	keyStr := ctx.Query(key)
	if keyStr != "" {
		parseInt, err := strconv.ParseInt(keyStr, 10, 64)
		if err == nil {
			value = parseInt
		}
	}
	return value
}

// 从gin的query中取值后解析成uint64,如果没有传或解析出错，都是返回0
func GetUint64FromQuery(ctx *gin.Context, key string) uint64 {
	var value uint64
	keyStr := ctx.Query(key)
	if keyStr != "" {
		parseInt, err := strconv.ParseUint(keyStr, 10, 64)
		if err == nil {
			value = parseInt
		}
	}
	return value
}

func GetUint32FromQuery(ctx *gin.Context, key string) uint32 {
	var value uint32
	keyStr := ctx.Query(key)
	if keyStr != "" {
		parseInt, err := strconv.ParseUint(keyStr, 10, 32)
		if err == nil {
			value = uint32(parseInt)
		}
	}
	return value
}

// 从gin的query中取值后解析成uint64,如果没有传或解析出错，都是返回0
func GetUint64SliceFromQuery(ctx *gin.Context, key string) []uint64 {
	res := make([]uint64, 0)
	keyStr := ctx.Query(key)
	if keyStr != "" {
		parseInt, err := strconv.ParseUint(keyStr, 10, 64)
		if err == nil || parseInt <= 0 {
			res = append(res, parseInt)
		}
	}
	return DuplicateUint64Slice(res)
}

// 从gin的query中取值后解析成y或n, 如果即传了y又传了n，就认为没有传
func GetYesOrNoFromQuery(ctx *gin.Context, key string) string {
	split := strings.Split(ctx.Query(key), ",")
	if len(split) == 1 {
		if split[0] == "y" {
			return "y"
		}
		if split[0] == "n" {
			return "n"
		}
	}
	return ""
}

// 从gin的query中取值后解析成 true和false,如果即传了true又传了false，就认为没有传
func GetBoolStringFromQuery(ctx *gin.Context, key string) string {
	ex := make(map[string]bool)

	array := ctx.QueryArray(key)
	for i := range array {
		if strings.ToLower(strings.TrimSpace(array[i])) == "true" {
			ex["true"] = true
		}
		if strings.ToLower(strings.TrimSpace(array[i])) == "false" {
			ex["false"] = true
		}
	}
	if ex["true"] && ex["false"] {
		return ""
	}
	if ex["true"] {
		return "true"
	}
	if ex["false"] {
		return "false"
	}
	return ""
}

func GetKeywordFromQuery(ctx *gin.Context, key string) string {
	word := ctx.Query(key)
	return strings.TrimSpace(word)
}

func GetLanguage(ctx *gin.Context) string {
	if strings.ToLower(ctx.GetHeader("Accept-Language")) == "en" ||
		strings.ToLower(ctx.GetHeader("accept-language")) == "en" {
		return "en"
	}
	return "zh"
}

// func DuplicateInt64Slice(va []int64) []int64 {
// 	exit := make(map[int64]struct{})
// 	ans := make([]int64, 0, len(va))
// 	for i := range va {
// 		if _, ok := exit[va[i]]; !ok {
// 			ans = append(ans, va[i])
// 			exit[va[i]] = struct{}{}
// 		}
// 	}
// 	return ans
// }
//
// func DuplicateIntSlice(va []int) []int {
// 	exit := make(map[int]struct{})
// 	ans := make([]int, 0, len(va))
// 	for i := range va {
// 		if _, ok := exit[va[i]]; !ok {
// 			ans = append(ans, va[i])
// 			exit[va[i]] = struct{}{}
// 		}
// 	}
// 	return ans
// }
//
// func DuplicateUint64Slice(va []uint64) []uint64 {
// 	exit := make(map[uint64]struct{})
// 	ans := make([]uint64, 0, len(va))
// 	for i := range va {
// 		if _, ok := exit[va[i]]; !ok {
// 			ans = append(ans, va[i])
// 			exit[va[i]] = struct{}{}
// 		}
// 	}
// 	return ans
// }
//
// func DuplicateUint32Slice(va []uint32) []uint32 {
// 	exit := make(map[uint32]struct{})
// 	ans := make([]uint32, 0, len(va))
// 	for i := range va {
// 		if _, ok := exit[va[i]]; !ok {
// 			ans = append(ans, va[i])
// 			exit[va[i]] = struct{}{}
// 		}
// 	}
// 	return ans
// }
//
// func DuplicateStringSlice(va []string) []string {
// 	exit := make(map[string]struct{})
// 	ans := make([]string, 0, len(va))
// 	for i := range va {
// 		if _, ok := exit[va[i]]; !ok {
// 			ans = append(ans, va[i])
// 			exit[va[i]] = struct{}{}
// 		}
// 	}
// 	return ans
// }

// func AesEncryptCBC(origData []byte, key []byte) ([]byte, error) {
// 	block, err := aes.NewCipher(key)
// 	if err != nil {
// 		return nil, err
// 	}
// 	blockSize := block.BlockSize()                  // 获取秘钥块的长度
// 	origData = PKCS5Padding(origData, blockSize)    // 补全码
// 	blockMode := cipher.NewCBCEncrypter(block, key) // 加密模式
// 	encrypted := make([]byte, len(origData))        // 创建数组
// 	blockMode.CryptBlocks(encrypted, origData)      // 加密
// 	return encrypted, nil
// }
