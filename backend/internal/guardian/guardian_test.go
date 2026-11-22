package guardian

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

func TestSignerIssuesTheDocumentedExample(t *testing.T) {
	signer := NewSigner([]byte("key"))
	token := signer.Issue("abc123", 1000)

	// The tag is HMAC-SHA256 with key "key" over "abc123.1000", computed
	// outside this program.
	const want = "abc123.1000.77a0747ef9318d4c526ee00e2721ae0e6e1432cdb6d63ae1cc556020bc578604"
	if token != want {
		t.Fatalf("Issue = %q, want %q", token, want)
	}

	if err := signer.Verify(token, "abc123", 999); err != nil {
		t.Errorf("Verify of a fresh token = %v, want nil", err)
	}
	if err := signer.Verify(token, "abc123", 1000); err != ErrForbidden {
		t.Errorf("Verify at the expiry = %v, want ErrForbidden", err)
	}
}

func TestSignerKeepsItsOwnCopyOfTheKey(t *testing.T) {
	key := []byte("original-key")
	signer := NewSigner(key)
	token := signer.Issue("abc", 1000)

	key[0] = 'X' // the caller changes its slice afterwards
	if err := signer.Verify(token, "abc", 1); err != nil {
		t.Errorf("Verify after the caller changed its key slice = %v, want nil", err)
	}
}

func TestSignerRefusesAnUppercaseTag(t *testing.T) {
	signer := NewSigner([]byte("key"))
	token := signer.Issue("abc", 1000)
	i := strings.LastIndex(token, ".")
	upper := token[:i+1] + strings.ToUpper(token[i+1:])

	if upper == token {
		t.Skip("the tag has no letters to change")
	}
	if err := signer.Verify(upper, "abc", 1); err != ErrForbidden {
		t.Errorf("Verify of an upper-case tag = %v, want ErrForbidden", err)
	}
}

// Every failure of Verify is the same value, so a caller cannot tell them apart.
func TestVerifyFailuresAreIdentical(t *testing.T) {
	signer := NewSigner([]byte("key"))
	good := signer.Issue("abc", 1000)
	expired := signer.Issue("abc", 10)
	other := NewSigner([]byte("other")).Issue("abc", 1000)

	failures := map[string]error{
		"empty":         signer.Verify("", "abc", 1),
		"two parts":     signer.Verify("abc.1000", "abc", 1),
		"four parts":    signer.Verify(good+".x", "abc", 1),
		"bad tag":       signer.Verify(other, "abc", 1),
		"other vault":   signer.Verify(good, "xyz", 1),
		"expired":       signer.Verify(expired, "abc", 500),
		"bad expiry":    signer.Verify("abc.zz."+strings.Repeat("0", 64), "abc", 1),
		"tag not hex":   signer.Verify("abc.1000."+strings.Repeat("g", 64), "abc", 1),
		"short tag":     signer.Verify("abc.1000.00", "abc", 1),
		"expiry sign +": signer.Verify(strings.Replace(good, ".1000.", ".+1000.", 1), "abc", 1),
	}
	for name, err := range failures {
		if err != ErrForbidden {
			t.Errorf("%s: Verify = %v, want exactly ErrForbidden", name, err)
		}
	}
}

// The token check comes before everything else: a bad token with a bad action is
// forbidden, not invalid.
func TestActChecksTheTokenBeforeTheAction(t *testing.T) {
	e := newEnv(false)
	v := e.vaultIn(t, store.StateLocked15M)

	_, err := e.guardian.Act(context.Background(), v.ID, Action{Action: "NOT-AN-ACTION", Token: "bad"})
	if err != ErrForbidden {
		t.Fatalf("Act = %v, want ErrForbidden, not ErrInvalidAction", err)
	}
	// Creating the vault made one store call (Create). Act made none.
	if calls, _, _ := e.cs.snapshot(); calls != 1 {
		t.Fatalf("the store saw %d calls, want only the 1 from creating the vault", calls)
	}
}

