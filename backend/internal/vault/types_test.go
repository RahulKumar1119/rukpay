package vault

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
)

// The JSON key names below are the contract with the front end
// (dual-screen-simulator, lib/api.ts). Changing one is a breaking change.
var (
	signalKeys = []string{
		"active_unknown_call",
		"amount_inr",
		"beneficiary_age_hours",
		"is_late_night",
		"rapid_retry_count",
	}
	identifierKeys = []string{
		"beneficiary_masked",
		"payer_account_masked",
		"phone_masked",
		"upi_masked",
	}
)

// secretFields are names a client might send that the types must never keep.
// The values are chosen so that none of them can appear in an encoded body by
// chance.
var secretFields = map[string]string{
	"otp":      "OTP-4829-LEAK",
	"pin":      "PIN-7731-LEAK",
	"password": "PASSWORD-LEAK",
	"passcode": "PASSCODE-LEAK",
	"cvv":      "CVV-LEAK",
	// Raw identifiers under their natural names, in case a client sends them.
	"account_number": "ACCOUNT-123456789012-LEAK",
	"upi_id":         "UPI-rahul@okaxis-LEAK",
	"phone":          "PHONE-9876543210-LEAK",
}

func fullRequest() EvaluateRequest {
	return EvaluateRequest{
		Signals: risk.Signals{
			AmountINR:         85000,
			BeneficiaryAgeHrs: 1,
			IsLateNight:       true,
			ActiveUnknownCall: true,
			RapidRetryCount:   2,
		},
		PayerAccountMasked: "XXXX-XXXX-4821",
		BeneficiaryMasked:  "XXXX-XXXX-9034",
		UPIMasked:          "r***@okaxis",
		PhoneMasked:        "+91-XXXXXX3210",
	}
}

func fullMetadata() Metadata {
	return Metadata{
		PayerAccountMasked: "XXXX-XXXX-4821",
		BeneficiaryMasked:  "XXXX-XXXX-9034",
		UPIMasked:          "r***@okaxis",
		PhoneMasked:        "+91-XXXXXX3210",
		AmountINR:          85000,
	}
}

// jsonObject decodes raw as one JSON object.
func jsonObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s is not a JSON object: %v", raw, err)
	}
	return m
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// concat returns the sorted union of the given key lists.
func concat(lists ...[]string) []string {
	var all []string
	for _, l := range lists {
		all = append(all, l...)
	}
	sort.Strings(all)
	return all
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%+v) = %v, want nil", v, err)
	}
	return raw
}

func TestEvaluateRequestJSONKeys(t *testing.T) {
	obj := jsonObject(t, marshal(t, fullRequest()))

	if got, want := sortedKeys(obj), concat(signalKeys, identifierKeys); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}

	// The signals are flat at the top level, not nested under "Signals".
	want := map[string]any{
		"amount_inr":            float64(85000),
		"beneficiary_age_hours": float64(1),
		"is_late_night":         true,
		"active_unknown_call":   true,
		"rapid_retry_count":     float64(2),
		"payer_account_masked":  "XXXX-XXXX-4821",
		"beneficiary_masked":    "XXXX-XXXX-9034",
		"upi_masked":            "r***@okaxis",
		"phone_masked":          "+91-XXXXXX3210",
	}
	if !reflect.DeepEqual(obj, want) {
		t.Fatalf("body = %v, want %v", obj, want)
	}
}

func TestEvaluateRequestOmitsEmptyIdentifiers(t *testing.T) {
	signalsOnly := EvaluateRequest{Signals: fullRequest().Signals}

	t.Run("none set", func(t *testing.T) {
		got := sortedKeys(jsonObject(t, marshal(t, signalsOnly)))
		if !reflect.DeepEqual(got, signalKeys) {
			t.Fatalf("keys = %v, want only the five signals %v", got, signalKeys)
		}
	})

	tests := []struct {
		key string
		set func(*EvaluateRequest)
	}{
		{"payer_account_masked", func(r *EvaluateRequest) { r.PayerAccountMasked = "XXXX-XXXX-4821" }},
		{"beneficiary_masked", func(r *EvaluateRequest) { r.BeneficiaryMasked = "XXXX-XXXX-9034" }},
		{"upi_masked", func(r *EvaluateRequest) { r.UPIMasked = "r***@okaxis" }},
		{"phone_masked", func(r *EvaluateRequest) { r.PhoneMasked = "+91-XXXXXX3210" }},
	}
	for _, tt := range tests {
		t.Run("only "+tt.key, func(t *testing.T) {
			req := signalsOnly
			tt.set(&req)
			got := sortedKeys(jsonObject(t, marshal(t, req)))
			want := concat(signalKeys, []string{tt.key})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("keys = %v, want %v", got, want)
			}
		})
	}
}

