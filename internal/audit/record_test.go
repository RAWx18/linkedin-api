// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestKeyIDFingerprint(t *testing.T) {
	if got := KeyID(""); got != AnonymousKey {
		t.Errorf("KeyID(\"\") = %q, want %q", got, AnonymousKey)
	}
	const secret = "super-secret-api-key"
	first := KeyID(secret)
	if first == KeyID("another-key") {
		t.Error("distinct keys produced the same fingerprint")
	}
	if KeyID(secret) != first {
		t.Error("fingerprint is not stable for the same key")
	}
	if strings.Contains(first, secret) {
		t.Errorf("fingerprint %q leaks the key material", first)
	}
}

func TestEventAnnotationsAreNilSafe(t *testing.T) {
	ctx := context.Background()
	SetProfileID(ctx, "x")
	MarkCacheHit(ctx)
	MarkUpstreamCalled(ctx)
	SetUpstreamOutcome(ctx, OutcomeOK)
	AddRetry(ctx)
	SetRateDecision(ctx, DecisionIPLimited)
	SetError(ctx, "boom")
}

func TestEventSnapshotMergesAnnotations(t *testing.T) {
	ev := NewEvent()
	ctx := WithEvent(context.Background(), ev)
	SetProfileID(ctx, "alice")
	MarkUpstreamCalled(ctx)
	SetUpstreamOutcome(ctx, OutcomeOK)
	AddRetry(ctx)
	AddRetry(ctx)

	rec := ev.Snapshot(Record{Status: 200, ClientIP: "1.1.1.1"})
	if rec.ProfileID != "alice" || !rec.UpstreamCalled || rec.UpstreamOutcome != OutcomeOK {
		t.Errorf("annotations not merged: %+v", rec)
	}
	if rec.Retries != 2 {
		t.Errorf("retries = %d, want 2", rec.Retries)
	}
	if rec.RateDecision != DecisionAllowed {
		t.Errorf("rate decision = %q, want default %q", rec.RateDecision, DecisionAllowed)
	}
	if rec.Status != 200 || rec.ClientIP != "1.1.1.1" {
		t.Errorf("base fields lost: %+v", rec)
	}
}

func TestEventConcurrentRetriesAreCounted(t *testing.T) {
	ev := NewEvent()
	ctx := WithEvent(context.Background(), ev)
	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			AddRetry(ctx)
		}()
	}
	wg.Wait()
	if got := ev.Snapshot(Record{}).Retries; got != n {
		t.Errorf("retries = %d, want %d", got, n)
	}
}
