package agentchat

import (
	"regexp"

	"go.uber.org/zap"
)

var diagnosticCredentials = regexp.MustCompile(`(?i)(https?://[^\s"'<>]+|(?:authorization\s*["']?\s*[:=]\s*["']?\s*)?(?:bearer|basic)\s+[^\s",;]+|sk-[a-z0-9_-]+|(?:api[_-]?key|access[_-]?token|authorization|password|secret)\s*["']?\s*[:=]\s*["']?[^\s"',;}&]+)`)

func logStreamError(stage string, err error) {
	zap.L().Error("agent chat failed",
		zap.String("stage", stage),
		zap.String("error", diagnosticCredentials.ReplaceAllString(err.Error(), "[REDACTED]")),
	)
}
