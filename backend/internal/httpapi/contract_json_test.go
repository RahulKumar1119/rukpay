package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// JSON contract tests (Requirement 27.11), in the style of vault/types_test.go.
// This file starts with the Contribution and the `explanation` key of a View.
// Later groups of the risk-intelligence plan add the beneficiary types here.

// viewKeysBeforeTheExplanation is the set of keys that every View carried before
// the Explanation existed and carries now, whatever its state. guardian_token and
// evidence_summary are left out because they appear only in LOCKED_15M and
// VETOED.
var viewKeysBeforeTheExplanation = []string{
	"metadata", "reason", "reconfirm_required", "remaining_seconds", "risk_score", "settled", "state", "vault_id",
}

// contractJSONFields lists, sorted, the JSON key of every exported field of t. It
// reads the declared fields, so a field added without a change to this test fails
// it.
func contractJSONFields(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// contractKeys returns the sorted top-level keys of the JSON object raw.
func contractKeys(tb testing.TB, raw []byte) []string {
	tb.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		tb.Fatalf("%s is not a JSON object: %v", raw, err)
	}
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// withKeys returns the sorted union of base and extra, as a new slice.
func withKeys(base []string, extra ...string) []string {
	all := append(append([]string{}, base...), extra...)
	sort.Strings(all)
	return all
}

// A Contribution is an object with exactly the keys signal, points and reason
// (Requirement 2.9). Each key is always written, even for a zero value.
func TestContributionJSONKeysAreExactlySignalPointsAndReason(t *testing.T) {
	want := []string{"points", "reason", "signal"}
	if got := contractJSONFields(reflect.TypeOf(store.Contribution{})); !reflect.DeepEqual(got, want) {
		t.Fatalf("declared JSON fields = %v, want %v", got, want)
	}
	if reflect.TypeOf(risk.Contribution{}) != reflect.TypeOf(store.Contribution{}) {
		t.Fatalf("risk.Contribution and store.Contribution are two types, want one shape")
	}

	samples := []store.Contribution{
		{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
		{Signal: "score_cap", Points: -10, Reason: "The total cannot go above its upper limit."},
		{},
	}
	for _, c := range samples {
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("Marshal(%+v) = %v, want nil", c, err)
		}
		if got := contractKeys(t, raw); !reflect.DeepEqual(got, want) {
			t.Errorf("keys of %s = %v, want %v", raw, got, want)
		}

		// signal and reason are strings. points is a JSON number with no fraction.
		var typed map[string]json.RawMessage
		if err := json.Unmarshal(raw, &typed); err != nil {
			t.Fatalf("Unmarshal(%s) = %v, want nil", raw, err)
		}
		var text string
		if json.Unmarshal(typed["signal"], &text) != nil || json.Unmarshal(typed["reason"], &text) != nil {
			t.Errorf("signal or reason in %s is not a JSON string", raw)
		}
		if p := string(typed["points"]); strings.ContainsAny(p, ".eE\"") {
			t.Errorf("points in %s is %s, want a number with no fraction and no quotes", raw, p)
		}

		var back store.Contribution
		if err := json.Unmarshal(raw, &back); err != nil || back != c {
			t.Errorf("round trip of %s = %+v (%v), want %+v", raw, back, err, c)
		}
	}
}

// A View declares the eight keys it had before the Explanation, the two that
// appear only in some states, and `explanation`. Nothing else.
func TestViewDeclaresTheExplanationKeyAndNothingElseNew(t *testing.T) {
	want := withKeys(viewKeysBeforeTheExplanation, "evidence_summary", "guardian_token", "explanation")
	if got := contractJSONFields(reflect.TypeOf(vault.View{})); !reflect.DeepEqual(got, want) {
		t.Fatalf("declared JSON fields of View = %v, want %v", got, want)
	}
}

// The `explanation` key is optional on a View. A nil pointer leaves it out, which
// is how a record from before the feature reads (Requirement 2.5). A pointer to
// an empty slice writes []. A pointer to a list writes the Contributions in
// order. Each case survives a round trip with its own meaning.
func TestViewExplanationKeyIsOptional(t *testing.T) {
	list := []risk.Contribution{
		{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
		{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
	}
	empty := []risk.Contribution{}

	tests := []struct {
		name     string
		view     vault.View
		wantKeys []string
		wantText string // the exact explanation member, or "" when the key must be absent
	}{
		{"nil pointer: key left out", vault.View{ID: "v1", State: store.StateInstantPass}, viewKeysBeforeTheExplanation, ""},
		{"pointer to empty slice: []", vault.View{ID: "v1", State: store.StateInstantPass, Explanation: &empty},
			withKeys(viewKeysBeforeTheExplanation, "explanation"), "[]"},
		{"pointer to a list: the list", vault.View{ID: "v1", State: store.StateLocked15M, Explanation: &list},
			withKeys(viewKeysBeforeTheExplanation, "explanation"),
			`[{"signal":"amount","points":35,"reason":"This is a very large payment."},` +
				`{"signal":"late_night","points":10,"reason":"The payment is being made late at night."}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.view)
			if err != nil {
				t.Fatalf("Marshal = %v, want nil", err)
			}
			if got := contractKeys(t, raw); !reflect.DeepEqual(got, tt.wantKeys) {
				t.Fatalf("keys of %s = %v, want %v", raw, got, tt.wantKeys)
			}
			var members map[string]json.RawMessage
			_ = json.Unmarshal(raw, &members)
			text, present := members["explanation"]
			if tt.wantText == "" && (present || strings.Contains(string(raw), "null")) {
				t.Errorf("View %s holds an explanation key or a null, want the key left out", raw)
			}
			if tt.wantText != "" && string(text) != tt.wantText {
				t.Errorf("explanation = %s, want %s", text, tt.wantText)
			}

			var back vault.View
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("Unmarshal = %v, want nil", err)
			}
			if !reflect.DeepEqual(back, tt.view) {
				t.Errorf("round trip = %+v, want %+v (absent, empty and filled must stay apart)", back, tt.view)
			}
		})
	}
}

// Through the router: a vault written before the feature reads with no
// `explanation` key and the key set it always had, a vault with an empty
// Explanation reads with [], and a new vault reads with the key (Requirements 2.1,
// 2.2, 2.5, 2.6, 23.7).
func TestRouterWritesTheExplanationKeyOnlyWhereThereIsOne(t *testing.T) {
	e := newTestEnv(false)
	ctx := context.Background()

	created := e.do(t, http.MethodPost, pathEvaluate, presetLocked())
	if created.status != http.StatusOK {
		t.Fatalf("evaluate = %d %s, want 200", created.status, created.body)
	}
	lockedKeys := withKeys(viewKeysBeforeTheExplanation, "guardian_token")
	if got, want := contractKeys(t, created.body), withKeys(lockedKeys, "explanation"); !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluate keys = %v, want %v", got, want)
	}
	id := created.view(t).ID

	rewrite := func(explanation []store.Contribution) reply {
		t.Helper()
		rec := e.record(t, id)
		next := rec
		next.Explanation = explanation
		if err := e.mem.UpdateIf(ctx, id, rec.State, next); err != nil {
			t.Fatalf("rewrite the record: %v", err)
		}
		res := e.do(t, http.MethodGet, pathVaultBase+id, nil)
		if res.status != http.StatusOK {
			t.Fatalf("GET = %d %s, want 200", res.status, res.body)
		}
		return res
	}

	old := rewrite(nil)
	if got := contractKeys(t, old.body); !reflect.DeepEqual(got, lockedKeys) {
		t.Errorf("an old record reads with the keys %v, want exactly %v", got, lockedKeys)
	}
	if strings.Contains(string(old.body), "explanation") || strings.Contains(string(old.body), "null") {
		t.Errorf("an old record reads as %s, want no explanation key and no null", old.body)
	}

	present := rewrite([]store.Contribution{})
	if got, want := contractKeys(t, present.body), withKeys(lockedKeys, "explanation"); !reflect.DeepEqual(got, want) {
		t.Errorf("a record with an empty Explanation reads with the keys %v, want %v", got, want)
	}
	if !strings.Contains(string(present.body), `"explanation":[]`) {
		t.Errorf("an empty Explanation reads as %s, want \"explanation\":[]", present.body)
	}
}
