package sessions

// import (
// 	"context"
// 	"testing"
// 	"time"
// )

// var validRecord = &Record{
// 	ID:               "valid",
// 	IdleDeadline:     time.Now().Add(24 * time.Hour),
// 	AbsoluteDeadline: time.Now().Add(24 * time.Hour * 365),
// }

// var expiredRecord = &Record{
// 	ID:               "expired",
// 	IdleDeadline:     time.Time{},
// 	AbsoluteDeadline: time.Time{},
// }

// func testStore(t *testing.T) *memoryStore {
// 	t.Helper()
// 	store := newMemoryStore()
// 	store.m[validRecord.ID] = validRecord
// 	store.m[expiredRecord.ID] = expiredRecord
// 	return store
// }

// func TestMemoryStoreLoad(t *testing.T) {
// 	ctx := context.Background()

// 	tests := []struct {
// 		id   string
// 		want *Record
// 	}{
// 		{validRecord.ID, validRecord},
// 		{expiredRecord.ID, nil},
// 		{"missing", nil},
// 	}

// 	for _, tt := range tests {
// 		t.Run("", func(t *testing.T) {
// 			store := testStore(t)
// 			r, err := store.Load(ctx, tt.id)
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 			if r != tt.want {
// 				t.Fatalf("got %v; want %v", r, tt.want)
// 			}
// 		})
// 	}
// }

// func TestMemoryStoreSave(t *testing.T) {
// 	ctx := context.Background()

// 	tests := []struct {
// 		record *Record
// 		want   *Record
// 	}{
// 		{validRecord, validRecord},
// 		{expiredRecord, nil},
// 	}

// 	for _, tt := range tests {
// 		t.Run("", func(t *testing.T) {
// 			store := newMemoryStore()
// 			if err := store.Save(ctx, tt.record); err != nil {
// 				t.Fatal(err)
// 			}
// 			got, err := store.Load(ctx, tt.record.ID)
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 			if got != tt.want {
// 				t.Fatalf("got %#v; want %#v", got, tt.want)
// 			}
// 		})
// 	}
// }

// func TestMemoryStoreDelete(t *testing.T) {
// 	ctx := context.Background()

// 	tests := []struct {
// 		id   string
// 		want *Record
// 	}{
// 		{validRecord.ID, nil},
// 		{expiredRecord.ID, nil},
// 		{"missing", nil},
// 	}

// 	for _, tt := range tests {
// 		t.Run("", func(t *testing.T) {
// 			store := testStore(t)
// 			if err := store.Delete(ctx, tt.id); err != nil {
// 				t.Fatal(err)
// 			}
// 			if r, _ := store.Load(ctx, tt.id); r != tt.want {
// 				t.Fatalf("got %v; want %v", r, tt.want)
// 			}
// 		})
// 	}
// }

// func TestMemoryStoreDeleteExpired(t *testing.T) {
// 	ctx := context.Background()

// 	tests := []struct {
// 		id   string
// 		want *Record
// 	}{
// 		{validRecord.ID, validRecord},
// 		{expiredRecord.ID, nil},
// 	}

// 	for _, tt := range tests {
// 		t.Run("", func(t *testing.T) {
// 			store := testStore(t)
// 			if err := store.DeleteExpired(ctx); err != nil {
// 				t.Fatal(err)
// 			}
// 			if r, _ := store.Load(ctx, tt.id); r != tt.want {
// 				t.Fatalf("got %v; want %v", r, tt.want)
// 			}
// 		})
// 	}
// }
