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
	"github.com/garudexlabs/linkedin-api/internal/linkedin/parse"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

const (
	sourceLinkedIn        = "linkedin"
	defaultProfileTimeout = 15 * time.Second
)

// LinkedInClient is the subset of the LinkedIn integration the service needs.
// Declaring it here keeps the dependency inverted and easy to mock in tests.
type LinkedInClient interface {
	FetchProfile(ctx context.Context, publicID string) (json.RawMessage, error)
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
// final error so the gate can update its circuit breaker.
type Gate interface {
	Enter(ctx context.Context) (func(error), error)
}

// Deps holds the profile service dependencies.
type Deps struct {
	Client         LinkedInClient
	Cache          Cache
	Negative       NegativeCache
	Gate           Gate
	Metrics        *observability.Metrics
	Logger         *slog.Logger
	ProfileTimeout time.Duration
}

// ProfileService coordinates profile retrieval, protection, and normalization.
type ProfileService struct {
	client         LinkedInClient
	cache          Cache
	negative       NegativeCache
	gate           Gate
	group          singleflight.Group
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
		metrics:        d.Metrics,
		logger:         d.Logger,
		profileTimeout: d.ProfileTimeout,
	}
}

type passthroughGate struct{}

func (passthroughGate) Enter(context.Context) (func(error), error) {
	return func(error) {}, nil
}

type openNegative struct{}

func (openNegative) Blocked(string) bool { return false }
func (openNegative) Remember(string)     {}

// GetProfile resolves a normalized profile for the validated reference. It
// serves cached results, short-circuits known-missing profiles, and coalesces
// concurrent identical lookups so at most one upstream retrieval runs per
// profile. Every upstream retrieval passes through the gate.
func (s *ProfileService) GetProfile(ctx context.Context, ref urlx.ProfileRef) (*domain.ProfileResult, error) {
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
		return s.retrieve(ctx, ref)
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

// retrieve performs one guarded upstream retrieval for a cache miss. It is the
// single-flight leader body, so it also populates the caches on the way out. The
// lookup runs under its own deadline covering the whole retrieval.
func (s *ProfileService) retrieve(ctx context.Context, ref urlx.ProfileRef) (*domain.ProfileResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.profileTimeout)
	defer cancel()

	release, err := s.gate.Enter(ctx)
	if err != nil {
		return nil, err
	}
	audit.MarkUpstreamCalled(ctx)

	result, ferr := s.fetchAndParse(ctx, ref)
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

// fetchAndParse makes the base profile call and normalizes it.
func (s *ProfileService) fetchAndParse(ctx context.Context, ref urlx.ProfileRef) (*domain.ProfileResult, error) {
	raw, err := s.client.FetchProfile(ctx, ref.PublicID)
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
