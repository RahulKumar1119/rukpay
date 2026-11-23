package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// Example and integration tests for the router, the middleware and the stream.

const (
	pathEvaluate  = "/api/v1/transactions/evaluate"
	pathStream    = "/api/v1/events/stream"
	pathVaultBase = "/api/v1/vault/"
)

func TestRoutesAreRegisteredForTheRightMethods(t *testing.T) {
	e := newTestEnv(true)
	v := e.vaultIn(t, store.StateLocked15M)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, pathEvaluate},
		{http.MethodPost, pathVaultBase + v.ID + "/reality-check"},
		{http.MethodPost, pathVaultBase + v.ID + "/guardian-action"},
		{http.MethodPost, pathVaultBase + v.ID + "/demo-fast-forward"},
		{http.MethodGet, pathVaultBase + v.ID},
		{http.MethodGet, pathStream},
	}
	for _, rt := range routes {
		// Another method gets 405 and an Allow header that names the right one.
		for _, other := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			if other == rt.method {
				continue
			}
			res := e.send(other, rt.path, nil, nil)
			if res.status != http.StatusMethodNotAllowed || res.header.Get("Allow") != rt.method {
				t.Errorf("%s %s = %d with Allow %q, want 405 and %q", other, rt.path, res.status, res.header.Get("Allow"), rt.method)
			}
			if res.errorCode(t) != "method_not_allowed" {
				t.Errorf("%s %s error code = %q, want method_not_allowed", other, rt.path, res.errorCode(t))
			}
		}
	}

	// The right method reaches the handler: the answer is not a routing 404 or 405.
	for _, rt := range routes[:5] {
		var body any
		if strings.HasSuffix(rt.path, "reality-check") {
			body = map[string]any{"reconfirm": true}
		}
		if strings.HasSuffix(rt.path, "guardian-action") {
			body = map[string]any{"action": "VETO", "token": "x"}
		}
		if rt.path == pathEvaluate {
			body = presetInstantPass()
		}
		res := e.do(t, rt.method, rt.path, body)
		if res.status == http.StatusMethodNotAllowed || (res.status == http.StatusNotFound && res.errorCode(t) != "not_found") {
			t.Errorf("%s %s = %d, want it routed", rt.method, rt.path, res.status)
		}
	}
}

func TestUnknownPathsGet404(t *testing.T) {
	e := newTestEnv(true)
	v := e.vaultIn(t, store.StateLocked15M)

	for _, path := range []string{
		"/",
		"/api",
		"/api/v1",
		"/api/v2/transactions/evaluate",
		pathEvaluate + "/",
		pathVaultBase,
		pathVaultBase + v.ID + "/",
		pathVaultBase + v.ID + "/unknown-action",
		pathVaultBase + v.ID + "/reality-check/extra",
		"/health",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			res := e.send(method, path, nil, nil)
			if res.status != http.StatusNotFound || res.errorCode(t) != "not_found" {
				t.Errorf("%s %s = %d %s, want a 404 not_found", method, path, res.status, res.body)
			}
		}
	}
}

func TestDemoPresetsOverHTTP(t *testing.T) {
	tests := []struct {
		name      string
		body      map[string]any
		wantState store.VaultState
		wantScore int
		wantToken bool
	}{
		{"safe daily payment", presetInstantPass(), store.StateInstantPass, 0, false},
		{"medium-risk transfer", presetRealityCheck(), store.StateRealityCheck, 45, false},
		{"high-risk panic transfer", presetLocked(), store.StateLocked15M, 90, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(false)
			res := e.do(t, http.MethodPost, pathEvaluate, tt.body)
			if res.status != http.StatusOK {
				t.Fatalf("evaluate = %d %s, want 200", res.status, res.body)
			}
			v := res.view(t)
			if v.State != tt.wantState || v.RiskScore != tt.wantScore || (v.GuardianToken != "") != tt.wantToken {
				t.Fatalf("View = state %s score %d token %q, want %s %d token %v",
					v.State, v.RiskScore, v.GuardianToken, tt.wantState, tt.wantScore, tt.wantToken)
			}
			if ct := res.header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if cc := res.header.Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store: a View can carry a token", cc)
			}
		})
	}
}

func TestEvaluateKeepsMaskedMetadataAndDropsUnknownFields(t *testing.T) {
	e := newTestEnv(false)
	body := presetInstantPass()
	body["payer_account_masked"] = "XXXX-XXXX-4821"
	body["upi_masked"] = "r***@okaxis"
	// Fields that the service does not declare must be dropped at decode.
	body["otp"] = "482913"
	body["pin"] = "5678"
	body["password"] = "hunter2-secret"
	body["answers_free_text"] = "I was told to say nothing"

	res := e.do(t, http.MethodPost, pathEvaluate, body)
	if res.status != http.StatusOK {
		t.Fatalf("evaluate = %d %s, want 200", res.status, res.body)
	}
	v := res.view(t)
	if v.Metadata.PayerAccountMasked != "XXXX-XXXX-4821" || v.Metadata.UPIMasked != "r***@okaxis" || v.Metadata.AmountINR != 450 {
		t.Errorf("Metadata = %+v, want the masked values and the amount", v.Metadata)
	}

	rec := e.record(t, v.ID)
	stored, _ := json.Marshal(rec)
	for _, secret := range []string{"482913", "5678", "hunter2-secret", "I was told to say nothing"} {
		if strings.Contains(string(res.body), secret) || strings.Contains(string(stored), secret) {
			t.Errorf("the value %q reached the response or the store", secret)
		}
	}
}

