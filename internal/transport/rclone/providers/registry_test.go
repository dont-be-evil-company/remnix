package providers

import "testing"

func TestRegistryIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range All() {
		if d.ID == "" || d.DisplayName == "" {
			t.Fatalf("%+v", d)
		}
		if seen[d.ID] {
			t.Fatalf("dup %s", d.ID)
		}
		seen[d.ID] = true
	}
	for _, id := range []string{"s3", "gcs", "dropbox", "azure-files", "icloud-drive", "onedrive", "google-drive", "webdav", "smb", "custom"} {
		if _, ok := ByID(id); !ok {
			t.Fatalf("missing %s", id)
		}
	}
	if _, ok := ByID("gcs"); !ok {
		t.Fatal("gcs")
	}
	if d, _ := ByID("gcs"); d.RcloneType == "drive" {
		t.Fatal("gcs must not be google drive")
	}
	if ByIDMust("s3").Intro() == "" || !ByIDMust("custom").AllowSaveOnFailedTest() {
		t.Fatal("hints")
	}
}

func ByIDMust(id string) Definition {
	d, ok := ByID(id)
	if !ok {
		panic(id)
	}
	return d
}
