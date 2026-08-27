// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin

import "net/url"

// profileRequest builds the DASH profile lookup for a public identifier. The
// identifier is validated by the URL layer before reaching here, so it is safe
// to place in the finder query against the fixed, trusted base URL.
func profileRequest(publicID string) (string, url.Values) {
	return "/voyager/api/identity/dash/profiles", url.Values{
		"q":              {"memberIdentity"},
		"memberIdentity": {publicID},
	}
}

// Accept headers. The top-card finder returns the plain collection; the section
// finders return LinkedIn's normalized data/included envelope.
const (
	acceptJSON       = "application/json"
	acceptNormalized = "application/vnd.linkedin.normalized+json+2.1"
)

// Section is a supported profile enrichment section. The set is closed: callers
// choose from these constants, so an arbitrary upstream fetch can never be
// triggered from outside this package.
type Section string

const (
	SectionExperience     Section = "experience"
	SectionEducation      Section = "education"
	SectionSkills         Section = "skills"
	SectionCertifications Section = "certifications"
	SectionLanguages      Section = "languages"
	SectionVolunteer      Section = "volunteer"
	SectionProjects       Section = "projects"
	SectionTestScores     Section = "test_scores"
)

// sectionResource maps each supported section to its DASH resource. A section
// absent from this map is rejected before any request is made.
var sectionResource = map[Section]string{
	SectionExperience:     "profilePositions",
	SectionEducation:      "profileEducations",
	SectionSkills:         "profileSkills",
	SectionCertifications: "profileCertifications",
	SectionLanguages:      "profileLanguages",
	SectionVolunteer:      "profileVolunteerExperiences",
	SectionProjects:       "profileProjects",
	SectionTestScores:     "profileTestScores",
}

// sectionRequest builds the DASH section lookup for a profile URN, reporting
// false when the section is not in the allowlist.
func sectionRequest(section Section, profileUrn string) (string, url.Values, bool) {
	resource, ok := sectionResource[section]
	if !ok {
		return "", nil, false
	}
	return "/voyager/api/identity/dash/" + resource, url.Values{
		"q":          {"viewee"},
		"profileUrn": {profileUrn},
	}, true
}

// ParseSection resolves a section name to a Section, reporting false when the
// name is not one of the supported sections.
func ParseSection(name string) (Section, bool) {
	s := Section(name)
	_, ok := sectionResource[s]
	return s, ok
}
