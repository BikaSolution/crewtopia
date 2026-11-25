package main

import (
	"net"
	"net/http"
	"time"
)

// healthcheck backs the container's HEALTHCHECK: the runtime image is
// distroless (no shell, no curl), so the binary probes itself. It returns a
// process exit code: 0 if GET /healthz on the loopback interface answers 200,
// 1 otherwise. listenAddr is the server's listen address, e.g. ":8080"; only
// its port is used.
func healthcheck(listenAddr string) int {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return 1
	}

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
