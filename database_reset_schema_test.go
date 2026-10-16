package main

import (
	"os"
	"strings"
	"testing"
)

// Keep resetDatabase's explicit allow-list synchronized with business tables.
// An explicit list is intentional: TRUNCATE ... CASCADE could unexpectedly
// clear users, settings, or future tables that the reset contract preserves.
func TestDatabaseResetIncludesAllTransactionalTables(t *testing.T) {
	source, err := os.ReadFile("owner_controls.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)

	for _, table := range []string{
		"capital_entries",
		"inventory_valuation_entries",
		"optimized_debts_setting_changes",
		"profit_drawings",
		"purchase_funding_allocations",
		"purchase_payments",
		"supplier_invoice_transactions",
		"supplier_invoices",
	} {
		if !strings.Contains(code, "    "+table+",") && !strings.Contains(code, "    "+table+"\n") {
			t.Errorf("database reset is missing transactional table %q", table)
		}
	}
}