func TestFlowLockedThenApprovedWithASecondTap(t *testing.T) {
	e := newTestEnv(false)
	v := e.evaluate(t, presetLocked())
	path := pathVaultBase + v.ID + "/guardian-action"

	first := e.do(t, http.MethodPost, path, map[string]any{"action": "APPROVE", "token": v.GuardianToken})
	var firstBody struct {
		Status string     `json:"status"`
		Vault  vault.View `json:"vault"`
	}
	first.json(t, &firstBody)
	if first.status != http.StatusOK || firstBody.Status != "CONFIRMATION_REQUIRED" || firstBody.Vault.State != store.StateLocked15M {
		t.Fatalf("first tap = %d %s, want 200 CONFIRMATION_REQUIRED on a locked vault", first.status, first.body)
	}
	if got := e.get(t, v.ID); got.State != store.StateLocked15M {
		t.Fatalf("the first tap changed the vault to %s", got.State)
	}

	second := e.do(t, http.MethodPost, path, map[string]any{"action": "APPROVE", "token": v.GuardianToken, "confirmed": true})
	var secondBody struct {
		Status string     `json:"status"`
		Vault  vault.View `json:"vault"`
	}
	second.json(t, &secondBody)
	if second.status != http.StatusOK || secondBody.Status != "OK" || secondBody.Vault.State != store.StateGuardianApproved || !secondBody.Vault.Settled {
		t.Fatalf("second tap = %d %s, want 200 OK and a settled GUARDIAN_APPROVED vault", second.status, second.body)
	}
	if secondBody.Vault.GuardianToken != "" {
		t.Errorf("an approved vault still shows a token")
	}
}

func TestFlowLockedThenVetoedWithEvidence(t *testing.T) {
	e := newTestEnv(false)
	body := presetLocked()
	body["payer_account_masked"] = "XXXX-XXXX-4821"
	v := e.evaluate(t, body)

	res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/guardian-action",
		map[string]any{"action": "VETO", "token": v.GuardianToken})
	var result struct {
		Status string     `json:"status"`
		Vault  vault.View `json:"vault"`
	}
	res.json(t, &result)
	if res.status != http.StatusOK || result.Vault.State != store.StateVetoed || result.Vault.Settled {
		t.Fatalf("VETO = %d %s, want 200 and an unsettled VETOED vault", res.status, res.body)
	}
	for _, want := range []string{v.ID, "XXXX-XXXX-4821", "1930", "cybercrime.gov.in"} {
		if !strings.Contains(result.Vault.Evidence, want) {
			t.Errorf("evidence %q does not contain %q", result.Vault.Evidence, want)
		}
	}
	if got := e.get(t, v.ID); got.Evidence != result.Vault.Evidence {
		t.Errorf("a later GET returns different evidence")
	}
}

func TestFlowFastForwardExpiresThenReconfirms(t *testing.T) {
	e := newTestEnv(true)
	v := e.evaluate(t, presetLocked())

	ff := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/demo-fast-forward", nil)
	if ff.status != http.StatusOK || ff.view(t).RemainingSeconds != 3 {
		t.Fatalf("fast-forward = %d %s, want 200 with 3 seconds left", ff.status, ff.body)
	}

	e.clock.AdvanceSeconds(3)
	expired := e.get(t, v.ID)
	if expired.State != store.StateExpired || !expired.ReconfirmNeeded || expired.Settled {
		t.Fatalf("GET after the lock = %+v, want an unsettled EXPIRED vault needing reconfirmation", expired)
	}

	res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/reality-check", map[string]any{"reconfirm": true})
	done := res.view(t)
	if res.status != http.StatusOK || done.State != store.StateExpired || !done.Settled || done.ReconfirmNeeded {
		t.Fatalf("reconfirm = %d %s, want 200 and a settled EXPIRED vault", res.status, res.body)
	}
}

func TestFlowRealityCheckLocksAndTheGuardianApproves(t *testing.T) {
	e := newTestEnv(false)
	v := e.evaluate(t, presetRealityCheck())

	res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/reality-check", map[string]any{
		"answers": map[string]any{"coercion": "YES", "secrecy": "NO", "verification": "YES"},
	})
	locked := res.view(t)
	if res.status != http.StatusOK || locked.State != store.StateLocked15M || locked.GuardianToken == "" {
		t.Fatalf("reality check = %d %s, want a LOCKED_15M vault with a token", res.status, res.body)
	}
	e.mustAct(t, locked, "APPROVE", true)
}

func TestConflictBodiesCarryTheCurrentView(t *testing.T) {
	e := newTestEnv(false)
	v := e.vaultIn(t, store.StateVetoed)

	res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/reality-check", map[string]any{"reconfirm": true})
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Vault *vault.View `json:"vault"`
	}
	res.json(t, &body)
	if res.status != http.StatusConflict || body.Error.Code != "state_conflict" || body.Error.Message == "" {
		t.Fatalf("reconfirm on a VETOED vault = %d %s, want a 409 state_conflict", res.status, res.body)
	}
	if body.Vault == nil || body.Vault.ID != v.ID || body.Vault.State != store.StateVetoed {
		t.Errorf("the 409 body carries %+v, want the current VETOED view", body.Vault)
	}
}

