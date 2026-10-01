package service

import "time"

func ptrInt64(v int64) *int64        { return &v }
func ptrFloat(v float64) *float64    { return &v }
func ptrTime(v time.Time) *time.Time { return &v }
