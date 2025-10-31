package httpsession

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"testing"
	"testing/synctest"
	"time"
)

// defined in net/http
type rwUnwrapper interface {
	Unwrap() http.ResponseWriter
}

var _ rwUnwrapper = (*sessionSaver[testSession])(nil)

func wantPanic(t *testing.T, wantRecover any) {
	t.Helper()
	if got := recover(); got != wantRecover {
		t.Fatalf("unexpected panic: recover() = %v; want %v", got, wantRecover)
	}
}

func noPanic(t *testing.T) {
	t.Helper()
	if got := recover(); got != nil {
		t.Fatalf("unexpected panic: recover() = %v", got)
	}
}

type testSession struct {
	N int
}

func TestMiddleware(t *testing.T) {
	ctx := t.Context()
	session := New[testSession]()
	store := newMemoryStore[testSession]()
	session.Store = store
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
		wantN int
		check func(t *testing.T, wantN int, cookie *http.Cookie)
	}{
		{
			req:   httptest.NewRequest("GET", "/renewid", nil),
			wantN: 0,
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				var r Record[testSession]
				if _, err := session.Store.Load(ctx, cookie.Value, &r); err != nil {
					t.Fatal(err)
				} else if got := r.Session.N; got != wantN {
					t.Fatalf("got %v; want %v", got, wantN)
				} else if r.ID != "0" {
					t.Fatal("unexpected id")
				}
			},
		},
		{
			req:   httptest.NewRequest("GET", "/increment", nil),
			wantN: 1,
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				var r Record[testSession]
				if _, err := session.Store.Load(ctx, cookie.Value, &r); err != nil {
					t.Fatal(err)
				} else if got := r.Session.N; got != wantN {
					t.Fatalf("got %v; want %v", got, wantN)
				}
			},
		},
		{
			req:   httptest.NewRequest("GET", "/increment", nil),
			wantN: 2,
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				var r Record[testSession]
				if _, err := session.Store.Load(ctx, cookie.Value, &r); err != nil {
					t.Fatal(err)
				} else if got := r.Session.N; got != wantN {
					t.Fatalf("got %v; want %v", got, wantN)
				}
			},
		},
		{
			req:   httptest.NewRequest("GET", "/renewid", nil),
			wantN: 2,
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				if _, ok := store.m["0"]; ok {
					t.Fatal("old session found after renewid")
				}
				var r Record[testSession]
				if _, err := session.Store.Load(ctx, cookie.Value, &r); err != nil {
					t.Fatal(err)
				} else if got := r.Session.N; got != wantN {
					t.Fatalf("got %v; want %v", got, wantN)
				} else if r.ID != "2" {
					t.Fatal("unexpected id")
				}
			},
		},
		{
			req:   httptest.NewRequest("GET", "/increment", nil),
			wantN: 3,
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				var r Record[testSession]
				if _, err := session.Store.Load(ctx, cookie.Value, &r); err != nil {
					t.Fatal(err)
				} else if got := r.Session.N; got != wantN {
					t.Fatalf("got %v; want %v", got, wantN)
				}
			},
		},
		{
			req: httptest.NewRequest("GET", "/delete", nil),
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				if cookie.MaxAge != -1 {
					t.Fatal("want MaxAge = -1")
				}
				if _, ok := store.m["2"]; ok {
					t.Fatal("old session found after renewid")
				}
			},
		},
		{
			req:   httptest.NewRequest("GET", "/increment", nil),
			wantN: 1,
			check: func(t *testing.T, wantN int, cookie *http.Cookie) {
				var r Record[testSession]
				if _, err := session.Store.Load(ctx, cookie.Value, &r); err != nil {
					t.Fatal(err)
				} else if got := r.Session.N; got != wantN {
					t.Fatalf("got %v; want %v", got, wantN)
				}
			},
		},
	}

	var cookie *http.Cookie
	for _, tt := range tests {
		tt.req.Header.Set("Cookie", cookie.String())
		cookie = nil
		w := httptest.NewRecorder()
		h.ServeHTTP(w, tt.req)
		resp := w.Result()
		for _, c := range resp.Cookies() {
			if c.Name == session.SetCookie.Name {
				cookie = c
				break
			}
		}
		if cookie == nil {
			t.Fatal("cookie = nil")
		}
		tt.check(t, tt.wantN, cookie)
	}
}

