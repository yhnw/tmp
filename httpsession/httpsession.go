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
	// If found, it returns true and nil.
	// If not found, it returns false and nil.
	Load(ctx context.Context, id string, ret *Record[T]) (found bool, err error)

	// Save saves a session record r.
	Save(ctx context.Context, r *Record[T]) error

	// Delete deletes a session record associated with id.
	Delete(ctx context.Context, id string) error

	// DeleteExpired deletes all expired session records.
	DeleteExpired(ctx context.Context) error
}

// Record holds information about an HTTP session.
type Record[T any] struct {
	dirty   bool
	deleted bool

	ID               string
	IdleDeadline     time.Time
	AbsoluteDeadline time.Time
	Session          T
}

func (r *Record[T]) init(deadline time.Time) {
	var zero T
	r.ID = rand.Text()
	r.IdleDeadline = time.Time{} // just in case
	r.AbsoluteDeadline = deadline
	r.Session = zero
}

func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "httpsession: "+err.Error())
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// SessionStore is a net/http middleware that automatically tracks HTTP sessions.
type SessionStore[T any] struct {
	// IdleTimeout defines the amount of time a session will remain active.
	IdleTimeout time.Duration
	// AbsoluteTimeout defines the maximum amount of time a session can be active.
	// See https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/Session_Management_Cheat_Sheet.md#absolute-timeout
	AbsoluteTimeout time.Duration
	// SetCookie is used as a template for a Set-Cookie header.
	SetCookie    http.Cookie
	Store        Store[T]
	ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

	active     sync.Map         // string -> struct{}
	now        func() time.Time // for tests
	recordPool sync.Pool
}

// https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/Session_Management_Cheat_Sheet.md#session-id-name-fingerprinting

const DefaultCookieName = "id"

// New returns a new instance of [SessionStore] with default settings.
func New[T any]() *SessionStore[T] {
	return &SessionStore[T]{
		IdleTimeout:     24 * time.Hour,
		AbsoluteTimeout: 7 * 24 * time.Hour,
		Store:           newMemoryStore[T](),
		ErrorHandler:    defaultErrorHandler,
		SetCookie: http.Cookie{
			Name:     DefaultCookieName,
			Path:     "/",
			Domain:   "",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		},
		now: time.Now,
		recordPool: sync.Pool{
			New: func() any {
				return new(Record[T])
			},
		},
	}
}

// Handler returns a middleware that automatically tracks HTTP sessions.
// After it was called, m's fields must not be mutated.
func (m *SessionStore[T]) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := m.getRecord()
		defer m.putRecord(record)

		var found bool
		var err error
		if cookies := r.CookiesNamed(m.SetCookie.Name); len(cookies) == 1 {
			found, err = m.Store.Load(r.Context(), cookies[0].Value, record)
			if err != nil {
				m.ErrorHandler(w, r, err)
				return
			}
			// if found && record.IdleDeadline.Before(m.now()) {
			// 	found = false
			// }
		}
		if !found {
			record.init(m.now().Add(m.AbsoluteTimeout))
		}

		if _, loaded := m.active.LoadOrStore(record.ID, struct{}{}); loaded {
			m.ErrorHandler(w, r, errors.New("httpsession: active session alreadly exists"))
			return
		}
		defer m.active.Delete(record.ID)

		ctx := m.newContextWithRecord(r.Context(), record)
		r = r.WithContext(ctx)
		ss := &sessionSaver[T]{
			ResponseWriter: w,
			req:            r,
			mw:             m,
		}
		next.ServeHTTP(ss, r)

		if !ss.done && !ss.failed {
			if err = m.ensureSave(r.Context()); err != nil {
				slog.ErrorContext(ctx, "httpsession: failed to save a record: "+err.Error())
			}
		}
	})
}

type sessionSaver[T any] struct {
	http.ResponseWriter
	req *http.Request
	mw  *SessionStore[T]

	done   bool
	failed bool
}

