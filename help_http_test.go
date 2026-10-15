package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func helpRequest(role, slug string) *http.Request {
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/help/"+slug,
		nil,
	)
	req.SetPathValue("slug", slug)

	user := &User{Role: role}

	return req.WithContext(
		context.WithValue(
			req.Context(),
			userContextKey,
			user,
		),
	)
}

func TestHelpArticleHTTPPermissions(t *testing.T) {
	tests := []struct {
		name string
		role string
		slug string
		want int
	}{
		{
			"cashier operating guide",
			"cashier",
			"CASHIER_GUIDE",
			http.StatusOK,
		},
		{
			"cashier denied inventory",
			"cashier",
			"INVENTORY_AND_PURCHASES",
			http.StatusForbidden,
		},
		{
			"cashier denied deployment",
			"cashier",
			"DEPLOYMENT",
			http.StatusForbidden,
		},
		{
			"admin inventory guide",
			"admin",
			"INVENTORY_AND_PURCHASES",
			http.StatusOK,
		},
		{
			"admin denied security guide",
			"admin",
			"SECURITY_AND_BACKUP",
			http.StatusForbidden,
		},
		{
			"superadmin deployment guide",
			"superadmin",
			"DEPLOYMENT",
			http.StatusOK,
		},
		{
			"unknown guide",
			"superadmin",
			"NOT_A_GUIDE",
			http.StatusNotFound,
		},
		{
			"path traversal",
			"superadmin",
			"..",
			http.StatusNotFound,
		},
	}

	server := &Server{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := helpRequest(tt.role, tt.slug)
			rec := httptest.NewRecorder()

			server.helpArticle(rec, req)

			if rec.Code != tt.want {
				t.Fatalf(
					"status = %d, want %d; body=%s",
					rec.Code,
					tt.want,
					rec.Body.String(),
				)
			}

			if rec.Code == http.StatusOK {
				var article struct {
					Slug    string `json:"slug"`
					Content string `json:"content"`
				}

				if err := json.Unmarshal(
					rec.Body.Bytes(),
					&article,
				); err != nil {
					t.Fatal(err)
				}

				if article.Slug != tt.slug {
					t.Errorf(
						"slug = %q, want %q",
						article.Slug,
						tt.slug,
					)
				}

				if article.Content == "" {
					t.Error("article content is empty")
				}
			}
		})
	}
}

func TestHelpIndexRoleFiltering(t *testing.T) {
	server := &Server{}

	for _, role := range []string{
		"cashier",
		"admin",
		"superadmin",
	} {
		t.Run(role, func(t *testing.T) {
			req := helpRequest(role, "unused")
			rec := httptest.NewRecorder()

			server.helpIndex(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("unexpected status: %d", rec.Code)
			}

			var docs []helpDocument

			if err := json.Unmarshal(
				rec.Body.Bytes(),
				&docs,
			); err != nil {
				t.Fatal(err)
			}

			for _, doc := range docs {
				for _, registered := range helpDocuments {
					if registered.Slug == doc.Slug &&
						!canReadHelp(role, registered.MinimumRole) {
						t.Errorf(
							"%s can see restricted guide %s",
							role,
							doc.Slug,
						)
					}
				}
			}

			if role == "superadmin" &&
				len(docs) != len(helpDocuments) {
				t.Errorf(
					"superadmin sees %d guides; want %d",
					len(docs),
					len(helpDocuments),
				)
			}
		})
	}
}
