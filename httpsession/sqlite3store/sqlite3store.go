package sqlite3store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/yhnw/tmp/httpsession"
)

type SessionStore struct {
	loadStmt          *sql.Stmt
	saveStmt          *sql.Stmt
	deleteStmt        *sql.Stmt
	deleteExpiredStmt *sql.Stmt
}

func NewSessionStore(db *sql.DB) *SessionStore {
	loadStmt, err1 := db.Prepare(queryLoad)
	saveStmt, err2 := db.Prepare(querySave)
	deleteStmt, err3 := db.Prepare(queryDelete)
	deleteExpiredStmt, err4 := db.Prepare(queryDeleteExpired)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		panic(fmt.Sprintf("sqlite3store.NewSessionStore: sql.DB.Prepare: %v", err))
	}
	return &SessionStore{loadStmt, saveStmt, deleteStmt, deleteExpiredStmt}
}

type rfc3339Nano time.Time

func (t *rfc3339Nano) Scan(src any) (err error) {
	var str string
	switch v := src.(type) {
	case nil:
		*(*time.Time)(t) = time.Time{}
		return nil
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return fmt.Errorf("sqlite3store: cannot scan to time.Time: (%#v, %T)", src, src)
	}
	*(*time.Time)(t), err = time.Parse(time.RFC3339Nano, str)
	if err != nil {
		return fmt.Errorf("sqlite3store: cannot parse time from %q: %v", str, err)
	}
	return nil
}

func (t rfc3339Nano) Value() (driver.Value, error) {
	if (time.Time)(t).IsZero() {
		return nil, nil
	}
	return (time.Time)(t).UTC().Format(time.RFC3339Nano), nil
}

const queryLoad = `
SELECT
	id,
	idle_deadline,
	absolute_deadline,
	data
FROM
	httpsession
WHERE
	id = ? AND julianday(idle_deadline) > julianday('now')`

func (s *SessionStore) Load(ctx context.Context, id string) (*httpsession.Record, error) {
	var r httpsession.Record
	err := s.loadStmt.QueryRowContext(ctx, id).Scan(
		&r.ID,
		(*rfc3339Nano)(&r.IdleDeadline),
		(*rfc3339Nano)(&r.AbsoluteDeadline),
		&r.Data,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

const querySave = `
INSERT INTO httpsession (
	id, idle_deadline, absolute_deadline, data
) VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
 	idle_deadline = excluded.idle_deadline,
 	absolute_deadline = excluded.absolute_deadline,
 	data = excluded.data`

func (s *SessionStore) Save(ctx context.Context, r *httpsession.Record) error {
	_, err := s.saveStmt.ExecContext(ctx,
		r.ID,
		rfc3339Nano(r.IdleDeadline),
		rfc3339Nano(r.AbsoluteDeadline),
		r.Data,
	)
	return err
}

const queryDelete = `DELETE FROM httpsession WHERE id = ?`

func (s *SessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.deleteStmt.ExecContext(ctx, id)
	return err
}

const queryDeleteExpired = `DELETE FROM httpsession WHERE julianday(idle_deadline) <= julianday('now')`

func (s *SessionStore) DeleteExpired(ctx context.Context) error {
	_, err := s.deleteExpiredStmt.ExecContext(ctx)
	return err
}
