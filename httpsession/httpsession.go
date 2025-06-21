// Package httpsession provides a net/http middleware that tracks HTTP sessions using HTTP cookies.
package httpsession

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Store is the interface that stores session records.
type Store[T any] interface {
	// Load loads a session record associated with id.
	// If found, it returns that record and nil.
	// If not found, it returns nil, nil.
	Load(ctx context.Context, id string, ret *Record[T]) (found bool, err error)

	// Save saves r.
	Save(ctx context.Context, r *Record[T]) error

	// Delete deletes a session record associated with id.
	Delete(ctx context.Context, id string) error

	// DeleteExpired deletes all expired session records.
	DeleteExpired(ctx context.Context) error
}

// Record holds information about an HTTP session.
type Record[T any] struct {
	ID               string
	IdleDeadline     time.Time
	AbsoluteDeadline time.Time
	Session          T

	deleted bool
}

func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "httpsession: "+err.Error())
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// Middleware is a net/http middleware that automatically tracks HTTP sessions.
type Middleware[T any] struct {
	// IdleTimeout defines the amount of time a session will remain active.
	IdleTimeout time.Duration
	// AbsoluteTimeout defines the maximum amount of time a session can be active.
	// See https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/Session_Management_Cheat_Sheet.md#absolute-timeout
	AbsoluteTimeout time.Duration
	// Cookie is used as a template for a Set-Cookie header.
	Cookie       http.Cookie
	Store        Store[T]
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

	activeSession sync.Map         // string -> struct{}
	now           func() time.Time // for tests
	pool          sync.Pool
}

// New returns a new instance of [Middleware] with default settings.
func New[T any](store Store[T]) *Middleware[T] {
	return &Middleware[T]{
		IdleTimeout:     24 * time.Hour,
		AbsoluteTimeout: 7 * 24 * time.Hour,
		Store:           store,
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
		pool: sync.Pool{
			New: func() any {
				return new(Record[T])
			},
		},
	}
}

// Handler returns a middleware that automatically tracks HTTP sessions.
// After it was called, m's fields must not be mutated.
func (m *Middleware[T]) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			record *Record[T]
			found  bool
			err    error
		)
		record = m.pool.Get().(*Record[T])
		*record = Record[T]{} // just in case
		defer m.pool.Put(record)
		if cookies := r.CookiesNamed(m.Cookie.Name); len(cookies) == 1 {
			found, err = m.Store.Load(r.Context(), cookies[0].Value, record)
			if err != nil {
				m.ErrorHandler(w, r, err)
				return
			}
		}
		if !found {
			record.ID = rand.Text()
			record.AbsoluteDeadline = m.now().Add(m.AbsoluteTimeout)
			record.deleted = false
		}

		if _, loaded := m.activeSession.LoadOrStore(record.ID, struct{}{}); loaded {
			m.ErrorHandler(w, r, errors.New("httpsession: active session alreadly exists"))
			return
		}
		defer m.activeSession.Delete(record.ID)

		// if found {
		// 	var session T
		// 	if err := json.Unmarshal(record.Data, &session); err != nil {
		// 		m.ErrorHandler(w, r, err)
		// 		return
		// 	}
		// 	record.session = &session
		// }

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
		}
	})
}

type sessionWriter[T any] struct {
	http.ResponseWriter
	req *http.Request
	mw  *Middleware[T]

	saved  bool
	failed bool
}

