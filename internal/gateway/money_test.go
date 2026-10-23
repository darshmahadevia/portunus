package gateway

import (
	"math"
	"testing"
)

func TestIntegerPricingRoundsUpAndRejectsOverflow(t *testing.T) {
	for _, tc := range []struct{ in, out, ip, op, want int64 }{{1, 1, 1, 1, 2}, {1000000, 1000000, 3, 7, 10}, {0, 0, 999, 999, 0}, {1000001, 0, 1, 0, 2}} {
		got, err := price(tc.in, tc.out, tc.ip, tc.op)
		if err != nil || got != tc.want {
			t.Fatalf("price(%+v)=%d,%v", tc, got, err)
		}
	}
	if _, err := price(math.MaxInt64, 1, 2, 1); err == nil {
		t.Fatal("overflow accepted")
	}
	if addBounded(math.MaxInt64-1, 5) != math.MaxInt64 {
		t.Fatal("counter overflow")
	}
	c := &clientState{config: ClientConfig{BudgetMicros: math.MaxInt64}, spent: math.MaxInt64 - 2, reserved: 5}
	if available(c) != 0 {
		t.Fatal("overflow bypassed ledger")
	}
}
