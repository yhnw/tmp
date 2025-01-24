package sessions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// defined in net/http
type rwUnwrapper interface {
	Unwrap() http.ResponseWriter
}

var _ rwUnwrapper = (*sessionWriter[testSession])(nil)

type testSession struct {
	N int
}

func TestMiddleware(t *testing.T) {
	ctx := context.Background()
	session := NewMiddleware(ctx, Config[testSession]{})
	h := session.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.RequestURI {
		case "/increment":
			sess := session.Get(r.Context())
			sess.N++
		case "/renewid":
			sess := session.Get(r.Context())
			if err := session.RenewID(r.Context(), strconv.Itoa(sess.N)); err != nil {
				t.Fatal(err)
			}
		case "/delete":
			if err := session.Delete(r.Context()); err != nil {
				t.Fatal(err)
			}
		default:
			panic("unreachable")
		}
		if _, err := w.Write(nil); err != nil {
			t.Fatal(err)
		}
	}))

	tests := []struct {
		req   *http.Request
		want  int
		check func(t *testing.T, want int, cookie *http.Cookie)
	}{
		{
			req:  httptest.NewRequest("GET", "/renewid", nil),
			want: 0,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.cfg.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 1,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.cfg.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 2,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.cfg.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/renewid", nil),
			want: 2,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.cfg.Store.Load(ctx, "0"); !(r == nil && err == nil) {
					t.Fatal("old session found after renewid")
				}
				if r, err := session.cfg.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 3,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.cfg.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req: httptest.NewRequest("GET", "/delete", nil),
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if cookie.MaxAge != -1 {
					t.Fatal("want MaxAge == -1")
				}
				if r, err := session.cfg.Store.Load(ctx, "2"); !(r == nil && err == nil) {
					t.Fatal("session found after delete")
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 1,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.cfg.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
	}

	var cookie *http.Cookie
	for _, tt := range tests {
		tt.req.Header.Set("Cookie", cookie.String())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, tt.req)
		resp := w.Result()
		for _, c := range resp.Cookies() {
			if c.Name == session.cfg.Cookie.Name {
				cookie = c
				break
			}
		}
		tt.check(t, tt.want, cookie)
	}
}

func TestGetBeforeWrap(t *testing.T) {
	defer func() {
		if err := recover(); err == nil {
			t.Error("should panic")
		}
	}()

	ctx := context.Background()
	session := NewMiddleware(ctx, Config[testSession]{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = session.Get(r.Context())
	})
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
}

func TestGetAfterDelete(t *testing.T) {
	defer func() {
		if err := recover(); err == nil {
			t.Error("should panic")
		}
	}()

	ctx := context.Background()
	session := NewMiddleware(ctx, Config[testSession]{})
	h := session.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Delete(r.Context()); err != nil {
			t.Fatal(err)
		}
		_ = session.Get(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
}

func TestMiddlewareNoWrite(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	session := NewMiddleware(ctx, Config[testSession]{Store: store})
	h := session.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := len(store.m); got != 1 {
		t.Errorf("got %v; want 1", got)
	}
}

func TestDeleteNoWrite(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	session := NewMiddleware(ctx, Config[testSession]{Store: store})
	record := session.newRecord()
	record.ID = "test"
	store.m[record.ID] = record
	h := session.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := session.newContextWithRecord(r.Context(), record)
		if err := session.Delete(ctx); err != nil {
			t.Fatal(err)
		}
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if _, ok := store.m[record.ID]; ok {
		t.Fatal("expected to be deleted")
	}
}

func TestRenewUpdate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	session := NewMiddleware(ctx, Config[testSession]{Store: store})
	now := time.Now()
	session.now = func() time.Time { return now }
	record := session.newRecord()
	record.ID = "oldID"
	ctx = session.newContextWithRecord(ctx, record)
	session.RenewID(ctx, "newID")
	if record.ID != "newID" {
		t.Errorf("got %v; want newID", record.ID)
	}
	got, want := record.AbsoluteDeadline, now.Add(session.cfg.AbsoluteTimeout)
	if !got.Equal(want) {
		t.Errorf("got %v; want %v", record.AbsoluteDeadline, want)
	}
}

type funcStore struct {
	LoadFunc          func(ctx context.Context, id string) (*Record, error)
	SaveFunc          func(ctx context.Context, r *Record) error
	DeleteFunc        func(ctx context.Context, id string) error
	DeleteExpiredFunc func(ctx context.Context) error
}

func (f funcStore) Load(ctx context.Context, id string) (*Record, error) {
	return f.LoadFunc(ctx, id)
}

func (f funcStore) Save(ctx context.Context, r *Record) error {
	return f.SaveFunc(ctx, r)
}

func (f funcStore) Delete(ctx context.Context, id string) error {
	return f.DeleteFunc(ctx, id)
}

func (f funcStore) DeleteExpired(ctx context.Context) error {
	return f.DeleteExpiredFunc(ctx)
}

func TestRenewDelete(t *testing.T) {
	var store funcStore
	called := false
	store.DeleteFunc = func(ctx context.Context, id string) error {
		called = true
		return nil
	}
	ctx := context.Background()
	session := NewMiddleware(ctx, Config[testSession]{Store: store})
	ctx = session.newContextWithRecord(ctx, session.newRecord())
	session.Renew(ctx)
	if !called {
		t.Errorf("Delete was not called")
	}
}

func TestID(t *testing.T) {
	ctx := context.Background()
	session := NewMiddleware(ctx, Config[testSession]{})
	r := session.newRecord()
	r.ID = "id"
	ctx = session.newContextWithRecord(ctx, r)
	if got := session.ID(ctx); got != "id" {
		t.Errorf("got %v; want id", got)
	}
}
