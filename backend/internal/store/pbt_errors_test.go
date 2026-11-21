package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// errorsLeakCheck fails the test unless err wraps injected, names the
// operation and the vault id, and holds none of the record's values.
func errorsLeakCheck(rt *rapid.T, err, injected error, op string, rec VaultRecord) {
	rt.Helper()

	if err == nil {
		rt.Fatalf("%s returned nil, want an error", op)
	}
	if !errors.Is(err, injected) {
		rt.Fatalf("%s error %q does not wrap the injected error", op, err)
	}

	msg := err.Error()
	if !strings.Contains(msg, op+" "+rec.ID) {
		rt.Fatalf("%s error %q does not contain the operation and the vault id %q", op, msg, rec.ID)
	}

	secrets := map[string]string{
		"amount_inr":           strconv.FormatInt(rec.AmountINR, 10),
		"risk_score":           strconv.Itoa(rec.RiskScore),
		"payer_account_masked": rec.PayerAccountMasked,
		"beneficiary_masked":   rec.BeneficiaryMasked,
		"upi_masked":           rec.UPIMasked,
		"phone_masked":         rec.PhoneMasked,
	}
	for name, value := range secrets {
		if value == "" {
			continue // an empty field has no value to leak
		}
		if strings.Contains(msg, value) {
			rt.Fatalf("%s error %q contains %s %q", op, msg, name, value)
		}
	}
}

// errorsEchoing returns an error whose text repeats every value of rec, the way
// an SDK error can echo the request that failed. If the store put this text in
// its own message, errorsLeakCheck would find the values.
func errorsEchoing(rec VaultRecord) error {
	return fmt.Errorf("request failed for %s amount %d score %d payer %s beneficiary %s upi %s phone %s",
		rec.ID, rec.AmountINR, rec.RiskScore,
		rec.PayerAccountMasked, rec.BeneficiaryMasked, rec.UPIMasked, rec.PhoneMasked)
}

// Feature: behavioral-risk-engine, Property 15: Errors wrap the cause and leak no record values
//
// DynamoDBStore only. The client fails with a non-conditional error whose own
// text repeats the record's values; the store's error must still wrap it,
// name the operation and the vault id, and hold none of the values.
//
// **Validates: Requirements 10.5, 12.6**
func TestDynamoDBErrorsWrapCauseAndLeakNoRecordValues(t *testing.T) {
	ctx := context.Background()

	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		rec := contractRecordGen().Draw(rt, "record")
		next := contractRecordGen().Draw(rt, "next")
		next.ID = rec.ID
		injected := errorsEchoing(rec)

		// Create: the put fails.
		env.fake.failNext(fakeDynamoDBOpPutItem, injected)
		errorsLeakCheck(rt, env.vs.Create(ctx, rec), injected, "create", rec)

		// The failed put stored nothing, so Create can run for real now.
		contractMustCreate(rt, env.vs, rec)

		// Get: the read fails.
		env.fake.failNext(fakeDynamoDBOpGetItem, injected)
		_, err := env.vs.Get(ctx, rec.ID)
		errorsLeakCheck(rt, err, injected, "get", rec)

		// UpdateIf: the put fails.
		env.fake.failNext(fakeDynamoDBOpPutItem, injected)
		err = env.vs.UpdateIf(ctx, rec.ID, rec.State, next)
		errorsLeakCheck(rt, err, injected, "update", rec)

		// UpdateIf: the condition fails, then the follow-up read fails.
		env.fake.failNextConditional(fakeDynamoDBOpPutItem)
		env.fake.failNext(fakeDynamoDBOpGetItem, injected)
		err = env.vs.UpdateIf(ctx, rec.ID, rec.State, next)
		errorsLeakCheck(rt, err, injected, "update", rec)

		// None of the failed calls changed the stored record.
		contractWantRecord(rt, env.vs, rec.ID, rec)
	})
}