func TestMissingAndUnknownVaults(t *testing.T) {
	e := newTestEnv(true)
	probe := "probe-value-that-must-not-be-echoed"
	missing := strings.Repeat("0", 32)

	tests := []struct {
		name       string
		method     string
		path       string
		body       any
		wantStatus int
	}{
		{"GET unknown id", http.MethodGet, pathVaultBase + probe, nil, http.StatusNotFound},
		{"GET unknown hex id", http.MethodGet, pathVaultBase + missing, nil, http.StatusNotFound},
		{"stream unknown id", http.MethodGet, pathStream + "?vault_id=" + probe, nil, http.StatusNotFound},
		{"stream missing vault_id", http.MethodGet, pathStream, nil, http.StatusBadRequest},
		{"stream empty vault_id", http.MethodGet, pathStream + "?vault_id=", nil, http.StatusBadRequest},
		{"reality check unknown id", http.MethodPost, pathVaultBase + missing + "/reality-check", map[string]any{"reconfirm": true}, http.StatusNotFound},
		{"fast-forward unknown id", http.MethodPost, pathVaultBase + missing + "/demo-fast-forward", nil, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := e.do(t, tt.method, tt.path, tt.body)
			if res.status != tt.wantStatus {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.path, res.status, res.body, tt.wantStatus)
			}
			if echoed, found := bodyContains(res, probe); found {
				t.Errorf("the response echoes the submitted value %q", echoed)
			}
			if res.status == http.StatusNotFound && res.errorCode(t) != "not_found" {
				t.Errorf("error code = %q, want not_found", res.errorCode(t))
			}
		})
	}
}

func TestCORS(t *testing.T) {
	const other = "https://evil.example.com"

	t.Run("the allowed origin is echoed", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodPost, pathEvaluate, mustJSON(t, presetInstantPass()), http.Header{"Origin": {harnessOrigin}})
		if got := res.header.Get("Access-Control-Allow-Origin"); got != harnessOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, harnessOrigin)
		}
		if got := res.header.Get("Vary"); !strings.Contains(got, "Origin") {
			t.Errorf("Vary = %q, want it to include Origin", got)
		}
	})

	t.Run("another origin gets no header", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodPost, pathEvaluate, mustJSON(t, presetInstantPass()), http.Header{"Origin": {other}})
		if got := res.header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q for %s, want none", got, other)
		}
	})

	t.Run("no origin gets no header", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodGet, pathVaultBase+strings.Repeat("0", 32), nil, nil)
		if got := res.header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q without an Origin, want none", got)
		}
	})

	t.Run("an error response carries the header too", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodGet, pathVaultBase+strings.Repeat("0", 32), nil, http.Header{"Origin": {harnessOrigin}})
		if res.status != http.StatusNotFound || res.header.Get("Access-Control-Allow-Origin") != harnessOrigin {
			t.Errorf("404 = %d with origin %q, want the browser to be able to read it", res.status, res.header.Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("preflight from the allowed origin", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodOptions, pathEvaluate, nil, http.Header{
			"Origin":                         {harnessOrigin},
			"Access-Control-Request-Method":  {"POST"},
			"Access-Control-Request-Headers": {"content-type"},
		})
		if res.status != http.StatusNoContent {
			t.Fatalf("OPTIONS = %d, want 204", res.status)
		}
		if got := res.header.Get("Access-Control-Allow-Origin"); got != harnessOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, harnessOrigin)
		}
		for _, want := range []string{"GET", "POST", "OPTIONS"} {
			if !strings.Contains(res.header.Get("Access-Control-Allow-Methods"), want) {
				t.Errorf("Access-Control-Allow-Methods = %q, want it to list %s", res.header.Get("Access-Control-Allow-Methods"), want)
			}
		}
		if got := res.header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Content-Type") {
			t.Errorf("Access-Control-Allow-Headers = %q, want Content-Type", got)
		}
		if len(res.body) != 0 {
			t.Errorf("preflight body = %q, want none", res.body)
		}
	})

	t.Run("preflight from another origin", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodOptions, pathEvaluate, nil, http.Header{"Origin": {other}, "Access-Control-Request-Method": {"POST"}})
		if res.status != http.StatusNoContent || res.header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("OPTIONS = %d with origin %q, want 204 and no origin header", res.status, res.header.Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("a preflight reaches no handler and no store", func(t *testing.T) {
		e := newTestEnv(false)
		e.send(http.MethodOptions, pathVaultBase+strings.Repeat("0", 32), nil, http.Header{"Origin": {harnessOrigin}})
		if calls, _ := e.cs.snapshot(); calls != 0 {
			t.Errorf("a preflight made %d store calls", calls)
		}
	})

	t.Run("the default origin is localhost 5173", func(t *testing.T) {
		cfg, err := LoadConfig(env(map[string]string{"HMAC_SECRET_KEY": "k"}))
		if err != nil {
			t.Fatalf("LoadConfig = %v", err)
		}
		e := newTestEnv(false, withOrigin(cfg.AllowedOrigin))
		allowed := e.send(http.MethodGet, pathVaultBase+strings.Repeat("0", 32), nil, http.Header{"Origin": {"http://localhost:5173"}})
		denied := e.send(http.MethodGet, pathVaultBase+strings.Repeat("0", 32), nil, http.Header{"Origin": {"http://localhost:3000"}})
		if allowed.header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" || denied.header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("default origin: allowed header %q, other header %q",
				allowed.header.Get("Access-Control-Allow-Origin"), denied.header.Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("a configured origin replaces the default", func(t *testing.T) {
		e := newTestEnv(false, withOrigin("https://app.example.com"))
		mine := e.send(http.MethodGet, pathVaultBase+strings.Repeat("0", 32), nil, http.Header{"Origin": {"https://app.example.com"}})
		dflt := e.send(http.MethodGet, pathVaultBase+strings.Repeat("0", 32), nil, http.Header{"Origin": {"http://localhost:5173"}})
		if mine.header.Get("Access-Control-Allow-Origin") != "https://app.example.com" || dflt.header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("configured origin: header %q, default-origin header %q",
				mine.header.Get("Access-Control-Allow-Origin"), dflt.header.Get("Access-Control-Allow-Origin"))
		}
	})
}

func mustJSON(tb testing.TB, v any) []byte {
	tb.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

func TestBodyLimit(t *testing.T) {
	// padded returns a valid evaluate body of exactly n bytes.
	padded := func(n int) []byte {
		base := mustJSON(t, presetInstantPass())
		return append(base, bytes.Repeat([]byte(" "), n-len(base))...)
	}

	t.Run("a declared length over 64 KiB is refused", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodPost, pathEvaluate, padded(64<<10+1), nil)
		if res.status != http.StatusRequestEntityTooLarge || res.errorCode(t) != "too_large" {
			t.Fatalf("65537 bytes = %d %s, want 413 too_large", res.status, res.body)
		}
		if calls, _ := e.cs.snapshot(); calls != 0 {
			t.Errorf("an oversized request reached the store")
		}
	})

	t.Run("a streamed body over 64 KiB is refused", func(t *testing.T) {
		e := newTestEnv(false)
		req := httptest.NewRequest(http.MethodPost, pathEvaluate, bytes.NewReader(padded(200<<10)))
		req.ContentLength = -1 // unknown length, as with chunked encoding
		res := e.serve(req)
		if res.status != http.StatusRequestEntityTooLarge || res.errorCode(t) != "too_large" {
			t.Fatalf("a chunked 200 KiB body = %d %s, want 413 too_large", res.status, res.body)
		}
	})

	t.Run("valid JSON followed by a large tail is refused", func(t *testing.T) {
		e := newTestEnv(false)
		req := httptest.NewRequest(http.MethodPost, pathEvaluate, bytes.NewReader(padded(200<<10)))
		req.ContentLength = -1
		if res := e.serve(req); res.status != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413: the tail must be counted", res.status)
		}
	})

	t.Run("a declared length over 64 KiB is refused where the handler reads no body", func(t *testing.T) {
		e := newTestEnv(true)
		v := e.vaultIn(t, store.StateLocked15M)
		res := e.send(http.MethodPost, pathVaultBase+v.ID+"/demo-fast-forward", bytes.Repeat([]byte("x"), 64<<10+1), nil)
		if res.status != http.StatusRequestEntityTooLarge || res.errorCode(t) != "too_large" {
			t.Fatalf("fast-forward with a 65537-byte body = %d %s, want 413 too_large", res.status, res.body)
		}
		if got := e.get(t, v.ID); got.RemainingSeconds != vault.LockDuration {
			t.Errorf("the refused request still changed the lock")
		}
	})

	t.Run("a body of exactly 64 KiB is accepted", func(t *testing.T) {
		e := newTestEnv(false)
		res := e.send(http.MethodPost, pathEvaluate, padded(64<<10), nil)
		if res.status != http.StatusOK {
			t.Fatalf("a 65536-byte body = %d %s, want 200", res.status, res.body)
		}
	})
}

