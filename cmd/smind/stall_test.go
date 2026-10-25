package main

import (
	"testing"
	"time"
)

func TestStallTickPeriod(t *testing.T) {
	for minutes, want := range map[int]time.Duration{
		1:   30 * time.Second,
		2:   time.Minute,
		15:  time.Minute,
		120: time.Minute,
	} {
		if got := stallTickPeriod(minutes); got != want {
			t.Errorf("stallTickPeriod(%d) = %v, want %v", minutes, got, want)
		}
	}
}
