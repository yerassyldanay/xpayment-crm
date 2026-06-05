package domain

import "testing"

func newPriceBook() *PriceBook {
	return &PriceBook{
		Tariffs: map[string]Tariff{
			"launch": {Key: "launch", PriceTenge: 9900, CashierLimit: 1},
			"growth": {Key: "growth", PriceTenge: 19900, CashierLimit: 5},
		},
		Placeholders: map[string]Placeholder{
			"support.phone": {Token: "support.phone", ValueRU: "+7 700", ValueKK: "+7 700"},
		},
	}
}

func TestRender_PricesAndLimits(t *testing.T) {
	pb := newPriceBook()
	got, err := pb.Render("Growth — до {{limit.growth}} касс, {{price.growth}}/мес.", "ru")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Growth — до 5 касс, 19 900 ₸/мес."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRender_UnknownTokenErrors(t *testing.T) {
	pb := newPriceBook()
	if _, err := pb.Render("{{price.enterprise}}", "ru"); err == nil {
		t.Fatal("expected error for unknown token")
	}
}

func TestRender_LeftoverBraceErrors(t *testing.T) {
	pb := newPriceBook()
	// A malformed/partial token leaves '{{' behind and must error, never ship.
	if _, err := pb.Render("цена {{ broken", "ru"); err == nil {
		t.Fatal("expected error for leftover brace")
	}
}

func TestRender_Placeholder(t *testing.T) {
	pb := newPriceBook()
	got, err := pb.Render("Звоните {{support.phone}}", "ru")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Звоните +7 700" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatTenge(t *testing.T) {
	cases := map[int64]string{0: "0 ₸", 990: "990 ₸", 9900: "9 900 ₸", 19900: "19 900 ₸", 1000000: "1 000 000 ₸"}
	for in, want := range cases {
		if got := formatTenge(in); got != want {
			t.Errorf("formatTenge(%d) = %q want %q", in, got, want)
		}
	}
}

func TestResolveAssets_DropsUnknownAndCaps(t *testing.T) {
	s := &Snapshot{Assets: []Asset{
		{Ref: "a", Kind: "image", URL: "/a"},
		{Ref: "b", Kind: "image", URL: "/b"},
		{Ref: "c", Kind: "image", URL: "/c"},
		{Ref: "d", Kind: "image", URL: "/d"},
	}}
	resolved, unknown := s.ResolveAssets([]string{"a", "x", "b", "c", "d"})
	if len(resolved) != 3 {
		t.Fatalf("expected cap at 3, got %d", len(resolved))
	}
	if len(unknown) != 1 || unknown[0] != "x" {
		t.Fatalf("expected unknown [x], got %v", unknown)
	}
}