func TestActApproveAndVetoEndToEnd(t *testing.T) {
	ctx := context.Background()

	t.Run("approve needs the second tap", func(t *testing.T) {
		e := newEnv(false)
		v := e.vaultIn(t, store.StateLocked15M)

		first, err := e.guardian.Act(ctx, v.ID, Action{Action: ActionApprove, Token: v.GuardianToken})
		if err != nil || first.Status != StatusConfirmationRequired || first.View.State != store.StateLocked15M {
			t.Fatalf("first tap = %+v, %v, want CONFIRMATION_REQUIRED on a locked vault", first, err)
		}
		second, err := e.guardian.Act(ctx, v.ID, Action{Action: ActionApprove, Token: v.GuardianToken, Confirmed: true})
		if err != nil || second.Status != StatusOK || second.View.State != store.StateGuardianApproved || !second.View.Settled {
			t.Fatalf("second tap = %+v, %v, want OK and a settled GUARDIAN_APPROVED vault", second, err)
		}
		if second.View.GuardianToken != "" {
			t.Errorf("an approved vault still shows the token %q", second.View.GuardianToken)
		}
	})

	t.Run("veto writes the full evidence", func(t *testing.T) {
		e := newEnv(false)
		v := e.vaultInWith(t, store.StateLocked15M, vault.EvaluateRequest{
			PayerAccountMasked: "XXXX-XXXX-4821",
			UPIMasked:          "r***@okaxis",
		})

		res, err := e.guardian.Act(ctx, v.ID, Action{Action: ActionVeto, Token: v.GuardianToken})
		if err != nil || res.Status != StatusOK || res.View.State != store.StateVetoed {
			t.Fatalf("VETO = %+v, %v, want OK and a VETOED vault", res, err)
		}
		for _, want := range []string{"Amount: INR 85000", "Payer account: XXXX-XXXX-4821", "UPI ID: r***@okaxis", "Risk score: 90", "1930", "cybercrime.gov.in"} {
			if !strings.Contains(res.View.Evidence, want) {
				t.Errorf("evidence %q does not contain %q: the guardian builder was not wired in", res.View.Evidence, want)
			}
		}
	})

	t.Run("the user can veto with the token from their own read", func(t *testing.T) {
		e := newEnv(false)
		created := e.vaultIn(t, store.StateLocked15M)
		read, err := e.vault.Get(ctx, created.ID)
		if err != nil || read.GuardianToken == "" {
			t.Fatalf("Get = %+v, %v, want a view with the token", read, err)
		}
		res, err := e.guardian.Act(ctx, created.ID, Action{Action: ActionVeto, Token: read.GuardianToken})
		if err != nil || res.View.State != store.StateVetoed {
			t.Fatalf("VETO with the user's token = %+v, %v, want a VETOED vault", res, err)
		}
	})

	t.Run("a token for one vault does not work on another", func(t *testing.T) {
		e := newEnv(false)
		a := e.vaultIn(t, store.StateLocked15M)
		b := e.vaultIn(t, store.StateLocked15M)

		_, err := e.guardian.Act(ctx, b.ID, Action{Action: ActionVeto, Token: a.GuardianToken})
		if err != ErrForbidden {
			t.Fatalf("Act on vault B with vault A's token = %v, want ErrForbidden", err)
		}
		if got, _ := e.vault.Get(ctx, b.ID); got.State != store.StateLocked15M {
			t.Errorf("vault B is %s, want it untouched", got.State)
		}
	})

	t.Run("a second action loses to the first", func(t *testing.T) {
		e := newEnv(false)
		v := e.vaultIn(t, store.StateLocked15M)
		if _, err := e.guardian.Act(ctx, v.ID, Action{Action: ActionVeto, Token: v.GuardianToken}); err != nil {
			t.Fatalf("VETO = %v, want nil", err)
		}
		_, err := e.guardian.Act(ctx, v.ID, Action{Action: ActionApprove, Token: v.GuardianToken, Confirmed: true})
		var conflict *vault.ConflictError
		if !errors.As(err, &conflict) || conflict.Current.State != store.StateVetoed {
			t.Fatalf("APPROVE after VETO = %v, want a conflict on a VETOED vault", err)
		}
	})

	t.Run("an unknown vault with a valid token is not found", func(t *testing.T) {
		e := newEnv(false)
		id := strings.Repeat("a", 32)
		token := e.signer.Issue(id, e.clock.Now().Unix()+100)
		_, err := e.guardian.Act(ctx, id, Action{Action: ActionVeto, Token: token})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Act on an unknown vault = %v, want store.ErrNotFound", err)
		}
	})
}

