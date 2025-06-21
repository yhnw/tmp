package httpsession

import (
	"testing"
	"time"
)

var validRecord = Record[testSession]{
	ID:               "valid",
	IdleDeadline:     time.Now().Add(24 * time.Hour),
	AbsoluteDeadline: time.Now().Add(24 * time.Hour * 365),
}

var expiredRecord = Record[testSession]{
	ID:               "expired",
	IdleDeadline:     time.Time{},
	AbsoluteDeadline: time.Time{},
}

func testStore(t *testing.T) *memoryStore[testSession] {
	t.Helper()
	store := NewMemoryStore[testSession]()
	store.m[validRecord.ID] = validRecord
	store.m[expiredRecord.ID] = expiredRecord
	return store
}

func TestMemoryStoreLoad(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		id    string
		found bool
	}{
		{validRecord.ID, true},
		{expiredRecord.ID, false},
		{"missing", false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			store := testStore(t)
			var r Record[testSession]
			found, err := store.Load(ctx, tt.id, &r)
			if err != nil {
				t.Fatal(err)
			}
			if tt.found != found {
				t.Fatalf("got %#v; want nil", found)
			} else if tt.found && r.ID != tt.id {
				t.Fatalf("got %#v; want %#v", found, tt.id)
			}
		})
	}
}

func TestMemoryStoreSave(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		record *Record[testSession]
		found  bool
	}{
		{&validRecord, true},
		{&expiredRecord, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			store := NewMemoryStore[testSession]()
			var r Record[testSession]
			if err := store.Save(ctx, tt.record); err != nil {
				t.Fatal(err)
			}
			got, err := store.Load(ctx, tt.record.ID, &r)
			if err != nil {
				t.Fatal(err)
			}
			if tt.found != got {
				t.Fatalf("got %#v; want nil", got)
			} else if tt.found && r.ID != tt.record.ID {
				t.Fatalf("got %#v; want %#v", r.ID, tt.record.ID)
			}
		})
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		id string
	}{
		{validRecord.ID},
		{expiredRecord.ID},
		{"foo"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			store := testStore(t)
			if err := store.Delete(ctx, tt.id); err != nil {
				t.Fatal(err)
			}
			var r Record[testSession]
			if found, _ := store.Load(ctx, tt.id, &r); found != false {
				t.Fatalf("got %#v; want %#v", r, false)
			}
		})
	}
}

func TestMemoryStoreDeleteExpired(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		id    string
		found bool
	}{
		{validRecord.ID, true},
		{expiredRecord.ID, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			store := testStore(t)
			if err := store.DeleteExpired(ctx); err != nil {
				t.Fatal(err)
			}
			var r Record[testSession]

			got, err := store.Load(ctx, tt.id, &r)
			if err != nil {
				t.Fatal(err)
			}
			if tt.found != got {
				t.Fatalf("got %#v; want nil", got)
			} else if tt.found && r.ID != tt.id {
				t.Fatalf("got %#v; want %#v", r.ID, tt.id)
			}
		})
	}
}
