package utils

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unsafe"
)

// Bytes2String convert []byte to string
func Bytes2String(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// String2Bytes convert string to []byte
func String2Bytes(s string) (b []byte) {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// String2IntArray convert string split by , to []int
func String2IntArray(ptr *string) ([]int, error) {
	if ptr == nil {
		return nil, errors.New("pointer is nil")
	}

	str := *ptr                             // 解引用字符串指针
	splitStrings := strings.Split(str, ",") // 按逗号分割字符串

	intArray := make([]int, len(splitStrings))
	for i, s := range splitStrings {
		// 转换字符串到整数并处理可能的错误
		intVal, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		intArray[i] = intVal
	}

	return intArray, nil
}

// type number interface {
// 	int8 | int16 | int | int32 | int64 |
// 		uint8 | uint16 | uint | uint32 | uint64 |
// 		float32 | float64
// }
//
// func NumberValue[T number](i *T, def ...T) T {
// 	if i == nil {
// 		if len(def) == 0 {
// 			return 0
// 		}
//
// 		return def[0]
// 	}
//
// 	return *i
// }
//
// func NumberPtr[T number](i T) *T {
// 	return &i
// }
//
// func StringValue[T string | []byte](s *T, def ...T) T {
// 	if s == nil {
// 		if len(def) == 0 {
// 			return T("")
// 		}
//
// 		return def[0]
// 	}
// 	return *s
// }
//
// func StringPtr[T string | []byte](s T) *T {
// 	return &s
// }

// PtrString convert *string to string
func PtrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// StringPtr convert string to *string
func StringPtr(s string) *string {
	return &s
}

// PtrInt32 convert *int32 to int32
func PtrInt32(s *int32) int32 {
	if s == nil {
		return 0
	}
	return *s
}

// Int32Ptr convert int32 to *int32
func Int32Ptr(i int32) *int32 {
	return &i
}

// PtrInt64 convert *int64 to int64
func PtrInt64(s *int64) int64 {
	if s == nil {
		return 0
	}
	return *s
}

// Int64Ptr convert int64 to *int64
func Int64Ptr(i int64) *int64 {
	return &i
}

// PtrUInt64 convert *uint64 to uint64
func PtrUint64(s *uint64) uint64 {
	if s == nil {
		return 0
	}
	return *s
}

// PtrBool convert *bool to bool
func PtrBool(s *bool) bool {
	if s == nil {
		return false
	}
	return *s
}

func GetDefaultString(s string, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Deprecated: use slices.Contains instead.
func ContainsStr(s []string, str string) bool {
	return slices.Contains(s, str)
}

// Deprecated: use slices.Contains instead.
func ContainsInt32(is []int32, i32 int32) bool {
	return slices.Contains(is, i32)
}

// Deprecated: use slices.Contains instead.
func ContainsInt64(is []int64, i64 int64) bool {
	return slices.Contains(is, i64)
}

func CreateInt64Ptr(i int64) *int64 {
	return &i
}

func CreateUint64Ptr(i uint64) *uint64 {
	return &i
}

func CreateStringPtr(s string) *string {
	return &s
}

func TrimLineBreaks(a string) string {
	return strings.Trim(a, "\n")
}
