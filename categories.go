package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type Category struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	ProductCount int    `json:"productCount"`
}

func categoryError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		problem(w, 409, "A category with this name already exists")
		return
	}
	problem(w, 500, "Could not save category")
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT c.id,c.name,count(p.id) FROM categories c LEFT JOIN products p ON p.category_id=c.id AND p.active GROUP BY c.id ORDER BY lower(c.name),c.id`)
	if err != nil {
		problem(w, 500, "Could not load categories")
		return
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err = rows.Scan(&c.ID, &c.Name, &c.ProductCount); err != nil {
			problem(w, 500, "Could not read categories")
			return
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read categories")
		return
	}
	respond(w, 200, out)
}

func (s *Server) saveCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 100 {
		problem(w, 400, "Enter a category name of 1–100 characters")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		categoryError(w, err)
		return
	}
	defer tx.Rollback()
	var id int64
	status := http.StatusCreated
	if r.Method == http.MethodPost {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO categories(name) VALUES($1) RETURNING id`, req.Name).Scan(&id)
	} else {
		status = http.StatusOK
		id, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			problem(w, 400, "Invalid category")
			return
		}
		err = tx.QueryRowContext(r.Context(), `UPDATE categories SET name=$1 WHERE id=$2 RETURNING id`, req.Name, id).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Category not found")
			return
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE products SET category=$1 WHERE category_id=$2`, req.Name, id)
		}
	}
	if err != nil {
		categoryError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		categoryError(w, err)
		return
	}
	respond(w, status, map[string]any{"id": id, "name": req.Name})
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		problem(w, 400, "Invalid category")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not delete category")
		return
	}
	defer tx.Rollback()
	var lockedID int64
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM categories WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Category not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not delete category")
		return
	}
	var used bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM products WHERE category_id=$1 AND active)`, id).Scan(&used); err != nil {
		problem(w, 500, "Could not check category")
		return
	}
	if used {
		problem(w, 409, "Move or delete the active products in this category before deleting it")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM categories WHERE id=$1`, id); err != nil {
		problem(w, 500, "Could not delete category")
		return
	}
	if err = tx.Commit(); err != nil {
		problem(w, 500, "Could not delete category")
		return
	}
	respond(w, 200, map[string]bool{"deleted": true})
}
