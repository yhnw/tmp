package httpsession

import (
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
	session := New(NewMemoryStore[testSession]())
	store := NewMemoryStore[testSession]()
	session.Store = store
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.RequestURI {
		case "/increment":
			sess := session.Get(r.Context())
			sess.N++
		case "/renewid":
			sess := session.Get(r.Context())
			if err := session.Renew(r.Context(), strconv.Itoa(sess.N)); err != nil {
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
	session := New(NewMemoryStore[testSession]())
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = session.Get(r.Context())
	})
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	defer wantPanic(t, "httpsession: middleware was not used")
	h.ServeHTTP(w, r)
}

func TestGetAfterDelete(t *testing.T) {
	session := New(NewMemoryStore[testSession]())
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
	store := NewMemoryStore[testSession]()
	session := New(NewMemoryStore[testSession]())
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
	store := NewMemoryStore[testSession]()
	session := New(NewMemoryStore[testSession]())
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
	store := NewMemoryStore[testSession]()
	session := New(NewMemoryStore[testSession]())
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
	session := New(NewMemoryStore[testSession]())
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
	session := New(NewMemoryStore[testSession]())

	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := session.Renew(r.Context(), ""); err != nil {
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
	session := New(NewMemoryStore[testSession]())
	now := time.Now()
	session.now = func() time.Time { return now }
	record := new(Record[testSession])
	record.ID = "oldID"
	ctx = session.newContextWithRecord(ctx, record)
	session.Renew(ctx, "newID")
	if record.ID != "newID" {
		t.Errorf("got %v; want newID", record.ID)
	}
	got, want := record.AbsoluteDeadline, now.Add(session.AbsoluteTimeout)
	if !got.Equal(want) {
		t.Errorf("got %v; want %v", record.AbsoluteDeadline, want)
	}
}

func TestRenewDelete(t *testing.T) {
	store := NewMemoryStore[testSession]()
	session := New(NewMemoryStore[testSession]())
	session.Store = store
	var oldID string
	h := session.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldID = session.recordFromContext(r.Context()).ID
		if err := session.Renew(r.Context(), "newid"); err != nil {
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
	session := New(NewMemoryStore[testSession]())
	r := new(Record[testSession])
	r.ID = "testid"
	ctx = session.newContextWithRecord(ctx, r)
	if got := session.ID(ctx); got != "testid" {
		t.Errorf("got %v; want testid", got)
	}
}

// func TestPopulate(t *testing.T) {
// 	store := newMemoryStore[testSession]()
// 	sess1 := &testSession{N: 1}
// 	sess2 := &testSession{N: 2}
// 	session := NewMiddleware[testSession]()
// 	session.Store = store
// 	session.Populate("sess1", sess1, "sess2", sess2)
// 	if _, ok := store.m["sess1"]; !ok {
// 		t.Error("sess1 not found")
// 	}
// 	if _, ok := store.m["sess2"]; !ok {
// 		t.Error("sess2 not found")
// 	}
// }

// func TestPopulatePanic(t *testing.T) {
// 	store := newMemoryStore[testSession]()
// 	sess1 := &testSession{N: 1}
// 	sess2 := &testSession{N: 2}
// 	session := NewMiddleware[testSession]()
// 	session.Store = store
// 	tests := []struct {
// 		name        string
// 		wantRecover string
// 		fn          func()
// 	}{
// 		{
// 			name:        "zero args",
// 			wantRecover: "Populate: args must have non-zero even length",
// 			fn: func() {
// 				session.Populate()
// 			},
// 		},
// 		{
// 			name:        "args odd length",
// 			wantRecover: "Populate: args must have non-zero even length",
// 			fn: func() {
// 				session.Populate(1, 2, 3)
// 			},
// 		},
// 		{
// 			name:        "arg 1 int",
// 			wantRecover: "Populate: arg 1 expected string but got int",
// 			fn: func() {
// 				session.Populate(0, sess1, "sess2", sess2)
// 			},
// 		},
// 		{
// 			name:        "arg 2 int",
// 			wantRecover: "Populate: arg 2 expected *httpsession.testSession but got int",
// 			fn: func() {
// 				session.Populate("sess1", 0, "sess2", sess2)
// 			},
// 		},
// 		{
// 			name:        "arg 3 int",
// 			wantRecover: "Populate: arg 3 expected string but got int",
// 			fn: func() {
// 				session.Populate("sess1", sess1, 0, sess2)
// 			},
// 		},
// 		{
// 			name:        "arg 4 int",
// 			wantRecover: "Populate: arg 4 expected *httpsession.testSession but got int",
// 			fn: func() {
// 				session.Populate("sess1", sess1, "sess2", 0)
// 			},
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			defer wantPanic(t, tt.wantRecover)
// 			tt.fn()
// 		})
// 	}
// }

func TestMiddlewareRace(t *testing.T) {
	synctest.Run(func() {
		var errhCalled bool
		errh := func(w http.ResponseWriter, r *http.Request, err error) {
			if err.Error() == "httpsession: active session alreadly exists" {
				errhCalled = true
			}
		}
		session := New(NewMemoryStore[testSession]())
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
		synctest.Wait()
		if !errhCalled {
			t.Error("errorHandler was not called")
		}
	})
}
