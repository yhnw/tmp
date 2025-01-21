// Package sessions provides a net/http middleware that tracks HTTP sessions using HTTP cookies.
package sessions

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	AbsoluteTimeout time.Duration
	CleanupInterval time.Duration
	// Cookie is used as a template for a Set-Cookie header.
	Cookie       http.Cookie
	Store        Store
	Codec        Codec[T]
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

type Middleware[T any] struct {
	idleTimeout time.Duration
	// AbsoluteTimeout defines the maximum amount of time a session can be active.
	absoluteTimeout time.Duration
	cookie          http.Cookie
	store           Store
	codec           Codec[T]
	errorHandler    func(w http.ResponseWriter, r *http.Request, err error)

	activeSession sync.Map // string -> struct{}
}

// Session represents an HTTP session.
// type Session[T any] struct {
// 	// IdleTimeout defines the amount of time a session will remain active.
// 	IdleTimeout time.Duration
// 	// AbsoluteTimeout defines the maximum amount of time a session can be active.
// 	AbsoluteTimeout time.Duration
// 	CleanupInterval time.Duration
// 	// Cookie is used as a template for a Set-Cookie header.
// 	Cookie       http.Cookie
// 	Store        Store
// 	Codec        Codec[T]
// 	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// 	activeSession sync.Map // string -> struct{}
// }

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

// New returns a new instance of Session with default settings.
// func New[T any]() *Session[T] {
// 	return &Session[T]{
// 		IdleTimeout:     24 * time.Hour,
// 		AbsoluteTimeout: 7 * 24 * time.Hour,
// 		CleanupInterval: 1 * time.Hour,
// 		Cookie: http.Cookie{
// 			Name:     "SESSION",
// 			Path:     "/",
// 			Domain:   "",
// 			HttpOnly: true,
// 			Secure:   true,
// 			SameSite: http.SameSiteLaxMode,
// 		},
// 		Store:        newMemoryStore(),
// 		Codec:        JSONCodec[T]{},
// 		ErrorHandler: defaultErrorHandler,
// 	}
// }

func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), fmt.Sprintf("sessions: %v", err))
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func NewMiddleware[T any](ctx context.Context, cfg Config[T]) *Middleware[T] {
	mw := &Middleware[T]{
		idleTimeout:     cfg.IdleTimeout,
		absoluteTimeout: cfg.AbsoluteTimeout,
		store:           cfg.Store,
		codec:           cfg.Codec,
		errorHandler:    cfg.ErrorHandler,
		cookie: http.Cookie{
			Name:        cmp.Or(cfg.Cookie.Name, "SESSIONID"),
			Path:        cmp.Or(cfg.Cookie.Path, "/"),
			Domain:      cmp.Or(cfg.Cookie.Domain, ""),
			HttpOnly:    cmp.Or(cfg.Cookie.HttpOnly, true),
			Secure:      cmp.Or(cfg.Cookie.Secure, true),
			SameSite:    cmp.Or(cfg.Cookie.SameSite, http.SameSiteLaxMode),
			Partitioned: cmp.Or(cfg.Cookie.Partitioned, false),
		},
	}
	if cfg.IdleTimeout <= 0 {
		mw.idleTimeout = 24 * time.Hour
	}
	if cfg.AbsoluteTimeout <= 0 {
		mw.absoluteTimeout = 7 * 24 * time.Hour
	}
	if cfg.Store == nil {
		mw.store = newMemoryStore()
	}
	if cfg.Codec == nil {
		mw.codec = JSONCodec[T]{}
	}
	if cfg.ErrorHandler == nil {
		mw.errorHandler = defaultErrorHandler
	}
	cleanupInterval(ctx, mw.store, cfg.CleanupInterval)
	return mw
}

