// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package cache

import (
	"testing"
	"time"
)

func TestNegativeRememberBlocked(t *testing.T) {
	n := NewNegative(time.Minute, 10)
	if n.Blocked("a") {
		t.Fatal("empty negative cache should not block")
	}
	n.Remember("a")
	if !n.Blocked("a") {
		t.Fatal("should block after remember")
	}
	if n.Blocked("b") {
		t.Fatal("unrelated key should not block")
	}
}

func TestNegativeExpiry(t *testing.T) {
	n := NewNegative(time.Minute, 10)
	now := time.Now()
	n.now = func() time.Time { return now }
	n.Remember("a")
	if !n.Blocked("a") {
		t.Fatal("should block before expiry")
	}
	n.now = func() time.Time { return now.Add(2 * time.Minute) }
	if n.Blocked("a") {
		t.Fatal("should expire after the TTL")
	}
}

func TestNegativeEviction(t *testing.T) {
	n := NewNegative(time.Minute, 2)
	n.Remember("a")
	n.Remember("b")
	n.Remember("c")

	present := 0
	for _, k := range []string{"a", "b", "c"} {
		if n.Blocked(k) {
			present++
		}
	}
	if present > 2 {
		t.Errorf("negative cache exceeded its max entries: %d present", present)
	}
}
