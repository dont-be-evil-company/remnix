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

func TestSanitizeBucketAndJoin(t *testing.T) {
	if got := sanitizeBucket(" s3://dont-be-evil-company/ "); got != "dont-be-evil-company" {
		t.Fatalf("sanitize: %q", got)
	}
	if got := joinRemotePath("dont-be-evil-company", "remnix"); got != "dont-be-evil-company/remnix" {
		t.Fatalf("join: %q", got)
	}
	if got := joinRemotePath("bucket/remnix", ""); got != "bucket/remnix" {
		t.Fatalf("join empty pick: %q", got)
	}
	if got := joinRemotePath("bucket", "remnix"); got != "bucket/remnix" {
		t.Fatalf("join pick: %q", got)
	}
	if got := joinRemotePath("bucket", ""); got != "bucket" {
		t.Fatalf("join empty prefix: %q", got)
	}
}
