package main

import "testing"

func TestProportionalRefundCost(t *testing.T) {
	tests := []struct {
		name             string
		originalQuantity int64
		originalCost     int64
		refundedQuantity int64
		refundedCost     int64
		returnQuantity   int64
		want             int64
		wantErr          bool
	}{
		{
			name:             "first partial refund",
			originalQuantity: 3,
			originalCost:     100,
			returnQuantity:   1,
			want:             33,
		},
		{
			name:             "second partial refund",
			originalQuantity: 3,
			originalCost:     100,
			refundedQuantity: 1,
			refundedCost:     33,
			returnQuantity:   1,
			want:             34,
		},
		{
			name:             "final partial refund",
			originalQuantity: 3,
			originalCost:     100,
			refundedQuantity: 2,
			refundedCost:     67,
			returnQuantity:   1,
			want:             33,
		},
		{
			name:             "full refund",
			originalQuantity: 5,
			originalCost:     625,
			returnQuantity:   5,
			want:             625,
		},
		{
			name:             "excessive refund",
			originalQuantity: 5,
			originalCost:     625,
			refundedQuantity: 4,
			returnQuantity:   2,
			wantErr:          true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := proportionalRefundCost(
				tc.originalQuantity,
				tc.originalCost,
				tc.refundedQuantity,
				tc.refundedCost,
				tc.returnQuantity,
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
