package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var productColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type productInput struct {
	SKU          string `json:"sku"`
	Barcode      string `json:"barcode"`
	ImageID      *int64 `json:"imageId"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	CategoryID   int64  `json:"categoryId"`
	Price        int    `json:"price"`
	Cost         int    `json:"cost"`
	Stock        int    `json:"stock"`
	ReorderLevel int    `json:"reorderLevel"`
	Art          string `json:"art"`
	Color        string `json:"color"`
}

func (p *productInput) validate() bool {
	p.SKU = strings.TrimSpace(p.SKU)
	p.Barcode = strings.TrimSpace(p.Barcode)
	p.Name = strings.TrimSpace(p.Name)
	p.Category = strings.TrimSpace(p.Category)
	if p.Art == "" {
		p.Art = "box"
	}
	if p.Color == "" {
		p.Color = "#eeeeee"
	}
	artOK := false
	for _, art := range []string{"box", "bottle", "none", "coffee", "iced", "matcha", "tea", "croissant", "cookie", "toast", "sandwich", "cake", "roll"} {
		if p.Art == art {
			artOK = true
		}
	}
	return len(p.SKU) <= 64 && len(p.Barcode) <= 64 && !strings.ContainsAny(p.SKU+p.Barcode, "\r\n\t") && (p.ImageID == nil || *p.ImageID > 0) && p.Name != "" && len(p.Name) <= 200 && (p.CategoryID > 0 || (p.Category != "" && len(p.Category) <= 100)) && p.CategoryID >= 0 &&
		p.Price >= 0 && p.Price <= 10000000 && p.Cost >= 0 && p.Cost <= 10000000 &&
		p.Stock >= 0 && p.Stock <= 1000000 && p.ReorderLevel >= 0 && p.ReorderLevel <= 1000000 &&
		artOK && productColor.MatchString(p.Color)
}

func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) {
	var p productInput
	if !decode(w, r, &p) {
		return
	}
	if !p.validate() {
		problem(w, 400, "Enter a name, category, valid amounts, whole stock quantities, and a valid appearance")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not save product")
		return
	}
	defer tx.Rollback()
	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	// Lock the category until the product is saved so renames and deletion cannot race.
	if p.CategoryID > 0 {
		err = tx.QueryRowContext(r.Context(), `SELECT id,name FROM categories WHERE id=$1 FOR SHARE`, p.CategoryID).Scan(&p.CategoryID, &p.Category)
	} else {
		err = tx.QueryRowContext(r.Context(), `SELECT id,name FROM categories WHERE lower(name)=lower($1) FOR SHARE`, p.Category).Scan(&p.CategoryID, &p.Category)
	}
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 400, "Choose an existing category or create one in Products")
		return
	}
	if err != nil {
		problem(w, 500, "Could not read category")
		return
	}
	var id int64
	if r.Method == http.MethodPost {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO products(name,category,price,cost,stock,reorder_level,art,color,category_id,sku,barcode,image_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
			p.Name, p.Category, p.Price, p.Cost, p.Stock, p.ReorderLevel, p.Art, p.Color, p.CategoryID, p.SKU, p.Barcode, p.ImageID).Scan(&id)
		if err == nil && p.Stock > 0 {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO inventory_movements(product_id,kind,quantity,note,created_by) VALUES($1,'opening',$2,'Opening stock',$3)`, id, p.Stock, principal(r).ID)
		}
	} else {
		id, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			problem(w, 400, "Invalid product")
			return
		}
		// Stock changes go through inventory adjustments, never a stale edit form.
		if p.Stock != 0 {
			problem(w, 400, "Use inventory adjustments to change stock")
			return
		}
		err = tx.QueryRowContext(r.Context(), `UPDATE products SET name=$1,category=$2,price=$3,cost=$4,reorder_level=$5,art=$6,color=$7,category_id=$9,sku=$10,barcode=$11,image_id=$12 WHERE id=$8 AND active RETURNING id`,
			p.Name, p.Category, p.Price, p.Cost, p.ReorderLevel, p.Art, p.Color, id, p.CategoryID, p.SKU, p.Barcode, p.ImageID).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Product not found")
		return
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		productWriteError(w, err)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	respond(w, status, map[string]any{"id": id})
}

func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		problem(w, 400, "Invalid product")
		return
	}
	result, err := s.auditedExec(r, `UPDATE products SET active=false WHERE id=$1 AND active`, id)
	if err != nil {
		problem(w, 500, "Could not delete product")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		problem(w, 500, "Could not delete product")
		return
	}
	if count == 0 {
		problem(w, 404, "Product not found")
		return
	}
	respond(w, 200, map[string]any{"deleted": count})
}

func (s *Server) deleteAllProducts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Confirmation != "DELETE ALL PRODUCTS" {
		problem(w, 400, "Type DELETE ALL PRODUCTS to confirm")
		return
	}
	result, err := s.auditedExec(r, `UPDATE products SET active=false WHERE active`)
	if err != nil {
		problem(w, 500, "Could not delete products")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		problem(w, 500, "Could not delete products")
		return
	}
	respond(w, 200, map[string]any{"deleted": count})
}
