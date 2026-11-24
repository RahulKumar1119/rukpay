package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Router example tests for the Explanation (Requirement 2).

// explanationMember returns the raw `explanation` member of a JSON object, and
// whether the key is there.
func explanationMember(tb testing.TB, raw []byte) (json.RawMessage, bool) {
	tb.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		tb.Fatalf("%s is not a JSON object: %v", raw, err)
	}
	member, ok := members["explanation"]
	return member, ok
}

// nestedExplanationMember is explanationMember for the View that a guardian
// action carries under "vault".
func nestedExplanationMember(tb testing.TB, raw []byte) (json.RawMessage, bool) {
	tb.Helper()
	var outer struct {
		Vault json.RawMessage `json:"vault"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil || outer.Vault == nil {
		tb.Fatalf("%s holds no vault member: %v", raw, err)
	}
	return explanationMember(tb, outer.Vault)
}

// rewriteExplanation replaces the Explanation stored for id and returns the
// record as it was.
func rewriteExplanation(tb testing.TB, e *testEnv, id string, explanation []store.Contribution) store.VaultRecord {
	tb.Helper()
	rec := e.record(tb, id)
	next := rec
	next.Explanation = explanation
	if err := e.mem.UpdateIf(context.Background(), id, rec.State, next); err != nil {
		tb.Fatalf("rewrite the Explanation: %v", err)
	}
	return rec
}

// The 85,000 rupee late-night payment on an unknown call returns its Explanation
// in the evaluate response, in the vault read and in the first stream event, with
// the same Contributions in the same order and with the same values (Requirements
// 2.1, 2.2, 2.8).
func TestExplanationIsTheSameInEvaluateReadAndStream(t *testing.T) {
	e := newTestEnv(false)
	created := e.do(t, http.MethodPost, pathEvaluate, presetLocked())
	if created.status != http.StatusOK {
		t.Fatalf("evaluate = %d %s, want 200", created.status, created.body)
	}
	v := created.view(t)

	// The four signals of this payment, in the fixed order, with their points.
	if v.Explanation == nil {
		t.Fatalf("evaluate response %s has no explanation", created.body)
	}
	wantSignals := []string{"amount", "beneficiary_age", "late_night", "unknown_call"}
	wantPoints := []int{35, 25, 10, 20}
	var gotSignals []string
	var gotPoints []int
	sum := 0
	for _, c := range *v.Explanation {
		if c.Reason == "" {
			t.Errorf("Contribution %+v has no reason", c)
		}
		gotSignals = append(gotSignals, c.Signal)
		gotPoints = append(gotPoints, c.Points)
		sum += c.Points
	}
	if !reflect.DeepEqual(gotSignals, wantSignals) || !reflect.DeepEqual(gotPoints, wantPoints) {
		t.Fatalf("explanation = %v %v, want %v %v", gotSignals, gotPoints, wantSignals, wantPoints)
	}
	if sum != v.RiskScore {
		t.Errorf("the points add up to %d, but risk_score is %d", sum, v.RiskScore)
	}

	fromEvaluate, _ := explanationMember(t, created.body)

	read := e.do(t, http.MethodGet, pathVaultBase+v.ID, nil)
	fromRead, ok := explanationMember(t, read.body)
	if !ok || !bytes.Equal(fromRead, fromEvaluate) {
		t.Errorf("GET explanation = %s, want the evaluate response's %s", fromRead, fromEvaluate)
	}

	srv := newTestServer(t, e.handler)
	c := openStream(t, srv, "?vault_id="+v.ID)
	line, open := c.next(t, 3*time.Second)
	if !open || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("first stream line = %q (open %v), want a data event", line, open)
	}
	fromStream, ok := explanationMember(t, []byte(strings.TrimPrefix(line, "data: ")))
	if !ok || !bytes.Equal(fromStream, fromEvaluate) {
		t.Errorf("first stream event explanation = %s, want %s", fromStream, fromEvaluate)
	}

	// The next event, sent for a change, carries the same Explanation.
	e.mustAct(t, v, "VETO", false)
	line, open = c.next(t, 3*time.Second)
	if !open || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("stream line after the veto = %q (open %v), want a data event", line, open)
	}
	afterVeto, ok := explanationMember(t, []byte(strings.TrimPrefix(line, "data: ")))
	if !ok || !bytes.Equal(afterVeto, fromEvaluate) {
		t.Errorf("stream event after the veto has explanation %s, want %s", afterVeto, fromEvaluate)
	}
}

// Every route that returns a View returns the Explanation computed at creation,
// whatever happens to the vault afterwards (Requirement 2.7).
func TestExplanationIsUnchangedByEveryAction(t *testing.T) {
	e := newTestEnv(true)
	created := e.do(t, http.MethodPost, pathEvaluate, presetRealityCheck())
	want, ok := explanationMember(t, created.body)
	if !ok || string(want) == "[]" {
		t.Fatalf("evaluate response %s has no non-empty explanation to compare", created.body)
	}
	v := created.view(t)
	scoreBefore := v.RiskScore

	check := func(name string, res reply) {
		t.Helper()
		if res.status != http.StatusOK {
			t.Fatalf("%s = %d %s, want 200", name, res.status, res.body)
		}
		got, present := explanationMember(t, res.body)
		if !present || !bytes.Equal(got, want) {
			t.Errorf("%s: explanation = %s, want %s", name, got, want)
		}
		if res.view(t).RiskScore != scoreBefore {
			t.Errorf("%s: risk_score changed from %d", name, scoreBefore)
		}
	}

	check("reality-check", e.do(t, http.MethodPost, pathVaultBase+v.ID+"/reality-check", map[string]any{
		"answers": map[string]any{"coercion": "YES", "secrecy": "NO", "verification": "YES"},
	}))
	check("fast-forward", e.do(t, http.MethodPost, pathVaultBase+v.ID+"/demo-fast-forward", nil))
	e.clock.AdvanceSeconds(10)
	check("read after the lock ran out", e.do(t, http.MethodGet, pathVaultBase+v.ID, nil))
	check("reconfirm", e.do(t, http.MethodPost, pathVaultBase+v.ID+"/reality-check", map[string]any{"reconfirm": true}))

	// A guardian action returns the View under "vault".
	other := e.do(t, http.MethodPost, pathEvaluate, presetLocked())
	otherWant, _ := explanationMember(t, other.body)
	otherView := other.view(t)
	approved := e.do(t, http.MethodPost, pathVaultBase+otherView.ID+"/guardian-action",
		map[string]any{"action": "APPROVE", "token": otherView.GuardianToken, "confirmed": true})
	if got, ok := nestedExplanationMember(t, approved.body); approved.status != http.StatusOK || !ok || !bytes.Equal(got, otherWant) {
		t.Errorf("guardian approve: %d, explanation %s, want 200 and %s", approved.status, got, otherWant)
	}
}

// A body that holds a field named `explanation` is treated like any unknown
// field: ignored, never stored and never returned (Requirement 2.12). This holds
// for every route that reads a body and for every kind of value.
func TestBodyExplanationFieldIsIgnored(t *testing.T) {
	const injected = "Injected sentence from the request."
	values := map[string]any{
		"a list":         []any{map[string]any{"signal": "amount", "points": 99, "reason": injected}},
		"an empty list":  []any{},
		"null":           nil,
		"a string":       injected,
		"an object":      map[string]any{"reason": injected},
		"a bare integer": 99,
	}

	for name, value := range values {
		t.Run("evaluate with "+name, func(t *testing.T) {
			e := newTestEnv(false)
			plain := newTestEnv(false)

			body := presetRealityCheck()
			body["explanation"] = value
			res := e.do(t, http.MethodPost, pathEvaluate, body)
			base := plain.do(t, http.MethodPost, pathEvaluate, presetRealityCheck())

			if res.status != http.StatusOK || base.status != http.StatusOK {
				t.Fatalf("evaluate = %d and %d, want 200 for both", res.status, base.status)
			}
			got, _ := explanationMember(t, res.body)
			want, _ := explanationMember(t, base.body)
			if !bytes.Equal(got, want) {
				t.Errorf("explanation with the injected field = %s, want the engine's %s", got, want)
			}
			stored, _ := json.Marshal(e.record(t, res.view(t).ID))
			if strings.Contains(string(res.body), injected) || strings.Contains(string(stored), injected) {
				t.Errorf("the injected sentence reached the response or the store")
			}
		})
	}

	t.Run("reality-check", func(t *testing.T) {
		e := newTestEnv(false)
		v := e.evaluate(t, presetRealityCheck())
		before := e.record(t, v.ID).Explanation

		res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/reality-check", map[string]any{
			"answers":     map[string]any{"coercion": "NO", "secrecy": "NO", "verification": "YES"},
			"explanation": values["a list"],
		})
		if res.status != http.StatusOK {
			t.Fatalf("reality-check = %d %s, want 200", res.status, res.body)
		}
		if after := e.record(t, v.ID).Explanation; !reflect.DeepEqual(after, before) {
			t.Errorf("stored Explanation = %+v, want the unchanged %+v", after, before)
		}
		if strings.Contains(string(res.body), injected) {
			t.Errorf("the injected sentence reached the response: %s", res.body)
		}
	})

	t.Run("guardian-action", func(t *testing.T) {
		e := newTestEnv(false)
		v := e.evaluate(t, presetLocked())
		before := e.record(t, v.ID).Explanation

		res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/guardian-action", map[string]any{
			"action": "APPROVE", "token": v.GuardianToken, "confirmed": true,
			"explanation": values["a list"],
		})
		if res.status != http.StatusOK {
			t.Fatalf("guardian-action = %d %s, want 200", res.status, res.body)
		}
		if after := e.record(t, v.ID).Explanation; !reflect.DeepEqual(after, before) {
			t.Errorf("stored Explanation = %+v, want the unchanged %+v", after, before)
		}
		if strings.Contains(string(res.body), injected) {
			t.Errorf("the injected sentence reached the response: %s", res.body)
		}
	})

	// A rejected request is rejected the same way with or without the field.
	t.Run("a rejected request is rejected the same way", func(t *testing.T) {
		e := newTestEnv(false)
		for _, body := range []map[string]any{
			{"amount_inr": -1},
			{"amount_inr": 100, "payer_account_masked": "123456789012"},
		} {
			plain := e.do(t, http.MethodPost, pathEvaluate, body)
			withField := map[string]any{"explanation": values["a list"]}
			for k, v := range body {
				withField[k] = v
			}
			got := e.do(t, http.MethodPost, pathEvaluate, withField)
			if got.status != plain.status || !bytes.Equal(got.body, plain.body) {
				t.Errorf("with the field: %d %s, without: %d %s, want the same", got.status, got.body, plain.status, plain.body)
			}
		}
	})
}

// A vault stored before the Explanation existed reads with no `explanation` key
// and every other field as it was (Requirements 2.5, 2.6).
func TestOldRecordReadsWithoutAnExplanationAndWithEveryOtherFieldUnchanged(t *testing.T) {
	e := newTestEnv(false)
	created := e.do(t, http.MethodPost, pathEvaluate, presetLocked())
	v := created.view(t)
	rewriteExplanation(t, e, v.ID, nil)

	read := e.do(t, http.MethodGet, pathVaultBase+v.ID, nil)
	if read.status != http.StatusOK {
		t.Fatalf("GET = %d %s, want 200", read.status, read.body)
	}
	if member, present := explanationMember(t, read.body); present {
		t.Fatalf("an old record reads with explanation %s, want the key left out", member)
	}

	// Every other member equals what the evaluate response held.
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(created.body, &before); err != nil {
		t.Fatalf("evaluate body: %v", err)
	}
	if err := json.Unmarshal(read.body, &after); err != nil {
		t.Fatalf("read body: %v", err)
	}
	delete(before, "explanation")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("an old record reads as %s, want every other field of %s", read.body, created.body)
	}

	// The stream sends no key for it either, and an action keeps it that way.
	srv := newTestServer(t, e.handler)
	c := openStream(t, srv, "?vault_id="+v.ID)
	line, _ := c.next(t, 3*time.Second)
	if _, present := explanationMember(t, []byte(strings.TrimPrefix(line, "data: "))); present {
		t.Errorf("the stream event for an old record holds an explanation: %s", line)
	}
	e.mustAct(t, v, "VETO", false)
	line, _ = c.next(t, 3*time.Second)
	if _, present := explanationMember(t, []byte(strings.TrimPrefix(line, "data: "))); present {
		t.Errorf("the stream event after a veto of an old record holds an explanation: %s", line)
	}
	if rec := e.record(t, v.ID); rec.Explanation != nil {
		t.Errorf("a veto gave an old record the Explanation %+v, want it to stay absent", rec.Explanation)
	}
}

// A request that is rejected, or whose vault cannot be stored, returns the error
// it returned before the Explanation existed, with no View and no explanation
// (Requirement 2.11).
func TestRejectedEvaluateReturnsTheShippedErrorWithoutAnExplanation(t *testing.T) {
	e := newTestEnv(false)
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"invalid amount", `{"amount_inr": 0}`, http.StatusBadRequest, "invalid_field"},
		{"negative age", `{"amount_inr": 100, "beneficiary_age_hours": -1}`, http.StatusBadRequest, "invalid_field"},
		{"raw identifier", `{"amount_inr": 100, "upi_masked": "rahul@okaxis"}`, http.StatusBadRequest, "invalid_identifier"},
		{"not JSON", `{"amount_inr": 100`, http.StatusBadRequest, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := e.send(http.MethodPost, pathEvaluate, []byte(tt.body), nil)
			if res.status != tt.wantStatus || res.errorCode(t) != tt.wantCode {
				t.Fatalf("evaluate = %d %s, want %d %s", res.status, res.body, tt.wantStatus, tt.wantCode)
			}
			if strings.Contains(string(res.body), "explanation") {
				t.Errorf("the error body %s mentions an explanation", res.body)
			}
			if _, present := explanationMember(t, res.body); present {
				t.Errorf("the error body %s carries an explanation member", res.body)
			}
		})
	}
	if _, writes := e.cs.snapshot(); writes != 0 {
		t.Errorf("rejected requests wrote %d records", writes)
	}

	t.Run("the vault cannot be stored", func(t *testing.T) {
		failing := newTestEnv(false)
		failing.handler = New(
			Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)},
			Deps{Store: failingStore{err: errors.New("store is down")}, Logger: log.New(io.Discard, "", 0)},
		)
		got := failing.do(t, http.MethodPost, pathEvaluate, presetLocked())
		if got.status != http.StatusInternalServerError || got.errorCode(t) != "internal" {
			t.Fatalf("evaluate over a failing store = %d %s, want 500 internal", got.status, got.body)
		}
		if strings.Contains(string(got.body), "explanation") || strings.Contains(string(got.body), "vault_id") {
			t.Errorf("the 500 body %s holds a View or an explanation", got.body)
		}
	})
}
