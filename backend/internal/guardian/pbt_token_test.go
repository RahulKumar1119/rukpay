package guardian

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// keyGen draws a signing key of 1 to 64 bytes.
func keyGen() *rapid.Generator[[]byte] {
	return rapid.SliceOfN(rapid.Byte(), 1, 64)
}

// sameHMACKey reports whether two keys of at most one block (64 bytes) give the
// same HMAC for every message. HMAC pads a short key with zero bytes, so keys that
// differ only by trailing zeros are the same key.
func sameHMACKey(a, b []byte) bool {
	return bytes.Equal(bytes.TrimRight(a, "\x00"), bytes.TrimRight(b, "\x00"))
}

// Feature: cooling-off-vault, Property 12: Guardian token round trip
//
// A token verifies for the vault id it was issued for while now is before its
// expiry. At or after the expiry it never verifies, even with a correct tag.
//
// **Validates: Requirements 5.1, 5.2, 5.3, 5.5, 12.5, 12.6**
func TestGuardianTokenRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key := keyGen().Draw(rt, "key")
		id := vaultIDGen().Draw(rt, "vault_id")
		expiry := rapid.Int64Range(1, 4_000_000_000).Draw(rt, "expiry")
		delta := rapid.OneOf(
			rapid.SampledFrom([]int64{-1000, -2, -1, 0, 1, 2, 1000}),
			rapid.Int64Range(-100000, 100000),
		).Draw(rt, "delta")
		now := expiry + delta

		signer := NewSigner(key)
		token := signer.Issue(id, expiry)
		err := signer.Verify(token, id, now)

		if delta < 0 {
			if err != nil {
				rt.Fatalf("Verify at %d before expiry %d = %v, want nil", now, expiry, err)
			}
			return
		}
		if err != ErrForbidden {
			rt.Fatalf("Verify at %d, at or after expiry %d = %v, want ErrForbidden", now, expiry, err)
		}
	})
}

// Feature: cooling-off-vault, Property 12: Guardian token round trip
//
// The token has the documented shape and the tag is HMAC-SHA256 of
// "<id>.<expiry>", computed here independently of the code under test.
//
// **Validates: Requirements 5.1, 5.2**
func TestGuardianTokenHasTheDocumentedShape(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key := keyGen().Draw(rt, "key")
		id := vaultIDGen().Draw(rt, "vault_id")
		expiry := rapid.Int64Range(1, 4_000_000_000).Draw(rt, "expiry")

		token := NewSigner(key).Issue(id, expiry)

		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(id + "." + strconv.FormatInt(expiry, 10)))
		want := id + "." + strconv.FormatInt(expiry, 10) + "." + hex.EncodeToString(mac.Sum(nil))
		if token != want {
			rt.Fatalf("Issue = %q, want %q", token, want)
		}
	})
}

// Feature: cooling-off-vault, Property 13: Any tampering breaks the token
//
// Changing any single byte of a valid token makes Verify return ErrForbidden.
//
// **Validates: Requirements 5.3, 5.4, 5.6, 5.7, 12.5**
func TestChangingOneByteBreaksTheToken(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key := keyGen().Draw(rt, "key")
		id := vaultIDGen().Draw(rt, "vault_id")
		expiry := rapid.Int64Range(1000, 4_000_000_000).Draw(rt, "expiry")

		signer := NewSigner(key)
		token := signer.Issue(id, expiry)
		if err := signer.Verify(token, id, expiry-1); err != nil {
			rt.Fatalf("the untouched token does not verify: %v", err)
		}

		index := rapid.IntRange(0, len(token)-1).Draw(rt, "index")
		replacement := rapid.Byte().Filter(func(b byte) bool { return b != token[index] }).Draw(rt, "replacement")
		tampered := []byte(token)
		tampered[index] = replacement

		if err := signer.Verify(string(tampered), id, expiry-1); err != ErrForbidden {
			rt.Fatalf("Verify of %q (byte %d changed to %#x) = %v, want ErrForbidden", tampered, index, replacement, err)
		}
	})
}

