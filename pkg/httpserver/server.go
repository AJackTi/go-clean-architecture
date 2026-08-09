// Package httpserver provides a small, explicit lifecycle wrapper around
// net/http.Server.
package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	defaultReadTimeout     = 10 * time.Second
	defaultWriteTimeout    = 15 * time.Second
	defaultIdleTimeout     = 60 * time.Second
	defaultAddr            = ":80"
	defaultShutdownTimeout = 10 * time.Second
	defaultMaxHeaderBytes  = 1 << 20
)

// Server owns an HTTP server and exposes its terminal serve error. New does
// not start a goroutine; callers explicitly call Start so startup ordering
// and tests remain deterministic.
type Server struct {
	server          *http.Server
	notify          chan error
	shutdownTimeout time.Duration
	listener        net.Listener
	startOnce       sync.Once
}

// New constructs a server without starting it.
func New(handler http.Handler, opts ...Option) *Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	s := &Server{
		server: &http.Server{
			Handler:           handler,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
			Addr:              defaultAddr,
			MaxHeaderBytes:    defaultMaxHeaderBytes,
			ReadHeaderTimeout: defaultReadTimeout,
		},
		notify:          make(chan error, 1),
		shutdownTimeout: defaultShutdownTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Start begins serving. Calling Start more than once is safe; the second call
// is ignored because net/http.Server cannot be restarted after shutdown.
func (s *Server) Start() {
	if s == nil || s.server == nil {
		return
	}
	s.startOnce.Do(func() {
		go func() {
			var err error
			if s.listener != nil {
				err = s.server.Serve(s.listener)
			} else {
				err = s.server.ListenAndServe()
			}
			// The serving goroutine is the only sender and closes the channel,
			// so consumers can safely wait for a terminal result.
			s.notify <- err
			close(s.notify)
		}()
	})
}

// Notify returns the terminal error from ListenAndServe. A normal shutdown
// reports http.ErrServerClosed, matching net/http semantics.
func (s *Server) Notify() <-chan error {
	if s == nil {
		return nil
	}
	return s.notify
}

// Shutdown gracefully stops the server using the configured timeout.
func (s *Server) Shutdown() error {
	if s == nil || s.server == nil {
		return nil
	}
	timeout := s.shutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.ShutdownContext(ctx)
}

// ShutdownContext gracefully stops the server with a caller-owned context.
func (s *Server) ShutdownContext(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("httpserver: nil shutdown context")
	}
	return s.server.Shutdown(ctx)
}

// Addr returns the configured listen address.
func (s *Server) Addr() string {
	if s == nil || s.server == nil {
		return ""
	}
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.server.Addr
}
