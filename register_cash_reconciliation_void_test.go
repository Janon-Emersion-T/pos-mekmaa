package main

import (
	"os"
	"strings"
	"testing"
)

func TestVoidReconciliationSafety(t *testing.T) {
	source, err := os.ReadFile("register_cash_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)

	required := []string{
		`req.Action == "void"`,
		`status != "verified"`,
		`len(req.Remarks) < 10`,
		`FROM register_cash_allocations`,
		`WHERE reconciliation_id=$1`,
		`if allocations != 0`,
		`status='voided'`,
		`verified_available_cash=NULL`,
		`verified_ledger_entry_id=NULL`,
	}

	for _, fragment := range required {
		if !strings.Contains(code, fragment) {
			t.Errorf("missing void safeguard: %s", fragment)
		}
	}
}

func TestVoidReconciliationMigration(t *testing.T) {
	source, err := os.ReadFile("migrations/034_void_register_cash_reconciliation.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "'voided'") {
		t.Fatal("migration must permit voided status")
	}
}

func TestVoidReconciliationFrontend(t *testing.T) {
	source, err := os.ReadFile("web/controls.js")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)

	for _, fragment := range []string{
		`Void verification`,
		`action === "void"`,
		`confirm("Void this verified reconciliation?`,
	} {
		if !strings.Contains(code, fragment) {
			t.Errorf("missing frontend safeguard: %s", fragment)
		}
	}
}
