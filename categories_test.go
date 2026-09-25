package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestCategoryMigrationPreservesProducts(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("category_test_%d", time.Now().UnixNano())
	if _, err = db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err = db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	initial, err := assets.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO products(name,category,price,active) VALUES('A',' Coffee ',100,true),('B','coffee',200,true),('Archived','Tea',300,false)`); err != nil {
		t.Fatal(err)
	}
	if err = runMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var categories, products, linked int
	if err = db.QueryRow("SELECT count(*) FROM categories").Scan(&categories); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*),count(category_id) FROM products").Scan(&products, &linked); err != nil {
		t.Fatal(err)
	}
	if categories != 2 || products != 3 || linked != 3 {
		t.Fatalf("categories=%d products=%d linked=%d", categories, products, linked)
	}
	var coffeeGroups int
	if err = db.QueryRow(`SELECT count(DISTINCT category_id) FROM products WHERE name IN ('A','B')`).Scan(&coffeeGroups); err != nil || coffeeGroups != 1 {
		t.Fatalf("duplicate categories not merged: %d %v", coffeeGroups, err)
	}
	if _, err = db.Exec(`DELETE FROM categories WHERE name='Tea'`); err != nil {
		t.Fatal(err)
	}
	var archivedName string
	var archivedCategory sql.NullInt64
	if err = db.QueryRow(`SELECT category,category_id FROM products WHERE name='Archived'`).Scan(&archivedName, &archivedCategory); err != nil {
		t.Fatal(err)
	}
	if archivedName != "Tea" || archivedCategory.Valid {
		t.Fatalf("archived category changed: %s %+v", archivedName, archivedCategory)
	}
}