// Feature: cooling-off-vault, Property 13: Any tampering breaks the token
//
// A token fails for another vault's path, for another key, and when it has fewer
// or more than three parts. Every failure is the same error value.
//
// **Validates: Requirements 5.3, 5.4, 5.6, 5.7, 12.5**
func TestTokenFailsForAnotherVaultKeyOrShape(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key := keyGen().Draw(rt, "key")
		otherKey := keyGen().Filter(func(k []byte) bool { return !sameHMACKey(k, key) }).Draw(rt, "other_key")
		id := vaultIDGen().Draw(rt, "vault_id")
		otherID := vaultIDGen().Filter(func(s string) bool { return s != id }).Draw(rt, "other_id")
		expiry := rapid.Int64Range(1000, 4_000_000_000).Draw(rt, "expiry")

		signer := NewSigner(key)
		token := signer.Issue(id, expiry)
		now := expiry - 1

		if err := signer.Verify(token, otherID, now); err != ErrForbidden {
			rt.Fatalf("Verify for vault %s of a token for %s = %v, want ErrForbidden", otherID, id, err)
		}
		if err := NewSigner(otherKey).Verify(token, id, now); err != ErrForbidden {
			rt.Fatalf("Verify with another key = %v, want ErrForbidden", err)
		}

		parts := strings.Split(token, ".")
		shapes := []string{
			"",
			".",
			"..",
			"...",
			id,
			parts[0] + "." + parts[1],
			parts[1] + "." + parts[2],
			token + ".extra",
			token + ".",
			"." + token,
			strings.Join(parts[:2], ".") + "..",
		}
		for _, shape := range shapes {
			if err := signer.Verify(shape, id, now); err != ErrForbidden {
				rt.Fatalf("Verify(%q) = %v, want ErrForbidden", shape, err)
			}
		}
	})
}

// Feature: cooling-off-vault, Property 13: Any tampering breaks the token
//
// A tampered token sent to Act is refused with the same error, and the store is
// not called at all, so no UpdateIf happens.
//
// **Validates: Requirements 5.3, 5.4, 12.5**
func TestTamperedTokenMakesNoStoreCall(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		action := rapid.SampledFrom([]Action{
			{Action: ActionApprove, Confirmed: true},
			{Action: ActionApprove, Confirmed: false},
			{Action: ActionVeto},
			{Action: "NOT-AN-ACTION"},
		}).Draw(rt, "action")

		e := newEnv(false)
		v := e.vaultIn(rt, store.StateLocked15M)
		token := v.GuardianToken
		before := e.record(rt, v.ID)
		callsBefore, _, _ := e.cs.snapshot()

		kind := rapid.SampledFrom([]string{"byte", "other vault", "other key", "shape", "expired"}).Draw(rt, "kind")
		switch kind {
		case "byte":
			index := rapid.IntRange(0, len(token)-1).Draw(rt, "index")
			b := []byte(token)
			b[index] ^= byte(rapid.IntRange(1, 255).Draw(rt, "flip"))
			action.Token = string(b)
		case "other vault":
			action.Token = e.signer.Issue(strings.Repeat("0", 32), e.clock.Now().Unix()+1000)
		case "other key":
			action.Token = NewSigner([]byte("some-other-key")).Issue(v.ID, e.clock.Now().Unix()+1000)
		case "shape":
			action.Token = rapid.SampledFrom([]string{"", "a.b", "a.b.c.d", v.ID}).Draw(rt, "shape")
		default: // an honest token that has run out
			e.clock.AdvanceSeconds(900)
			action.Token = token
		}

		res, err := e.guardian.Act(context.Background(), v.ID, action)
		if err != ErrForbidden {
			rt.Fatalf("Act with a %s token = %v, want ErrForbidden", kind, err)
		}
		if res != (Result{}) {
			rt.Fatalf("Act with a %s token returned %+v, want the zero Result", kind, res)
		}

		calls, _, updateIf := e.cs.snapshot()
		if calls != callsBefore || updateIf != 0 {
			rt.Fatalf("Act with a %s token called the store (%d calls, %d UpdateIf)", kind, calls-callsBefore, updateIf)
		}
		if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) {
			rt.Fatalf("Act with a %s token changed the record: %+v -> %+v", kind, before, after)
		}
	})
}
