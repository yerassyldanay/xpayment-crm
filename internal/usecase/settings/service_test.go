package settings

import "testing"

type fakeStore struct {
	data  map[string]string
	saved map[string]string
}

func (f *fakeStore) BridgeSettings() (map[string]string, error) { return f.data, nil }
func (f *fakeStore) SaveBridgeSettings(values map[string]string, _ string) error {
	f.saved = values
	return nil
}

type fakeApplier struct{ applied *Bridge }

func (f *fakeApplier) ApplyBridge(b Bridge) { f.applied = &b }

func TestCurrentOverlaysStoredOnDefaults(t *testing.T) {
	defaults := Bridge{EvolutionBaseURL: "http://env:9700", EvolutionAPIKey: "envkey", ChatwootAccountID: "1"}
	svc := NewService(&fakeStore{data: map[string]string{"EVOLUTION_API_KEY": "dbkey"}}, nil, defaults)

	got, err := svc.Current()
	if err != nil {
		t.Fatal(err)
	}
	if got.EvolutionAPIKey != "dbkey" {
		t.Errorf("stored value should override default: got %q", got.EvolutionAPIKey)
	}
	if got.EvolutionBaseURL != "http://env:9700" {
		t.Errorf("unstored key should fall back to default: got %q", got.EvolutionBaseURL)
	}
	if got.ChatwootAccountID != "1" {
		t.Errorf("unstored default lost: got %q", got.ChatwootAccountID)
	}
}

func TestSaveNormalizesPersistsAndApplies(t *testing.T) {
	store := &fakeStore{data: map[string]string{}}
	applier := &fakeApplier{}
	svc := NewService(store, applier, Bridge{})

	in := Bridge{
		EvolutionBaseURL:  "http://host:9700/", // trailing slash trimmed
		EvolutionAPIKey:   "  k  ",             // whitespace trimmed
		ChatwootAccountID: "2",
	}
	if err := svc.Save(in, "admin"); err != nil {
		t.Fatal(err)
	}
	if applier.applied == nil {
		t.Fatal("applier was not invoked")
	}
	if applier.applied.EvolutionBaseURL != "http://host:9700" {
		t.Errorf("URL not trimmed before apply: %q", applier.applied.EvolutionBaseURL)
	}
	if applier.applied.EvolutionAPIKey != "k" {
		t.Errorf("key not trimmed before apply: %q", applier.applied.EvolutionAPIKey)
	}
	if store.saved["EVOLUTION_API_BASE_URL"] != "http://host:9700" {
		t.Errorf("persisted base url wrong: %v", store.saved)
	}
	if store.saved["CHATWOOT_ACCOUNT_ID"] != "2" {
		t.Errorf("persisted account id wrong: %v", store.saved)
	}
}
