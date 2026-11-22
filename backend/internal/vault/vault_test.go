package vault

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

func TestVaultDemoPresets(t *testing.T) {
	tests := []struct {
		name      string
		signals   risk.Signals
		wantState store.VaultState
		wantScore int
		settled   bool
		locked    bool
	}{
		{"450 rupees to a known contact", signalsInstantPass(), store.StateInstantPass, 0, true, false},
		{"18,000 rupees to a new contact", signalsRealityCheck(), store.StateRealityCheck, 45, false, false},
		{"85,000 rupees late at night on a call", signalsLocked(), store.StateLocked15M, 90, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(false)
			v := e.create(t, tt.signals)

			if v.State != tt.wantState || v.RiskScore != tt.wantScore || v.Settled != tt.settled {
				t.Fatalf("View = state %s score %d settled %v, want %s %d %v",
					v.State, v.RiskScore, v.Settled, tt.wantState, tt.wantScore, tt.settled)
			}
			if v.Reason == "" {
				t.Error("Reason is empty, want a plain-language message")
			}
			if tt.locked {
				if v.GuardianToken == "" {
					t.Error("a locked vault has no guardian token")
				}
				if v.RemainingSeconds != LockDuration {
					t.Errorf("RemainingSeconds = %d, want %d", v.RemainingSeconds, LockDuration)
				}
				if !strings.Contains(v.Reason, "15-minute") {
					t.Errorf("Reason %q does not state the 15-minute hold", v.Reason)
				}
			} else if v.GuardianToken != "" {
				t.Errorf("an unlocked vault has guardian token %q", v.GuardianToken)
			}
		})
	}
}

func TestVaultLockedReasonNamesOnlyTrueSignals(t *testing.T) {
	e := newEnv(false)
	v := e.create(t, signalsLocked())

	for _, want := range []string{"a large amount", "a brand-new recipient", "the late hour", "an active unknown call"} {
		if !strings.Contains(v.Reason, want) {
			t.Errorf("Reason %q does not mention %q", v.Reason, want)
		}
	}
	if strings.Contains(v.Reason, "retries") {
		t.Errorf("Reason %q mentions retries, which did not happen", v.Reason)
	}
}

func TestVaultIDFormatAndUniqueness(t *testing.T) {
	const count = 10000
	e := newEnv(false)
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := make(map[string]struct{}, count)

	for i := 0; i < count; i++ {
		v, err := e.svc.Evaluate(context.Background(), EvaluateRequest{Signals: signalsInstantPass()})
		if err != nil {
			t.Fatalf("Evaluate #%d = %v, want nil", i, err)
		}
		if !hex32.MatchString(v.ID) {
			t.Fatalf("id %q is not 32 lower-case hex characters", v.ID)
		}
		if _, dup := seen[v.ID]; dup {
			t.Fatalf("id %q was generated twice", v.ID)
		}
		seen[v.ID] = struct{}{}
	}
}

func TestVaultEvaluateRejectsBadInputBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name    string
		req     EvaluateRequest
		wantErr func(error) bool
	}{
		{
			name: "invalid amount",
			req:  EvaluateRequest{Signals: risk.Signals{AmountINR: 0}},
			wantErr: func(err error) bool {
				var ve *risk.ValidationError
				return errors.As(err, &ve) && ve.Field == "amount_inr"
			},
		},
		{
			name: "raw account number",
			req: EvaluateRequest{
				Signals:            signalsInstantPass(),
				PayerAccountMasked: "123456789012",
			},
			wantErr: func(err error) bool { return errors.Is(err, store.ErrUnmaskedIdentifier) },
		},
		{
			name: "raw UPI id",
			req: EvaluateRequest{
				Signals:   signalsInstantPass(),
				UPIMasked: "rahul@okaxis",
			},
			wantErr: func(err error) bool { return errors.Is(err, store.ErrUnmaskedIdentifier) },
		},
		{
			name: "bad identifier wins over a bad signal",
			req: EvaluateRequest{
				Signals:     risk.Signals{AmountINR: 0},
				PhoneMasked: "9876543210",
			},
			wantErr: func(err error) bool { return errors.Is(err, store.ErrUnmaskedIdentifier) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(false)
			_, err := e.svc.Evaluate(context.Background(), tt.req)
			if err == nil || !tt.wantErr(err) {
				t.Fatalf("Evaluate = %v, want the expected error", err)
			}
			if e.cs.writeCount() != 0 {
				t.Fatalf("a rejected request wrote %d records", e.cs.writeCount())
			}
		})
	}
}

