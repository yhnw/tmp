package sessions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// defined in stdlib
type rwUnwrapper interface {
	Unwrap() http.ResponseWriter
}

var _ rwUnwrapper = (*sessionWriter[testSession])(nil)

type testSession struct {
	N int
}

func TestMiddleware(t *testing.T) {
	ctx := context.Background()
	session := New[testSession]()
	mw := session.Middleware(ctx)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.RequestURI {
		case "/increment":
			session.Get(r.Context()).N++
		case "/renew":
			if err := session.Renew(r.Context()); err != nil {
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
		check func(t *testing.T, want int, old, new *http.Cookie)
	}{
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 1,
			check: func(t *testing.T, want int, old, new *http.Cookie) {
				if r, err := session.Store.Load(ctx, new.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 2,
			check: func(t *testing.T, want int, old, new *http.Cookie) {
				if r, err := session.Store.Load(ctx, new.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/renew", nil),
			want: 2,
			check: func(t *testing.T, want int, old, new *http.Cookie) {
				if r, err := session.Store.Load(ctx, old.Value); !(r == nil && err == nil) {
					t.Fatal("old session found after renew")
				}
				if r, err := session.Store.Load(ctx, new.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 3,
			check: func(t *testing.T, want int, old, new *http.Cookie) {
				if r, err := session.Store.Load(ctx, new.Value); err != nil {
					t.Fatal(err)
				} else if got := r.session.(*testSession).N; got != want {
					t.Fatalf("got %v; want %v", got, want)
				}
			},
		},
		{
			req: httptest.NewRequest("GET", "/delete", nil),
			check: func(t *testing.T, want int, old, new *http.Cookie) {
				if new.MaxAge != -1 {
					t.Fatal("want MaxAge == -1")
				}
				if r, err := session.Store.Load(ctx, old.Value); !(r == nil && err == nil) {
					t.Fatal("session found after delete")
				}
			},
		},
		{
			req:  httptest.NewRequest("GET", "/increment", nil),
			want: 1,
			check: func(t *testing.T, want int, old, new *http.Cookie) {
				if r, err := session.Store.Load(ctx, new.Value); err != nil {
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
		old := cookie
		for _, c := range resp.Cookies() {
			if c.Name == session.Cookie.Name {
				cookie = c
				break
			}
		}
		tt.check(t, tt.want, old, cookie)
	}
}
