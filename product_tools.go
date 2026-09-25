package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func productWriteError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		problem(w, 409, "SKU or barcode already belongs to another product")
		return
	}
	if errors.As(err, &pg) && pg.Code == "23503" {
		problem(w, 400, "Category or image no longer exists")
		return
	}
	problem(w, 500, "Could not save product")
}
func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		problem(w, 400, "Choose a JPEG or PNG image under 2 MB")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width > 4096 || config.Height > 4096 || config.Width < 1 || config.Height < 1 {
		problem(w, 400, "Choose a valid JPEG or PNG up to 4096 × 4096 pixels")
		return
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		problem(w, 400, "Image is corrupt")
		return
	}
	mime := "image/" + format
	var id int64
	if err = s.db.QueryRowContext(r.Context(), `INSERT INTO product_images(mime_type,data) VALUES($1,$2) RETURNING id`, mime, data).Scan(&id); err != nil {
		problem(w, 500, "Could not save image")
		return
	}
	respond(w, 201, map[string]any{"id": id})
}
func (s *Server) productImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var mime string
	var data []byte
	if err = s.db.QueryRowContext(r.Context(), `SELECT mime_type,data FROM product_images WHERE id=$1`, id).Scan(&mime, &data); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(data)
}
func csvSafe(s string) string {
	if strings.ContainsAny(strings.TrimLeft(s, " "), "\t\r\n") || strings.HasPrefix(strings.TrimLeft(s, " "), "=") || strings.HasPrefix(strings.TrimLeft(s, " "), "+") || strings.HasPrefix(strings.TrimLeft(s, " "), "-") || strings.HasPrefix(strings.TrimLeft(s, " "), "@") {
		return "'" + s
	}
	return s
}
func (s *Server) exportProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT sku,barcode,name,category,price,cost,stock,reorder_level FROM products WHERE active ORDER BY id`)
	if err != nil {
		problem(w, 500, "Could not export products")
		return
	}
	defer rows.Close()
	records := [][]string{{"sku", "barcode", "name", "category", "price", "cost", "stock", "reorder_level"}}
	for rows.Next() {
		var sku, barcode, name, category string
		var price, cost, stock, reorder int
		if rows.Scan(&sku, &barcode, &name, &category, &price, &cost, &stock, &reorder) != nil {
			problem(w, 500, "Could not read products")
			return
		}
		records = append(records, []string{csvSafe(sku), csvSafe(barcode), csvSafe(name), csvSafe(category), decimalAmount(price), decimalAmount(cost), strconv.Itoa(stock), strconv.Itoa(reorder)})
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read products")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="products.csv"`)
	c := csv.NewWriter(w)
	c.WriteAll(records)
}

var amountPattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

func parseAmount(s string) (int, error) {
	if !amountPattern.MatchString(s) {
		return 0, errors.New("use a nonnegative amount with up to 2 decimal places")
	}
	parts := strings.Split(s, ".")
	whole, err := strconv.Atoi(parts[0])
	if err != nil || whole > 100000 {
		return 0, errors.New("amount too large")
	}
	fraction := 0
	if len(parts) == 2 {
		part := parts[1] + "0"
		fraction, _ = strconv.Atoi(part[:2])
	}
	value := whole*100 + fraction
	if value > 10000000 {
		return 0, errors.New("amount too large")
	}
	return value, nil
}
func (s *Server) importProducts(w http.ResponseWriter, r *http.Request) {
	reader := csv.NewReader(http.MaxBytesReader(w, r.Body, 1<<20))
	header, err := reader.Read()
	expected := []string{"sku", "barcode", "name", "category", "price", "cost", "stock", "reorder_level"}
	if err != nil || len(header) != len(expected) {
		problem(w, 400, "Use the product export CSV columns")
		return
	}
	for i, v := range expected {
		if strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff")) != v {
			problem(w, 400, "CSV columns must be: "+strings.Join(expected, ","))
			return
		}
	}
	var inputs []productInput
	for row := 2; ; row++ {
		fields, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil || len(inputs) >= 1000 {
			problem(w, 400, fmt.Sprintf("Invalid CSV at row %d; maximum 1000 products and 1 MB", row))
			return
		}
		p := productInput{SKU: strings.TrimSpace(fields[0]), Barcode: strings.TrimSpace(fields[1]), Name: fields[2], Category: fields[3]}
		p.Price, e = parseAmount(strings.TrimSpace(fields[4]))
		if e == nil {
			p.Cost, e = parseAmount(strings.TrimSpace(fields[5]))
		}
		if e == nil {
			p.Stock, e = strconv.Atoi(strings.TrimSpace(fields[6]))
		}
		if e == nil {
			p.ReorderLevel, e = strconv.Atoi(strings.TrimSpace(fields[7]))
		}
		if e != nil || !p.validate() {
			problem(w, 400, fmt.Sprintf("Invalid product at CSV row %d", row))
			return
		}
		inputs = append(inputs, p)
	}
	if len(inputs) == 0 {
		problem(w, 400, "CSV contains no products")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not import products")
		return
	}
	defer tx.Rollback()
	if auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	for i, p := range inputs {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO categories(name) VALUES($1) ON CONFLICT DO NOTHING`, p.Category); err != nil {
			problem(w, 400, fmt.Sprintf("Invalid category at row %d", i+2))
			return
		}
		if err = tx.QueryRowContext(r.Context(), `SELECT id,name FROM categories WHERE lower(name)=lower($1) FOR SHARE`, p.Category).Scan(&p.CategoryID, &p.Category); err != nil {
			problem(w, 400, "Could not resolve category")
			return
		}
		var id int64
		err = tx.QueryRowContext(r.Context(), `INSERT INTO products(name,category,category_id,price,cost,stock,reorder_level,art,color,sku,barcode) VALUES($1,$2,$3,$4,$5,$6,$7,'box','#eeeeee',$8,$9) RETURNING id`, p.Name, p.Category, p.CategoryID, p.Price, p.Cost, p.Stock, p.ReorderLevel, p.SKU, p.Barcode).Scan(&id)
		if err != nil {
			problem(w, 409, fmt.Sprintf("Row %d conflicts with an existing SKU or barcode. No products were imported.", i+2))
			return
		}
		if p.Stock > 0 {
			if _, err = tx.ExecContext(r.Context(), `INSERT INTO inventory_movements(product_id,kind,quantity,note,created_by) VALUES($1,'opening',$2,'CSV import',$3)`, id, p.Stock, principal(r).ID); err != nil {
				problem(w, 500, "Could not record imported stock")
				return
			}
		}
	}
	if tx.Commit() != nil {
		problem(w, 500, "Could not finish import")
		return
	}
	respond(w, 201, map[string]any{"imported": len(inputs)})
}

var _ = sql.ErrNoRows
