package wizard

import "testing"

func TestExtractURL(t *testing.T) {
	if got := extractURL("open https://accounts.google.com/o/oauth2 then press enter"); got != "https://accounts.google.com/o/oauth2" {
		t.Fatalf("got %q", got)
	}
	if extractURL("no url here") != "" {
		t.Fatal("expected empty")
	}
}
