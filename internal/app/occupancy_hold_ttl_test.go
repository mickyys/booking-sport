package app

import (
	"testing"
	"time"
)

func TestHoldTTLFromEnv(t *testing.T) {
	cases := map[string]time.Duration{
		"":     defaultMPHoldTTL,
		"1m":   time.Minute,
		"90s":  90 * time.Second,
		"5s":   defaultMPHoldTTL, // bajo el mínimo
		"nope": defaultMPHoldTTL,
	}
	for in, want := range cases {
		if got := holdTTLFromEnv(in); got != want {
			t.Errorf("holdTTLFromEnv(%q) = %v, want %v", in, got, want)
		}
	}
}
