// Package sessions provides a net/http middleware that tracks HTTP sessions using HTTP cookies.
package sessions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Session represents an HTTP session.
type Session[T any] struct {
	// IdleTimeout defines the amount of time a session will remain active.
	IdleTimeout time.Duration
	// AbsoluteTimeout defines the maximum amount of time a session can be active.
	AbsoluteTimeout time.Duration
	CleanupInterval time.Duration
	// Cookie is used as a template for a Set-Cookie header.
	Cookie       http.Cookie
	Store        Store
	Codec        Codec[T]
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

// Store is the interface that stores session records.
type Store interface {
	// Load loads a session record associated with token.
	// If found, it returns that record and nil.
	// If not found, it returns nil, nil.
	Load(ctx context.Context, token string) (*Record, error)

	// Save saves r.
	Save(ctx context.Context, r *Record) error

	// Delete deletes a session record associated with token.
	Delete(ctx context.Context, token string) error

	// DeleteExpired deletes all expired session records.
	DeleteExpired(ctx context.Context) error
}

// Record holds information about an HTTP session.
type Record struct {
	Token            string
	IdleDeadline     time.Time
	AbsoluteDeadline time.Time
	Data             []byte

	session any // *T
}

// New returns a new instance of Session with default settings.
func New[T any]() Session[T] {
	return Session[T]{
		IdleTimeout:     24 * time.Hour,
		AbsoluteTimeout: 7 * 24 * time.Hour,
		CleanupInterval: 1 * time.Hour,
		Cookie: http.Cookie{
			Name:     "SESSION",
			Path:     "/",
			Domain:   "",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		},
		Store:        newMemoryStore(),
		Codec:        JSONCodec[T]{},
		ErrorHandler: defaultErrorHandler,
	}
}

func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), fmt.Sprintf("sessions: %v", err))
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// Middleware returns a middleware that automatically tracks HTTP sessions.
// After it was called, Session's fields must not be mutated.
// If s.CleanupInterval > 0, it also starts a goroutine that deletes expired sessions
// after each CleanupInterval.
func (s *Session[T]) Middleware(ctx context.Context) func(next http.Handler) http.Handler {
	s.deleteExpiredInterval(ctx)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := r.Context(), (error)(nil)
			if cookies := r.CookiesNamed(s.Cookie.Name); len(cookies) == 1 {
				ctx, err = s.loadOrCreate(ctx, cookies[0].Value)
				if err != nil {
					s.ErrorHandler(w, r, err)
					return
				}
			} else {
				ctx = s.create(ctx)
			}
			r = r.WithContext(ctx)
			sw := &sessionWriter[T]{
				ResponseWriter: w,
				req:            r,
				session:        s,
			}
			next.ServeHTTP(sw, r)
			if !sw.saved && !sw.failed {
				panic("unreachable")
			}
		})
	}
}

type sessionWriter[T any] struct {
	http.ResponseWriter
	req     *http.Request
	session *Session[T]
	saved   bool
	failed  bool
}

func (w *sessionWriter[T]) Write(b []byte) (int, error) {
	if w.failed {
		return len(b), nil
	}
	if !w.saved {
		if err := w.session.save(w.req.Context(), w.ResponseWriter); err != nil {
			w.session.ErrorHandler(w.ResponseWriter, w.req, err)
			w.failed = true
			return len(b), nil
		}
		w.saved = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *sessionWriter[T]) WriteHeader(code int) {
	if w.failed {
		return
	}
	if !w.saved {
		if err := w.session.save(w.req.Context(), w.ResponseWriter); err != nil {
			w.session.ErrorHandler(w.ResponseWriter, w.req, err)
			w.failed = true
			return
		}
		w.saved = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *sessionWriter[T]) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

type recordContextKey struct{}

func newContextWithRecord(ctx context.Context, r *Record) context.Context {
	return context.WithValue(ctx, recordContextKey{}, r)
}

func recordFromContext(ctx context.Context) *Record {
	r, _ := ctx.Value(recordContextKey{}).(*Record)
	if r == nil {
		panic("sessions: middleware is not used")
	}
	return r
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (s *Session[T]) loadOrCreate(ctx context.Context, token string) (context.Context, error) {
	r, err := s.Store.Load(ctx, token)
	if err != nil {
		return nil, err
	} else if r == nil {
		// not found
		return s.create(ctx), nil
	}

	if r.session, err = s.Codec.Decode(r.Data); err != nil {
		return nil, err
	}
	return newContextWithRecord(ctx, r), nil
}

func (s *Session[T]) save(ctx context.Context, w http.ResponseWriter) (err error) {
	r := recordFromContext(ctx)

	if r.session == nil {
		// Delete was called; delete the cookie
		cookie := s.Cookie
		cookie.Value = ""
		cookie.MaxAge = -1
		http.SetCookie(w, &cookie)
		return nil
	}

	r.IdleDeadline = time.Now().Add(s.IdleTimeout)
	if r.AbsoluteDeadline.Before(r.IdleDeadline) {
		r.IdleDeadline = r.AbsoluteDeadline
	}

	cookie := s.Cookie
	cookie.Value = r.Token
	cookie.MaxAge = int(time.Until(r.IdleDeadline).Seconds())

	if r.Data, err = s.Codec.Encode(r.session.(*T)); err != nil {
		return err
	}

	if err = s.Store.Save(ctx, r); err != nil {
		return err
	}

	http.SetCookie(w, &cookie)
	return nil
}

func (s *Session[T]) create(ctx context.Context) context.Context {
	r := &Record{
		Token:            randomToken(),
		AbsoluteDeadline: time.Now().Add(s.AbsoluteTimeout),
		session:          new(T),
	}
	return newContextWithRecord(ctx, r)
}

func (s *Session[T]) Get(ctx context.Context) *T {
	r := recordFromContext(ctx)
	return r.session.(*T)
}

func (*Session[T]) Token(ctx context.Context) string {
	r := recordFromContext(ctx)
	return r.Token
}

func (s *Session[T]) Delete(ctx context.Context) error {
	r := recordFromContext(ctx)
	if err := s.Store.Delete(ctx, r.Token); err != nil {
		return err
	}
	r.session = nil
	return nil
}

func (s *Session[T]) Renew(ctx context.Context) error {
	return s.renewToken(ctx, randomToken())
}

func (s *Session[T]) RenewToken(ctx context.Context, token string) error {
	return s.renewToken(ctx, token)
}

func (s *Session[T]) renewToken(ctx context.Context, token string) error {
	r := recordFromContext(ctx)
	err := s.Store.Delete(ctx, r.Token)
	if err != nil {
		return err
	}

	r.Token = token
	r.AbsoluteDeadline = time.Now().Add(s.AbsoluteTimeout)
	return nil
}

func (s *Session[T]) deleteExpiredInterval(ctx context.Context) {
	if s.CleanupInterval <= 0 {
		return
	}
	go func() {
		c := time.Tick(s.CleanupInterval)
		for {
			select {
			case <-c:
				if err := s.Store.DeleteExpired(ctx); err != nil {
					slog.ErrorContext(ctx, fmt.Sprintf("sessions: %v", err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// func (s *Session[T]) DeleteExpired(ctx context.Context) error {
// 	return s.Store.DeleteExpired(ctx)
// }
