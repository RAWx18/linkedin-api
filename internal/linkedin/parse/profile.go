// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package parse

import (
	"encoding/json"
	"html"
	"strings"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

// dashResponse is the collection envelope returned by the profiles endpoint.
type dashResponse struct {
	Elements []dashProfile `json:"elements"`
}

// dashProfile is the single member entity inside the collection. Only the fields
// the domain model needs are decoded; the rest of the large payload is ignored.
type dashProfile struct {
	FirstName         string        `json:"firstName"`
	LastName          string        `json:"lastName"`
	Headline          string        `json:"headline"`
	Summary           string        `json:"summary"`
	Premium           bool          `json:"premium"`
	Influencer        bool          `json:"influencer"`
	Creator           bool          `json:"creator"`
	Student           bool          `json:"student"`
	Memorialized      bool          `json:"memorialized"`
	Created           int64         `json:"created"`
	ProfilePicture    *dashPhoto    `json:"profilePicture"`
	BackgroundPicture *dashPhoto    `json:"backgroundPicture"`
	Location          *dashLocation `json:"location"`
	Websites          []dashWebsite `json:"websites"`
	PrimaryLocale     *dashLocale   `json:"primaryLocale"`
	SupportedLocales  []dashLocale  `json:"supportedLocales"`
	CreatorInfo       *dashCreator  `json:"creatorInfo"`
	TopVoiceBadge     *struct {
		BadgeText string `json:"badgeText"`
	} `json:"topVoiceBadge"`
	VerificationData *struct {
		VerificationState *struct {
			Verified json.RawMessage `json:"verified"`
		} `json:"verificationState"`
	} `json:"verificationData"`
}

type dashLocation struct {
	CountryCode string `json:"countryCode"`
}

type dashWebsite struct {
	URL      string `json:"url"`
	Category string `json:"category"`
}

type dashLocale struct {
	Country  string `json:"country"`
	Language string `json:"language"`
}

type dashCreator struct {
	CreatorWebsite *struct {
		Text string `json:"text"`
	} `json:"creatorWebsite"`
	AssociatedHashtagUrns []string `json:"associatedHashtagUrns"`
}

// dashPhoto wraps the vector image the profile and background pictures share.
type dashPhoto struct {
	DisplayImage *struct {
		VectorImage *vectorImage `json:"vectorImage"`
	} `json:"displayImage"`
}

func (d *dashPhoto) image() *domain.Image {
	if d == nil || d.DisplayImage == nil {
		return nil
	}
	return d.DisplayImage.VectorImage.image()
}

// Profile normalizes a DASH profiles response (q=memberIdentity) into the domain
// profile. Identity fields come from the validated request reference, never from
// the response body. An empty collection means the profile does not exist.
func Profile(raw json.RawMessage, ref urlx.ProfileRef) (*domain.Profile, error) {
	var resp dashResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, domain.UpstreamParse(err)
	}
	if len(resp.Elements) == 0 {
		return nil, domain.NotFound("the requested LinkedIn profile was not found")
	}
	e := resp.Elements[0]
	first, last := cleanText(e.FirstName), cleanText(e.LastName)
	return &domain.Profile{
		PublicIdentifier: ref.PublicID,
		ProfileURL:       ref.CanonicalURL,
		FirstName:        first,
		LastName:         last,
		FullName:         strings.TrimSpace(first + " " + last),
		Headline:         cleanText(e.Headline),
		Summary:          strPtr(cleanText(e.Summary)),
		ProfileLanguage:  e.language(),
		SupportedLocales: e.supportedLocales(),
		Premium:          e.Premium,
		Influencer:       e.Influencer,
		Creator:          e.Creator,
		TopVoice:         e.TopVoiceBadge != nil,
		Student:          e.Student,
		Memorialized:     e.Memorialized,
		Verified:         e.verified(),
		ProfilePicture:   e.ProfilePicture.image(),
		BackgroundImage:  e.BackgroundPicture.image(),
		Location:         e.location(),
		Websites:         e.sites(),
		CreatorWebsite:   e.creatorWebsite(),
		Topics:           e.topics(),
		CreatedAt:        e.created(),
	}, nil
}

