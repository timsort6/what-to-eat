package utils

import (
	"github.com/sony/sonyflake"
	"go.uber.org/zap"
	"math/rand"
	"strconv"
	"time"
)

var (
	rd = rand.New(rand.NewSource(time.Now().UnixNano()))
	sf *sonyflake.Sonyflake
)

const max = 1<<16 - 1

func init() {
	st := sonyflake.Settings{
		StartTime: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		MachineID: func() (uint16, error) {
			return uint16(rd.Intn(max)), nil
		},
		CheckMachineID: nil,
	}

	sf = sonyflake.NewSonyflake(st)
	if sf == nil {
		zap.L().Panic("sonyflake init fails")
	}
}

func NextID() string {
	ui64id, _ := sf.NextID()
	return strconv.FormatUint(ui64id, 10)
}
