// Package sessions provides a net/http middleware that tracks HTTP sessions using HTTP cookies.
package sessions

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Store is the interface that stores session records.
type Store interface {
	// Load loads a session record associated with id.
	// If found, it returns that record and nil.
	// If not found, it returns nil, nil.
	Load(ctx context.Context, id string) (*Record, error)

	// Save saves r.
	Save(ctx context.Context, r *Record) error

	// Delete deletes a session record associated with id.
	Delete(ctx context.Context, id string) error

	// DeleteExpired deletes all expired session records.
	DeleteExpired(ctx context.Context) error
}

// Record holds information about an HTTP session.
type Record struct {
	ID               string
	IdleDeadline     time.Time
	AbsoluteDeadline time.Time
	Data             []byte

	session any // *T
}

func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), fmt.Sprintf("sessions: %v", err))
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// Middleware is a net/http middleware that automatically tracks HTTP sessions.
type Middleware[T any] struct {
	// IdleTimeout defines the amount of time a session will remain active.
	IdleTimeout time.Duration
	// AbsoluteTimeout defines the maximum amount of time a session can be active.
	// See https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/Session_Management_Cheat_Sheet.md#absolute-timeout
	AbsoluteTimeout time.Duration
	CleanupInterval time.Duration
	// Cookie is used as a template for a Set-Cookie header.
	Cookie       http.Cookie
	Store        Store
	Codec        Codec[T]
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

	activeSession sync.Map // string -> struct{}
	now           func() time.Time
}

// NewMiddleware returns a new instance of [Middleware] with default settings.
func NewMiddleware[T any]() *Middleware[T] {
	return &Middleware[T]{
		IdleTimeout:     24 * time.Hour,
		AbsoluteTimeout: 7 * 24 * time.Hour,
		Store:           newMemoryStore(),
		Codec:           JSONCodec[T]{},
		ErrorHandler:    defaultErrorHandler,
		Cookie: http.Cookie{
			Name:        "id",
			Path:        "/",
			Domain:      "",
			HttpOnly:    true,
			Secure:      true,
			SameSite:    http.SameSiteLaxMode,
			Partitioned: false,
		},
		now: time.Now,
	}
}

func (m *Middleware[T]) DeleteExpiredInterval(ctx context.Context, interval time.Duration) {
	cleanup := func() {
		c := time.Tick(interval)
		for {
			select {
			case <-c:
				if err := m.Store.DeleteExpired(ctx); err != nil {
					slog.ErrorContext(ctx, err.Error())
				}
			case <-ctx.Done():
				return
			}
		}
	}
	go cleanup()
}

// Handler returns a middleware that automatically tracks HTTP sessions.
// After it was called, Session's fields must not be mutated.
// If s.CleanupInterval > 0, it also starts a goroutine that deletes expired sessions
// after each CleanupInterval.
func (m *Middleware[T]) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		if cookies := r.CookiesNamed(m.Cookie.Name); len(cookies) == 1 {
			id = cookies[0].Value
		}
		record, err := m.loadOrCreate(r.Context(), id)
		if err != nil {
			m.ErrorHandler(w, r, err)
			return
		}

		if _, loaded := m.activeSession.LoadOrStore(record.ID, struct{}{}); loaded {
			m.ErrorHandler(w, r, errors.New("active session alreadly exists"))
			return
		}
		defer m.activeSession.Delete(record.ID)

		ctx := m.newContextWithRecord(r.Context(), record)
		r = r.WithContext(ctx)

		sw := &sessionWriter[T]{
			ResponseWriter: w,
			req:            r,
			mw:             m,
		}
		next.ServeHTTP(sw, r)

		if !sw.saved && !sw.failed {
			if _, err := m.saveRecord(r.Context()); err != nil {
				m.ErrorHandler(w, r, err)
			}
			// panic("unreachable")
		}
	})
}

type sessionWriter[T any] struct {
	http.ResponseWriter
	req *http.Request
	mw  *Middleware[T]

	mu     sync.Mutex // necessary?
	saved  bool
	failed bool
}

