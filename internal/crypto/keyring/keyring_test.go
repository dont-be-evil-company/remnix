package keyring

import (
	"bytes"
	"os"
	"testing"
)

func TestMemoryRoundTrip(t *testing.T) {
	mem := NewMemory()
	Use(mem)
	t.Cleanup(func() { Use(nil) })
	smks := map[string][]byte{"g1": {1, 2, 3, 4}}
	if err := Set("dev1", smks); err != nil {
		t.Fatal(err)
	}
	got, err := Get("dev1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got["g1"], smks["g1"]) {
		t.Fatalf("got %v", got)
	}
	missing, err := Get("nope")
	if err != nil || missing != nil {
		t.Fatalf("missing: %v %v", missing, err)
	}
	if err := Delete("dev1"); err != nil {
		t.Fatal(err)
	}
	got, err = Get("dev1")
	if err != nil || len(got) != 0 {
		t.Fatalf("after delete: %v %v", got, err)
	}
}

func TestLiveKeyring(t *testing.T) {
	if os.Getenv("REMNIX_KEYRING_LIVE") != "1" {
		t.Skip("set REMNIX_KEYRING_LIVE=1 to talk to the OS keyring")
	}
	Use(nil)
	id := "live-test-device"
	defer func() { _ = Delete(id) }()
	smks := map[string][]byte{"g1": bytes.Repeat([]byte{7}, 32)}
	if err := Set(id, smks); err != nil {
		t.Fatal(err)
	}
	got, err := Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got["g1"], smks["g1"]) {
		t.Fatal("live mismatch")
	}
}
