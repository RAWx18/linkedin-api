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
