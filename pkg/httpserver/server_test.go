package httpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerLifecycleWithInjectedListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), Listener(listener), ShutdownTimeout(2*time.Second))
	if server.Addr() != listener.Addr().String() {
		t.Fatalf("Addr() = %q, want %q", server.Addr(), listener.Addr())
	}
	server.Start()
	server.Start() // idempotent

	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	for attempt := 0; attempt < 20; attempt++ {
		response, err = client.Get("http://" + listener.Addr().String())
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET server: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	if err := server.ShutdownContext(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-server.Notify(); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve error = %v, want http.ErrServerClosed", err)
	}
}

func TestShutdownRejectsNilContext(t *testing.T) {
	server := New(http.NotFoundHandler())
	if err := server.ShutdownContext(nilContext()); err == nil {
		t.Fatal("ShutdownContext(nil) succeeded")
	}
}

func nilContext() context.Context { return nil }