func TestVaultEvaluateKeepsMaskedMetadata(t *testing.T) {
	e := newEnv(false)
	req := EvaluateRequest{
		Signals:            signalsInstantPass(),
		PayerAccountMasked: "XXXX-XXXX-4821",
		BeneficiaryMasked:  "XXXX-XXXX-1100",
		UPIMasked:          "r***@okaxis",
		PhoneMasked:        "+91-XXXXXX3210",
	}
	v, err := e.svc.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("Evaluate = %v, want nil", err)
	}
	want := Metadata{
		PayerAccountMasked: "XXXX-XXXX-4821",
		BeneficiaryMasked:  "XXXX-XXXX-1100",
		UPIMasked:          "r***@okaxis",
		PhoneMasked:        "+91-XXXXXX3210",
		AmountINR:          450,
	}
	if v.Metadata != want {
		t.Fatalf("Metadata = %+v, want %+v", v.Metadata, want)
	}
}

func TestVaultCancelledContextWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("evaluate", func(t *testing.T) {
		e := newEnv(false)
		_, err := e.svc.Evaluate(ctx, EvaluateRequest{Signals: signalsLocked()})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Evaluate = %v, want context.Canceled", err)
		}
		if e.cs.writeCount() != 0 {
			t.Fatalf("a cancelled Evaluate wrote %d records", e.cs.writeCount())
		}
	})

	t.Run("operations on an existing vault", func(t *testing.T) {
		e := newEnv(true)
		locked := e.create(t, signalsLocked())
		rc := e.create(t, signalsRealityCheck())
		writes := e.cs.writeCount()
		before := e.record(t, locked.ID)

		calls := map[string]func() error{
			"get":          func() error { _, err := e.svc.Get(ctx, locked.ID); return err },
			"approve":      func() error { _, err := e.svc.Approve(ctx, locked.ID); return err },
			"veto":         func() error { _, err := e.svc.Veto(ctx, locked.ID); return err },
			"fast-forward": func() error { _, err := e.svc.FastForward(ctx, locked.ID); return err },
			"reconfirm":    func() error { _, err := e.svc.Reconfirm(ctx, locked.ID); return err },
			"reality check": func() error {
				_, err := e.svc.RealityCheck(ctx, rc.ID, Answers{AnswerYes, AnswerYes, AnswerNo})
				return err
			},
		}
		for name, call := range calls {
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Errorf("%s = %v, want context.Canceled", name, err)
			}
		}
		if got := e.cs.writeCount(); got != writes {
			t.Errorf("cancelled calls wrote %d records", got-writes)
		}
		if after := e.record(t, locked.ID); after != before {
			t.Errorf("record changed: %+v -> %+v", before, after)
		}
	})
}

