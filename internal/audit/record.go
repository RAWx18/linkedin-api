// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

// Package audit records privacy-safe metadata about every public API request in
// a durable store so operators can investigate abuse, measure LinkedIn upstream
// load, and answer usage questions without turning application logs or the
// metrics system into a query engine. It deliberately persists no secrets: API
// keys, session cookies, authorization headers, and full request URLs never
// reach the store.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Rate-limit decisions recorded for each request.
const (
	DecisionAllowed    = "allowed"
	DecisionIPLimited  = "ip_limited"
	DecisionKeyLimited = "key_limited"
)

// AnonymousKey is the key identifier stored when a request carries no API key.
const AnonymousKey = "anonymous"

// OutcomeOK marks a successful upstream retrieval.
const OutcomeOK = "ok"

// Caller-session outcomes distinguish a rejected caller session from a session
// already known to be expired, without ever naming the credential.
const (
	OutcomeCallerAuthFailed = "caller_session_auth_failed"
	OutcomeCallerExpired    = "caller_session_expired"
)

// Record is one immutable audit row. It captures what happened to a request
// without any secret material: the profile is identified by its normalized
// public identifier rather than the full URL, and the caller by a non-reversible
// key fingerprint rather than the key itself.
type Record struct {
	Time            time.Time
	RequestID       string
	ClientIP        string
	KeyID           string
	ProfileID       string
	CredentialMode  string
	CredFP          string
	Status          int
	RateDecision    string
	Cached          bool
	UpstreamCalled  bool
	UpstreamOutcome string
	Retries         int
	Latency         time.Duration
	ErrorClass      string
}

// KeyID reduces an API key to a short, non-reversible fingerprint suitable for
// grouping traffic by caller without ever storing the key. An empty key maps to
// the shared anonymous identifier.
func KeyID(key string) string {
	if key == "" {
		return AnonymousKey
	}
	sum := sha256.Sum256([]byte(key))
	return "key_" + hex.EncodeToString(sum[:6])
}

type ctxKey int

const eventKey ctxKey = iota

// Event accumulates audit fields as a request flows through the layers. The
// middleware seeds it, downstream code annotates it through the nil-safe helpers
// below, and the middleware reads the final values. A mutex guards the fields so
// the helpers stay safe regardless of which goroutine calls them.
type Event struct {
	mu              sync.Mutex
	profileID       string
	credentialMode  string
	credFP          string
	cached          bool
	upstreamCalled  bool
	upstreamOutcome string
	retries         int
	rateDecision    string
	errorClass      string
}

// NewEvent returns an event that defaults to the allowed rate-limit decision.
func NewEvent() *Event {
	return &Event{rateDecision: DecisionAllowed}
}

// WithEvent stores e in ctx so downstream layers can annotate it.
func WithEvent(ctx context.Context, e *Event) context.Context {
	return context.WithValue(ctx, eventKey, e)
}

// FromContext returns the request's event, or nil when auditing is disabled.
func FromContext(ctx context.Context) *Event {
	e, _ := ctx.Value(eventKey).(*Event)
	return e
}

// SetProfileID records the normalized public identifier once it is known.
func SetProfileID(ctx context.Context, id string) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.profileID = id
		e.mu.Unlock()
	}
}

// SetCredential records the request's credential mode and, for caller sessions,
// the non-reversible fingerprint. It never receives or stores any raw credential.
func SetCredential(ctx context.Context, mode, fingerprint string) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.credentialMode = mode
		e.credFP = fingerprint
		e.mu.Unlock()
	}
}

// MarkCacheHit records that the response was served from the cache.
func MarkCacheHit(ctx context.Context) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.cached = true
		e.mu.Unlock()
	}
}

// MarkUpstreamCalled records that a real LinkedIn request was made.
func MarkUpstreamCalled(ctx context.Context) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.upstreamCalled = true
		e.mu.Unlock()
	}
}

// SetUpstreamOutcome records the result of the LinkedIn retrieval.
func SetUpstreamOutcome(ctx context.Context, outcome string) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.upstreamOutcome = outcome
		e.mu.Unlock()
	}
}

// AddRetry increments the request's upstream retry count.
func AddRetry(ctx context.Context) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.retries++
		e.mu.Unlock()
	}
}

// SetRateDecision records how the rate limiters treated the request.
func SetRateDecision(ctx context.Context, decision string) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.rateDecision = decision
		e.mu.Unlock()
	}
}

// SetError records the final client-facing error classification.
func SetError(ctx context.Context, class string) {
	if e := FromContext(ctx); e != nil {
		e.mu.Lock()
		e.errorClass = class
		e.mu.Unlock()
	}
}

// Snapshot merges the request-level base fields with the annotations accumulated
// during handling and returns the finished record.
func (e *Event) Snapshot(base Record) Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	base.ProfileID = e.profileID
	base.CredentialMode = e.credentialMode
	base.CredFP = e.credFP
	base.Cached = e.cached
	base.UpstreamCalled = e.upstreamCalled
	base.UpstreamOutcome = e.upstreamOutcome
	base.Retries = e.retries
	base.RateDecision = e.rateDecision
	base.ErrorClass = e.errorClass
	return base
}
