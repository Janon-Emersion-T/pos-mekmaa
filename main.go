package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web/* migrations/*.sql
var assets embed.FS

type Product struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Price        int    `json:"price"`
	Cost         int    `json:"cost"`
	Stock        int    `json:"stock"`
	ReorderLevel int    `json:"reorderLevel"`
	Art          string `json:"art"`
	Color        string `json:"color"`
}
type Line struct {
	ProductID int `json:"productId"`
	Quantity  int `json:"quantity"`
	UnitCost  int `json:"unitCost,omitempty"`
}
type Sale struct {
	ID        int64     `json:"id"`
	Created   time.Time `json:"created"`
	Total     int       `json:"total"`
	Payment   string    `json:"payment"`
	Items     string    `json:"items"`
	SessionID int64     `json:"sessionId"`
}
type Session struct {
	ID           int64      `json:"id"`
	OpenedAt     time.Time  `json:"openedAt"`
	ClosedAt     *time.Time `json:"closedAt"`
	OpeningCash  int        `json:"openingCash"`
	ClosingCash  *int       `json:"closingCash"`
	ExpectedCash int        `json:"expectedCash"`
	Status       string     `json:"status"`
}
type Server struct{ db *sql.DB }

func openDB(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	m, err := assets.ReadFile("migrations/001_init.sql")
	if err == nil {
		_, err = db.ExecContext(ctx, string(m))
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("run migration: %w", err)
	}
	if err = ensureSuperadmin(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create superadmin: %w", err)
	}
	return db, nil
}
func ensureSuperadmin(ctx context.Context, db *sql.DB) error {
	const email = "janon@lkprofessionals.com"
	const passwordHash = "$2a$10$kBHVZlCmocrXwUFAy7B3leIkgjZkXJNmFSStd.vbtxZHPGE0FcZVG"
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := db.ExecContext(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,$2,'superadmin')`, email, passwordHash)
	return err
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		problem(w, 400, "Invalid request")
		return false
	}
	return true
}
func (s *Server) activeSession(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (Session, error) {
	var x Session
	err := q.QueryRowContext(ctx, `SELECT id,opened_at,closed_at,opening_cash,closing_cash,status,opening_cash+COALESCE((SELECT SUM(total) FROM sales WHERE session_id=register_sessions.id AND payment='cash'),0)+COALESCE((SELECT SUM(CASE WHEN direction='in' THEN amount ELSE -amount END) FROM petty_cash_entries WHERE session_id=register_sessions.id),0) FROM register_sessions WHERE status='open' ORDER BY id DESC LIMIT 1`).Scan(&x.ID, &x.OpenedAt, &x.ClosedAt, &x.OpeningCash, &x.ClosingCash, &x.Status, &x.ExpectedCash)
	return x, err
}
func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,name,category,price,cost,stock,reorder_level,art,color FROM products WHERE active ORDER BY id`)
	if err != nil {
		problem(w, 500, "Could not load products")
		return
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		var p Product
		if rows.Scan(&p.ID, &p.Name, &p.Category, &p.Price, &p.Cost, &p.Stock, &p.ReorderLevel, &p.Art, &p.Color) != nil {
			problem(w, 500, "Could not read products")
			return
		}
		out = append(out, p)
	}
	respond(w, 200, out)
}
func (s *Server) sales(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT s.id,s.created_at,s.total,s.payment,string_agg(si.quantity||' × '||si.product_name,', ' ORDER BY si.id),s.session_id FROM sales s JOIN sale_items si ON si.sale_id=s.id GROUP BY s.id ORDER BY s.id DESC LIMIT 100`)
	if err != nil {
		problem(w, 500, "Could not load sales")
		return
	}
	defer rows.Close()
	out := []Sale{}
	for rows.Next() {
		var x Sale
		if rows.Scan(&x.ID, &x.Created, &x.Total, &x.Payment, &x.Items, &x.SessionID) != nil {
			problem(w, 500, "Could not read sales")
			return
		}
		out = append(out, x)
	}
	respond(w, 200, out)
}
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items   []Line `json:"items"`
		Payment string `json:"payment"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 100 || (req.Payment != "cash" && req.Payment != "card") {
		problem(w, 400, "Invalid order")
		return
	}
	combined := make(map[int]int)
	for _, line := range req.Items {
		combined[line.ProductID] += line.Quantity
	}
	req.Items = req.Items[:0]
	for productID, quantity := range combined {
		req.Items = append(req.Items, Line{ProductID: productID, Quantity: quantity})
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start checkout")
		return
	}
	defer tx.Rollback()
	session, err := s.activeSession(r.Context(), tx)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 409, "Open a register session before making a sale")
		return
	}
	if err != nil {
		problem(w, 500, "Could not read session")
		return
	}
	total := 0
	type item struct {
		Line
		Name  string
		Price int
	}
	items := []item{}
	for _, line := range req.Items {
		if line.Quantity < 1 || line.Quantity > 100 {
			problem(w, 400, "Invalid quantity")
			return
		}
		var x item
		x.Line = line
		var stock int
		if err = tx.QueryRowContext(r.Context(), `SELECT name,price,stock FROM products WHERE id=$1 AND active FOR UPDATE`, line.ProductID).Scan(&x.Name, &x.Price, &stock); err != nil {
			problem(w, 400, "Product not found")
			return
		}
		if stock < line.Quantity {
			problem(w, 409, x.Name+" has insufficient stock")
			return
		}
		total += x.Price * line.Quantity
		items = append(items, x)
	}
	var id int64
	created := time.Now()
	if err = tx.QueryRowContext(r.Context(), `INSERT INTO sales(session_id,total,payment) VALUES($1,$2,$3) RETURNING id,created_at`, session.ID, total, req.Payment).Scan(&id, &created); err != nil {
		problem(w, 500, "Could not save sale")
		return
	}
	names := []string{}
	for _, x := range items {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO sale_items(sale_id,product_id,product_name,quantity,unit_price) VALUES($1,$2,$3,$4,$5)`, id, x.ProductID, x.Name, x.Quantity, x.Price)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE products SET stock=stock-$1 WHERE id=$2`, x.Quantity, x.ProductID)
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO inventory_movements(product_id,kind,quantity,reference_type,reference_id,note) VALUES($1,'sale',$2,'sale',$3,'POS sale')`, x.ProductID, -x.Quantity, id)
		}
		if err != nil {
			problem(w, 500, "Could not update inventory")
			return
		}
		names = append(names, fmt.Sprintf("%d × %s", x.Quantity, x.Name))
	}
	if tx.Commit() != nil {
		problem(w, 500, "Could not complete sale")
		return
	}
	respond(w, 201, Sale{id, created, total, req.Payment, strings.Join(names, ", "), session.ID})
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		x, err := s.activeSession(r.Context(), s.db)
		if errors.Is(err, sql.ErrNoRows) {
			respond(w, 200, nil)
			return
		}
		if err != nil {
			problem(w, 500, "Could not load session")
			return
		}
		respond(w, 200, x)
		return
	}
	var req struct {
		OpeningCash int `json:"openingCash"`
		ClosingCash int `json:"closingCash"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.OpeningCash < 0 || req.ClosingCash < 0 {
		problem(w, 400, "Cash cannot be negative")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/open") {
		var id int64
		err := s.db.QueryRowContext(r.Context(), `INSERT INTO register_sessions(opening_cash) SELECT $1 WHERE NOT EXISTS(SELECT 1 FROM register_sessions WHERE status='open') RETURNING id`, req.OpeningCash).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 409, "A register session is already open")
			return
		}
		if err != nil {
			problem(w, 500, "Could not open session")
			return
		}
		respond(w, 201, map[string]any{"id": id})
		return
	}
	x, err := s.activeSession(r.Context(), s.db)
	if err != nil {
		problem(w, 409, "No register session is open")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE register_sessions SET status='closed',closed_at=now(),closing_cash=$1 WHERE id=$2`, req.ClosingCash, x.ID)
	if err != nil {
		problem(w, 500, "Could not close session")
		return
	}
	respond(w, 200, map[string]any{"expectedCash": x.ExpectedCash, "closingCash": req.ClosingCash, "difference": req.ClosingCash - x.ExpectedCash})
}
func (s *Server) pettyCash(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.db.QueryContext(r.Context(), `SELECT id,created_at,direction,amount,category,note,session_id FROM petty_cash_entries ORDER BY id DESC LIMIT 100`)
		if err != nil {
			problem(w, 500, "Could not load petty cash")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, amount, session int64
			var created time.Time
			var direction, category, note string
			if rows.Scan(&id, &created, &direction, &amount, &category, &note, &session) != nil {
				problem(w, 500, "Could not read petty cash")
				return
			}
			out = append(out, map[string]any{"id": id, "created": created, "direction": direction, "amount": amount, "category": category, "note": note, "sessionId": session})
		}
		respond(w, 200, out)
		return
	}
	var req struct {
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
		Category  string `json:"category"`
		Note      string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	if (req.Direction != "in" && req.Direction != "out") || req.Amount <= 0 || strings.TrimSpace(req.Category) == "" {
		problem(w, 400, "Direction, amount and category are required")
		return
	}
	x, err := s.activeSession(r.Context(), s.db)
	if err != nil {
		problem(w, 409, "Open a register session first")
		return
	}
	var id int64
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO petty_cash_entries(session_id,direction,amount,category,note) VALUES($1,$2,$3,$4,$5) RETURNING id`, x.ID, req.Direction, req.Amount, req.Category, req.Note).Scan(&id)
	if err != nil {
		problem(w, 500, "Could not save entry")
		return
	}
	respond(w, 201, map[string]any{"id": id})
}
func (s *Server) purchases(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.db.QueryContext(r.Context(), `SELECT p.id,p.created_at,p.supplier,p.invoice_number,p.total,p.status,COALESCE(string_agg(pi.quantity||' × '||pr.name,', ' ORDER BY pi.id),'') FROM purchases p LEFT JOIN purchase_items pi ON pi.purchase_id=p.id LEFT JOIN products pr ON pr.id=pi.product_id GROUP BY p.id ORDER BY p.id DESC LIMIT 100`)
		if err != nil {
			problem(w, 500, "Could not load purchases")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, total int64
			var created time.Time
			var supplier, invoice, status, items string
			if rows.Scan(&id, &created, &supplier, &invoice, &total, &status, &items) != nil {
				problem(w, 500, "Could not read purchases")
				return
			}
			out = append(out, map[string]any{"id": id, "created": created, "supplier": supplier, "invoiceNumber": invoice, "total": total, "status": status, "items": items})
		}
		respond(w, 200, out)
		return
	}
	var req struct {
		Supplier      string `json:"supplier"`
		InvoiceNumber string `json:"invoiceNumber"`
		Items         []Line `json:"items"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Supplier) == "" || len(req.Items) == 0 {
		problem(w, 400, "Supplier and items are required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start purchase")
		return
	}
	defer tx.Rollback()
	total := 0
	for _, x := range req.Items {
		if x.ProductID < 1 || x.Quantity < 1 || x.UnitCost < 0 {
			problem(w, 400, "Invalid purchase item")
			return
		}
		total += x.Quantity * x.UnitCost
	}
	var id int64
	if err = tx.QueryRowContext(r.Context(), `INSERT INTO purchases(supplier,invoice_number,total,status) VALUES($1,$2,$3,'received') RETURNING id`, req.Supplier, req.InvoiceNumber, total).Scan(&id); err != nil {
		problem(w, 500, "Could not save purchase")
		return
	}
	for _, x := range req.Items {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO purchase_items(purchase_id,product_id,quantity,unit_cost) VALUES($1,$2,$3,$4)`, id, x.ProductID, x.Quantity, x.UnitCost)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE products SET stock=stock+$1,cost=$2 WHERE id=$3`, x.Quantity, x.UnitCost, x.ProductID)
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO inventory_movements(product_id,kind,quantity,reference_type,reference_id,note) VALUES($1,'purchase',$2,'purchase',$3,'Stock received')`, x.ProductID, x.Quantity, id)
		}
		if err != nil {
			problem(w, 400, "Invalid purchase product")
			return
		}
	}
	if tx.Commit() != nil {
		problem(w, 500, "Could not receive purchase")
		return
	}
	respond(w, 201, map[string]any{"id": id, "total": total})
}
func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.db.QueryContext(r.Context(), `SELECT m.id,m.created_at,m.product_id,p.name,m.kind,m.quantity,m.note FROM inventory_movements m JOIN products p ON p.id=m.product_id ORDER BY m.id DESC LIMIT 200`)
		if err != nil {
			problem(w, 500, "Could not load movements")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, pid, qty int64
			var created time.Time
			var name, kind, note string
			if rows.Scan(&id, &created, &pid, &name, &kind, &qty, &note) != nil {
				problem(w, 500, "Could not read movements")
				return
			}
			out = append(out, map[string]any{"id": id, "created": created, "productId": pid, "product": name, "kind": kind, "quantity": qty, "note": note})
		}
		respond(w, 200, out)
		return
	}
	var req struct {
		ProductID int    `json:"productId"`
		Quantity  int    `json:"quantity"`
		Note      string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ProductID < 1 || req.Quantity == 0 || strings.TrimSpace(req.Note) == "" {
		problem(w, 400, "Product, quantity and note are required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not adjust inventory")
		return
	}
	defer tx.Rollback()
	var stock int
	if err = tx.QueryRowContext(r.Context(), `SELECT stock FROM products WHERE id=$1 FOR UPDATE`, req.ProductID).Scan(&stock); err != nil {
		problem(w, 404, "Product not found")
		return
	}
	if stock+req.Quantity < 0 {
		problem(w, 409, "Adjustment would make stock negative")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE products SET stock=stock+$1 WHERE id=$2`, req.Quantity, req.ProductID)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO inventory_movements(product_id,kind,quantity,note) VALUES($1,'adjustment',$2,$3)`, req.ProductID, req.Quantity, req.Note)
	}
	if err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not adjust inventory")
		return
	}
	respond(w, 201, map[string]bool{"ok": true})
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	var id int64
	var hash string
	var active bool
	err := s.db.QueryRowContext(r.Context(), `SELECT id,password_hash,active FROM users WHERE lower(email)=lower($1)`, strings.TrimSpace(req.Email)).Scan(&id, &hash, &active)
	if err != nil || !active || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		problem(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		problem(w, 500, "Could not create session")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if _, err = s.db.ExecContext(r.Context(), `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '12 hours')`, id, tokenHash(token)); err != nil {
		problem(w, 500, "Could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "counter_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 12 * 60 * 60})
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("counter_session"); err == nil {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE token_hash=$1`, tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "counter_session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: -1})
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie("counter_session")
	if err != nil || len(c.Value) < 32 {
		return false
	}
	var ok bool
	err = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM auth_sessions a JOIN users u ON u.id=a.user_id WHERE a.token_hash=$1 AND a.expires_at>now() AND u.active)`, tokenHash(c.Value)).Scan(&ok)
	return err == nil && ok
}
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticated(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				problem(w, http.StatusUnauthorized, "Authentication required")
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) routes() http.Handler {
	private := http.NewServeMux()
	private.HandleFunc("GET /api/products", s.products)
	private.HandleFunc("GET /api/sales", s.sales)
	private.HandleFunc("POST /api/checkout", s.checkout)
	private.HandleFunc("GET /api/session", s.sessions)
	private.HandleFunc("POST /api/session/open", s.sessions)
	private.HandleFunc("POST /api/session/close", s.sessions)
	private.HandleFunc("GET /api/petty-cash", s.pettyCash)
	private.HandleFunc("POST /api/petty-cash", s.pettyCash)
	private.HandleFunc("GET /api/purchases", s.purchases)
	private.HandleFunc("POST /api/purchases", s.purchases)
	private.HandleFunc("GET /api/inventory/movements", s.inventory)
	private.HandleFunc("POST /api/inventory/adjustments", s.inventory)
	sub, _ := fs.Sub(assets, "web")
	private.Handle("/", http.FileServer(http.FS(sub)))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /styles.css", func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, sub, "styles.css") })
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if s.authenticated(r) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.ServeFileFS(w, r, sub, "login.html")
	})
	mux.Handle("/", s.requireAuth(private))
	return mux
}
func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://counter:counter@localhost:5432/counter?sslmode=disable"
	}
	db, err := openDB(context.Background(), url)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8886"
	}
	display := addr
	if strings.HasPrefix(display, ":") {
		display = "localhost" + display
	}
	log.Printf("Counter POS running at http://%s", display)
	log.Fatal((&http.Server{Addr: addr, Handler: (&Server{db}).routes(), ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
