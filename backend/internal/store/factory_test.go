package store

import (
	"context"
	"testing"
)

// historyOnlyStore is a VaultStore that also implements HistoryStore and nothing
// else, so Extensions must report one extension and leave the other nil.
type historyOnlyStore struct {
	VaultStore
	HistoryStore
}

// beneficiaryOnlyStore is the mirror: a VaultStore and a BeneficiaryStore.
type beneficiaryOnlyStore struct {
	VaultStore
	BeneficiaryStore
}

// plainVaultStore hides every method of its wrapped store except the three of
// VaultStore, as a store written before the extensions existed would.
type plainVaultStore struct{ inner VaultStore }

func (p plainVaultStore) Create(ctx context.Context, rec VaultRecord) error {
	return p.inner.Create(ctx, rec)
}

func (p plainVaultStore) Get(ctx context.Context, id string) (VaultRecord, error) {
	return p.inner.Get(ctx, id)
}

func (p plainVaultStore) UpdateIf(ctx context.Context, id string, expected VaultState, next VaultRecord) error {
	return p.inner.UpdateIf(ctx, id, expected, next)
}

// Both adapters implement both extensions, and Extensions hands back the very
// store it was given, so a caller gets one shared set of items.
func TestExtensionsReturnBothExtensionsOfBothAdapters(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		history, beneficiaries := Extensions(env.vs)
		if history == nil || beneficiaries == nil {
			t.Fatalf("Extensions(%T) = %v, %v, want two non-nil results", env.vs, history, beneficiaries)
		}
		if any(history) != any(env.vs) || any(beneficiaries) != any(env.vs) {
			t.Fatalf("Extensions(%T) returned another store, want the same one", env.vs)
		}

		// The results work: a recipient and an event written through them are
		// read back through the same store.
		ctx := context.Background()
		rec := BeneficiaryRecord{ID: "a", IdentifierMasked: "r***@okaxis", Trust: TrustNew, Protection: ProtectionExtraCare}
		if err := beneficiaries.CreateBeneficiary(ctx, PayerKey, rec); err != nil {
			t.Fatalf("CreateBeneficiary through the extension = %v, want nil", err)
		}
		if err := history.AppendHistory(ctx, PayerKey, HistoryEvent{ID: "e", At: 1}); err != nil {
			t.Fatalf("AppendHistory through the extension = %v, want nil", err)
		}
		if _, err := beneficiaries.GetBeneficiary(ctx, PayerKey, "a"); err != nil {
			t.Fatalf("GetBeneficiary = %v, want nil", err)
		}
	})
}

func TestExtensionsReturnNilForWhatAStoreDoesNotImplement(t *testing.T) {
	mem := NewMemoryStore()
	tests := []struct {
		name        string
		vs          VaultStore
		wantHistory bool
		wantBene    bool
	}{
		{"a store with only the three vault methods", plainVaultStore{inner: mem}, false, false},
		{"a store that adds only history", historyOnlyStore{VaultStore: mem, HistoryStore: mem}, true, false},
		{"a store that adds only beneficiaries", beneficiaryOnlyStore{VaultStore: mem, BeneficiaryStore: mem}, false, true},
		{"no store at all", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history, beneficiaries := Extensions(tt.vs)
			// Compare with nil as an interface value: a typed nil inside a
			// non-nil interface would pass a pointer check and still panic.
			if (history != nil) != tt.wantHistory {
				t.Errorf("history extension = %v, want non-nil %v", history, tt.wantHistory)
			}
			if (beneficiaries != nil) != tt.wantBene {
				t.Errorf("beneficiary extension = %v, want non-nil %v", beneficiaries, tt.wantBene)
			}
		})
	}
}

// NewFromEnv keeps returning the adapters themselves, so a caller can still
// assert on the concrete type, and Extensions finds both extensions on them.
func TestExtensionsOnWhatNewFromEnvBuilds(t *testing.T) {
	t.Setenv(envStorageDriver, driverMemory)
	vs, err := NewFromEnv(context.Background())
	if err != nil {
		t.Fatalf("NewFromEnv(memory) = %v, want nil", err)
	}
	if _, ok := vs.(*MemoryStore); !ok {
		t.Fatalf("NewFromEnv(memory) returned %T, want *MemoryStore", vs)
	}
	if history, beneficiaries := Extensions(vs); history == nil || beneficiaries == nil {
		t.Fatalf("Extensions of the memory store = %v, %v, want both non-nil", history, beneficiaries)
	}

	awsTestEnv(t)
	t.Setenv(envStorageDriver, driverDynamoDB)
	t.Setenv(envTableName, "")
	t.Setenv(envEndpoint, "http://localhost:8000")
	vs, err = NewFromEnv(context.Background())
	if err != nil {
		t.Fatalf("NewFromEnv(dynamodb) = %v, want nil", err)
	}
	if _, ok := vs.(*DynamoDBStore); !ok {
		t.Fatalf("NewFromEnv(dynamodb) returned %T, want *DynamoDBStore", vs)
	}
	if history, beneficiaries := Extensions(vs); history == nil || beneficiaries == nil {
		t.Fatalf("Extensions of the DynamoDB store = %v, %v, want both non-nil", history, beneficiaries)
	}
}
