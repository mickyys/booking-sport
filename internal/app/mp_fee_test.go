package app

import (
	"math"
	"testing"
)

func TestMarketplaceFeeForAmount(t *testing.T) {
	tests := []struct {
		name       string
		amount     float64
		feePercent int
		want       float64
	}{
		{"default 7% when zero", 10000, 0, 700},
		{"default 7% when negative", 10000, -1, 700},
		{"default 7% when >100", 10000, 150, 700},
		{"custom 10%", 10000, 10, 1000},
		{"custom 5%", 20000, 5, 1000},
		{"custom 3.5 rounding", 1000, 3, 30},
		{"partial amount with default", 5000, 0, 350},
		{"amount zero", 0, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := marketplaceFeeForAmount(tt.amount, tt.feePercent)
			if math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("marketplaceFeeForAmount(%v, %d) = %v, want %v", tt.amount, tt.feePercent, got, tt.want)
			}
		})
	}
}
