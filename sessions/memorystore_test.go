package sessions

import (
	"context"
	"testing"
	"time"
)

var validRecord = &Record{
	Token:            "valid",
	IdleDeadline:     time.Now().Add(24 * time.Hour),
	AbsoluteDeadline: time.Now().Add(24 * time.Hour * 365),
}

var expiredRecord = &Record{
	Token:            "expired",
	IdleDeadline:     time.Time{},
	AbsoluteDeadline: time.Time{},
}

func testStore(t *testing.T) *memoryStore {
	t.Helper()
	store := newMemoryStore()

	if err := store.Save(context.Background(), validRecord); err != nil {
		t.Fatalf("testStore: %v", err)
	}
	if err := store.Save(context.Background(), expiredRecord); err != nil {
		t.Fatalf("testStore: %v", err)
	}
	return store
}

func TestMemoryStoreLoad(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tests := []struct {
		token string
		want  *Record
	}{
		{"valid", validRecord},
		{"expired", nil},
		{"missing", nil},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			r, err := store.Load(ctx, tt.token)
			if err != nil {
				t.Fatal(err)
			}
			if r != tt.want {
				t.Fatalf("got %v; want %v", r, tt.want)
			}
		})
	}
}

func TestMemoryStoreSave(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()

	tests := []struct {
		record *Record
		want   *Record
	}{
		{validRecord, validRecord},
		{expiredRecord, nil},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if err := store.Save(ctx, tt.record); err != nil {
				t.Fatal(err)
			}
			got, err := store.Load(ctx, tt.record.Token)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %#v; want %#v", got, tt.want)
			}
		})
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tests := []struct {
		token string
		want  *Record
	}{
		{"valid", nil},
		{"missing", nil},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if err := store.Delete(ctx, tt.token); err != nil {
				t.Fatal(err)
			}
			if r, _ := store.Load(ctx, tt.token); r != tt.want {
				t.Fatalf("got %v; want %v", r, tt.want)
			}
		})
	}
}

func TestMemoryStoreDeleteExpired(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tests := []struct {
		token string
		want  *Record
	}{
		{"valid", validRecord},
		{"expired", nil},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if err := store.DeleteExpired(ctx); err != nil {
				t.Fatal(err)
			}
			if r, _ := store.Load(ctx, tt.token); r != tt.want {
				t.Fatalf("got %v; want %v", r, tt.want)
			}
		})
	}
}
