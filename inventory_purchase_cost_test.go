package main

import (
	"math"
	"testing"
)

func TestInventoryPurchaseCost(t *testing.T) {
	tests := []struct {
		name    string
		qty     int64
		cost    int64
		want    int64
		wantErr bool
	}{
		{"standard purchase", 10, 150, 1500, false},
		{"zero cost", 10, 0, 0, false},
		{"large valid cost", 2, 1000000000, 2000000000, false},
		{"overflow", math.MaxInt64, 2, 0, true},
		{"zero quantity", 0, 100, 0, true},
		{"negative cost", 1, -1, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inventoryPurchaseCost(tc.qty, tc.cost)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}
