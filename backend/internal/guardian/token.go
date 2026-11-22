// Package guardian holds the guardian side of the cooling-off vault: the signed
// token that lets one family member act on one vault, the approve and veto
// flow, the evidence summary, and the hub that fans changes out to open event
// streams.
//
// The package depends on vault and never the other way round. vault reaches it
// only through the TokenIssuer and Observer interfaces, which Signer and Hub
// implement.
package guardian

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// ErrForbidden is the one error Verify returns, for every kind of failure: a
// malformed token, a wrong tag, a token for another vault, or an expired token.
// A caller cannot tell the causes apart, so a probe learns nothing from which
// check failed (Requirements 5.4, 5.7).
var ErrForbidden = errors.New("forbidden")

// Signer issues and verifies guardian tokens. Create it with NewSigner.
type Signer struct {
	key []byte
}

var _ vault.TokenIssuer = (*Signer)(nil)

// NewSigner returns a Signer that signs with key. It keeps its own copy of the
// key. The caller must pass a non-empty, secret key. httpapi.LoadConfig refuses
// to start without one.
func NewSigner(key []byte) *Signer {
	return &Signer{key: append([]byte(nil), key...)}
}

// Issue returns the guardian token for a vault:
//
//	<vault id>.<expiry>.<hex(HMAC-SHA256(key, vault id + "." + expiry))>
//
// expiry is the Token_Expiry in epoch seconds, the original lock deadline
// (Requirements 5.1, 5.2). The token is a pure function of the key, the id and
// the expiry, so it is never stored: vault recomputes it on every read.
//
// The vault id must not contain a ".". Vault ids are hex, so they never do.
func (s *Signer) Issue(vaultID string, expiry int64) string {
	payload := vaultID + "." + strconv.FormatInt(expiry, 10)
	return payload + "." + s.tag(payload)
}

// Verify checks token for the vault in the request path. It returns nil only
// when the token has exactly three parts, its tag is the one this key produces
// for the id and expiry in the token, the id equals pathID, and the expiry is
// later than now (epoch seconds). Every other case returns ErrForbidden
// (Requirements 5.3 to 5.7).
//
// The tag is compared with crypto/hmac.Equal before any other part of the token
// is used. The comparison is on the canonical lower-case hex form, so a token
// with one byte changed never verifies, including a tag whose hex digits only
// changed case.
func (s *Signer) Verify(token, pathID string, now int64) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ErrForbidden
	}
	id, expiryText, gotTag := parts[0], parts[1], parts[2]

	wantTag := s.tag(id + "." + expiryText)
	if !hmac.Equal([]byte(wantTag), []byte(gotTag)) {
		return ErrForbidden
	}
	if id != pathID {
		return ErrForbidden
	}
	expiry, err := strconv.ParseInt(expiryText, 10, 64)
	if err != nil || expiry <= now {
		return ErrForbidden
	}
	return nil
}

// tag returns the lower-case hex HMAC-SHA256 of payload.
func (s *Signer) tag(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
