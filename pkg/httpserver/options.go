package httpserver

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// Option customizes a Server before it starts.
type Option func(*Server)

// Listener injects an already-bound listener. It is useful when the caller
// needs bind-time errors before starting the serving goroutine and in tests.
// The Server takes ownership of the listener after Start.
func Listener(listener net.Listener) Option {
	return func(s *Server) {
		if listener != nil {
			s.listener = listener
		}
	}
}

// Port configures a TCP port while listening on all interfaces inside the
// process. Public exposure is controlled by the container/orchestrator.
func Port(port string) Option {
	return func(s *Server) {
		port = strings.TrimSpace(port)
		if _, err := strconv.Atoi(port); err != nil {
			return
		}
		s.server.Addr = net.JoinHostPort("", port)
	}
}

// ReadTimeout limits reading a request, including its headers.
func ReadTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.server.ReadTimeout = timeout
			s.server.ReadHeaderTimeout = timeout
		}
	}
}

// WriteTimeout limits writing a response.
func WriteTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.server.WriteTimeout = timeout
		}
	}
}

// IdleTimeout limits keep-alive idle connections.
func IdleTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.server.IdleTimeout = timeout
		}
	}
}

// ShutdownTimeout sets the timeout used by Shutdown.
func ShutdownTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.shutdownTimeout = timeout
		}
	}
}