func (w *sessionSaver[T]) Write(b []byte) (int, error) {
	if w.failed {
		panic("httpsession: (ResponseWriter).Write was called after a call to ErrorHandler")
	}
	if !w.done {
		if err := w.mw.save(w.req.Context(), w.ResponseWriter); err != nil {
			w.mw.ErrorHandler(w.ResponseWriter, w.req, err)
			w.failed = true
			return 0, err
		}
		w.done = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *sessionSaver[T]) WriteHeader(code int) {
	if w.failed {
		panic("httpsession: (ResponseWriter).WriteHeader was called after a call to ErrorHandler")
	}
	if !w.done {
		if err := w.mw.save(w.req.Context(), w.ResponseWriter); err != nil {
			w.mw.ErrorHandler(w.ResponseWriter, w.req, err)
			w.failed = true
			return
		}
		w.done = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *sessionSaver[T]) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (m *SessionStore[T]) ensureSave(ctx context.Context) error {
	record := m.recordFromContext(ctx)
	if !record.dirty || record.deleted {
		return nil
	}
	return m.saveRecord(ctx, record)
}

func (m *SessionStore[T]) save(ctx context.Context, w http.ResponseWriter) error {
	record := m.recordFromContext(ctx)
	if !record.dirty {
		// no-op
	} else if record.deleted {
		m.deleteCookie(w)
	} else {
		if err := m.saveRecord(ctx, record); err != nil {
			return err
		}
		m.setCookie(w, record)
	}
	return nil
}

type recordContextKey[T any] struct{}

func (m *SessionStore[T]) newContextWithRecord(ctx context.Context, r *Record[T]) context.Context {
	return context.WithValue(ctx, recordContextKey[T]{}, r)
}

func (m *SessionStore[T]) recordFromContext(ctx context.Context) *Record[T] {
	r, _ := ctx.Value(recordContextKey[T]{}).(*Record[T])
	if r == nil {
		panic("httpsession: middleware was not used")
	}
	return r
}

func (m *SessionStore[T]) setCookie(w http.ResponseWriter, r *Record[T]) {
	cookie := m.SetCookie
	cookie.Value = r.ID
	cookie.MaxAge = int(r.IdleDeadline.Sub(m.now()).Seconds())
	http.SetCookie(w, &cookie)
}

func (m *SessionStore[T]) deleteCookie(w http.ResponseWriter) {
	cookie := m.SetCookie
	cookie.MaxAge = -1
	http.SetCookie(w, &cookie)
}

// If session was deleted, it returns record (session == nil) and nil.
func (m *SessionStore[T]) saveRecord(ctx context.Context, r *Record[T]) error {
	r.IdleDeadline = m.now().Add(m.IdleTimeout)
	if r.AbsoluteDeadline.Before(r.IdleDeadline) {
		r.IdleDeadline = r.AbsoluteDeadline
	}
	return m.Store.Save(ctx, r)
}

func (m *SessionStore[T]) getRecord() *Record[T] {
	r := m.recordPool.Get().(*Record[T])
	r.deleted = false
	r.dirty = false
	return r
}

func (m *SessionStore[T]) putRecord(r *Record[T]) {
	m.recordPool.Put(r)
}

func (m *SessionStore[T]) Get(ctx context.Context) *T {
	r := m.recordFromContext(ctx)
	if r.deleted {
		panic("httpsession: session alreadly deleted")
	}
	r.dirty = true
	return &r.Session
}

func (m *SessionStore[T]) Read(ctx context.Context) *T {
	r := m.recordFromContext(ctx)
	if r.deleted {
		panic("httpsession: session alreadly deleted")
	}
	return &r.Session
}

func (m *SessionStore[T]) ID(ctx context.Context) string {
	r := m.recordFromContext(ctx)
	return r.ID
}

func (m *SessionStore[T]) Delete(ctx context.Context) error {
	r := m.recordFromContext(ctx)
	if err := m.Store.Delete(ctx, r.ID); err != nil {
		return err
	}
	r.deleted = true
	r.dirty = true
	return nil
}

// It is caller's responsibility to choose a unique id.

func (m *SessionStore[T]) Renew(ctx context.Context, id string) error {
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
	r.dirty = true
	return nil
}

func (m *SessionStore[T]) Cleanup(ctx context.Context, interval time.Duration) {
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

// func (m *SessionStore[T]) saveSession(ctx context.Context, w http.ResponseWriter, record *Record[T]) error {
// 	// r, err := m.saveRecord(ctx)
// 	// if err != nil {
// 	// 	return err
// 	// }
// 	if record.deleted {
// 		m.deleteCookie(w)
// 	} else {
// 		if err := m.saveRecord(ctx, record); err != nil {
// 			return err
// 		}
// 		m.setCookie(w, record)
// 	}
// 	return nil
// }
