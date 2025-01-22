// Package sessions provides a net/http middleware that tracks HTTP sessions using HTTP cookies.
package sessions

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Session represents an HTTP session.
type Config[T any] struct {
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
}

// Store is the interface that stores session records.
type Store interface {
	// Load loads a session record associated with token.
	// If found, it returns that record and nil.
	// If not found, it returns nil, nil.
	Load(ctx context.Context, id string) (*Record, error)

	// Save saves r.
	Save(ctx context.Context, r *Record) error

	// Delete deletes a session record associated with token.
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

func cleanupInterval(ctx context.Context, store Store, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		c := time.Tick(interval)
		for {
			select {
			case <-c:
				if err := store.DeleteExpired(ctx); err != nil {
					slog.ErrorContext(ctx, fmt.Sprintf("sessions: %v", err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Middleware is a net/http middleware that automatically tracks HTTP sessions.
type Middleware[T any] struct {
	cfg           Config[T]
	activeSession sync.Map // string -> struct{}
}

// NewMiddleware returns a new instance of Middleware with default settings.
func NewMiddleware[T any](ctx context.Context, cfg Config[T]) *Middleware[T] {
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 24 * time.Hour
	}
	if cfg.AbsoluteTimeout <= 0 {
		cfg.AbsoluteTimeout = 7 * 24 * time.Hour
	}
	if cfg.Store == nil {
		cfg.Store = newMemoryStore()
	}
	if cfg.Codec == nil {
		cfg.Codec = JSONCodec[T]{}
	}
	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = defaultErrorHandler
	}
	cfg.Cookie = http.Cookie{
		Name:        cmp.Or(cfg.Cookie.Name, "SESSIONID"),
		Path:        cmp.Or(cfg.Cookie.Path, "/"),
		Domain:      cmp.Or(cfg.Cookie.Domain, ""),
		HttpOnly:    cmp.Or(cfg.Cookie.HttpOnly, true),
		Secure:      cmp.Or(cfg.Cookie.Secure, true),
		SameSite:    cmp.Or(cfg.Cookie.SameSite, http.SameSiteLaxMode),
		Partitioned: cmp.Or(cfg.Cookie.Partitioned, false),
	}
	cleanupInterval(ctx, cfg.Store, cfg.CleanupInterval)
	return &Middleware[T]{cfg: cfg}
}

// Middleware returns a middleware that automatically tracks HTTP sessions.
// After it was called, Session's fields must not be mutated.
// If s.CleanupInterval > 0, it also starts a goroutine that deletes expired sessions
// after each CleanupInterval.
func (m *Middleware[T]) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		if cookies := r.CookiesNamed(m.cfg.Cookie.Name); len(cookies) == 1 {
			id = cookies[0].Value
		}
		record, err := m.loadOrCreate(r.Context(), id)
		if err != nil {
			m.cfg.ErrorHandler(w, r, err)
			return
		}

		if _, exits := m.activeSession.LoadOrStore(record.ID, struct{}{}); exits {
			m.cfg.ErrorHandler(w, r, errors.New("another active session exists"))
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
			panic("unreachable")
		}
	})
}

type sessionWriter[T any] struct {
	http.ResponseWriter
	req    *http.Request
	mw     *Middleware[T]
	saved  bool
	failed bool
}

func (w *sessionWriter[T]) Write(b []byte) (int, error) {
	if w.failed {
		return len(b), nil
	}
	if !w.saved {
		if err := w.mw.save(w.req.Context(), w.ResponseWriter); err != nil {
			w.mw.cfg.ErrorHandler(w.ResponseWriter, w.req, err)
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
		if err := w.mw.save(w.req.Context(), w.ResponseWriter); err != nil {
			w.mw.cfg.ErrorHandler(w.ResponseWriter, w.req, err)
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

// TODO: replace this with rand.Text in go 1.24
func randomText() string {
	const base32alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	src := make([]byte, 26)
	if _, err := rand.Read(src); err != nil {
		panic(err)
	}
	for i := range src {
		src[i] = base32alphabet[src[i]%32]
	}
	return string(src)
}

func (m *Middleware[T]) loadOrCreate(ctx context.Context, id string) (*Record, error) {
	if id == "" {
		return m.newRecord(), nil
	}
	r, err := m.cfg.Store.Load(ctx, id)
	if err != nil {
		return nil, err
	} else if r == nil {
		// not found
		return m.newRecord(), nil
	}

	if r.session, err = m.cfg.Codec.Decode(r.Data); err != nil {
		return nil, err
	}
	return r, nil
}

func (m *Middleware[T]) save(ctx context.Context, w http.ResponseWriter) (err error) {
	r := m.recordFromContext(ctx)

	if r.session == nil {
		// Delete was called; delete the cookie
		cookie := m.cfg.Cookie
		cookie.MaxAge = -1
		http.SetCookie(w, &cookie)
		return nil
	}

	r.IdleDeadline = time.Now().Add(m.cfg.IdleTimeout)
	if r.AbsoluteDeadline.Before(r.IdleDeadline) {
		r.IdleDeadline = r.AbsoluteDeadline
	}

	cookie := m.cfg.Cookie
	cookie.Value = r.ID
	cookie.MaxAge = int(time.Until(r.IdleDeadline).Seconds())

	if r.Data, err = m.cfg.Codec.Encode(r.session.(*T)); err != nil {
		return err
	}

	if err = m.cfg.Store.Save(ctx, r); err != nil {
		return err
	}

	http.SetCookie(w, &cookie)
	return nil
}

func (m *Middleware[T]) newRecord() *Record {
	r := &Record{
		ID:               randomText(),
		AbsoluteDeadline: time.Now().Add(m.cfg.AbsoluteTimeout),
		session:          new(T),
	}
	return r
}

func (m *Middleware[T]) Get(ctx context.Context) *T {
	r := m.recordFromContext(ctx)
	return r.session.(*T)
}

func (m *Middleware[T]) ID(ctx context.Context) string {
	r := m.recordFromContext(ctx)
	return r.ID
}

func (m *Middleware[T]) Delete(ctx context.Context) error {
	r := m.recordFromContext(ctx)
	if err := m.cfg.Store.Delete(ctx, r.ID); err != nil {
		return err
	}
	r.session = nil
	return nil
}

func (m *Middleware[T]) Renew(ctx context.Context) error {
	return m.renewID(ctx, randomText())
}

// It is caller's responsibility to choose a unique id.
func (m *Middleware[T]) RenewID(ctx context.Context, id string) error {
	return m.renewID(ctx, id)
}

func (m *Middleware[T]) renewID(ctx context.Context, id string) error {
	r := m.recordFromContext(ctx)
	err := m.cfg.Store.Delete(ctx, r.ID)
	if err != nil {
		return err
	}

	r.ID = id
	r.AbsoluteDeadline = time.Now().Add(m.cfg.AbsoluteTimeout)
	return nil
}
