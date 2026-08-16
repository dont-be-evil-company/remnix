package generations

import (
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/envelope"
	"github.com/dont-be-evil-company/remnix/internal/crypto/slots"
)

func TestManifestSignVerify(t *testing.T) {
	smk, _ := envelope.GenerateSMK()
	m := Manifest{
		Version:      1,
		GenerationID: "g1",
		Seq:          1,
		Counter:      1,
		Active:       true,
		Slots:        []slots.Slot{{ID: "s1", Type: slots.TypeRecovery, Status: slots.StatusActive}},
	}
	if err := Sign(&m, smk); err != nil {
		t.Fatal(err)
	}
	if err := Verify(m, smk); err != nil {
		t.Fatal(err)
	}
	m.Counter = 2
	if err := Verify(m, smk); err == nil {
		t.Fatal("expected mismatch")
	}
}