func TestGetBeforeHandler(t *testing.T) {
	session := New[testSession]()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = session.Get(r.Context())
	})
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	defer wantPanic(t, "httpsession: middleware was not used")
	h.ServeHTTP(w, r)
}

func TestGetAfterDelete(t *testing.T) {
	session := New[testSession]()
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Delete(r.Context()); err != nil {
			t.Fatal(err)
		}
		_ = session.Get(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	defer wantPanic(t, "httpsession: session alreadly deleted")
	h.ServeHTTP(w, r)
}

func TestMiddlewareNoWrite(t *testing.T) {
	store := newMemoryStore[testSession]()
	session := New[testSession]()
	session.Store = store
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := len(store.m); got != 0 {
		t.Errorf("got %v; want 0", got)
	}
}

func TestDeleteNoWrite(t *testing.T) {
	store := newMemoryStore[testSession]()
	session := New[testSession]()
	session.Store = store
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Delete(r.Context()); err != nil {
			t.Fatal(err)
		}
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if len(store.m) != 0 {
		t.Fatalf("len(store.m) = %v; want 0", len(store.m))
	}
}

func TestRead(t *testing.T) {
	store := newMemoryStore[testSession]()
	session := New[testSession]()
	session.Store = store
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = session.Read(r.Context())
		w.Write(nil)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if len(store.m) != 0 {
		t.Fatalf("len(store.m) = %v; want 0", len(store.m))
	}
}
func TestReadNoWrite(t *testing.T) {
	store := newMemoryStore[testSession]()
	session := New[testSession]()
	session.Store = store
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = session.Read(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if len(store.m) != 0 {
		t.Fatalf("len(store.m) = %v; want 0", len(store.m))
	}
}

func TestGetAfterDeletePanic(t *testing.T) {
	session := New[testSession]()
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Delete(r.Context()); err != nil {
			t.Fatal(err)
		}
		session.Get(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	defer wantPanic(t, "httpsession: session alreadly deleted")
	h.ServeHTTP(w, r)
}

func TestGetAfterRenew(t *testing.T) {
	session := New[testSession]()

	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Renew(r.Context()); err != nil {
			t.Fatal(err)
		}
		session.Get(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	defer noPanic(t)
	h.ServeHTTP(w, r)
}

func TestRenewUpdateAbsoluteDeadline(t *testing.T) {
	ctx := t.Context()
	session := New[testSession]()
	now := time.Now()
	session.now = func() time.Time { return now }
	record := new(Record[testSession])
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

func TestRenewDelete(t *testing.T) {
	store := newMemoryStore[testSession]()
	session := New[testSession]()
	session.Store = store
	var oldID string
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldID = session.recordFromContext(r.Context()).ID
		if err := session.RenewID(r.Context(), "newid"); err != nil {
			t.Fatal(err)
		}
		session.Get(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if _, ok := store.m[oldID]; ok {
		t.Fatal("old session found")
	}
	if _, ok := store.m["newid"]; !ok {
		t.Fatal("new session not found")
	}
}

func TestID(t *testing.T) {
	ctx := t.Context()
	session := New[testSession]()
	r := new(Record[testSession])
	r.ID = "testid"
	ctx = session.newContextWithRecord(ctx, r)
	if got := session.ID(ctx); got != "testid" {
		t.Errorf("got %v; want testid", got)
	}
}

func TestMiddlewareRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var errhCalled bool
		errh := func(w http.ResponseWriter, r *http.Request, err error) {
			if err.Error() == "httpsession: active session alreadly exists" {
				errhCalled = true
			}
		}
		session := New[testSession]()
		session.ErrorHandler = errh
		h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session.Get(r.Context())
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
		req1.AddCookie(cookie)
		req2.AddCookie(cookie)

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
		synctest.Wait()
		if !errhCalled {
			t.Error("errorHandler was not called")
		}
		time.Sleep(1 * time.Millisecond)
	})
}

func TestResponseController(t *testing.T) {
	session := New[testSession]()
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.Flush(); err != nil {
			t.Error(err)
		}
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !w.Flushed {
		t.Errorf("Flush was not called")
	}
}

type mockStore[T any] struct {
	LoadFunc          func(context.Context, string, *Record[T]) (bool, error)
	SaveFunc          func(context.Context, *Record[T]) error
	DeleteFunc        func(context.Context, string) error
	DeleteExpiredFunc func(context.Context) error
}

func (s *mockStore[T]) Load(ctx context.Context, id string, ret *Record[T]) (found bool, err error) {
	if s.LoadFunc != nil {
		return s.LoadFunc(ctx, id, ret)
	}
	return false, errors.ErrUnsupported
}

func (s *mockStore[T]) Save(ctx context.Context, r *Record[T]) error {
	if s.SaveFunc != nil {
		return s.SaveFunc(ctx, r)
	}
	return errors.ErrUnsupported
}

func (s *mockStore[T]) Delete(ctx context.Context, id string) error {
	if s.DeleteFunc != nil {
		return s.DeleteFunc(ctx, id)
	}
	return errors.ErrUnsupported
}

func (s *mockStore[T]) DeleteExpired(ctx context.Context) error {
	if s.DeleteExpiredFunc != nil {
		return s.DeleteExpiredFunc(ctx)
	}
	return errors.ErrUnsupported
}

func TestErrorHandler(t *testing.T) {
	session := New[testSession]()
	session.Store = &mockStore[testSession]{}
	var called bool
	session.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		called = true
	}
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Get(r.Context())
		w.Write(nil)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Error("ErrorHandler was not called")
	}
}

func TestDefaultErrorHandler(t *testing.T) {
	session := New[testSession]()
	session.Store = &mockStore[testSession]{}
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Get(r.Context())
		w.WriteHeader(200)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Errorf("got %v; want 500", w.Code)
	}
}

