package pwdutil

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode"

	"food-agent/pkg/utils"

	"golang.org/x/crypto/bcrypt"
)

type PasswordRatingType string

const (
	VeryStrong PasswordRatingType = "veryStrong"
	Strong     PasswordRatingType = "strong"
	Moderate   PasswordRatingType = "moderate"
	Weak       PasswordRatingType = "week"
)

type Strategy struct {
	Sum       int  `json:"sum"`
	IsDigit   bool `json:"is_digit"`   // 是否包含数字
	IsUpper   bool `json:"is_upper"`   // 是否包含大写字母
	IsLower   bool `json:"is_lower"`   // 是否包含小写字母
	IsSpecial bool `json:"is_special"` // 是否包含特殊符号
}

func (strategy *Strategy) checkLength(s string) {
	data := []rune(s)
	if len(data) <= 4 {
		strategy.Sum += 0
	} else if len(data) <= 8 {
		strategy.Sum += 10
	} else {
		strategy.Sum += 20
	}
}

func (strategy *Strategy) checkLetters(str string) {
	for _, s := range str {
		if strategy.IsUpper && strategy.IsLower {
			break
		}

		if !strategy.IsUpper {
			strategy.IsUpper = 'A' <= s && s <= 'Z'
		}
		if !strategy.IsLower {
			strategy.IsLower = 'a' <= s && s <= 'z'
		}
	}

	if strategy.IsUpper && strategy.IsLower {
		strategy.Sum += 20
	}
	if strategy.IsUpper || strategy.IsLower {
		strategy.Sum += 10
	}
}
func (strategy *Strategy) checkNumbers(str string) {
	var count int
	for _, s := range str {
		if '0' <= s && s <= '9' {
			count++
		}
	}

	if count > 0 {
		strategy.IsDigit = true
		if count >= 3 {
			strategy.Sum += 15
		} else {
			strategy.Sum += 10
		}
	}
}

func (strategy *Strategy) checkRepeat(s string) {
	m := make(map[rune]bool)
	for _, r := range s {
		if m[r] {
			strategy.Sum += 5
			return
		}
		m[r] = true
	}
	strategy.Sum += 10
}

func (strategy *Strategy) checkSpecialCharacter(s string) {
	var count int
	for _, i := range s {
		if unicode.IsSymbol(i) || unicode.IsPunct(i) {
			count++
		}
	}
	if count == 0 {
		return
	} else if count == 1 {
		strategy.IsSpecial = true
		strategy.Sum += 10
		return
	} else {
		strategy.IsSpecial = true
		strategy.Sum += 20
		return
	}
}

func calcPwdScore(s string) int {
	strategy := &Strategy{}
	strategy.checkLength(s)
	strategy.checkLetters(s)
	strategy.checkNumbers(s)
	strategy.checkSpecialCharacter(s)
	strategy.checkRepeat(s)

	if strategy.IsLower && strategy.IsUpper && strategy.IsDigit && strategy.IsSpecial {
		strategy.Sum += 15
	} else if strategy.IsDigit && strategy.IsSpecial && strategy.IsLower {
		strategy.Sum += 10
	} else if strategy.IsDigit && (strategy.IsLower || strategy.IsUpper) {
		strategy.Sum += 5
	} else {
		strategy.Sum += 0
	}
	return strategy.Sum
}

func CheckPasswordRating(pwd string, rating PasswordRatingType) bool {
	standardScore := 60 // model.Strong
	switch rating {
	case VeryStrong:
		standardScore = 80
	case Moderate:
		standardScore = 50
	case Weak:
		return true
	}

	return calcPwdScore(pwd) >= standardScore
}

func PasswordHash(pwd string) (string, error) {
	b, err := bcrypt.GenerateFromPassword(utils.String2Bytes(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return utils.Bytes2String(b), err
}

func PasswordVerify(pwd, hash string) bool {
	return bcrypt.CompareHashAndPassword(utils.String2Bytes(hash), utils.String2Bytes(pwd)) == nil
}

func RandPassword(passwordLength int, special ...bool) (string, error) {
	rd := rand.New(rand.NewSource(time.Now().UnixNano()))

	// 密码长度
	if passwordLength < 6 {
		return "", fmt.Errorf("passwordLength less than 6")
	}

	// 可用于密码的字符集，包含大写字母、小写字母、数字和特殊字符
	specialCharacters := "~!@$%^&*."
	characters := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" + specialCharacters

	// 生成随机字节数组
	randomBytes := make([]byte, passwordLength-1)
	if _, err := rd.Read(randomBytes); err != nil {
		return "", err
	}

	// 使用 characters 中的字符生成密码
	password := ""
	for i := 0; i < passwordLength-1; i++ {
		password += string(characters[int(randomBytes[i])%len(characters)])
	}

	// 确保生成的密码肯定有特殊字符
	if len(special) > 0 && special[0] && !strings.ContainsAny(password, specialCharacters) {
		// 插入一个特殊字符
		specialChar := string(specialCharacters[rd.Intn(len(specialCharacters))])
		specialIdx := rd.Intn(passwordLength)
		password = password[:specialIdx] + specialChar + password[specialIdx:]
	}

	return password, nil
}
