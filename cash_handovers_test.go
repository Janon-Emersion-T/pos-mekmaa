package main

import (
	"os"
	"strings"
	"testing"
)

func TestCashHandoverSessionAccountingSource(t *testing.T) {
	source, err := os.ReadFile("session_tracking.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	for _, fragment := range []string{
		"SUM(approved_amount)",
		"t.cash_session_id=r.id",
		"t.status IN ('approved','partially_approved')",
		"t.affects_session_cash",
	} {
		if !strings.Contains(code, fragment) {
			t.Errorf("session expected cash is missing handover safeguard %q", fragment)
		}
	}
}

func TestCashierCanOpenFromAccountableCashSource(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	for _, fragment := range []string{
		`"staff_cash", &cashierID`,
		`Opening cash must match your current accountable cash balance`,
		`SET cash_session_id=$1`,
		`AND status='pending'`,
	} {
		if !strings.Contains(code, fragment) {
			t.Errorf("accountable-cash opening is missing %q", fragment)
		}
	}
}

func TestCashHandoverRoutesAndOwnershipSource(t *testing.T) {
	source, err := os.ReadFile("cash_handovers.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	for _, fragment := range []string{
		`GET /api/cash-handovers`,
		`POST /api/cash-handovers`,
		`GET /api/cash-handovers/review`,
		`POST /api/cash-handovers/{id}/review`,
		`"staff_cash", &actor.ID`,
		`role='superadmin' AND active=true`,
		`source_account_id=$1 AND status='pending'`,
	} {
		if !strings.Contains(code, fragment) {
			t.Errorf("cash handover implementation is missing %q", fragment)
		}
	}
}
