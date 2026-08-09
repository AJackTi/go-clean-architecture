// Command healthcheck probes the local application's readiness endpoint.
// It exists as a static binary because the runtime image intentionally has no
// shell or package manager.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

const timeout = 2 * time.Second

func main() {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		fail(fmt.Errorf("HTTP_PORT must be between 1 and 65535"))
	}
	port = strconv.Itoa(portNumber)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// The hostname is fixed to loopback and the only variable component is a
	// range-checked integer port, so this request cannot target another host.
	//nolint:gosec // G704: intentional local-only health probe.
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://"+net.JoinHostPort("127.0.0.1", port)+"/api/healthz",
		nil,
	)
	if err != nil {
		fail(err)
	}
	//nolint:gosec // G704: request URL is constrained to validated loopback above.
	response, err := (&http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}).Do(request)
	if err != nil {
		fail(err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK {
		fail(fmt.Errorf("readiness returned HTTP %d", response.StatusCode))
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
	os.Exit(1)
}
