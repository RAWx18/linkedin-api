// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package cache

import (
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

func result(id string) *domain.ProfileResult {
	return &domain.ProfileResult{Profile: &domain.Profile{PublicIdentifier: id}}
}

func TestCacheSetGet(t *testing.T) {
	c := NewTTL(time.Minute, 10)
	c.Set("a", result("a"))
	got, ok := c.Get("a")
	if !ok || got.Profile.PublicIdentifier != "a" {
		t.Fatal("expected a cache hit")
	}
	if _, ok := c.Get("missing"); ok {
		t.Error("expected a cache miss")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := NewTTL(time.Minute, 10)
	now := time.Now()
	c.now = func() time.Time { return now }
	c.Set("a", result("a"))
	if _, ok := c.Get("a"); !ok {
		t.Fatal("should be present before expiry")
	}
	c.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, ok := c.Get("a"); ok {
		t.Error("should be expired")
	}
}

func TestCacheEviction(t *testing.T) {
	c := NewTTL(time.Minute, 2)
	c.Set("a", result("a"))
	c.Set("b", result("b"))
	c.Set("c", result("c"))

	present := 0
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := c.Get(k); ok {
			present++
		}
	}
	if present > 2 {
		t.Errorf("cache exceeded max entries: %d present", present)
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("most recently inserted entry should remain")
	}
}

func TestNoopCache(t *testing.T) {
	var n Noop
	n.Set("a", result("a"))
	if _, ok := n.Get("a"); ok {
		t.Error("noop cache should never hit")
	}
}