// A lazy expiry that loses a race must re-read and use what the winner wrote.
func TestVaultLazyExpiryRaceRereads(t *testing.T) {
	e := newEnv(false)
	v := e.create(t, signalsLocked())

	// Just before the lazy expiry's UpdateIf runs, a guardian vetoes the vault
	// through the store, so the expiry write finds the state changed.
	e.cs.beforeUpdate = func(id string, expected store.VaultState) {
		rec := e.record(t, id)
		rec.State = store.StateVetoed
		rec.Version++
		if err := e.mem.UpdateIf(context.Background(), id, store.StateLocked15M, rec); err != nil {
			t.Errorf("racing UpdateIf = %v, want nil", err)
		}
	}
	e.clock.AdvanceSeconds(LockDuration + 5)

	got, err := e.svc.Get(context.Background(), v.ID)
	if err != nil {
		t.Fatalf("Get = %v, want nil", err)
	}
	if got.State != store.StateVetoed {
		t.Fatalf("Get returned state %s, want VETOED, the state the racing writer wrote", got.State)
	}
	if e.cs.updateIfCall != 1 {
		t.Errorf("UpdateIf calls = %d, want 1", e.cs.updateIfCall)
	}
}

func TestVaultLazyExpiryPublishesOnce(t *testing.T) {
	e := newEnv(false)
	v := e.create(t, signalsLocked())
	e.clock.AdvanceSeconds(LockDuration)

	for i := 0; i < 3; i++ {
		got, err := e.svc.Get(context.Background(), v.ID)
		if err != nil || got.State != store.StateExpired || got.Settled || !got.ReconfirmNeeded {
			t.Fatalf("Get #%d = %+v, %v, want an unsettled EXPIRED vault that needs reconfirmation", i, got, err)
		}
	}

	views := e.obs.all()
	if len(views) != 1 || views[0].State != store.StateExpired {
		t.Fatalf("observer saw %d views %+v, want exactly one EXPIRED view", len(views), views)
	}
	if views[0].GuardianToken != "" {
		t.Error("the EXPIRED view carries a guardian token")
	}
}

func TestVaultRealityCheckRejectsASettledVault(t *testing.T) {
	e := newEnv(false)
	rc := e.create(t, signalsRealityCheck())
	calm := Answers{Coercion: AnswerNo, Secrecy: AnswerNo, Verification: AnswerYes}

	first, err := e.svc.RealityCheck(context.Background(), rc.ID, calm)
	if err != nil || first.State != store.StateRealityCheck || !first.Settled {
		t.Fatalf("first check = %+v, %v, want a settled REALITY_CHECK vault", first, err)
	}
	before := e.record(t, rc.ID)

	// A second submission, even a scary one, must not change a passed check.
	scary := Answers{Coercion: AnswerYes, Secrecy: AnswerYes, Verification: AnswerNo}
	_, err = e.svc.RealityCheck(context.Background(), rc.ID, scary)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second check = %v, want *ConflictError", err)
	}
	if conflict.Current.State != store.StateRealityCheck || !conflict.Current.Settled {
		t.Errorf("conflict carries %+v, want the settled REALITY_CHECK view", conflict.Current)
	}
	if after := e.record(t, rc.ID); after != before {
		t.Errorf("record changed: %+v -> %+v", before, after)
	}
}

func TestVaultMissingVault(t *testing.T) {
	e := newEnv(true)
	ctx := context.Background()
	id := strings.Repeat("0", 32)

	calls := map[string]func() error{
		"get":          func() error { _, err := e.svc.Get(ctx, id); return err },
		"approve":      func() error { _, err := e.svc.Approve(ctx, id); return err },
		"veto":         func() error { _, err := e.svc.Veto(ctx, id); return err },
		"reconfirm":    func() error { _, err := e.svc.Reconfirm(ctx, id); return err },
		"fast-forward": func() error { _, err := e.svc.FastForward(ctx, id); return err },
		"reality check": func() error {
			_, err := e.svc.RealityCheck(ctx, id, Answers{AnswerNo, AnswerNo, AnswerYes})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s = %v, want store.ErrNotFound", name, err)
		}
	}
}

func TestVaultFastForwardNeedsDemoMode(t *testing.T) {
	e := newEnv(false)
	v := e.create(t, signalsLocked())
	before := e.record(t, v.ID)

	_, err := e.svc.FastForward(context.Background(), v.ID)
	if !errors.Is(err, ErrNotDemoMode) {
		t.Fatalf("FastForward with demo off = %v, want ErrNotDemoMode", err)
	}
	if after := e.record(t, v.ID); after != before {
		t.Errorf("record changed: %+v -> %+v", before, after)
	}
}

