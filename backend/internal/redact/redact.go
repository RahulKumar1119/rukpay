// Package redact removes OTP, PIN and password values from text before it is
// stored, logged or published (Requirements 11.3, 6.8, 8.9).
//
// It lives in its own package so that vault, guardian and httpapi can all use
// it without an import cycle.
package redact

import "regexp"

// Replacement is the text that stands in for a removed value.
const Replacement = "[REDACTED]"

// sensitive matches one of the words otp, pin, password or passcode, an
// optional ":" or "=", and the value that follows it.
var sensitive = regexp.MustCompile(`(?i)\b(otp|pin|password|passcode)\b\s*[:=]?\s*\S+`)

// Text returns s with every OTP, PIN, password or passcode field, together with
// its value, replaced by Replacement. Matching ignores case.
//
// The value is the next run of non-space characters after the field name and
// an optional ":" or "=". A value that is separated from its name by another
// word, as in "my PIN is 1234", is not recognised. The request types never
// carry such fields, so Text is a last line of defense for text that the
// service builds itself, not a filter for free-form input.
func Text(s string) string {
	return sensitive.ReplaceAllString(s, Replacement)
}