func TestInvalidJSONIsA400(t *testing.T) {
	e := newTestEnv(false)
	v := e.vaultIn(t, store.StateRealityCheck)

	bodies := map[string]string{
		"empty":             "",
		"just whitespace":   "   ",
		"unterminated":      `{"amount_inr": 450`,
		"not json":          "not json at all",
		"an array":          `[1, 2, 3]`,
		"a string":          `"text"`,
		"a wrong type":      `{"amount_inr": "four hundred"}`,
		"a fractional int":  `{"amount_inr": 450.5}`,
		"two values":        `{"amount_inr": 450}{"amount_inr": 451}`,
		"trailing garbage":  `{"amount_inr": 450} trailing`,
		"a null body":       `null`,
		"invalid utf8 text": "{\"amount_inr\": 450, \"x\": \"\xff\xfe\"",
	}
	routes := []struct {
		name string
		path string
	}{
		{"evaluate", pathEvaluate},
		{"reality-check", pathVaultBase + v.ID + "/reality-check"},
		{"guardian-action", pathVaultBase + v.ID + "/guardian-action"},
	}
	// Valid JSON that holds none of a route's fields is not a syntax problem. The
	// guardian-action route reads it as an action with no token, and that is a 403.
	validJSONForGuardian := map[string]bool{"a wrong type": true, "a fractional int": true, "a null body": true}

	for _, rt := range routes {
		for name, body := range bodies {
			if rt.name == "guardian-action" && validJSONForGuardian[name] {
				res := e.send(http.MethodPost, rt.path, []byte(body), nil)
				if res.status != http.StatusForbidden {
					t.Errorf("guardian-action with %s = %d %s, want 403 for a request without a token", name, res.status, res.body)
				}
				continue
			}
			res := e.send(http.MethodPost, rt.path, []byte(body), nil)
			if res.status != http.StatusBadRequest {
				t.Errorf("%s with %s = %d %s, want 400", rt.name, name, res.status, res.body)
				continue
			}
			if code := res.errorCode(t); code != "invalid_request" && !(rt.name == "evaluate" && name == "a null body" && code == "invalid_field") {
				t.Errorf("%s with %s: code = %q, want invalid_request", rt.name, name, code)
			}
		}
	}
}

func TestEvaluateNamesTheInvalidField(t *testing.T) {
	e := newTestEnv(false)
	tests := []struct {
		name      string
		body      map[string]any
		wantField string
	}{
		{"empty body object", map[string]any{}, "amount_inr"},
		{"zero amount", map[string]any{"amount_inr": 0}, "amount_inr"},
		{"negative age", map[string]any{"amount_inr": 100, "beneficiary_age_hours": -1}, "beneficiary_age_hours"},
		{"negative retries", map[string]any{"amount_inr": 100, "rapid_retry_count": -5}, "rapid_retry_count"},
		{"amount wins over age", map[string]any{"amount_inr": -1, "beneficiary_age_hours": -1}, "amount_inr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := e.do(t, http.MethodPost, pathEvaluate, tt.body)
			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Field   string `json:"field"`
				} `json:"error"`
			}
			res.json(t, &body)
			if res.status != http.StatusBadRequest || body.Error.Code != "invalid_field" || body.Error.Field != tt.wantField || body.Error.Message == "" {
				t.Fatalf("evaluate = %d %s, want a 400 invalid_field naming %s", res.status, res.body, tt.wantField)
			}
		})
	}
	if _, writes := e.cs.snapshot(); writes != 0 {
		t.Errorf("rejected requests wrote %d records", writes)
	}
}