// A change made through Act reaches the hub, without the token.
func TestActPublishesToTheHubWithoutTheToken(t *testing.T) {
	e := newEnv(false)
	v := e.vaultIn(t, store.StateLocked15M)
	events, cancel := e.hub.Subscribe(v.ID)
	defer cancel()

	if _, err := e.guardian.Act(context.Background(), v.ID, Action{Action: ActionVeto, Token: v.GuardianToken}); err != nil {
		t.Fatalf("VETO = %v, want nil", err)
	}

	select {
	case raw := <-events:
		var got vault.View
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("event %q is not a View: %v", raw, err)
		}
		if got.ID != v.ID || got.State != store.StateVetoed {
			t.Errorf("event = %+v, want the VETOED view of %s", got, v.ID)
		}
		if strings.Contains(string(raw), "guardian_token") || strings.Contains(string(raw), v.GuardianToken) {
			t.Errorf("event %s carries the guardian token", raw)
		}
		if strings.Contains(string(raw), string(harnessKey)) {
			t.Errorf("event %s carries the signing key", raw)
		}
	default:
		t.Fatal("the veto published no event")
	}
}

// Lazy expiry that a read triggers is published too.
func TestLazyExpiryReachesTheHub(t *testing.T) {
	e := newEnv(false)
	v := e.vaultIn(t, store.StateLocked15M)
	events, cancel := e.hub.Subscribe(v.ID)
	defer cancel()

	e.clock.AdvanceSeconds(vault.LockDuration)
	if _, err := e.vault.Get(context.Background(), v.ID); err != nil {
		t.Fatalf("Get = %v, want nil", err)
	}

	select {
	case raw := <-events:
		var got vault.View
		if err := json.Unmarshal(raw, &got); err != nil || got.State != store.StateExpired || !got.ReconfirmNeeded {
			t.Errorf("event = %s, %v, want an EXPIRED view that needs reconfirmation", raw, err)
		}
	default:
		t.Fatal("the lazy expiry published no event")
	}
}

// The signing key never shows up in anything a caller can see.
func TestSigningKeyNeverAppearsInResults(t *testing.T) {
	e := newEnv(false)
	v := e.vaultIn(t, store.StateLocked15M)
	res, err := e.guardian.Act(context.Background(), v.ID, Action{Action: ActionVeto, Token: v.GuardianToken})
	if err != nil {
		t.Fatalf("VETO = %v, want nil", err)
	}

	body, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal Result: %v", err)
	}
	if strings.Contains(string(body), string(harnessKey)) {
		t.Errorf("the result carries the signing key: %s", body)
	}
	if rec := e.record(t, v.ID); strings.Contains(rec.Reason+rec.EvidenceSummary, string(harnessKey)) {
		t.Errorf("the stored record carries the signing key")
	}
	if _, err := e.guardian.Act(context.Background(), v.ID, Action{Action: ActionVeto, Token: "bad"}); strings.Contains(err.Error(), string(harnessKey)) {
		t.Errorf("an error message carries the signing key: %v", err)
	}
}

// HMAC pads a short key with zero bytes, so two keys that differ only by trailing
// zero bytes sign identically. Real signing keys are random and long, so this is
// not a weakness, but a test must not assume that every pair of keys differs.
func TestKeysThatDifferOnlyByTrailingZerosSignTheSame(t *testing.T) {
	a := NewSigner([]byte{0x1e})
	b := NewSigner([]byte{0x1e, 0x00})

	if a.Issue("abc", 1000) != b.Issue("abc", 1000) {
		t.Fatal("keys that differ only by a trailing zero byte signed differently")
	}
	if err := b.Verify(a.Issue("abc", 1000), "abc", 1); err != nil {
		t.Errorf("Verify = %v, want nil: HMAC treats the two keys as one", err)
	}
	if NewSigner([]byte{0x1e, 0x01}).Issue("abc", 1000) == a.Issue("abc", 1000) {
		t.Fatal("keys that differ in a non-zero byte signed the same")
	}
}
