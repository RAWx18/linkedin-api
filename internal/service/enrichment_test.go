// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/cache"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

func profileWithURN() func() (json.RawMessage, error) {
	return func() (json.RawMessage, error) {
		return json.RawMessage(`{"elements":[{"firstName":"Ada","lastName":"Lovelace","entityUrn":"urn:li:fsd_profile:ACoAATEST"}]}`), nil
	}
}

func enrichService(m *mockClient, c service.Cache, sections []linkedin.Section, concurrency int) *service.ProfileService {
	if c == nil {
		c = cache.Noop{}
	}
	return service.NewProfileService(service.Deps{
		Client: m, Cache: c, Metrics: observability.NewMetrics(), Logger: discardLogger(),
		EnrichmentSections: sections, EnrichmentConcurrency: concurrency,
	})
}

// countingGate allows the first `allow` reservations and rejects the rest, so a
// test can let the core call through while closing the gate on enrichment.
type countingGate struct {
	mu    sync.Mutex
	calls int
	allow int
}

func (g *countingGate) Enter(context.Context, domain.CredentialMode) (func(error), error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	if n > g.allow {
		return nil, domain.UpstreamUnavailable(errors.New("gate closed"))
	}
	return func(error) {}, nil
}

func TestEnrichmentPopulatesSections(t *testing.T) {
	m := &mockClient{profile: profileWithURN(), section: func(s linkedin.Section) (json.RawMessage, error) {
		switch s {
		case linkedin.SectionExperience:
			return json.RawMessage(`{"data":{"*elements":["u1"]},"included":[{"entityUrn":"u1","title":"Engineer","companyName":"Acme","companyUrn":"urn:li:fsd_company:5"}]}`), nil
		case linkedin.SectionEducation:
			return json.RawMessage(`{"data":{"*elements":["u2"]},"included":[{"entityUrn":"u2","schoolName":"MIT"}]}`), nil
		}
		return json.RawMessage(`{"data":{"*elements":[]},"included":[]}`), nil
	}}
	svc := enrichService(m, nil, []linkedin.Section{linkedin.SectionExperience, linkedin.SectionEducation}, 4)

	res, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Profile.Experience) != 1 || res.Profile.Experience[0].Title != "Engineer" {
		t.Errorf("experience = %+v", res.Profile.Experience)
	}
	if len(res.Profile.Education) != 1 || res.Profile.Education[0].School != "MIT" {
		t.Errorf("education = %+v", res.Profile.Education)
	}
	if res.Meta.Sections["experience"] != "ok" || res.Meta.Sections["education"] != "ok" {
		t.Errorf("sections meta = %v", res.Meta.Sections)
	}
}

func TestEnrichmentBoundedRequestCount(t *testing.T) {
	all := []linkedin.Section{
		linkedin.SectionExperience, linkedin.SectionEducation, linkedin.SectionSkills,
		linkedin.SectionCertifications, linkedin.SectionLanguages, linkedin.SectionVolunteer,
		linkedin.SectionProjects, linkedin.SectionTestScores,
	}
	m := &mockClient{profile: profileWithURN()}
	svc := enrichService(m, nil, all, 4)

	if _, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exactly one upstream request per configured section, plus one core call: no bursts.
	if got := atomic.LoadInt32(&m.sectionCalls); got != int32(len(all)) {
		t.Errorf("section calls = %d, want %d", got, len(all))
	}
	if got := atomic.LoadInt32(&m.profileCalls); got != 1 {
		t.Errorf("profile calls = %d, want 1", got)
	}
}

func TestEnrichmentPartialFailureIsolated(t *testing.T) {
	m := &mockClient{profile: profileWithURN(), section: func(s linkedin.Section) (json.RawMessage, error) {
		switch s {
		case linkedin.SectionEducation:
			return nil, domain.UpstreamUnavailable(errors.New("boom"))
		case linkedin.SectionExperience:
			return json.RawMessage(`{"data":{"*elements":["u1"]},"included":[{"entityUrn":"u1","title":"Engineer"}]}`), nil
		}
		return json.RawMessage(`{"data":{"*elements":[]},"included":[]}`), nil
	}}
	svc := enrichService(m, nil, []linkedin.Section{linkedin.SectionExperience, linkedin.SectionEducation}, 4)

	res, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("a failing section must not fail the core profile: %v", err)
	}
	if len(res.Profile.Experience) != 1 {
		t.Errorf("experience should still be present: %+v", res.Profile.Experience)
	}
	if res.Meta.Sections["experience"] != "ok" || res.Meta.Sections["education"] != "unavailable" {
		t.Errorf("sections meta = %v", res.Meta.Sections)
	}
}

