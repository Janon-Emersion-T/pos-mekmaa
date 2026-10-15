package main

import (
	"io/fs"
	"testing"
)

func TestHelpPermissions(t *testing.T) {
	tests := []struct {
		role string
		slug string
		want bool
	}{
		{"cashier", "cashier", true},
		{"cashier", "admin", false},
		{"cashier", "superadmin", false},
		{"admin", "cashier", true},
		{"admin", "admin", true},
		{"admin", "superadmin", false},
		{"superadmin", "superadmin", true},
		{"unknown", "cashier", false},
	}

	for _, tt := range tests {
		if got := canReadHelp(tt.role, tt.slug); got != tt.want {
			t.Errorf(
				"canReadHelp(%q, %q) = %v; want %v",
				tt.role, tt.slug, got, tt.want,
			)
		}
	}
}

func TestEmbeddedHelpDocuments(t *testing.T) {
	for _, doc := range helpDocuments {
		content, err := fs.ReadFile(assets, "docs/"+doc.Slug+".md")
		if err != nil {
			t.Errorf("missing embedded guide %s: %v", doc.Slug, err)
			continue
		}
		if len(content) == 0 {
			t.Errorf("empty embedded guide: %s", doc.Slug)
		}
	}
}
