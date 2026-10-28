package main

import (
	"testing"
	"time"
)

func TestRussianTime(t *testing.T) {
	cases := []struct {
		hour, minute int
		want         string
	}{
		{0, 0, "0 часов 0 минут"},
		{1, 1, "1 час 1 минута"},
		{3, 44, "3 часа 44 минуты"},
		{5, 12, "5 часов 12 минут"},
		{11, 21, "11 часов 21 минута"},
		{21, 22, "21 час 22 минуты"},
	}
	for _, tc := range cases {
		now := time.Date(2026, 10, 28, tc.hour, tc.minute, 0, 0, time.Local)
		if got := russianTime(now); got != tc.want {
			t.Errorf("russianTime(%02d:%02d) = %q, want %q", tc.hour, tc.minute, got, tc.want)
		}
	}
}
