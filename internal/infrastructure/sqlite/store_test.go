package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/admin"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestMigrateAndSeedLoadsSnapshot(t *testing.T) {
	st := openTemp(t)
	snap, err := st.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snap.Config.Persona == "" {
		t.Fatal("seed persona missing")
	}
	if _, ok := snap.Prices.Tariffs["growth"]; !ok {
		t.Fatal("seed tariffs missing")
	}
	if len(snap.Topics) == 0 || len(snap.Assets) == 0 {
		t.Fatal("seed topics/assets missing")
	}
	// The seeded snapshot must validate (tokens resolve, urls non-empty).
	if _, err := domain.ValidateSnapshot(snap); err != nil {
		t.Fatalf("seed snapshot should validate: %v", err)
	}
}

func TestDraftPublishLifecycle(t *testing.T) {
	st := openTemp(t)

	if err := st.SaveDraftConfig(admin.ConfigInput{
		Persona: "new persona", Guardrails: "g", ReplyMaxWords: 100,
	}, "tester"); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	draft, err := st.DraftConfig()
	if err != nil || draft == nil {
		t.Fatalf("draft should exist: %v", err)
	}
	if err := st.PublishDraft("tester"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	pub, err := st.PublishedConfig()
	if err != nil || pub == nil {
		t.Fatalf("published should exist: %v", err)
	}
	if pub.Persona != "new persona" {
		t.Fatalf("published persona = %q", pub.Persona)
	}
	// After publishing, the draft is consumed (promoted), so none remains.
	if d, _ := st.DraftConfig(); d != nil {
		t.Fatal("draft should be promoted, not lingering")
	}
}

func TestRollbackRepublishesEarlierVersion(t *testing.T) {
	st := openTemp(t)
	// seed is version 1 (published). Create + publish v2.
	_ = st.SaveDraftConfig(admin.ConfigInput{Persona: "v2", Guardrails: "g"}, "t")
	_ = st.PublishDraft("t")
	// Rollback to version 1.
	if err := st.RollbackTo(1, "t"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	pub, _ := st.PublishedConfig()
	if pub == nil || pub.Persona == "v2" {
		t.Fatalf("rollback did not restore v1 persona, got %+v", pub)
	}
}

func TestMarkProcessedIdempotent(t *testing.T) {
	st := openTemp(t)
	ok, err := st.MarkProcessed("msg-1")
	if err != nil || !ok {
		t.Fatalf("first mark should succeed: ok=%v err=%v", ok, err)
	}
	ok, _ = st.MarkProcessed("msg-1")
	if ok {
		t.Fatal("second mark of same id should report already-processed")
	}
}
