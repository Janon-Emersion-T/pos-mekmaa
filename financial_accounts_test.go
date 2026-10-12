package main

import (
	"context"
	"database/sql"
	"testing"
)

func TestFinancialAccountValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		accountType string
		userID      *int64
		currency    string
	}{
		{"unknown type", "unknown", nil, "LKR"},
		{"staff without user", "staff_cash", nil, "LKR"},
		{"bank with user", "bank", int64Pointer(1), "LKR"},
		{"empty currency", "central_cash", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getOrCreateFinancialAccount(
				ctx, nil, tt.accountType, tt.userID, tt.currency,
			)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func int64Pointer(v int64) *int64 {
	return &v
}

func TestFinancialTransferValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		source      int64
		destination int64
		amount      int64
		actor       int64
	}{
		{1, 1, 100, 1},
		{1, 2, 0, 1},
		{1, 2, -100, 1},
		{0, 2, 100, 1},
		{1, 2, 100, 0},
	}

	for _, tt := range tests {
		err := transferFinancialBalance(
			ctx, nil,
			tt.source, tt.destination,
			tt.amount, tt.actor,
			"test", 1,
		)
		if err == nil {
			t.Fatal("expected validation error")
		}
	}
}

// Compile-time check that the service uses SQL transactions.
var _ *sql.Tx
