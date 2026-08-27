// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package domain

import "time"

// SchemaVersion identifies the shape of the public profile response. Bump it
// whenever the response contract changes in a backward-incompatible way.
const SchemaVersion = "2.2"

// Profile is the normalized, transport-independent representation of a LinkedIn
// profile. Optional scalars use pointers and collections are nil when absent so
// that missing information stays absent rather than surfacing as empty noise.
type Profile struct {
	PublicIdentifier string     `json:"public_identifier"`
	ProfileURL       string     `json:"profile_url"`
	FirstName        string     `json:"first_name,omitempty"`
	LastName         string     `json:"last_name,omitempty"`
	FullName         string     `json:"full_name,omitempty"`
	Headline         string     `json:"headline,omitempty"`
	Summary          *string    `json:"summary,omitempty"`
	ProfileLanguage  string     `json:"profile_language,omitempty"`
	SupportedLocales []string   `json:"supported_locales,omitempty"`
	Location         *Location  `json:"location,omitempty"`
	ProfilePicture   *Image     `json:"profile_picture,omitempty"`
	BackgroundImage  *Image     `json:"background_image,omitempty"`
	Websites         []Website  `json:"websites,omitempty"`
	CreatorWebsite   string     `json:"creator_website,omitempty"`
	Topics           []string   `json:"topics,omitempty"`
	Verified         bool       `json:"verified,omitempty"`
	Influencer       bool       `json:"influencer,omitempty"`
	Premium          bool       `json:"premium,omitempty"`
	Creator          bool       `json:"creator,omitempty"`
	TopVoice         bool       `json:"top_voice,omitempty"`
	Student          bool       `json:"student,omitempty"`
	Memorialized     bool       `json:"memorialized,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`

	Experience          []Experience          `json:"experience,omitempty"`
	Education           []Education           `json:"education,omitempty"`
	Skills              []string              `json:"skills,omitempty"`
	Certifications      []Certification       `json:"certifications,omitempty"`
	Languages           []Language            `json:"languages,omitempty"`
	VolunteerExperience []VolunteerExperience `json:"volunteer_experience,omitempty"`
	Projects            []Project             `json:"projects,omitempty"`
	TestScores          []TestScore           `json:"test_scores,omitempty"`
}

// Date is a partial calendar date. Month and day are omitted when the source
// does not provide them.
type Date struct {
	Year  int `json:"year"`
	Month int `json:"month,omitempty"`
	Day   int `json:"day,omitempty"`
}

// DateRange is a start with an optional end. A missing end marks an ongoing entry.
type DateRange struct {
	Start *Date `json:"start,omitempty"`
	End   *Date `json:"end,omitempty"`
}

// Experience is one position the member has held.
type Experience struct {
	Title       string     `json:"title,omitempty"`
	Company     string     `json:"company,omitempty"`
	CompanyURL  string     `json:"company_url,omitempty"`
	Location    string     `json:"location,omitempty"`
	Description string     `json:"description,omitempty"`
	DateRange   *DateRange `json:"date_range,omitempty"`
}

// Education is one school the member attended.
type Education struct {
	School       string     `json:"school,omitempty"`
	SchoolURL    string     `json:"school_url,omitempty"`
	Degree       string     `json:"degree,omitempty"`
	FieldOfStudy string     `json:"field_of_study,omitempty"`
	Grade        string     `json:"grade,omitempty"`
	Activities   string     `json:"activities,omitempty"`
	Description  string     `json:"description,omitempty"`
	DateRange    *DateRange `json:"date_range,omitempty"`
}

// Certification is a license or certification the member holds.
type Certification struct {
	Name          string     `json:"name,omitempty"`
	Authority     string     `json:"authority,omitempty"`
	AuthorityURL  string     `json:"authority_url,omitempty"`
	URL           string     `json:"url,omitempty"`
	LicenseNumber string     `json:"license_number,omitempty"`
	DateRange     *DateRange `json:"date_range,omitempty"`
}

// Language is a language the member lists with an optional proficiency.
type Language struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency,omitempty"`
}

// VolunteerExperience is one volunteer role the member has held.
type VolunteerExperience struct {
	Role            string     `json:"role,omitempty"`
	Organization    string     `json:"organization,omitempty"`
	OrganizationURL string     `json:"organization_url,omitempty"`
	Cause           string     `json:"cause,omitempty"`
	Description     string     `json:"description,omitempty"`
	DateRange       *DateRange `json:"date_range,omitempty"`
}

// Project is one project the member lists.
type Project struct {
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	DateRange   *DateRange `json:"date_range,omitempty"`
}

// TestScore is one test result the member lists.
type TestScore struct {
	Name        string `json:"name,omitempty"`
	Score       string `json:"score,omitempty"`
	Description string `json:"description,omitempty"`
	Date        *Date  `json:"date,omitempty"`
}

// Location is a resolved place. The base profile exposes the country code; the
// human-readable text is filled only when the source provides it.
type Location struct {
	CountryCode string `json:"country_code,omitempty"`
	Text        string `json:"text,omitempty"`
}

// Image holds the highest-resolution URL plus every sized variant the source
// exposes, ordered from smallest to largest.
type Image struct {
	URL      string         `json:"url"`
	Variants []ImageVariant `json:"variants,omitempty"`
}

// ImageVariant is one sized rendition of an image asset.
type ImageVariant struct {
	Width  int    `json:"width"`
	Height int    `json:"height,omitempty"`
	URL    string `json:"url"`
}

// Website is a link the member publishes on their profile.
type Website struct {
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
}

// Meta describes how and when a profile result was produced.
type Meta struct {
	RetrievedAt   time.Time         `json:"retrieved_at"`
	SchemaVersion string            `json:"schema_version"`
	Source        string            `json:"source"`
	Cached        bool              `json:"cached"`
	Sections      map[string]string `json:"sections,omitempty"`
}

// ProfileResult is the full envelope returned by the service and serialized by
// the API layer as {"data": ..., "meta": ...}.
type ProfileResult struct {
	Profile *Profile `json:"data"`
	Meta    Meta     `json:"meta"`
}