func TestVaultFullFlows(t *testing.T) {
	ctx := context.Background()

	t.Run("locked then approved", func(t *testing.T) {
		e := newEnv(false)
		v := e.create(t, signalsLocked())
		got, err := e.svc.Approve(ctx, v.ID)
		if err != nil || got.State != store.StateGuardianApproved || !got.Settled || got.GuardianToken != "" {
			t.Fatalf("Approve = %+v, %v, want a settled GUARDIAN_APPROVED view without a token", got, err)
		}
	})

	t.Run("locked then vetoed", func(t *testing.T) {
		e := newEnv(false)
		v := e.create(t, signalsLocked())
		got, err := e.svc.Veto(ctx, v.ID)
		if err != nil || got.State != store.StateVetoed || got.Settled {
			t.Fatalf("Veto = %+v, %v, want an unsettled VETOED view", got, err)
		}
		for _, want := range []string{v.ID, "1930", "cybercrime.gov.in"} {
			if !strings.Contains(got.Evidence, want) {
				t.Errorf("evidence %q does not contain %q", got.Evidence, want)
			}
		}
		again, err := e.svc.Get(ctx, v.ID)
		if err != nil || again.Evidence != got.Evidence {
			t.Errorf("later Get evidence = %q, %v, want it unchanged", again.Evidence, err)
		}
	})

	t.Run("locked then fast-forward then expired then reconfirmed", func(t *testing.T) {
		e := newEnv(true)
		v := e.create(t, signalsLocked())

		ff, err := e.svc.FastForward(ctx, v.ID)
		if err != nil || ff.State != store.StateLocked15M || ff.RemainingSeconds != fastForwardSeconds {
			t.Fatalf("FastForward = %+v, %v, want LOCKED_15M with %d seconds left", ff, err, fastForwardSeconds)
		}
		if rec := e.record(t, v.ID); rec.TokenExpiry != rec.CreatedAt.Unix()+LockDuration {
			t.Errorf("TokenExpiry = %d, want the original deadline %d", rec.TokenExpiry, rec.CreatedAt.Unix()+LockDuration)
		}

		e.clock.AdvanceSeconds(fastForwardSeconds)
		expired, err := e.svc.Get(ctx, v.ID)
		if err != nil || expired.State != store.StateExpired || !expired.ReconfirmNeeded {
			t.Fatalf("Get after the lock = %+v, %v, want EXPIRED needing reconfirmation", expired, err)
		}

		done, err := e.svc.Reconfirm(ctx, v.ID)
		if err != nil || done.State != store.StateExpired || !done.Settled || done.ReconfirmNeeded {
			t.Fatalf("Reconfirm = %+v, %v, want a settled EXPIRED view", done, err)
		}
	})

	t.Run("reality check locks and then the guardian approves", func(t *testing.T) {
		e := newEnv(false)
		rc := e.create(t, signalsRealityCheck())
		locked, err := e.svc.RealityCheck(ctx, rc.ID, Answers{Coercion: AnswerYes, Secrecy: AnswerNo, Verification: AnswerYes})
		if err != nil || locked.State != store.StateLocked15M || locked.GuardianToken == "" {
			t.Fatalf("RealityCheck = %+v, %v, want LOCKED_15M with a token", locked, err)
		}
		if !strings.Contains(locked.Reason, "15-minute") || !strings.Contains(locked.Reason, "on a call") {
			t.Errorf("Reason %q should state the hold and the call", locked.Reason)
		}
		approved, err := e.svc.Approve(ctx, rc.ID)
		if err != nil || approved.State != store.StateGuardianApproved {
			t.Fatalf("Approve = %+v, %v, want GUARDIAN_APPROVED", approved, err)
		}
	})
}
