package config

import "testing"

func TestValidateColor(t *testing.T) {
	if err := validateColor("ui.colors.accent", "#F5C2E7"); err != nil {
		t.Fatal(err)
	}
	if err := validateColor("ui.colors.accent", "#abc"); err != nil {
		t.Fatal(err)
	}
	if err := validateColor("ui.colors.accent", ""); err != nil {
		t.Fatal(err)
	}
	if err := validateColor("ui.colors.accent", "default"); err != nil {
		t.Fatal(err)
	}
	if err := validateColor("ui.colors.accent", "not-a-color"); err == nil {
		t.Fatal("expected error")
	}
	if err := validateColor("ui.colors.accent", "#GGHHII"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIconAliases(t *testing.T) {
	cfg := Default()
	if cfg.IconTyped() != "›" || cfg.IconHistory() != "*" || cfg.IconCompletion() != "+" {
		t.Fatalf("defaults: %+v %+v", cfg.Suggest.Icons, cfg.UI.Icons)
	}
	cfg.UI.Icons.SuggestionTyped = ">"
	if cfg.IconTyped() != ">" {
		t.Fatal(cfg.IconTyped())
	}
}