func (w *sessionWriter[T]) Write(b []byte) (int, error) {
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

func (m *Middleware[T]) newContextWithRecord(ctx context.Context, r *Record[T]) context.Context {
	return context.WithValue(ctx, recordContextKey[T]{}, r)
}

func (m *Middleware[T]) recordFromContext(ctx context.Context) *Record[T] {
	r, _ := ctx.Value(recordContextKey[T]{}).(*Record[T])
	if r == nil {
		panic("httpsession: middleware was not used")
	}
	return r
}

func (m *Middleware[T]) saveSession(ctx context.Context, w http.ResponseWriter) error {
	r, err := m.saveRecord(ctx)
	if err != nil {
		return err
	}
	if r == nil {
		m.deleteCookie(w)
	} else {
		m.setCookie(w, r)
	}
	return nil
}

func (m *Middleware[T]) setCookie(w http.ResponseWriter, r *Record[T]) {
	cookie := m.Cookie
	cookie.Value = r.ID
	cookie.MaxAge = int(r.IdleDeadline.Sub(m.now()).Seconds())
	http.SetCookie(w, &cookie)
}

func (m *Middleware[T]) deleteCookie(w http.ResponseWriter) {
	cookie := m.Cookie
	cookie.MaxAge = -1
	http.SetCookie(w, &cookie)
}

// If session was deleted, it returns record (session == nil) and nil.
func (m *Middleware[T]) saveRecord(ctx context.Context) (_ *Record[T], err error) {
	r := m.recordFromContext(ctx)
	if r.deleted {
		// session was deleted
		return nil, nil
	}

	r.IdleDeadline = m.now().Add(m.IdleTimeout)
	if r.AbsoluteDeadline.Before(r.IdleDeadline) {
		r.IdleDeadline = r.AbsoluteDeadline
	}

	// if r.Data, err = json.Marshal(r.session.(*T)); err != nil {
	// 	return nil, err
	// }
	return r, m.Store.Save(ctx, r)
}

// func (m *Middleware[T]) newRecord() *Record[T] {
// 	r := m.pool.Get().(*Record[T])
// 	r.ID = rand.Text()
// 	r.AbsoluteDeadline = m.now().Add(m.AbsoluteTimeout)
// 	return r
// }

// func (m *Middleware[T]) putRecord(r *Record[T]) {
// 	m.pool.Put(r)
// }

// func (m *Middleware[T]) Populate(idSessionPairs ...any) {
// 	if l := len(idSessionPairs); l <= 0 || l%2 != 0 {
// 		panic("Populate: args must have non-zero even length")
// 	}
// 	argPos := 1
// 	for pair := range slices.Chunk(idSessionPairs, 2) {
// 		id, ok := pair[0].(string)
// 		if !ok {
// 			panic(fmt.Sprintf("Populate: arg %v expected string but got %T", argPos, pair[0]))
// 		}
// 		// session, ok := pair[1].(*T)
// 		if !ok {
// 			panic(fmt.Sprintf("Populate: arg %v expected %T but got %T", argPos+1, new(T), pair[1]))
// 		}
// 		record := m.newRecord()
// 		record.ID = id
// 		// record.session = session
// 		ctx := m.newContextWithRecord(context.Background(), record)
// 		if _, err := m.saveRecord(ctx); err != nil {
// 			panic("Populate: " + err.Error())
// 		}
// 		argPos += 2
// 	}
// }

func (m *Middleware[T]) Get(ctx context.Context) *T {
	r := m.recordFromContext(ctx)
	if r.deleted {
		panic("httpsession: session alreadly deleted")
	}
	return &r.Session
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
	var zero T
	r.Session = zero
	r.deleted = true
	return nil
}

// It is caller's responsibility to choose a unique id.

func (m *Middleware[T]) Renew(ctx context.Context, id string) error {
	r := m.recordFromContext(ctx)
	err := m.Store.Delete(ctx, r.ID)
	if err != nil {
		return err
	}

	if id == "" {
		id = rand.Text()
	}
	r.ID = id
	r.AbsoluteDeadline = m.now().Add(m.AbsoluteTimeout)
	return nil
}

func (m *Middleware[T]) DeleteExpiredInterval(ctx context.Context, interval time.Duration) {
	cleanup := func() {
		c := time.Tick(interval)
		for {
			select {
			case <-c:
				if err := m.Store.DeleteExpired(ctx); err != nil {
					slog.ErrorContext(ctx, "httpsession.DeleteExpiredInterval: "+err.Error())
				}
			case <-ctx.Done():
				return
			}
		}
	}
	go cleanup()
}

// func (m *Middleware[T]) DeleteExpiredInterval(ctx context.Context, interval time.Duration, errorHandler func(error)) {
// 	if errorHandler == nil {
// 		errorHandler = func(err error) {
// 			slog.ErrorContext(ctx, "httpsession.DeleteExpiredInterval: "+err.Error())
// 		}
// 	}
// 	cleanup := func() {
// 		c := time.Tick(interval)
// 		for {
// 			select {
// 			case <-c:
// 				if err := m.Store.DeleteExpired(ctx); err != nil {
// 					errorHandler(err)
// 				}
// 			case <-ctx.Done():
// 				return
// 			}
// 		}
// 	}
// 	go cleanup()
// }
