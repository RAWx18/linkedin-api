// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
	"github.com/garudexlabs/linkedin-api/internal/linkedin/parse"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

const (
	sourceLinkedIn        = "linkedin"
	defaultProfileTimeout = 15 * time.Second
)

// LinkedInClient is the subset of the LinkedIn integration the service needs.
// Declaring it here keeps the dependency inverted and easy to mock in tests. The
// credential selects which session authenticates the single request.
type LinkedInClient interface {
	FetchProfile(ctx context.Context, publicID string, cred linkedin.Credential) (json.RawMessage, error)
}

// Cache is the caching behavior the service relies on.
type Cache interface {
	Get(key string) (*domain.ProfileResult, bool)
	Set(key string, result *domain.ProfileResult)
}

// NegativeCache remembers profiles that were recently confirmed missing so the
// same lookup does not repeatedly reach LinkedIn.
type NegativeCache interface {
	Blocked(key string) bool
	Remember(key string)
}

// Gate guards access to LinkedIn. Enter reserves capacity or returns an error
// with no upstream work; the returned release must run with the operation's
// final error so the gate can update its circuit breaker. The mode lets the gate
// keep server-session and caller-session failures isolated.
type Gate interface {
	Enter(ctx context.Context, mode domain.CredentialMode) (func(error), error)
}

// Deps holds the profile service dependencies.
type Deps struct {
	Client           LinkedInClient
	Cache            Cache
	Negative         NegativeCache
	Gate             Gate
	Metrics          *observability.Metrics
	Logger           *slog.Logger
	ProfileTimeout   time.Duration
	CallerSessionTTL time.Duration
}

// ProfileService coordinates profile retrieval, protection, and normalization.
type ProfileService struct {
	client         LinkedInClient
	cache          Cache
	negative       NegativeCache
	gate           Gate
	group          singleflight.Group
	callerHealth   *callerHealth
	metrics        *observability.Metrics
	logger         *slog.Logger
	profileTimeout time.Duration
}

// NewProfileService wires the service dependencies, substituting inert defaults
// for an absent gate or negative cache so callers can opt out cleanly.
func NewProfileService(d Deps) *ProfileService {
	if d.Gate == nil {
		d.Gate = passthroughGate{}
	}
	if d.Negative == nil {
		d.Negative = openNegative{}
	}
	if d.ProfileTimeout <= 0 {
		d.ProfileTimeout = defaultProfileTimeout
	}
	return &ProfileService{
		client:         d.Client,
		cache:          d.Cache,
		negative:       d.Negative,
		gate:           d.Gate,
		callerHealth:   newCallerHealth(d.CallerSessionTTL),
		metrics:        d.Metrics,
		logger:         d.Logger,
		profileTimeout: d.ProfileTimeout,
	}
}

type passthroughGate struct{}

func (passthroughGate) Enter(context.Context, domain.CredentialMode) (func(error), error) {
	return func(error) {}, nil
}

type openNegative struct{}

func (openNegative) Blocked(string) bool { return false }
func (openNegative) Remember(string)     {}

// GetProfile resolves a normalized profile for the validated reference using the
// selected credential. Server-session requests are cached, negatively cached, and
// coalesced so at most one upstream retrieval runs per profile. Caller-session
// requests are fully isolated: they never read or write the shared caches, never
// coalesce with any other request, and are fast-failed while that caller's
// session is known to be rejected, so one caller's data or session state can
// never affect the server session or another caller.
func (s *ProfileService) GetProfile(ctx context.Context, ref urlx.ProfileRef, cred linkedin.Credential) (*domain.ProfileResult, error) {
	if cred.IsCaller() {
		return s.getCaller(ctx, ref, cred)
	}
	return s.getServer(ctx, ref, cred)
}

// getServer serves a server-session request through the shared cache, negative
// cache, and single-flight coalescing.
func (s *ProfileService) getServer(ctx context.Context, ref urlx.ProfileRef, cred linkedin.Credential) (*domain.ProfileResult, error) {
	if cached, ok := s.cache.Get(ref.PublicID); ok {
		s.metrics.Cache.WithLabelValues("hit").Inc()
		audit.MarkCacheHit(ctx)
		result := *cached
		result.Meta.Cached = true
		return &result, nil
	}
	if s.negative.Blocked(ref.PublicID) {
		s.metrics.Cache.WithLabelValues("negative").Inc()
		return nil, domain.NotFound("the requested LinkedIn profile was not found")
	}
	s.metrics.Cache.WithLabelValues("miss").Inc()

	v, err, shared := s.group.Do(ref.PublicID, func() (any, error) {
		return s.retrieve(ctx, ref, cred)
	})
	if shared {
		s.metrics.Coalesced.Inc()
	}
	if err != nil {
		s.metrics.Profiles.WithLabelValues("failure").Inc()
		return nil, err
	}
	s.metrics.Profiles.WithLabelValues("success").Inc()
	return v.(*domain.ProfileResult), nil
}

