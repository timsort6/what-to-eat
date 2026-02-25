package utils

import (
	"fmt"
	"hash/fnv"
	"os"
	"reflect"
	"strings"
	"time"
)

// value从右到左的第flag位设置成0
func SetBit0(value uint64, flag uint64) uint64 {
	pre := value
	pre &= ^(1 << flag)
	return pre
}

// value从右到左的第flag位设置成1
func SetBit1(value uint64, flag uint64) uint64 {
	pre := value
	pre |= 1 << (flag)
	return pre
}

func ExistBit1(value uint64, flag uint64) bool {
	return (value>>flag)&1 == 1
}

func ExistBit0(value uint64, flag uint64) bool {
	return (value>>flag)&1 == 0
}

func ExistInStringSlice(value []string, flag string) bool {
	for i := range value {
		if flag == value[i] {
			return true
		}
	}
	return false
}

func ExistInInt64Slice(value []int64, flag int64) bool {

	for i := range value {
		if flag == value[i] {
			return true
		}
	}
	return false
}

func MaxInt64(data ...int64) int64 {
	if len(data) == 0 {
		return 0
	}
	ans := data[0]
	for i := range data {
		if data[i] > ans {
			ans = data[i]
		}
	}
	return ans
}

func MinInt64(data ...int64) int64 {
	if len(data) == 0 {
		return 0
	}
	ans := data[0]
	for i := range data {
		if data[i] < ans {
			ans = data[i]
		}
	}
	return ans
}

func GenerateUUID64(strs ...string) uint64 {
	s := strings.Join(strs, "/")
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func DuplicateInt64Slice(va []int64) []int64 {
	exit := make(map[int64]struct{})
	ans := make([]int64, 0, len(va))
	for i := range va {
		if _, ok := exit[va[i]]; !ok {
			ans = append(ans, va[i])
			exit[va[i]] = struct{}{}
		}
	}
	return ans
}

func DuplicateIntSlice(va []int) []int {
	exit := make(map[int]struct{})
	ans := make([]int, 0, len(va))
	for i := range va {
		if _, ok := exit[va[i]]; !ok {
			ans = append(ans, va[i])
			exit[va[i]] = struct{}{}
		}
	}
	return ans
}

func DuplicateUint64Slice(va []uint64) []uint64 {
	exit := make(map[uint64]struct{})
	ans := make([]uint64, 0, len(va))
	for i := range va {
		if _, ok := exit[va[i]]; !ok {
			ans = append(ans, va[i])
			exit[va[i]] = struct{}{}
		}
	}
	return ans
}

func DuplicateUint32Slice(va []uint32) []uint32 {
	exit := make(map[uint32]struct{})
	ans := make([]uint32, 0, len(va))
	for i := range va {
		if _, ok := exit[va[i]]; !ok {
			ans = append(ans, va[i])
			exit[va[i]] = struct{}{}
		}
	}
	return ans
}

func DuplicateStringSlice(va []string) []string {
	exit := make(map[string]struct{})
	ans := make([]string, 0, len(va))
	for i := range va {
		if _, ok := exit[va[i]]; !ok {
			ans = append(ans, va[i])
			exit[va[i]] = struct{}{}
		}
	}
	return ans
}

func GetInterSetInt64(pre, after []int64) []int64 {

	preMap := make(map[int64]struct{})
	afterMap := make(map[int64]struct{})
	for i := range pre {
		preMap[pre[i]] = struct{}{}
	}
	for i := range after {
		afterMap[after[i]] = struct{}{}
	}
	ans := make([]int64, 0)
	for k := range preMap {
		if _, ok := afterMap[k]; ok {
			ans = append(ans, k)
		}
	}

	return ans
}

func GetInterSetString(pre, after []string) []string {

	preMap := make(map[string]struct{})
	afterMap := make(map[string]struct{})
	for i := range pre {
		preMap[pre[i]] = struct{}{}
	}
	for i := range after {
		afterMap[after[i]] = struct{}{}
	}
	ans := make([]string, 0)
	for k := range preMap {
		if _, ok := afterMap[k]; ok {
			ans = append(ans, k)
		}
	}

	return ans
}

func GetTimeUnixMilli(ti *time.Time) int64 {
	if ti == nil || ti.IsZero() {
		return 0
	}
	return ti.UnixMilli()
}

func GetDirAllFilename(pa string) ([]string, error) {
	dir, err := os.ReadDir(pa)
	if err != nil {
		return nil, err
	}
	ans := make([]string, 0)
	for i := range dir {
		if dir[i].IsDir() {
			continue
		}
		ans = append(ans, fmt.Sprintf("%s/%s", pa, dir[i].Name()))
	}
	return ans, nil
}

func GetEnvWithDefault(envKey string, def string) string {
	e := os.Getenv(envKey)
	if e == "" {
		e = def
	}
	return e
}

// 谨慎使用
// slice 必须是[]*struct 的方式，fieldName 必须是该结构体存在的
func RemoveDuplicatesByField(slice interface{}, fieldName string) interface{} {
	sliceValue := reflect.ValueOf(slice)
	if sliceValue.Kind() != reflect.Slice {
		return nil
	}

	encountered := map[interface{}]struct{}{}
	resultSlice := reflect.MakeSlice(sliceValue.Type(), 0, 0)

	for i := 0; i < sliceValue.Len(); i++ {
		item := sliceValue.Index(i)
		fieldValue := item.Elem().FieldByName(fieldName).Interface()
		if _, ok := encountered[fieldValue]; !ok {
			encountered[fieldValue] = struct{}{}
			resultSlice = reflect.Append(resultSlice, item)
		}
	}

	return resultSlice.Interface()
}