func TestEvaluateRefusesUnmaskedIdentifiers(t *testing.T) {
	e := newTestEnv(false)
	for field, raw := range map[string]string{
		"payer_account_masked": "123456789012",
		"beneficiary_masked":   "987654321098",
		"upi_masked":           "rahul@okaxis",
		"phone_masked":         "9876543210",
	} {
		body := presetInstantPass()
		body[field] = raw
		res := e.do(t, http.MethodPost, pathEvaluate, body)
		if res.status != http.StatusBadRequest || res.errorCode(t) != "invalid_identifier" {
			t.Errorf("%s = %q gave %d %s, want a 400 invalid_identifier", field, raw, res.status, res.body)
		}
		if echoed, found := bodyContains(res, raw); found {
			t.Errorf("the response echoes the raw value %q", echoed)
		}
	}
	if calls, _ := e.cs.snapshot(); calls != 0 {
		t.Errorf("rejected identifiers reached the store (%d calls)", calls)
	}
}

// Every way a guardian token can fail gets the same status and the same body, so
// a prober learns nothing from which check failed.
func TestForbiddenBodiesAreIdentical(t *testing.T) {
	e := newTestEnv(false)
	v := e.evaluate(t, presetLocked())
	other := e.evaluate(t, presetLocked())
	path := pathVaultBase + v.ID + "/guardian-action"

	tampered := []byte(v.GuardianToken)
	tampered[len(tampered)-1] ^= 0x01

	tokens := map[string]string{
		"a changed tag":    string(tampered),
		"malformed":        "not-a-token",
		"empty":            "",
		"two parts":        v.ID + ".1000",
		"four parts":       v.GuardianToken + ".extra",
		"another vault":    other.GuardianToken,
		"a forged expiry":  v.ID + ".99999999999." + strings.Repeat("0", 64),
		"an uppercase tag": v.ID + "." + strings.Split(v.GuardianToken, ".")[1] + "." + strings.ToUpper(strings.Split(v.GuardianToken, ".")[2]),
	}

	var want []byte
	for name, token := range tokens {
		res := e.do(t, http.MethodPost, path, map[string]any{"action": "VETO", "token": token})
		if res.status != http.StatusForbidden || res.errorCode(t) != "forbidden" {
			t.Errorf("%s: %d %s, want a 403 forbidden", name, res.status, res.body)
			continue
		}
		if want == nil {
			want = res.body
		} else if !bytes.Equal(res.body, want) {
			t.Errorf("%s: body %s differs from %s", name, res.body, want)
		}
	}

	// An honest token that has run out gets the same answer.
	e.clock.AdvanceSeconds(vault.LockDuration)
	res := e.do(t, http.MethodPost, path, map[string]any{"action": "VETO", "token": v.GuardianToken})
	if res.status != http.StatusForbidden || !bytes.Equal(res.body, want) {
		t.Errorf("an expired token: %d %s, want the same 403 body", res.status, res.body)
	}
}

func TestErrorBodiesCarryOnlyACodeAndAMessage(t *testing.T) {
	e := newTestEnv(false)
	v := e.vaultIn(t, store.StateLocked15M)
	secrets := []string{"7777777", "z***@distinctivebank", "123456789012", "token-that-must-not-echo", "probe-id-value"}

	requests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"invalid amount", http.MethodPost, pathEvaluate, `{"amount_inr": -7777777, "upi_masked": "z***@distinctivebank"}`},
		{"raw identifier", http.MethodPost, pathEvaluate, `{"amount_inr": 100, "payer_account_masked": "123456789012"}`},
		{"bad json", http.MethodPost, pathEvaluate, `{"amount_inr": 7777777`},
		{"bad token", http.MethodPost, pathVaultBase + v.ID + "/guardian-action", `{"action":"VETO","token":"token-that-must-not-echo"}`},
		{"bad action", http.MethodPost, pathVaultBase + v.ID + "/guardian-action", mustJSONString(map[string]any{"action": "probe-id-value", "token": v.GuardianToken})},
		{"unknown vault", http.MethodGet, pathVaultBase + "probe-id-value", ""},
		{"wrong method", http.MethodDelete, pathEvaluate, ""},
		{"no route", http.MethodGet, "/probe-id-value", ""},
		{"bad answers", http.MethodPost, pathVaultBase + e.evaluate(t, presetRealityCheck()).ID + "/reality-check", `{"answers":{"coercion":"probe-id-value"}}`},
		{"conflict", http.MethodPost, pathVaultBase + v.ID + "/reality-check", `{"reconfirm": true}`},
		{"stream without id", http.MethodGet, pathStream, ""},
	}
	forbidden := []string{"goroutine", "panic", "runtime.", ".go:", "stack", "vault not found", "unmasked identifier", "store:", "dynamodb", "UpdateIf", "invalid transition"}
	for _, rq := range requests {
		t.Run(rq.name, func(t *testing.T) {
			res := e.send(rq.method, rq.path, []byte(rq.body), nil)
			if res.status < 400 {
				t.Fatalf("%s %s = %d, want an error", rq.method, rq.path, res.status)
			}

			var generic map[string]json.RawMessage
			res.json(t, &generic)
			for key := range generic {
				if key != "error" && key != "vault" {
					t.Errorf("error body has the top-level key %q", key)
				}
			}
			if _, hasVault := generic["vault"]; hasVault && res.status != http.StatusConflict {
				t.Errorf("a %d body carries a vault", res.status)
			}
			var detail map[string]string
			if err := json.Unmarshal(generic["error"], &detail); err != nil {
				t.Fatalf("error is not an object of strings: %v", err)
			}
			for key := range detail {
				if key != "code" && key != "message" && key != "field" {
					t.Errorf("error object has the key %q", key)
				}
			}
			if detail["code"] == "" || detail["message"] == "" {
				t.Errorf("error object = %v, want a code and a message", detail)
			}

			// A 409 carries a View that holds a token by design, so look for the
			// submitted secrets and the leaked internals in the error object only.
			text := string(generic["error"])
			for _, secret := range secrets {
				if strings.Contains(text, secret) {
					t.Errorf("the error object echoes %q", secret)
				}
			}
			for _, word := range forbidden {
				if strings.Contains(text, word) {
					t.Errorf("the error object contains %q", word)
				}
			}
		})
	}
}

func mustJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// bannedAlarmText lists the phrases and technical terms that no message may use.
func bannedAlarmText() []string {
	return []string{
		"FATAL ERROR", "SECURITY BREACH", "ILLEGAL TRANSACTION", "YOUR ACCOUNT WILL BE BLOCKED", "FRAUDSTER DETECTED",
		"heuristic mismatch", "anomaly score", "velocity threshold", "cryptographic hash", "HMAC", "SSL/TLS",
		"payload", "telemetry", "IP reputation", "rule engine",
	}
}

// Every message constant in errors.go is calm. The test reads the source, so a
// constant added later is checked too.
func TestEveryErrorMessageIsCalm(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatalf("parse errors.go: %v", err)
	}
	checked := 0
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "msg") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}
				checked++
				for _, banned := range bannedAlarmText() {
					if strings.Contains(strings.ToLower(text), strings.ToLower(banned)) {
						t.Errorf("%s = %q contains %q", name.Name, text, banned)
					}
				}
			}
		}
	}
	if checked < 8 {
		t.Fatalf("found only %d msg constants in errors.go, so the check would be vacuous", checked)
	}
}

func TestFastForwardIsHiddenWithoutDemoMode(t *testing.T) {
	e := newTestEnv(false)
	v := e.vaultIn(t, store.StateLocked15M)
	callsBefore, _ := e.cs.snapshot()

	existing := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/demo-fast-forward", nil)
	missing := e.do(t, http.MethodPost, pathVaultBase+strings.Repeat("0", 32)+"/demo-fast-forward", nil)

	if existing.status != http.StatusNotFound || missing.status != http.StatusNotFound {
		t.Fatalf("fast-forward with demo off = %d and %d, want 404 for both", existing.status, missing.status)
	}
	if !bytes.Equal(existing.body, missing.body) {
		t.Errorf("an existing and a missing vault get different bodies: %s vs %s", existing.body, missing.body)
	}
	if callsAfter, _ := e.cs.snapshot(); callsAfter != callsBefore {
		t.Errorf("fast-forward with demo off read the vault (%d store calls)", callsAfter-callsBefore)
	}
	if got := e.get(t, v.ID); got.RemainingSeconds != vault.LockDuration {
		t.Errorf("the lock changed: %d seconds left", got.RemainingSeconds)
	}
}

func TestFastForwardWithDemoModeConflictsOutsideTheLock(t *testing.T) {
	e := newTestEnv(true)
	v := e.vaultIn(t, store.StateVetoed)
	res := e.do(t, http.MethodPost, pathVaultBase+v.ID+"/demo-fast-forward", nil)
	if res.status != http.StatusConflict || res.errorCode(t) != "state_conflict" {
		t.Fatalf("fast-forward on a VETOED vault = %d %s, want 409", res.status, res.body)
	}
}

// ---- event stream ----

// newTestServer starts a real server for h. It registers the shutdown with
// t.Cleanup before any stream client registers its own cleanup, so the clients
// close first (cleanups run last-in, first-out). A server does not shut down
// while a stream is still open.
func newTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// sseClient reads a Server-Sent Events response line by line.
type sseClient struct {
	resp  *http.Response
	lines chan string
}

func openStream(t *testing.T, srv *httptest.Server, query string) *sseClient {
	t.Helper()
	resp, err := http.Get(srv.URL + pathStream + query)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	c := &sseClient{resp: resp, lines: make(chan string, 64)}
	go func() {
		defer close(c.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				c.lines <- line
			}
		}
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return c
}

// next returns the next non-empty line, or fails the test after the timeout.
func (c *sseClient) next(t *testing.T, timeout time.Duration) (string, bool) {
	t.Helper()
	select {
	case line, ok := <-c.lines:
		return line, ok
	case <-time.After(timeout):
		t.Fatalf("no line within %v", timeout)
		return "", false
	}
}

func (c *sseClient) nextView(t *testing.T) vault.View {
	t.Helper()
	line, ok := c.next(t, 3*time.Second)
	if !ok || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("next line = %q (open %v), want a data event", line, ok)
	}
	var v vault.View
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &v); err != nil {
		t.Fatalf("event %q is not a View: %v", line, err)
	}
	return v
}

