// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package domain

// CredentialMode names which LinkedIn session a request uses. Every request is
// exactly one mode: the server-configured session, or a caller-supplied session
// carried only for that request. The mode is safe to log, label, and store; it
// never reveals any credential material.
type CredentialMode string

const (
	// ModeServer uses the server-configured LinkedIn session.
	ModeServer CredentialMode = "server_session"
	// ModeCaller uses a session supplied by the caller for a single request.
	ModeCaller CredentialMode = "caller_session"
)
