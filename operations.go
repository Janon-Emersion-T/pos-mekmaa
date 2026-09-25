package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
)

var requestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,100}$`)

func auditActor(ctx context.Context, tx *sql.Tx, u *User) error {
	if u == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `SELECT set_config('app.actor_id',$1,true),set_config('app.actor_email',$2,true)`, fmtID(u.ID), u.Email)
	return err
}
func (s *Server) auditedExec(r *http.Request, query string, args ...any) (sql.Result, error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// A transaction-scoped lock serializes retries before any register/stock locks.
func requestReplay(ctx context.Context, tx *sql.Tx, id, kind string, actor int64, body any) (json.RawMessage, string, error) {
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if !requestIDPattern.MatchString(id) {
		return nil, hash, errors.New("A unique requestId of 16–100 letters, digits, underscores or hyphens is required")
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id); err != nil {
		return nil, hash, err
	}
	var oldHash, oldKind string
	var oldActor int64
	var response json.RawMessage
	err := tx.QueryRowContext(ctx, `SELECT actor_id,kind,request_hash,response FROM request_results WHERE request_id=$1`, id).Scan(&oldActor, &oldKind, &oldHash, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, hash, nil
	}
	if err != nil {
		return nil, hash, err
	}
	if oldActor != actor || oldHash != hash || oldKind != kind {
		return nil, hash, errors.New("This requestId was already used for a different request")
	}
	return response, hash, nil
}
func saveRequest(ctx context.Context, tx *sql.Tx, id, kind, hash string, actor int64, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO request_results(request_id,actor_id,kind,request_hash,response) VALUES($1,$2,$3,$4,$5)`, id, actor, kind, hash, raw)
	return err
}
