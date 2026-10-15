package main

import (
	"math"
	"testing"
)

func TestWeightedAverageIssueValue(t *testing.T) {
	tests := []struct {
		name     string
		stock    int64
		balance  int64
		quantity int64
		want     int64
		wantErr  bool
	}{
		{"five units from twenty", 20, 2500, 5, 625, false},
		{"all remaining stock", 15, 1875, 15, 1875, false},
		{"fractional average rounded", 3, 100, 1, 33, false},
		{"fractional remainder", 2, 67, 1, 34, false},
		{"zero value inventory", 10, 0, 5, 0, false},
		{"zero stock", 0, 100, 1, 0, true},
		{"excessive quantity", 5, 100, 6, 0, true},
		{"negative balance", 5, -1, 1, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := weightedAverageIssueValue(
				tc.stock,
				tc.balance,
				tc.quantity,
			)

			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}

			if !tc.wantErr && got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestInventoryPurchaseValue(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		qty     int64
		cost    int64
		want    int64
		err     bool
	}{
		{"initial purchase", 0, 10, 100, 1000, false},
		{"second purchase", 1000, 10, 150, 2500, false},
		{"free inventory", 500, 5, 0, 500, false},
		{"negative cost", 0, 5, -1, 0, true},
		{"overflow", math.MaxInt64, 2, 100, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inventoryPurchaseValue(
				tc.balance,
				tc.qty,
				tc.cost,
			)

			if (err != nil) != tc.err {
				t.Fatalf("error=%v wantErr=%v", err, tc.err)
			}

			if !tc.err && got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}
