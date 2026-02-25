package dbresolver

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"gorm.io/gorm/logger"
)

type resolverLogger struct {
	logger.Interface
	flag string
}

func (l resolverLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	_, file, line, _ := runtime.Caller(3)
	caller := fmt.Sprintf("%s:%d", file, line)

	var splitFn = func() (sql string, rowsAffected int64) {
		sql, rowsAffected = fc()
		sql = fmt.Sprintf("%s [%s] %s", caller, l.flag, sql)
		return
	}

	l.Interface.Trace(ctx, begin, splitFn, err)
}

func NewResolverLogger(l logger.Interface, flag string) logger.Interface {
	if _, ok := l.(resolverLogger); ok {
		return l
	}
	return resolverLogger{
		Interface: l,
		flag:      flag,
	}
}