func TestAbsoluteDeadline(t *testing.T) {
	session := New[testSession]()
	now := time.Now()
	session.now = func() time.Time { return now }
	session.AbsoluteTimeout = 0
	var record *Record[testSession]
	session.Store = &mockStore[testSession]{
		SaveFunc: func(ctx context.Context, r *Record[testSession]) error {
			record = r
			return nil
		},
	}
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Get(r.Context())
		w.Write(nil)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !record.IdleDeadline.Equal(now) {
		t.Errorf("got %v; want %v", record.IdleDeadline, now)
	}
}

func TestCleanup(t *testing.T) {
	session := New[testSession]()
	record := Record[testSession]{
		ID: "testid",
	}
	if err := session.Store.Save(t.Context(), &record); err != nil {
		t.Fatal(err)
	}
	session.Cleanup(t.Context(), 500*time.Microsecond)
	time.Sleep(1 * time.Millisecond)
	if found, err := session.Store.Load(t.Context(), record.ID, &record); err != nil || found {
		t.Fatalf("Load() = %v, %t", err, found)
	}
}

func TestCleanupNoLeak(t *testing.T) {
	session := New[testSession]()
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(t.Context())
	session.Cleanup(ctx, 1*time.Second)
	cancel()
	time.Sleep(10 * time.Millisecond)
	after := runtime.NumGoroutine()
	if before != after {
		t.Fatalf("%v => %v", before, after)
	}
}