func (m *Middleware[T]) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		if cookies := r.CookiesNamed(m.cookie.Name); len(cookies) == 1 {
			id = cookies[0].Value
		}
		record, err := m.loadOrCreate(r.Context(), id)
		if err != nil {
			m.errorHandler(w, r, err)
			return
		}

		if _, exits := m.activeSession.LoadOrStore(record.ID, struct{}{}); exits {
			m.errorHandler(w, r, errors.New("another active session exists"))
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

// Middleware returns a middleware that automatically tracks HTTP sessions.
// After it was called, Session's fields must not be mutated.
// If s.CleanupInterval > 0, it also starts a goroutine that deletes expired sessions
// after each CleanupInterval.
// func (m *Middleware[T]) Middleware(ctx context.Context) func(next http.Handler) http.Handler {
// 	s.deleteExpiredInterval(ctx)

// 	return func(next http.Handler) http.Handler {
// 		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 			token := ""
// 			if cookies := r.CookiesNamed(s.Cookie.Name); len(cookies) == 1 {
// 				token = cookies[0].Value
// 			}
// 			record, err := s.loadOrCreate(r.Context(), token)
// 			if err != nil {
// 				s.ErrorHandler(w, r, err)
// 				return
// 			}

// 			if _, exits := s.activeSession.LoadOrStore(record.Token, struct{}{}); exits {
// 				s.ErrorHandler(w, r, errors.New("another active session exists"))
// 				return
// 			}
// 			defer s.activeSession.Delete(record.Token)

// 			ctx := s.newContextWithRecord(r.Context(), record)
// 			r = r.WithContext(ctx)

// 			sw := &sessionWriter[T]{
// 				ResponseWriter: w,
// 				req:            r,
// 				session:        s,
// 			}
// 			next.ServeHTTP(sw, r)
// 			if !sw.saved && !sw.failed {
// 				panic("unreachable")
// 			}
// 		})
// 	}
// }

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
			w.mw.errorHandler(w.ResponseWriter, w.req, err)
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
			w.mw.errorHandler(w.ResponseWriter, w.req, err)
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

func randomID() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (m *Middleware[T]) loadOrCreate(ctx context.Context, id string) (*Record, error) {
	if id == "" {
		return m.newRecord(), nil
	}
	r, err := m.store.Load(ctx, id)
	if err != nil {
		return nil, err
	} else if r == nil {
		// not found
		return m.newRecord(), nil
	}

	if r.session, err = m.codec.Decode(r.Data); err != nil {
		return nil, err
	}
	return r, nil
}

func (m *Middleware[T]) save(ctx context.Context, w http.ResponseWriter) (err error) {
	r := m.recordFromContext(ctx)

	if r.session == nil {
		// Delete was called; delete the cookie
		cookie := m.cookie
		cookie.MaxAge = -1
		http.SetCookie(w, &cookie)
		return nil
	}

	r.IdleDeadline = time.Now().Add(m.idleTimeout)
	if r.AbsoluteDeadline.Before(r.IdleDeadline) {
		r.IdleDeadline = r.AbsoluteDeadline
	}

	cookie := m.cookie
	cookie.Value = r.ID
	cookie.MaxAge = int(time.Until(r.IdleDeadline).Seconds())

	if r.Data, err = m.codec.Encode(r.session.(*T)); err != nil {
		return err
	}

	if err = m.store.Save(ctx, r); err != nil {
		return err
	}

	http.SetCookie(w, &cookie)
	return nil
}

func (m *Middleware[T]) newRecord() *Record {
	r := &Record{
		ID:               randomID(),
		AbsoluteDeadline: time.Now().Add(m.absoluteTimeout),
		session:          new(T),
	}
	return r
}

func (m *Middleware[T]) Get(ctx context.Context) *T {
	r := m.recordFromContext(ctx)
	return r.session.(*T)
}

func (m *Middleware[T]) Token(ctx context.Context) string {
	r := m.recordFromContext(ctx)
	return r.ID
}

func (m *Middleware[T]) Delete(ctx context.Context) error {
	r := m.recordFromContext(ctx)
	if err := m.store.Delete(ctx, r.ID); err != nil {
		return err
	}
	r.session = nil
	return nil
}

func (m *Middleware[T]) Renew(ctx context.Context) error {
	return m.renewID(ctx, randomID())
}

// It is caller's responsibility to choose a unique id.
func (m *Middleware[T]) RenewID(ctx context.Context, id string) error {
	return m.renewID(ctx, id)
}

func (m *Middleware[T]) renewID(ctx context.Context, id string) error {
	r := m.recordFromContext(ctx)
	err := m.store.Delete(ctx, r.ID)
	if err != nil {
		return err
	}

	r.ID = id
	r.AbsoluteDeadline = time.Now().Add(m.absoluteTimeout)
	return nil
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
