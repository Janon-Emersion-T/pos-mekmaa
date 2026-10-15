package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

type helpDocument struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	MinimumRole string `json:"-"`
}

var helpDocuments = []helpDocument{
	{"README", "Documentation overview", "Find the right guide for your task.", "Getting started", "cashier"},
	{"CASHIER_GUIDE", "Cashier guide", "Daily POS and cashier operations.", "Daily operations", "cashier"},
	{"SALES_AND_RETURNS", "Sales and returns", "Checkout, receipts, and refunds.", "Daily operations", "cashier"},
	{"CUSTOMER_CREDIT_AND_DEBTS", "Customer credit and debts", "Customer accounts, credit, and collections.", "Daily operations", "cashier"},
	{"CASH_SESSIONS_AND_TRANSFERS", "Cash sessions and transfers", "Opening, closing, and transferring cash.", "Daily operations", "cashier"},
	{"INVENTORY_AND_PURCHASES", "Inventory and purchases", "Stock management and supplier purchases.", "Management", "admin"},
	{"SUPERADMIN_GUIDE", "Superadmin guide", "Privileged management and system controls.", "Administration", "superadmin"},
	{"FINANCE_AND_CAPITAL", "Finance and capital", "Financial accounts, funding, and capital.", "Administration", "superadmin"},
	{"OPTIMIZED_DEBTS", "Optimized Debts", "Restricted feature documentation; disabled by default.", "Administration", "superadmin"},
	{"INSTALLATION", "Installation", "Local installation and configuration.", "Technical", "superadmin"},
	{"DEPLOYMENT", "Deployment", "Production deployment and release process.", "Technical", "superadmin"},
	{"SECURITY_AND_BACKUP", "Security and backup", "Security controls, backups, and recovery.", "Technical", "superadmin"},
	{"TROUBLESHOOTING", "Troubleshooting", "Diagnose and resolve application problems.", "Technical", "superadmin"},
}

func canReadHelp(role, minimumRole string) bool {
	switch minimumRole {
	case "cashier":
		return role == "cashier" || role == "admin" || role == "superadmin"
	case "admin":
		return role == "admin" || role == "superadmin"
	case "superadmin":
		return role == "superadmin"
	}
	return false
}

func (s *Server) helpIndex(w http.ResponseWriter, r *http.Request) {
	u := principal(r)
	if u == nil {
		problem(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	visible := make([]helpDocument, 0, len(helpDocuments))
	for _, doc := range helpDocuments {
		if canReadHelp(u.Role, doc.MinimumRole) {
			visible = append(visible, doc)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(visible)
}

func (s *Server) helpArticle(w http.ResponseWriter, r *http.Request) {
	u := principal(r)
	if u == nil {
		problem(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	slug := r.PathValue("slug")

	// Only explicitly registered documentation can be accessed.
	// Never accept arbitrary file paths.
	if slug == "" || strings.ContainsAny(slug, "/\\.") {
		problem(w, http.StatusNotFound, "Documentation not found")
		return
	}

	for _, doc := range helpDocuments {
		if doc.Slug != slug {
			continue
		}

		if !canReadHelp(u.Role, doc.MinimumRole) {
			problem(w, http.StatusForbidden, "Documentation access denied")
			return
		}

		content, err := fs.ReadFile(assets, "docs/"+doc.Slug+".md")
		if err != nil {
			problem(w, http.StatusInternalServerError, "Documentation unavailable")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"slug":     doc.Slug,
			"title":    doc.Title,
			"category": doc.Category,
			"content":  string(content),
		})
		return
	}

	problem(w, http.StatusNotFound, "Documentation not found")
}