func TestStreamSendsTheCurrentViewFirstAndThenChanges(t *testing.T) {
	e := newTestEnv(false)
	v := e.evaluate(t, presetLocked())
	srv := newTestServer(t, e.handler)

	c := openStream(t, srv, "?vault_id="+v.ID)

	if got := c.resp.StatusCode; got != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", got)
	}
	h := c.resp.Header
	if !strings.HasPrefix(h.Get("Content-Type"), "text/event-stream") || h.Get("Cache-Control") != "no-cache" || h.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers = Content-Type %q, Cache-Control %q, X-Accel-Buffering %q", h.Get("Content-Type"), h.Get("Cache-Control"), h.Get("X-Accel-Buffering"))
	}

	first := c.nextView(t)
	if first.ID != v.ID || first.State != store.StateLocked15M || first.RemainingSeconds != vault.LockDuration {
		t.Errorf("first event = %+v, want the current LOCKED_15M view", first)
	}
	if first.GuardianToken != "" {
		t.Errorf("the first event carries the guardian token")
	}

	// A change made through the API reaches the open stream, without the token.
	e.mustAct(t, v, "VETO", false)
	got := c.nextView(t)
	if got.ID != v.ID || got.State != store.StateVetoed || got.GuardianToken != "" || got.Evidence == "" {
		t.Errorf("event after the veto = %+v, want the VETOED view with evidence and no token", got)
	}
}

func TestStreamEventsGoOnlyToTheirOwnVault(t *testing.T) {
	e := newTestEnv(false)
	a := e.evaluate(t, presetLocked())
	b := e.evaluate(t, presetLocked())
	srv := newTestServer(t, e.handler)

	streamA := openStream(t, srv, "?vault_id="+a.ID)
	streamA.nextView(t)

	e.mustAct(t, b, "VETO", false) // a change to vault B
	e.mustAct(t, a, "VETO", false) // then a change to vault A

	if got := streamA.nextView(t); got.ID != a.ID || got.State != store.StateVetoed {
		t.Errorf("stream A received %+v, want only vault A's veto", got)
	}
}

func TestStreamWritesKeepAliveComments(t *testing.T) {
	e := newTestEnv(false, withKeepAlive(20*time.Millisecond))
	v := e.evaluate(t, presetLocked())
	srv := newTestServer(t, e.handler)

	c := openStream(t, srv, "?vault_id="+v.ID)
	c.nextView(t)

	for i := 0; i < 3; i++ {
		line, ok := c.next(t, 3*time.Second)
		if !ok || line != ": keep-alive" {
			t.Fatalf("line %d = %q (open %v), want the keep-alive comment", i, line, ok)
		}
	}
}

func TestStreamDisconnectRemovesTheSubscription(t *testing.T) {
	e := newTestEnv(false)
	v := e.evaluate(t, presetLocked())
	srv := newTestServer(t, e.handler)

	c := openStream(t, srv, "?vault_id="+v.ID)
	c.nextView(t)
	if got := e.server.hub.Subscribers(v.ID); got != 1 {
		t.Fatalf("Subscribers = %d with the stream open, want 1", got)
	}

	c.resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for e.server.hub.Subscribers(v.ID) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("Subscribers = %d three seconds after the client left, want 0", e.server.hub.Subscribers(v.ID))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStreamOnLambdaClosesAtTheLimit(t *testing.T) {
	e := newTestEnv(false, withOnLambda(150*time.Millisecond))
	v := e.evaluate(t, presetLocked())
	srv := newTestServer(t, e.handler)

	started := time.Now()
	c := openStream(t, srv, "?vault_id="+v.ID)
	c.nextView(t)

	if _, open := c.next(t, 3*time.Second); open {
		t.Fatal("the stream is still open after the Lambda limit, want it closed")
	}
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Errorf("the stream closed after %v, before the 150ms limit", elapsed)
	}
	deadline := time.Now().Add(2 * time.Second)
	for e.server.hub.Subscribers(v.ID) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := e.server.hub.Subscribers(v.ID); got != 0 {
		t.Errorf("Subscribers = %d after the stream closed, want 0", got)
	}
}

func TestStreamOffLambdaStaysOpen(t *testing.T) {
	e := newTestEnv(false, withKeepAlive(time.Hour))
	e.server.lambdaMax = 50 * time.Millisecond // would close the stream if it applied
	v := e.evaluate(t, presetLocked())
	srv := newTestServer(t, e.handler)

	c := openStream(t, srv, "?vault_id="+v.ID)
	c.nextView(t)

	select {
	case line, open := <-c.lines:
		t.Fatalf("got %q (open %v), want a quiet open stream", line, open)
	case <-time.After(300 * time.Millisecond):
	}
}

// ---- logging ----

var logLine = regexp.MustCompile(`^route="(?:[A-Z]+ /[^"]*)?-?" status=\d{3} vault=(?:[0-9a-f]{32}|-)$`)

