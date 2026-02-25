package utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
)

func PKCS5Padding(cipherText []byte, blockSize int) []byte {
	padding := blockSize - len(cipherText)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(cipherText, padText...)
}

func PKCS5UnPadding(origData []byte) ([]byte, error) {
	length := len(origData)
	unpadding := int(origData[length-1])
	if length-unpadding < 0 {
		return nil, fmt.Errorf("illeagl data")
	}
	return origData[:(length - unpadding)], nil
}

// =================== AES CBC ======================
func AesEncryptCBC(origData []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()                  // 获取秘钥块的长度
	origData = PKCS5Padding(origData, blockSize)    // 补全码
	blockMode := cipher.NewCBCEncrypter(block, key) // 加密模式
	encrypted := make([]byte, len(origData))        // 创建数组
	blockMode.CryptBlocks(encrypted, origData)      // 加密
	return encrypted, nil
}

func AesDecryptCBC(encrypted []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key) // 分组秘钥
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()                              // 获取秘钥块的长度
	blockMode := cipher.NewCBCDecrypter(block, key[:blockSize]) // 加密模式
	decrypted := make([]byte, len(encrypted))                   // 创建数组

	if len(encrypted)%blockMode.BlockSize() != 0 {
		return nil, errors.New("input not full blocks")
	}
	blockMode.CryptBlocks(decrypted, encrypted) // 解密
	return PKCS5UnPadding(decrypted)            // 去除补全码
}
