// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the built UI file system rooted at the dist directory.
func Assets() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