func TestEvaluateRequestRoundTrip(t *testing.T) {
	for name, want := range map[string]EvaluateRequest{
		"full":         fullRequest(),
		"signals only": {Signals: fullRequest().Signals},
		"zero":         {},
	} {
		t.Run(name, func(t *testing.T) {
			var got EvaluateRequest
			if err := json.Unmarshal(marshal(t, want), &got); err != nil {
				t.Fatalf("Unmarshal = %v, want nil", err)
			}
			if got != want {
				t.Fatalf("round trip = %+v, want %+v", got, want)
			}
		})
	}
}

// withSecrets returns body with every secretFields entry added at the top level.
func withSecrets(t *testing.T, body map[string]any) []byte {
	t.Helper()
	merged := make(map[string]any, len(body)+len(secretFields))
	for k, v := range body {
		merged[k] = v
	}
	for k, v := range secretFields {
		merged[k] = v
	}
	return marshal(t, merged)
}

// assertNoSecrets fails if raw holds a secret field name or value.
func assertNoSecrets(t *testing.T, raw []byte) {
	t.Helper()
	obj := jsonObject(t, raw)
	for name, value := range secretFields {
		if _, ok := obj[name]; ok {
			t.Errorf("re-encoded body has the key %q: %s", name, raw)
		}
		if bytes.Contains(raw, []byte(value)) {
			t.Errorf("re-encoded body holds the value of %q: %s", name, raw)
		}
	}
}

func TestEvaluateRequestDropsUnknownFields(t *testing.T) {
	in := withSecrets(t, jsonObject(t, marshal(t, fullRequest())))

	var got EvaluateRequest
	if err := json.Unmarshal(in, &got); err != nil {
		t.Fatalf("Unmarshal = %v, want nil: unknown fields must be dropped, not rejected", err)
	}
	if want := fullRequest(); got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}

	out := marshal(t, got)
	assertNoSecrets(t, out)
	if got, want := sortedKeys(jsonObject(t, out)), concat(signalKeys, identifierKeys); !reflect.DeepEqual(got, want) {
		t.Fatalf("re-encoded keys = %v, want %v", got, want)
	}
}

func TestMetadataJSONKeys(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		got := jsonObject(t, marshal(t, fullMetadata()))
		want := map[string]any{
			"amount_inr":           float64(85000),
			"payer_account_masked": "XXXX-XXXX-4821",
			"beneficiary_masked":   "XXXX-XXXX-9034",
			"upi_masked":           "r***@okaxis",
			"phone_masked":         "+91-XXXXXX3210",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("body = %v, want %v", got, want)
		}
	})

	t.Run("empty identifiers are omitted and the amount stays", func(t *testing.T) {
		got := string(marshal(t, Metadata{AmountINR: 450}))
		if want := `{"amount_inr":450}`; got != want {
			t.Fatalf("body = %s, want %s", got, want)
		}
	})

	t.Run("zero value still carries the amount", func(t *testing.T) {
		got := string(marshal(t, Metadata{}))
		if want := `{"amount_inr":0}`; got != want {
			t.Fatalf("body = %s, want %s", got, want)
		}
	})
}

func TestMetadataRoundTripAndUnknownFields(t *testing.T) {
	var got Metadata
	if err := json.Unmarshal(marshal(t, fullMetadata()), &got); err != nil {
		t.Fatalf("Unmarshal = %v, want nil", err)
	}
	if want := fullMetadata(); got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	in := withSecrets(t, jsonObject(t, marshal(t, fullMetadata())))
	var dropped Metadata
	if err := json.Unmarshal(in, &dropped); err != nil {
		t.Fatalf("Unmarshal = %v, want nil: unknown fields must be dropped, not rejected", err)
	}
	if want := fullMetadata(); dropped != want {
		t.Fatalf("decoded = %+v, want %+v", dropped, want)
	}
	assertNoSecrets(t, marshal(t, dropped))
}

// jsonFieldNames lists the JSON key of every exported field of t. A field that
// is embedded without a tag contributes its own fields, as encoding/json does.
func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			names = append(names, jsonFieldNames(f.Type)...)
			continue
		}
		if !f.IsExported() || name == "-" {
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

// TestRequestTypesDeclareOnlyKnownFields fails when someone adds a field. The
// marshal tests above cannot see a new field that no test populates, so this
// one reads the types themselves (Requirements 11.3, 12.5).
func TestRequestTypesDeclareOnlyKnownFields(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{"EvaluateRequest", reflect.TypeOf(EvaluateRequest{}), concat(signalKeys, identifierKeys)},
		{"Metadata", reflect.TypeOf(Metadata{}), concat([]string{"amount_inr"}, identifierKeys)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonFieldNames(tt.typ); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("JSON fields = %v, want %v", got, tt.want)
			}
		})
	}
}
