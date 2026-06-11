package sqlite

import "testing"

func TestBridgeSettingsRoundTrip(t *testing.T) {
	st := openTemp(t)

	// Empty to start.
	got, err := st.BridgeSettings()
	if err != nil {
		t.Fatalf("BridgeSettings: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no settings, got %v", got)
	}

	// Save then read back; a second save upserts.
	if err := st.SaveBridgeSettings(map[string]string{"EVOLUTION_API_KEY": "k1", "CHATWOOT_ACCOUNT_ID": "2"}, "admin"); err != nil {
		t.Fatalf("SaveBridgeSettings: %v", err)
	}
	if err := st.SaveBridgeSettings(map[string]string{"EVOLUTION_API_KEY": "k2"}, "admin"); err != nil {
		t.Fatalf("SaveBridgeSettings upsert: %v", err)
	}
	got, err = st.BridgeSettings()
	if err != nil {
		t.Fatalf("BridgeSettings: %v", err)
	}
	if got["EVOLUTION_API_KEY"] != "k2" {
		t.Errorf("upsert failed, got %q", got["EVOLUTION_API_KEY"])
	}
	if got["CHATWOOT_ACCOUNT_ID"] != "2" {
		t.Errorf("existing key lost, got %q", got["CHATWOOT_ACCOUNT_ID"])
	}
}