// cleanText decodes the HTML entities LinkedIn embeds in display strings, such as
// &amp; for &, so the normalized profile carries readable text.
func cleanText(s string) string {
	return html.UnescapeString(s)
}

// ProfileURN extracts the member's profile URN from a DASH profiles response; it
// keys the section enrichment lookups and is empty when absent.
func ProfileURN(raw json.RawMessage) string {
	var resp struct {
		Elements []struct {
			EntityURN string `json:"entityUrn"`
		} `json:"elements"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Elements) == 0 {
		return ""
	}
	return resp.Elements[0].EntityURN
}

func (e dashProfile) verified() bool {
	vd := e.VerificationData
	if vd == nil || vd.VerificationState == nil {
		return false
	}
	v := vd.VerificationState.Verified
	return len(v) > 0 && string(v) != "null"
}

func (e dashProfile) location() *domain.Location {
	if e.Location == nil || e.Location.CountryCode == "" {
		return nil
	}
	return &domain.Location{CountryCode: e.Location.CountryCode}
}

func (e dashProfile) sites() []domain.Website {
	if len(e.Websites) == 0 {
		return nil
	}
	out := make([]domain.Website, 0, len(e.Websites))
	for _, w := range e.Websites {
		if w.URL == "" {
			continue
		}
		out = append(out, domain.Website{URL: w.URL, Category: w.Category})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// localeString renders a locale as language_COUNTRY, or just the language, and
// is empty when no language is present.
func localeString(l dashLocale) string {
	lang := strings.TrimSpace(l.Language)
	if lang == "" {
		return ""
	}
	if country := strings.TrimSpace(l.Country); country != "" {
		return lang + "_" + country
	}
	return lang
}

// language builds the primary locale as language_COUNTRY, or just the language.
func (e dashProfile) language() string {
	if e.PrimaryLocale == nil {
		return ""
	}
	return localeString(*e.PrimaryLocale)
}

// supportedLocales lists the distinct locales the profile publishes content in,
// preserving order and dropping duplicates and empty entries.
func (e dashProfile) supportedLocales() []string {
	if len(e.SupportedLocales) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(e.SupportedLocales))
	out := make([]string, 0, len(e.SupportedLocales))
	for _, l := range e.SupportedLocales {
		s := localeString(l)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// created converts the profile creation timestamp in milliseconds to a UTC time,
// and is nil when the source omits it.
func (e dashProfile) created() *time.Time {
	if e.Created <= 0 {
		return nil
	}
	t := time.UnixMilli(e.Created).UTC()
	return &t
}

// creatorWebsite returns the featured creator link when present.
func (e dashProfile) creatorWebsite() string {
	if e.CreatorInfo == nil || e.CreatorInfo.CreatorWebsite == nil {
		return ""
	}
	return strings.TrimSpace(e.CreatorInfo.CreatorWebsite.Text)
}

// topics extracts the distinct hashtag names a creator is associated with,
// preserving order and dropping duplicates and malformed urns.
func (e dashProfile) topics() []string {
	if e.CreatorInfo == nil || len(e.CreatorInfo.AssociatedHashtagUrns) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(e.CreatorInfo.AssociatedHashtagUrns))
	out := make([]string, 0, len(e.CreatorInfo.AssociatedHashtagUrns))
	for _, urn := range e.CreatorInfo.AssociatedHashtagUrns {
		name := hashtagName(urn)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// hashtagName extracts the tag from a hashtag urn of the form
// urn:li:fsd_hashtag:(name,urn:li:activity:-).
func hashtagName(urn string) string {
	open := strings.IndexByte(urn, '(')
	if open < 0 {
		return ""
	}
	rest := urn[open+1:]
	if end := strings.IndexAny(rest, ",)"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