func (w *sessionWriter[T]) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.failed {
		return len(b), nil
	}
	if !w.saved {
		if err := w.mw.saveSession(w.req.Context(), w.ResponseWriter); err != nil {
			w.mw.ErrorHandler(w.ResponseWriter, w.req, err)
			w.failed = true
			return len(b), nil
		}
		w.saved = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *sessionWriter[T]) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.failed {
		return
	}
	if !w.saved {
		if err := w.mw.saveSession(w.req.Context(), w.ResponseWriter); err != nil {
			w.mw.ErrorHandler(w.ResponseWriter, w.req, err)
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

type recordContextKey[T any] struct{}

func (m *Middleware[T]) newContextWithRecord(ctx context.Context, r *Record) context.Context {
	return context.WithValue(ctx, recordContextKey[T]{}, r)
}

func (m *Middleware[T]) recordFromContext(ctx context.Context) *Record {
	r, _ := ctx.Value(recordContextKey[T]{}).(*Record)
	if r == nil {
		panic("sessions: middleware is not used")
	}
	return r
}

func (m *Middleware[T]) loadOrCreate(ctx context.Context, id string) (*Record, error) {
	if id == "" {
		return m.newRecord(), nil
	}
	r, err := m.Store.Load(ctx, id)
	if err != nil {
		return nil, err
	} else if r == nil {
		// not found
		return m.newRecord(), nil
	}

	if s, err := m.Codec.Decode(r.Data); err != nil {
		return nil, err
	} else {
		r.session = s
	}
	return r, nil
}

func (m *Middleware[T]) saveSession(ctx context.Context, w http.ResponseWriter) error {
	r, err := m.saveRecord(ctx)
	if err != nil {
		return err
	}

	if r.session == nil {
		// Delete was called; delete the cookie
		cookie := m.Cookie
		cookie.MaxAge = -1
		http.SetCookie(w, &cookie)
		return nil
	}

	cookie := m.Cookie
	cookie.Value = r.ID
	cookie.MaxAge = int(r.IdleDeadline.Sub(m.now()).Seconds())
	http.SetCookie(w, &cookie)
	return nil
}

// If session was deleted, it returns record (session == nil) and nil.
func (m *Middleware[T]) saveRecord(ctx context.Context) (_ *Record, err error) {
	r := m.recordFromContext(ctx)
	if r.session == nil {
		// session was deleted
		return r, nil
	}

	r.IdleDeadline = m.now().Add(m.IdleTimeout)
	if r.AbsoluteDeadline.Before(r.IdleDeadline) {
		r.IdleDeadline = r.AbsoluteDeadline
	}

	if r.Data, err = m.Codec.Encode(r.session.(*T)); err != nil {
		return nil, err
	}
	return r, m.Store.Save(ctx, r)
}

func (m *Middleware[T]) newRecord() *Record {
	r := &Record{
		ID:               rand.Text(),
		AbsoluteDeadline: m.now().Add(m.AbsoluteTimeout),
	}
	r.session = new(T)
	return r
}

func (m *Middleware[T]) Get(ctx context.Context) *T {
	r := m.recordFromContext(ctx)
	if r.session == nil {
		panic("session alreadly deleted")
	}
	return r.session.(*T)
}

func (m *Middleware[T]) ID(ctx context.Context) string {
	r := m.recordFromContext(ctx)
	return r.ID
}

func (m *Middleware[T]) Delete(ctx context.Context) error {
	r := m.recordFromContext(ctx)
	if err := m.Store.Delete(ctx, r.ID); err != nil {
		return err
	}
	r.session = nil
	return nil
}

func (m *Middleware[T]) Renew(ctx context.Context) error {
	return m.renewID(ctx, rand.Text())
}

// It is caller's responsibility to choose a unique id.
func (m *Middleware[T]) RenewID(ctx context.Context, id string) error {
	return m.renewID(ctx, id)
}

func (m *Middleware[T]) renewID(ctx context.Context, id string) error {
	r := m.recordFromContext(ctx)
	err := m.Store.Delete(ctx, r.ID)
	if err != nil {
		return err
	}

	r.ID = id
	r.AbsoluteDeadline = m.now().Add(m.AbsoluteTimeout)
	return nil
}
