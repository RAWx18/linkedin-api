// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"testing"
)

func TestClientIP(t *testing.T) {
	newReq := func(remote, xff string) *http.Request {
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	tests := []struct {
		name   string
		depth  int
		remote string
		xff    string
		want   string
	}{
		{"no proxy uses remote addr", 0, "203.0.113.5:4444", "", "203.0.113.5"},
		{"no proxy ignores forwarded header", 0, "203.0.113.5:4444", "1.2.3.4", "203.0.113.5"},
		{"one hop reads forwarded client", 1, "10.0.0.1:8080", "198.51.100.7", "198.51.100.7"},
		{"one hop is spoof-resistant", 1, "10.0.0.1:8080", "9.9.9.9, 198.51.100.7", "198.51.100.7"},
		{"two hops counts from the right", 2, "10.0.0.1:8080", "198.51.100.7, 172.16.0.9", "198.51.100.7"},
		{"one hop falls back without forwarded header", 1, "203.0.113.5:4444", "", "203.0.113.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientIP(newReq(tt.remote, tt.xff), tt.depth); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
