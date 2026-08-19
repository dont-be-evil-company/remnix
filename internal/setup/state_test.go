package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupStateRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	st := &State{Phase: PhaseKeys, DeviceID: "d1", DeviceName: "n", GenerationID: "g1"}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState()
	if err != nil || got == nil || got.GenerationID != "g1" || got.Phase != PhaseKeys {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if err := ClearState(); err != nil {
		t.Fatal(err)
	}
	got, err = LoadState()
	if err != nil || got != nil {
		t.Fatalf("cleared %+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "setup-state.json")); !os.IsNotExist(err) {
		t.Fatalf("state file still present: %v", err)
	}
}
