package main

import (
	"strings"
	"testing"
)

func TestCarryForwardAvailabilityRequiresLatestReconciliation(t *testing.T) {
	sql := reconciliationCurrentAvailableSQL(false)

	required := []string{
		"r.status='verified'",
		"NOT EXISTS",
		"register_cash_reconciliations newer",
		"newer.user_id=r.user_id",
		"newer.currency=r.currency",
		"newer.session_id>r.session_id",
	}

	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("missing reconciliation safeguard: %s", fragment)
		}
	}
}

func TestCarryForwardAvailabilityReservesExistingAllocations(t *testing.T) {
	sql := reconciliationCurrentAvailableSQL(false)

	required := []string{
		"register_cash_allocations a",
		"a.reconciliation_id=r.id",
		"a.status IN ('pending','used')",
	}

	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("missing allocation reservation: %s", fragment)
		}
	}
}

func TestCarryForwardAvailabilityExcludesCurrentAllocation(t *testing.T) {
	sql := reconciliationCurrentAvailableSQL(true)

	if !strings.Contains(sql, "AND a.id<>$2") {
		t.Fatal("opening validation must exclude its own allocation")
	}
}
