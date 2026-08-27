// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package parse

import (
	"os"
	"testing"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

func TestApplyExperience(t *testing.T) {
	var p domain.Profile
	n, err := ApplySection(&p, "experience", loadFixture(t, "dash_experience.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 3 || len(p.Experience) != 3 {
		t.Fatalf("count = %d, experience = %+v", n, p.Experience)
	}
	// Ordering follows data.*elements, not the shuffled included array.
	if p.Experience[0].Title != "Co-chair" || p.Experience[1].Title != "Founder" || p.Experience[2].Title != "Co-founder" {
		t.Errorf("order = %q/%q/%q", p.Experience[0].Title, p.Experience[1].Title, p.Experience[2].Title)
	}
	if p.Experience[1].Company != "Breakthrough Energy" {
		t.Errorf("company should be trimmed: %q", p.Experience[1].Company)
	}
	if p.Experience[0].CompanyURL != "https://www.linkedin.com/company/8736/" {
		t.Errorf("company_url = %q", p.Experience[0].CompanyURL)
	}
	if dr := p.Experience[0].DateRange; dr == nil || dr.Start == nil || dr.Start.Year != 2000 || dr.End != nil {
		t.Errorf("date_range = %+v", p.Experience[0].DateRange)
	}
	ms := p.Experience[2]
	if ms.DateRange.End == nil || ms.DateRange.End.Year != 2008 || ms.DateRange.Start.Month != 4 {
		t.Errorf("microsoft dates = %+v", ms.DateRange)
	}
	if ms.Location != "Redmond, Washington" {
		t.Errorf("location = %q", ms.Location)
	}
	if ms.Description != "Building software & tools." {
		t.Errorf("description should decode entities: %q", ms.Description)
	}
}

func TestApplyEducation(t *testing.T) {
	var p domain.Profile
	n, err := ApplySection(&p, "education", loadFixture(t, "dash_education.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Fatalf("count = %d", n)
	}
	if p.Education[0].School != "Harvard University" {
		t.Errorf("first school = %q", p.Education[0].School)
	}
	if p.Education[0].SchoolURL != "https://www.linkedin.com/school/18483/" {
		t.Errorf("school url from schoolUrn = %q", p.Education[0].SchoolURL)
	}
	e := p.Education[1]
	if e.SchoolURL != "https://www.linkedin.com/company/1646/" {
		t.Errorf("school url should fall back to companyUrn: %q", e.SchoolURL)
	}
	if e.Degree != "B.S." || e.FieldOfStudy != "Computer Science" || e.Grade != "3.9 GPA" {
		t.Errorf("education fields = %+v", e)
	}
	if e.Activities != "Robotics Club\nHackathon Team" {
		t.Errorf("activities = %q", e.Activities)
	}
}

func TestApplySkills(t *testing.T) {
	var p domain.Profile
	n, err := ApplySection(&p, "skills", loadFixture(t, "dash_skills.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Go (Programming Language)", "Distributed Systems", "Kubernetes"}
	if n != 3 || len(p.Skills) != 3 {
		t.Fatalf("skills = %v", p.Skills)
	}
	for i, w := range want {
		if p.Skills[i] != w {
			t.Errorf("skill %d = %q, want %q", i, p.Skills[i], w)
		}
	}
}

func TestApplyCertifications(t *testing.T) {
	var p domain.Profile
	if _, err := ApplySection(&p, "certifications", loadFixture(t, "dash_certifications.json")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Certifications) != 2 {
		t.Fatalf("certs = %+v", p.Certifications)
	}
	if p.Certifications[0].Name != "AWS Certified Solutions Architect" || p.Certifications[0].AuthorityURL != "" {
		t.Errorf("cert0 = %+v", p.Certifications[0])
	}
	c := p.Certifications[1]
	if c.Authority != "The Linux Foundation" || c.AuthorityURL != "https://www.linkedin.com/company/208777/" {
		t.Errorf("authority url = %+v", c)
	}
	if c.URL != "https://example.com/credential/ckad-123" || c.LicenseNumber != "LF-CKA-123" {
		t.Errorf("credential = %+v", c)
	}
}

func TestApplyLanguages(t *testing.T) {
	var p domain.Profile
	if _, err := ApplySection(&p, "languages", loadFixture(t, "dash_languages.json")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Languages) != 2 || p.Languages[0].Name != "English" || p.Languages[0].Proficiency != "NATIVE_OR_BILINGUAL" {
		t.Errorf("languages = %+v", p.Languages)
	}
}

func TestApplyVolunteer(t *testing.T) {
	var p domain.Profile
	if _, err := ApplySection(&p, "volunteer", loadFixture(t, "dash_volunteer.json")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.VolunteerExperience) != 1 {
		t.Fatalf("volunteer = %+v", p.VolunteerExperience)
	}
	v := p.VolunteerExperience[0]
	if v.Role != "Event Lead & Core Team" || v.Organization != "Community Tech Fest" || v.Cause != "EDUCATION" {
		t.Errorf("volunteer = %+v", v)
	}
	if v.OrganizationURL != "https://www.linkedin.com/company/104401257/" {
		t.Errorf("organization url = %q", v.OrganizationURL)
	}
}

func TestApplyProjectsAndTestScores(t *testing.T) {
	var p domain.Profile
	if _, err := ApplySection(&p, "projects", loadFixture(t, "dash_projects.json")); err != nil {
		t.Fatalf("projects error: %v", err)
	}
	if len(p.Projects) != 1 || p.Projects[0].Title != "Open Source Profile Service" {
		t.Errorf("projects = %+v", p.Projects)
	}
	if _, err := ApplySection(&p, "test_scores", loadFixture(t, "dash_testscores.json")); err != nil {
		t.Fatalf("test scores error: %v", err)
	}
	ts := p.TestScores
	if len(ts) != 1 || ts[0].Name != "GRE" || ts[0].Score != "334/340" {
		t.Fatalf("test scores = %+v", ts)
	}
	if ts[0].Date == nil || ts[0].Date.Year != 2019 || ts[0].Date.Month != 9 {
		t.Errorf("test score date = %+v", ts[0].Date)
	}
}

func TestApplySectionEmpty(t *testing.T) {
	var p domain.Profile
	n, err := ApplySection(&p, "skills", []byte(`{"data":{"*elements":[]},"included":[]}`))
	if err != nil || n != 0 || p.Skills != nil {
		t.Errorf("empty section: n=%d err=%v skills=%v", n, err, p.Skills)
	}
}

func TestApplySectionMissingIncluded(t *testing.T) {
	// data references two urns but only one is present in included.
	raw := []byte(`{"data":{"*elements":["urn:li:fsd_skill:(x,1)","urn:li:fsd_skill:(x,2)"]},` +
		`"included":[{"entityUrn":"urn:li:fsd_skill:(x,2)","name":"Rust"}]}`)
	var p domain.Profile
	n, err := ApplySection(&p, "skills", raw)
	if err != nil || n != 1 || len(p.Skills) != 1 || p.Skills[0] != "Rust" {
		t.Errorf("missing included: n=%d err=%v skills=%v", n, err, p.Skills)
	}
}

func TestApplySectionMalformed(t *testing.T) {
	var p domain.Profile
	_, err := ApplySection(&p, "experience", []byte(`not json`))
	if de, ok := domain.AsError(err); !ok || de.Code != domain.CodeUpstreamParseError {
		t.Fatalf("expected upstream_parse_error, got %v", err)
	}
}

func TestApplySectionUnknown(t *testing.T) {
	var p domain.Profile
	n, err := ApplySection(&p, "not_a_section", []byte(`{}`))
	if err != nil || n != 0 {
		t.Errorf("unknown section should be a no-op: n=%d err=%v", n, err)
	}
}

func TestEntityURL(t *testing.T) {
	cases := map[string]string{
		"urn:li:fsd_company:8736":  "https://www.linkedin.com/company/8736/",
		"urn:li:fsd_company:(1,2)": "",
		"":                         "",
		"notaurn":                  "",
	}
	for urn, want := range cases {
		if got := entityURL(urn, "company"); got != want {
			t.Errorf("entityURL(%q) = %q, want %q", urn, got, want)
		}
	}
}

func BenchmarkApplySections(b *testing.B) {
	exp, err := os.ReadFile("testdata/dash_experience.json")
	if err != nil {
		b.Fatal(err)
	}
	edu, err := os.ReadFile("testdata/dash_education.json")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var p domain.Profile
		_, _ = ApplySection(&p, "experience", exp)
		_, _ = ApplySection(&p, "education", edu)
	}
}
