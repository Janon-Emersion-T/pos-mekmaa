package main

import (
	"os"
	"strings"
	"testing"
)

func TestLegacyReconciliationAccountHandling(t *testing.T) {
	source, err := os.ReadFile("register_cash_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}

	code := string(source)

	reject := strings.Index(code, `if req.Action == "reject"`)
	initialize := strings.Index(code, `getOrCreateFinancialAccount(`)
	verify := strings.Index(code, `verifiedPhysicalAvailable(r.Context(), tx, id)`)

	if reject < 0 || initialize < 0 || verify < 0 {
		t.Fatal("expected reconciliation review stages are missing")
	}

	if !(reject < initialize && initialize < verify) {
		t.Fatal("cashier account initialization must occur after rejection and before verification")
	}

	if !strings.Contains(code, `"staff_cash", &userID, currency`) {
		t.Fatal("verification must initialize the correct cashier account")
	}

	reviewCode := code[strings.Index(code, "func (s *Server) reviewRegisterCashReconciliation"):]

	if strings.Contains(reviewCode, "transferFinancialBalance(") ||
		strings.Contains(reviewCode, "INSERT INTO financial_ledger_entries") {
		t.Fatal("reconciliation review must not create financial credits or transfers")
	}
}
