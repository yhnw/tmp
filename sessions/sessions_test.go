package sessions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/synctest"
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
	session := NewMiddleware[testSession]()
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				if r, err := session.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.Load().(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 1,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.Load().(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 2,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.Load().(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/renewid", nil),
			want: 2,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.Store.Load(ctx, "0"); !(r == nil && err == nil) {
					t.Fatal("old session found after renewid")
				}
				if r, err := session.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.Load().(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 3,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.Load().(*testSession).N; got != want {
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
				if r, err := session.Store.Load(ctx, "2"); !(r == nil && err == nil) {
					t.Fatal("session found after delete")
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 1,
			check: func(t *testing.T, want int, cookie *http.Cookie) {
				if r, err := session.Store.Load(ctx, cookie.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.Load().(*testSession).N; got != want {
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
			if c.Name == session.Cookie.Name {
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

	session := NewMiddleware[testSession]()
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

	session := NewMiddleware[testSession]()
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	store := newMemoryStore()
	session := NewMiddleware[testSession]()
	session.Store = store
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := len(store.m); got != 1 {
		t.Errorf("got %v; want 1", got)
	}
}

func TestDeleteNoWrite(t *testing.T) {
	store := newMemoryStore()
	session := NewMiddleware[testSession]()
	session.Store = store
	record := session.newRecord()
	record.ID = "test"
	store.m[record.ID] = record
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestGetAfterDeletePanic(t *testing.T) {
	var store funcStore
	store.DeleteFunc = func(ctx context.Context, id string) error {
		return nil
	}
	ctx := context.Background()
	session := NewMiddleware[testSession]()
	session.Store = store
	ctx = session.newContextWithRecord(ctx, session.newRecord())
	session.Delete(ctx)
	defer func() {
		if err := recover(); err != "session alreadly deleted" {
			t.Fatal("unexpected")
		}
	}()
	session.Get(ctx)
}

func TestGetAfterRenew(t *testing.T) {
	var store funcStore
	store.DeleteFunc = func(ctx context.Context, id string) error {
		return nil
	}
	ctx := context.Background()
	session := NewMiddleware[testSession]()
	ctx = session.newContextWithRecord(ctx, session.newRecord())
	session.Renew(ctx)
	defer func() {
		if err := recover(); err != nil {
			t.Fatal("unexpected panic")
		}
	}()
	session.Get(ctx)
}

func TestRenewUpdate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	session := NewMiddleware[testSession]()
	session.Store = store
	now := time.Now()
	session.now = func() time.Time { return now }
	record := session.newRecord()
	record.ID = "oldID"
	ctx = session.newContextWithRecord(ctx, record)
	session.RenewID(ctx, "newID")
	if record.ID != "newID" {
		t.Errorf("got %v; want newID", record.ID)
	}
	got, want := record.AbsoluteDeadline, now.Add(session.AbsoluteTimeout)
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

// type writeChecker struct {
// 	writeCalled       bool
// 	writeHeaderCalled bool
// }

// func (writeChecker) Header() http.Header { return nil }

func TestRenewDelete(t *testing.T) {
	var store funcStore
	called := false
	store.DeleteFunc = func(ctx context.Context, id string) error {
		called = true
		return nil
	}
	ctx := context.Background()

	session := NewMiddleware[testSession]()
	session.Store = store
	ctx = session.newContextWithRecord(ctx, session.newRecord())
	session.Renew(ctx)
	if !called {
		t.Errorf("Delete was not called")
	}
}

func TestID(t *testing.T) {
	ctx := context.Background()
	session := NewMiddleware[testSession]()
	r := session.newRecord()
	r.ID = "id"
	ctx = session.newContextWithRecord(ctx, r)
	if got := session.ID(ctx); got != "id" {
		t.Errorf("got %v; want id", got)
	}
}

// func TestCleanupNoop(t *testing.T) {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	cancel()

// 	for _, interval := range []time.Duration{-1, 0} {
// 		if started := cleanup(ctx, nil, interval, defaultErrorHandler); started {
// 			t.Errorf("cleanup goroutine started with interval = %v", interval)
// 		}
// 	}
// }

// func TestCleanup(t *testing.T) {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	cancel()

// 	if started := cleanup(ctx, nil, 10*time.Second, defaultErrorHandler); !started {
// 		t.Errorf("cleanup gorountine has not been started")
// 	}
// }

// func TestCleanupDelete(t *testing.T) {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()
// 	var store funcStore
// 	called := false
// 	once := sync.OnceFunc(func() { called = true })
// 	store.DeleteExpiredFunc = func(ctx context.Context) error {
// 		once()
// 		return nil
// 	}

// 	if started := cleanup(ctx, store, 1, defaultErrorHandler); !started {
// 		t.Errorf("cleanup gorountine has not been started")
// 	}
// 	time.Sleep(100 * time.Millisecond)
// 	if !called {
// 		t.Error("DeleteExpired was not called")
// 	}
// }

func TestMiddlewareRace(t *testing.T) {
	synctest.Run(func() {
		var errhCalled bool
		errh := func(w http.ResponseWriter, r *http.Request, err error) {
			if err.Error() == "active session alreadly exists" {
				errhCalled = true
			}
		}
		session := NewMiddleware[testSession]()
		session.ErrorHandler = errh
		h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(1 * time.Millisecond)
			w.Write(nil)
		}))
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		res := w.Result()
		cookie := res.Cookies()[0]
		req1 := httptest.NewRequest("GET", "/", nil)
		req2 := httptest.NewRequest("GET", "/", nil)
		req1.Header.Set("Cookie", cookie.String())
		req2.Header.Set("Cookie", cookie.String())

		w1 := httptest.NewRecorder()
		w2 := httptest.NewRecorder()

		go func() {
			h.ServeHTTP(w1, req1)
		}()
		synctest.Wait()
		if errhCalled {
			t.Error("unexpected errorHandler call")
		}
		go func() {
			h.ServeHTTP(w2, req2)
		}()
		// time.Sleep(1 * time.Millisecond)
		synctest.Wait()
		if !errhCalled {
			t.Error("errorHandler was not called")
		}
	})
}