func TestLogLinesHoldOnlyMethodRouteStatusAndVaultID(t *testing.T) {
	e := newTestEnv(true)

	secrets := []string{"7777777", "z***@distinctivebank", "XXXX-XXXX-9999", "+91-XXXXXX1234", harnessKey}
	body := map[string]any{
		"amount_inr": 7777777, "beneficiary_age_hours": 1, "is_late_night": true, "active_unknown_call": true,
		"upi_masked": "z***@distinctivebank", "payer_account_masked": "XXXX-XXXX-9999", "phone_masked": "+91-XXXXXX1234",
	}
	v := e.evaluate(t, body)
	secrets = append(secrets, v.GuardianToken)

	e.do(t, http.MethodGet, pathVaultBase+v.ID, nil)
	e.do(t, http.MethodPost, pathVaultBase+v.ID+"/guardian-action", map[string]any{"action": "APPROVE", "token": v.GuardianToken})
	e.do(t, http.MethodPost, pathVaultBase+v.ID+"/guardian-action", map[string]any{"action": "VETO", "token": "wrong-token"})
	e.do(t, http.MethodPost, pathVaultBase+v.ID+"/demo-fast-forward", nil)
	e.do(t, http.MethodPost, pathEvaluate, map[string]any{"amount_inr": -1})
	e.do(t, http.MethodGet, "/no/such/route", nil)
	e.send(http.MethodGet, pathVaultBase+"abc%0Aroute=%22forged%22%20status=200", nil, nil) // an id with a newline
	e.send(http.MethodGet, pathStream+"?vault_id=x%0Aforged-line", nil, nil)
	e.send(http.MethodOptions, pathEvaluate, nil, http.Header{"Origin": {harnessOrigin}})

	logs := e.logs.String()
	lines := strings.Split(strings.TrimRight(logs, "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("%d log lines for 10 requests, want one each:\n%s", len(lines), logs)
	}
	for _, line := range lines {
		if !logLine.MatchString(line) {
			t.Errorf("log line %q does not have the form route, status, vault id", line)
		}
	}
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("the log holds %q:\n%s", secret, logs)
		}
	}
	if !strings.Contains(logs, `route="POST /api/v1/transactions/evaluate" status=200 vault=`+v.ID) {
		t.Errorf("the evaluate line does not name the new vault:\n%s", logs)
	}
	if !strings.Contains(logs, `route="GET /api/v1/vault/{id}" status=200 vault=`+v.ID) {
		t.Errorf("the GET line does not hold the route pattern and the vault id:\n%s", logs)
	}
	if !strings.Contains(logs, `route="POST /api/v1/vault/{id}/guardian-action" status=403 vault=`+v.ID) {
		t.Errorf("the forbidden line is missing:\n%s", logs)
	}
	if strings.Contains(logs, "forged") {
		t.Errorf("a caller-chosen value reached the log:\n%s", logs)
	}
}

// ---- building ----

func TestBuildServesWithTheMemoryDriver(t *testing.T) {
	cfg := Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, DemoMode: true, HMACKey: []byte(harnessKey)}
	handler, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build = %v, want nil", err)
	}

	srv := newTestServer(t, handler)
	resp, err := http.Post(srv.URL+pathEvaluate, "application/json", bytes.NewReader(mustJSON(t, presetLocked())))
	if err != nil {
		t.Fatalf("POST evaluate: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var v vault.View
	if err := json.Unmarshal(raw, &v); err != nil || resp.StatusCode != http.StatusOK || v.State != store.StateLocked15M || v.GuardianToken == "" {
		t.Fatalf("evaluate = %d %s (%v), want a locked vault with a token", resp.StatusCode, raw, err)
	}
}

func TestBuildRefusesAnUnknownDriver(t *testing.T) {
	t.Setenv("STORAGE_DRIVER", "carrier-pigeon")
	cfg := Config{StorageDriver: "carrier-pigeon", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)}

	handler, err := Build(context.Background(), cfg)
	if err == nil || handler != nil {
		t.Fatalf("Build = %v, %v, want an error and no handler", handler, err)
	}
	if !strings.Contains(err.Error(), "STORAGE_DRIVER") || strings.Contains(err.Error(), "carrier-pigeon") {
		t.Errorf("error %q should name the variable and not echo its value", err)
	}
}

func TestNewTakesProductionDefaults(t *testing.T) {
	cfg := Config{AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)}
	handler := New(cfg, Deps{Logger: log.New(io.Discard, "", 0)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, pathEvaluate, bytes.NewReader(mustJSON(t, presetInstantPass()))))
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate over the default store and clock = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

// failingStore fails every call with an error whose text holds values that must
// never reach a caller.
type failingStore struct {
	store.VaultStore
	err error
}

func (f failingStore) Create(context.Context, store.VaultRecord) error { return f.err }

func (f failingStore) Get(context.Context, string) (store.VaultRecord, error) {
	return store.VaultRecord{}, f.err
}

func (f failingStore) UpdateIf(context.Context, string, store.VaultState, store.VaultRecord) error {
	return f.err
}

// A store failure becomes a 500 with a fixed message. The store's own text, which
// can hold a table name or a request value, reaches neither the body nor the log.
func TestStoreFailuresBecomeAFixedInternalError(t *testing.T) {
	const leak = "boom: payer XXXX-XXXX-4821 in table SecretVaultsTable"
	logs := &logBuffer{}
	handler := New(
		Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, DemoMode: true, HMACKey: []byte(harnessKey)},
		Deps{Store: failingStore{err: errors.New(leak)}, Logger: log.New(logs, "", 0)},
	)

	id := strings.Repeat("a", 32)
	requests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"evaluate", http.MethodPost, pathEvaluate, mustJSON(t, presetInstantPass())},
		{"get", http.MethodGet, pathVaultBase + id, nil},
		{"reality-check", http.MethodPost, pathVaultBase + id + "/reality-check", []byte(`{"reconfirm":true}`)},
		{"fast-forward", http.MethodPost, pathVaultBase + id + "/demo-fast-forward", nil},
		{"stream", http.MethodGet, pathStream + "?vault_id=" + id, nil},
	}
	var first []byte
	for _, rq := range requests {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(rq.method, rq.path, bytes.NewReader(rq.body)))
		body := rec.Body.Bytes()

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s = %d %s, want 500", rq.name, rec.Code, body)
			continue
		}
		for _, secret := range []string{"boom", "XXXX-XXXX-4821", "SecretVaultsTable"} {
			if strings.Contains(string(body), secret) {
				t.Errorf("%s: the body holds %q", rq.name, secret)
			}
		}
		if first == nil {
			first = body
		} else if !bytes.Equal(body, first) {
			t.Errorf("%s: body %s differs from the other 500 bodies %s", rq.name, body, first)
		}
		var parsed struct {
			Error struct{ Code string }
		}
		if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error.Code != "internal" {
			t.Errorf("%s: body %s, want the code internal", rq.name, body)
		}
	}
	if strings.Contains(logs.String(), "boom") || strings.Contains(logs.String(), "SecretVaultsTable") {
		t.Errorf("the log holds the store error text:\n%s", logs.String())
	}
}
