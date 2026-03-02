package utils

import (
	"crypto"
	"encoding/hex"
	"fmt"
)

func MD5(str string) []byte {
	hashed, _ := Hash(crypto.MD5, str)
	return hashed
}

func MD5Hex(str string) string {
	hashed, _ := HashHex(crypto.MD5, str)
	return hashed
}

func SHA1(str string) []byte {
	hashed, _ := Hash(crypto.SHA1, str)
	return hashed
}

func SHA1Hex(str string) string {
	hashed, _ := HashHex(crypto.SHA1, str)
	return hashed
}

func SHA256(str string) []byte {
	hashed, _ := Hash(crypto.SHA256, str)
	return hashed
}

func SHA256Hex(str string) string {
	hashed, _ := HashHex(crypto.SHA256, str)
	return hashed
}

func Hash(hash crypto.Hash, str string) ([]byte, error) {
	if !hash.Available() {
		return nil, fmt.Errorf("invilid hash")
	}

	h := hash.New()
	h.Write([]byte(str))
	return h.Sum(nil), nil
}

func HashHex(h crypto.Hash, str string) (string, error) {
	hashed, err := Hash(h, str)
	if err != nil {
		return "", nil
	}

	return hex.EncodeToString(hashed), nil
}

func HashPolicyID(platform, service, policy string) string {
	return MD5Hex(platform + "$" + service + "$" + policy)
}

func HashServiceID(platform, service string) string {
	return MD5Hex(platform + "$" + service)
}

func HashRegionID(platform, region string) string {
	return MD5Hex(platform + "$" + region)
}

func HashAssetsID(credentialID int64, region, service, instanceId string) string {
	return MD5Hex(fmt.Sprintf("%d$%s$%s$%s", credentialID, region, service, instanceId))
}

func HashRiskID(credentialID int64, assetHashID string, policyID string) string {
	return MD5Hex(fmt.Sprintf("%d$%s$%s", credentialID, assetHashID, policyID))
}
func HashFragileChainID(instanceHashId1 string, instanceHashId2 string, vpcHashId string) string {
	return MD5Hex(fmt.Sprintf("%s$%s$%s", instanceHashId1, instanceHashId2, vpcHashId))
}

// 生成 risk_scan_history_counts(每次扫描的全局risk计数) 快照hashID
func HashSnapshotID(taskID int64, startAt string) string {
	return MD5Hex(fmt.Sprintf("%d$%s", taskID, startAt))
}
