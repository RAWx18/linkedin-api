// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package parse

import (
	"encoding/json"
	"strings"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

// ApplySection parses a normalized DASH section response and assigns it to the
// matching profile field, returning the number of entries populated. An unknown
// section is a no-op. Ordering follows LinkedIn's element order.
func ApplySection(p *domain.Profile, section string, raw json.RawMessage) (int, error) {
	switch section {
	case "experience":
		v, err := parseSection(raw, decodeExperience)
		p.Experience = v
		return len(v), err
	case "education":
		v, err := parseSection(raw, decodeEducation)
		p.Education = v
		return len(v), err
	case "skills":
		v, err := parseSection(raw, decodeSkill)
		p.Skills = v
		return len(v), err
	case "certifications":
		v, err := parseSection(raw, decodeCertification)
		p.Certifications = v
		return len(v), err
	case "languages":
		v, err := parseSection(raw, decodeLanguage)
		p.Languages = v
		return len(v), err
	case "volunteer":
		v, err := parseSection(raw, decodeVolunteer)
		p.VolunteerExperience = v
		return len(v), err
	case "projects":
		v, err := parseSection(raw, decodeProject)
		p.Projects = v
		return len(v), err
	case "test_scores":
		v, err := parseSection(raw, decodeTestScore)
		p.TestScores = v
		return len(v), err
	}
	return 0, nil
}

// parseSection decodes each ordered element of a normalized response, preserving
// LinkedIn's ordering and skipping entries that are missing or fail to decode.
func parseSection[T any](raw json.RawMessage, decode func(json.RawMessage) (T, bool)) ([]T, error) {
	var resp struct {
		Data struct {
			Elements []string `json:"*elements"`
		} `json:"data"`
		Included []json.RawMessage `json:"included"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, domain.UpstreamParse(err)
	}
	byURN := make(map[string]json.RawMessage, len(resp.Included))
	for _, item := range resp.Included {
		var id struct {
			EntityURN string `json:"entityUrn"`
		}
		if json.Unmarshal(item, &id) == nil && id.EntityURN != "" {
			byURN[id.EntityURN] = item
		}
	}
	out := make([]T, 0, len(resp.Data.Elements))
	for _, urn := range resp.Data.Elements {
		item, ok := byURN[urn]
		if !ok {
			continue
		}
		if v, ok := decode(item); ok {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

type rawDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func (d *rawDate) date() *domain.Date {
	if d == nil || d.Year == 0 {
		return nil
	}
	return &domain.Date{Year: d.Year, Month: d.Month, Day: d.Day}
}

type rawDateRange struct {
	Start *rawDate `json:"start"`
	End   *rawDate `json:"end"`
}

func (r *rawDateRange) dateRange() *domain.DateRange {
	if r == nil {
		return nil
	}
	start, end := r.Start.date(), r.End.date()
	if start == nil && end == nil {
		return nil
	}
	return &domain.DateRange{Start: start, End: end}
}

// entityURL turns a simple urn (urn:li:fsd_company:123) into its public URL, and
// is empty for compound or missing urns.
func entityURL(urn, kind string) string {
	i := strings.LastIndexByte(urn, ':')
	if i < 0 {
		return ""
	}
	id := urn[i+1:]
	if id == "" || strings.ContainsAny(id, "(),") {
		return ""
	}
	return "https://www.linkedin.com/" + kind + "/" + id + "/"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func decodeExperience(raw json.RawMessage) (domain.Experience, bool) {
	var p struct {
		Title           string        `json:"title"`
		CompanyName     string        `json:"companyName"`
		CompanyURN      string        `json:"companyUrn"`
		LocationName    string        `json:"locationName"`
		GeoLocationName string        `json:"geoLocationName"`
		Description     string        `json:"description"`
		DateRange       *rawDateRange `json:"dateRange"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return domain.Experience{}, false
	}
	e := domain.Experience{
		Title:       cleanText(p.Title),
		Company:     strings.TrimSpace(cleanText(p.CompanyName)),
		CompanyURL:  entityURL(p.CompanyURN, "company"),
		Location:    cleanText(firstNonEmpty(p.LocationName, p.GeoLocationName)),
		Description: cleanText(p.Description),
		DateRange:   p.DateRange.dateRange(),
	}
	return e, e.Title != "" || e.Company != ""
}

func decodeEducation(raw json.RawMessage) (domain.Education, bool) {
	var e struct {
		SchoolName   string        `json:"schoolName"`
		SchoolURN    string        `json:"schoolUrn"`
		CompanyURN   string        `json:"companyUrn"`
		DegreeName   string        `json:"degreeName"`
		FieldOfStudy string        `json:"fieldOfStudy"`
		Grade        string        `json:"grade"`
		Activities   string        `json:"activities"`
		Description  string        `json:"description"`
		DateRange    *rawDateRange `json:"dateRange"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return domain.Education{}, false
	}
	link := entityURL(e.SchoolURN, "school")
	if link == "" {
		link = entityURL(e.CompanyURN, "company")
	}
	out := domain.Education{
		School:       cleanText(e.SchoolName),
		SchoolURL:    link,
		Degree:       cleanText(e.DegreeName),
		FieldOfStudy: cleanText(e.FieldOfStudy),
		Grade:        cleanText(e.Grade),
		Activities:   cleanText(e.Activities),
		Description:  cleanText(e.Description),
		DateRange:    e.DateRange.dateRange(),
	}
	return out, out.School != ""
}

func decodeSkill(raw json.RawMessage) (string, bool) {
	var s struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	name := cleanText(s.Name)
	return name, name != ""
}

func decodeCertification(raw json.RawMessage) (domain.Certification, bool) {
	var c struct {
		Name          string        `json:"name"`
		Authority     string        `json:"authority"`
		CompanyURN    string        `json:"companyUrn"`
		URL           string        `json:"url"`
		LicenseNumber string        `json:"licenseNumber"`
		DateRange     *rawDateRange `json:"dateRange"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return domain.Certification{}, false
	}
	out := domain.Certification{
		Name:          cleanText(c.Name),
		Authority:     cleanText(c.Authority),
		AuthorityURL:  entityURL(c.CompanyURN, "company"),
		URL:           strings.TrimSpace(c.URL),
		LicenseNumber: cleanText(c.LicenseNumber),
		DateRange:     c.DateRange.dateRange(),
	}
	return out, out.Name != ""
}

func decodeLanguage(raw json.RawMessage) (domain.Language, bool) {
	var l struct {
		Name        string `json:"name"`
		Proficiency string `json:"proficiency"`
	}
	if json.Unmarshal(raw, &l) != nil {
		return domain.Language{}, false
	}
	name := cleanText(l.Name)
	return domain.Language{Name: name, Proficiency: l.Proficiency}, name != ""
}

func decodeVolunteer(raw json.RawMessage) (domain.VolunteerExperience, bool) {
	var v struct {
		Role        string        `json:"role"`
		CompanyName string        `json:"companyName"`
		CompanyURN  string        `json:"companyUrn"`
		Cause       string        `json:"cause"`
		Description string        `json:"description"`
		DateRange   *rawDateRange `json:"dateRange"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return domain.VolunteerExperience{}, false
	}
	out := domain.VolunteerExperience{
		Role:            cleanText(v.Role),
		Organization:    strings.TrimSpace(cleanText(v.CompanyName)),
		OrganizationURL: entityURL(v.CompanyURN, "company"),
		Cause:           cleanText(v.Cause),
		Description:     cleanText(v.Description),
		DateRange:       v.DateRange.dateRange(),
	}
	return out, out.Role != "" || out.Organization != ""
}

func decodeProject(raw json.RawMessage) (domain.Project, bool) {
	var p struct {
		Title       string        `json:"title"`
		Description string        `json:"description"`
		DateRange   *rawDateRange `json:"dateRange"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return domain.Project{}, false
	}
	out := domain.Project{
		Title:       cleanText(p.Title),
		Description: cleanText(p.Description),
		DateRange:   p.DateRange.dateRange(),
	}
	return out, out.Title != ""
}

func decodeTestScore(raw json.RawMessage) (domain.TestScore, bool) {
	var t struct {
		Name        string   `json:"name"`
		Score       string   `json:"score"`
		Description string   `json:"description"`
		DateOn      *rawDate `json:"dateOn"`
	}
	if json.Unmarshal(raw, &t) != nil {
		return domain.TestScore{}, false
	}
	out := domain.TestScore{
		Name:        cleanText(t.Name),
		Score:       cleanText(t.Score),
		Description: cleanText(t.Description),
		Date:        t.DateOn.date(),
	}
	return out, out.Name != "" || out.Score != ""
}
