package redact

import "testing"

func TestTextRedactsSensitiveFields(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"otp with colon", "otp: 482913", "[REDACTED]"},
		{"pin with equals", "pin=4821", "[REDACTED]"},
		{"password with space", "password hunter2", "[REDACTED]"},
		{"passcode", "passcode:abc123", "[REDACTED]"},
		{"upper case", "OTP: 123456", "[REDACTED]"},
		{"mixed case", "PaSsWoRd=secret", "[REDACTED]"},
		{"inside a sentence", "Sent. otp: 123456 was used.", "Sent. [REDACTED] was used."},
		{"two fields", "pin=1111 password=abcd", "[REDACTED] [REDACTED]"},
		{"no field", "Payment of 450 rupees to a known contact", "Payment of 450 rupees to a known contact"},
		{"masked identifiers stay", "XXXX-XXXX-4821 and r***@okaxis", "XXXX-XXXX-4821 and r***@okaxis"},
		{"word that only contains a field name", "spinning pinned shopping", "spinning pinned shopping"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.in); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
