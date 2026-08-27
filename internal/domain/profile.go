// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package domain

import "time"

// SchemaVersion identifies the shape of the public profile response. Bump it
// whenever the response contract changes in a backward-incompatible way.
const SchemaVersion = "2.0"

// Profile is the normalized, transport-independent representation of a LinkedIn
// profile. Optional scalars use pointers and collections are nil when absent so
// that missing information stays absent rather than surfacing as empty noise.
type Profile struct {
	PublicIdentifier string    `json:"public_identifier"`
	ProfileURL       string    `json:"profile_url"`
	FirstName        string    `json:"first_name,omitempty"`
	LastName         string    `json:"last_name,omitempty"`
	FullName         string    `json:"full_name,omitempty"`
	Headline         string    `json:"headline,omitempty"`
	Summary          *string   `json:"summary,omitempty"`
	Location         *Location `json:"location,omitempty"`
	ProfilePicture   *Image    `json:"profile_picture,omitempty"`
	BackgroundImage  *Image    `json:"background_image,omitempty"`
	Websites         []Website `json:"websites,omitempty"`
	Verified         bool      `json:"verified,omitempty"`
	Influencer       bool      `json:"influencer,omitempty"`
	Premium          bool      `json:"premium,omitempty"`
}

// Location is a resolved place. The base profile exposes the country code; the
// human-readable text is filled only when the source provides it.
type Location struct {
	CountryCode string `json:"country_code,omitempty"`
	Text        string `json:"text,omitempty"`
}

// Image holds the highest-resolution URL resolved from LinkedIn's vector assets.
type Image struct {
	URL string `json:"url"`
}

// Website is a link the member publishes on their profile.
type Website struct {
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
}

// Meta describes how and when a profile result was produced.
type Meta struct {
	RetrievedAt   time.Time `json:"retrieved_at"`
	SchemaVersion string    `json:"schema_version"`
	Source        string    `json:"source"`
	Cached        bool      `json:"cached"`
}

// ProfileResult is the full envelope returned by the service and serialized by
// the API layer as {"data": ..., "meta": ...}.
type ProfileResult struct {
	Profile *Profile `json:"data"`
	Meta    Meta     `json:"meta"`
}
