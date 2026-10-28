package agentchat

import (
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogStreamError_PreservesFailureAndRedactsCredentials(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	secrets := []string{"header-value", "sk-private-key", "private-key", "query-token", "private-password"}
	err := errors.New(`status code: 400, Invalid tools[0].function.name; Authorization: Bearer header-value api_key="private-key" sk-private-key password=private-password https://example.test/chat?token=query-token`)

	logStreamError("start", err)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("错误日志数量=%d，预期 1", len(entries))
	}
	fields := entries[0].ContextMap()
	message, ok := fields["error"].(string)
	if !ok || fields["stage"] != "start" || !strings.Contains(message, "status code: 400") || !strings.Contains(message, "tools[0].function.name") {
		t.Fatalf("诊断字段未保留状态与错误位置：%v", fields)
	}
	for _, secret := range secrets {
		if strings.Contains(message, secret) {
			t.Fatal("诊断日志包含敏感字段")
		}
	}
}
