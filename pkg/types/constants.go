package types

import "time"

const (
	DefaultTimeout       = time.Second * 500
	DefaultExportTimeout = time.Second * 60
	DefaultRetryTimeDely = time.Second * 2
	DefaultRetryTimeout  = time.Second * 10
)