func TestEnrichmentEmptySection(t *testing.T) {
	m := &mockClient{profile: profileWithURN()} // default section returns empty envelope
	svc := enrichService(m, nil, []linkedin.Section{linkedin.SectionSkills}, 4)

	res, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Profile.Skills != nil {
		t.Errorf("empty section should leave the field absent: %v", res.Profile.Skills)
	}
	if res.Meta.Sections["skills"] != "empty" {
		t.Errorf("sections meta = %v", res.Meta.Sections)
	}
}

func TestEnrichmentSkippedWithoutURN(t *testing.T) {
	m := &mockClient{} // default profile has no entityUrn
	svc := enrichService(m, nil, []linkedin.Section{linkedin.SectionExperience}, 4)

	res, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&m.sectionCalls); got != 0 {
		t.Errorf("no section requests without a profile urn, got %d", got)
	}
	if res.Meta.Sections != nil {
		t.Errorf("no sections metadata expected, got %v", res.Meta.Sections)
	}
}

func TestEnrichmentGateRejectionIsolated(t *testing.T) {
	m := &mockClient{profile: profileWithURN(), section: func(linkedin.Section) (json.RawMessage, error) {
		return json.RawMessage(`{"data":{"*elements":["u1"]},"included":[{"entityUrn":"u1","title":"X"}]}`), nil
	}}
	// allow=1 lets the core call through and closes the gate on every section.
	svc := service.NewProfileService(service.Deps{
		Client: m, Cache: cache.Noop{}, Gate: &countingGate{allow: 1},
		Metrics: observability.NewMetrics(), Logger: discardLogger(),
		EnrichmentSections: []linkedin.Section{linkedin.SectionExperience}, EnrichmentConcurrency: 4,
	})

	res, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("core must succeed when only enrichment is gated: %v", err)
	}
	if res.Meta.Sections["experience"] != "unavailable" {
		t.Errorf("gated section should be unavailable: %v", res.Meta.Sections)
	}
	if got := atomic.LoadInt32(&m.sectionCalls); got != 0 {
		t.Errorf("gate rejection must prevent the section request, got %d", got)
	}
}

func TestEnrichmentConcurrencyBounded(t *testing.T) {
	var inflight, maxInflight int32
	m := &mockClient{profile: profileWithURN(), section: func(linkedin.Section) (json.RawMessage, error) {
		n := atomic.AddInt32(&inflight, 1)
		for {
			old := atomic.LoadInt32(&maxInflight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInflight, old, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&inflight, -1)
		return json.RawMessage(`{"data":{"*elements":[]},"included":[]}`), nil
	}}
	all := []linkedin.Section{
		linkedin.SectionExperience, linkedin.SectionEducation, linkedin.SectionSkills,
		linkedin.SectionCertifications, linkedin.SectionLanguages, linkedin.SectionVolunteer,
	}
	svc := enrichService(m, nil, all, 2)

	if _, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&maxInflight); got > 2 {
		t.Errorf("max concurrent section fetches = %d, want <= 2", got)
	}
	if got := atomic.LoadInt32(&maxInflight); got < 2 {
		t.Errorf("sections should run concurrently, max in-flight = %d", got)
	}
}

func TestEnrichmentCachedAsUnit(t *testing.T) {
	m := &mockClient{profile: profileWithURN(), section: func(linkedin.Section) (json.RawMessage, error) {
		return json.RawMessage(`{"data":{"*elements":["u1"]},"included":[{"entityUrn":"u1","title":"Engineer"}]}`), nil
	}}
	svc := enrichService(m, cache.NewTTL(time.Minute, 10), []linkedin.Section{linkedin.SectionExperience}, 4)

	if _, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Meta.Cached || len(res.Profile.Experience) != 1 {
		t.Errorf("cached result should carry enrichment: cached=%v experience=%+v", res.Meta.Cached, res.Profile.Experience)
	}
	// Second lookup is served from cache: no additional core or section traffic.
	if got := atomic.LoadInt32(&m.sectionCalls); got != 1 {
		t.Errorf("section calls = %d, want 1 (second lookup cached)", got)
	}
	if got := atomic.LoadInt32(&m.profileCalls); got != 1 {
		t.Errorf("profile calls = %d, want 1 (second lookup cached)", got)
	}
}