// retrieve performs one guarded server-session retrieval for a cache miss. It is
// the single-flight leader body, so it also populates the caches on the way out.
// The lookup runs under its own deadline covering the whole retrieval.
func (s *ProfileService) retrieve(ctx context.Context, ref urlx.ProfileRef, cred linkedin.Credential) (*domain.ProfileResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.profileTimeout)
	defer cancel()

	release, err := s.gate.Enter(ctx, domain.ModeServer)
	if err != nil {
		return nil, err
	}
	audit.MarkUpstreamCalled(ctx)

	result, ferr := s.fetchAndParse(ctx, ref, cred)
	release(ferr)

	if ferr != nil {
		audit.SetUpstreamOutcome(ctx, errCode(ferr))
		if de, ok := domain.AsError(ferr); ok && de.Code == domain.CodeProfileNotFound {
			s.negative.Remember(ref.PublicID)
		}
		return nil, ferr
	}
	audit.SetUpstreamOutcome(ctx, audit.OutcomeOK)
	s.cache.Set(ref.PublicID, result)
	return result, nil
}

// getCaller performs one fully isolated caller-session retrieval. It shares no
// cache, negative cache, or coalescing with any other request. A caller session
// LinkedIn has already rejected is fast-failed with no upstream traffic, no
// retry, and no fallback to the server session; a fresh caller session (a new
// fingerprint) is processed normally, subject to all shared upstream limits.
func (s *ProfileService) getCaller(ctx context.Context, ref urlx.ProfileRef, cred linkedin.Credential) (*domain.ProfileResult, error) {
	fp := cred.Fingerprint()
	if s.callerHealth.unhealthy(fp) {
		s.metrics.Profiles.WithLabelValues("failure").Inc()
		s.metrics.CallerSessionInvalid.Inc()
		audit.SetUpstreamOutcome(ctx, audit.OutcomeCallerExpired)
		return nil, errCallerSessionInvalid()
	}

	ctx, cancel := context.WithTimeout(ctx, s.profileTimeout)
	defer cancel()

	release, err := s.gate.Enter(ctx, domain.ModeCaller)
	if err != nil {
		s.metrics.Profiles.WithLabelValues("failure").Inc()
		return nil, err
	}
	audit.MarkUpstreamCalled(ctx)

	result, ferr := s.fetchAndParse(ctx, ref, cred)
	release(ferr)

	if ferr != nil {
		s.metrics.Profiles.WithLabelValues("failure").Inc()
		if de, ok := domain.AsError(ferr); ok && de.Code == domain.CodeUpstreamAuthFailed {
			s.callerHealth.markUnhealthy(fp)
			s.updateCallerGauge()
			s.metrics.CallerSessionInvalid.Inc()
			audit.SetUpstreamOutcome(ctx, audit.OutcomeCallerAuthFailed)
			return nil, errCallerSessionInvalid()
		}
		audit.SetUpstreamOutcome(ctx, errCode(ferr))
		return nil, ferr
	}
	s.callerHealth.clear(fp)
	s.updateCallerGauge()
	s.metrics.Profiles.WithLabelValues("success").Inc()
	audit.SetUpstreamOutcome(ctx, audit.OutcomeOK)
	return result, nil
}

func (s *ProfileService) updateCallerGauge() {
	if s.metrics != nil {
		s.metrics.CallerSessionsUnhealthy.Set(float64(s.callerHealth.tracked()))
	}
}

// errCallerSessionInvalid is the controlled response when a caller session is
// rejected or already known to be expired. It carries no credential material.
func errCallerSessionInvalid() error {
	return domain.CallerSessionInvalid("the supplied LinkedIn session is invalid or expired; provide a fresh authorized session")
}

// fetchAndParse makes the base profile call with the given credential and
// normalizes it.
func (s *ProfileService) fetchAndParse(ctx context.Context, ref urlx.ProfileRef, cred linkedin.Credential) (*domain.ProfileResult, error) {
	raw, err := s.client.FetchProfile(ctx, ref.PublicID, cred)
	if err != nil {
		return nil, err
	}
	profile, err := parse.Profile(raw, ref)
	if err != nil {
		if de, ok := domain.AsError(err); ok && de.Code == domain.CodeUpstreamParseError {
			s.metrics.ParseFailures.Inc()
		}
		return nil, err
	}
	return &domain.ProfileResult{
		Profile: profile,
		Meta: domain.Meta{
			RetrievedAt:   time.Now().UTC(),
			SchemaVersion: domain.SchemaVersion,
			Source:        sourceLinkedIn,
		},
	}, nil
}

// errCode reduces an error to a safe, non-sensitive label for the audit trail.
func errCode(err error) string {
	if de, ok := domain.AsError(err); ok {
		return string(de.Code)
	}
	return "unexpected_error"
}
