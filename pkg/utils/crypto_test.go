package utils

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestAesDecryptCBC(t *testing.T) {
	origData := []byte("test aes cbc") // 待加密的数据
	key := []byte(`-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEA6fYmTuHnU+kG0nfrsN/EwQlPPsgy5E7g0jp9WNdJK7LLuBub9kQD
mX4l61ewy5DR3XqjNL4D3q32CZqjLjagR9dW2EBv/F0ijjmX7y/nJIH3/NoyEyOV
VUPyB7svKTTvFcVIAwCAngz5gE5C4A6kfuuRV+ztp2sBAI7M1L//pa9i+COJdPUT
EnsDKcRS8YlXSEvxPQ8OU1z28DgSUfcClydTgDhjpuLCxw7m4yeijihNeMKUb1U3
Hd3fUMvBv46SpGl0cPccn+Lhb/u3WLMj05TxPYOWSh4a6qcz71iLoclT6tXx6HWV
3EO+3cRc8364qAz8xobrBLxWQZ9jZqDb/wIDAQAB
-----END RSA PUBLIC KEY-----`) // 加密的密钥
	t.Log("原文：", string(origData))

	encrypted, err := AesEncryptCBC(origData, key[31:47])
	if err != nil {
		t.Fatal(err)
	}
	t.Log("密文(hex)：", hex.EncodeToString(encrypted))
	t.Log("密文(base64)：", base64.StdEncoding.EncodeToString(encrypted))
	decrypted, err := AesDecryptCBC(encrypted, key[31:47])
	if err != nil {
		t.Fatal(err)
	}
	t.Log("解密结果：", string(decrypted), err)

}
