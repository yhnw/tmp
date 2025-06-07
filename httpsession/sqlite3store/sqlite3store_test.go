package sqlite3store

import (
	"database/sql"
	"flag"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/yhnw/tmp/httpsession"
	// _ "github.com/tailscale/sqlite"
	// _ "modernc.org/sqlite"
	// _ "github.com/ncruces/go-sqlite3/driver"
	// _ "github.com/ncruces/go-sqlite3/embed"
)

var (
	recordNotExpired = &httpsession.Record{
		ID:               "notexpired",
		IdleDeadline:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		AbsoluteDeadline: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Data:             []byte(`{"log":"record data"}`),
	}
	recordExpired = &httpsession.Record{
		ID:               "expired",
		IdleDeadline:     time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		AbsoluteDeadline: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Data:             []byte(`{"log":"expired record data"}`),
	}
)

var keepDB = flag.Bool("keepdb", false, "keep db for debugging")

func testDB(t testing.TB) *sql.DB {
	dsn := "file:" + t.TempDir() + "/test.db"
	if *keepDB {
		dsn = "file:./sqlite3store.db"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		db, err = sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS httpsession (
       id TEXT NOT NULL PRIMARY KEY,
       idle_deadline TEXT NOT NULL,
       absolute_deadline TEXT NOT NULL,
       data BLOB NOT NULL
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS httpsession_idle_deadline_idx ON httpsession(idle_deadline)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func testStore(t testing.TB) *SessionStore {
	t.Helper()
	db := testDB(t)
	store := NewSessionStore(db)
	if err := store.Save(t.Context(), recordNotExpired); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), recordExpired); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestLoad(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	record, err := store.Load(ctx, recordNotExpired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != recordNotExpired.ID {
		t.Errorf("got %v; want %v", record.ID, recordNotExpired.ID)
	}
	record, err = store.Load(ctx, recordExpired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		t.Errorf("unexpected record %#v", record)
	}
}

func TestSave(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	record := &httpsession.Record{
		ID:               "savetest",
		IdleDeadline:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		AbsoluteDeadline: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		Data:             []byte(`{"log":"record data"}`),
	}
	if err := store.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("record not found")
	}
	if got.ID != record.ID {
		t.Errorf("got %v; want %v", got.ID, record.ID)
	}
}

func TestDelete(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	if err := store.Delete(ctx, recordNotExpired.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, recordNotExpired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("record found")
	}
}

func TestDeleteExpired(t *testing.T) {
	ctx := t.Context()
	store := testStore(t)
	if err := store.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx, recordExpired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expired record found")
	}
	got, err = store.Load(ctx, recordNotExpired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("record not found")
	}
}
