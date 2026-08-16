package fido2

import (
	"os"
	"testing"
)

func TestLiveHIDHmacSecret(t *testing.T) {
	if os.Getenv("SYNCSH_FIDO2_LIVE") != "1" {
		t.Skip("set SYNCSH_FIDO2_LIVE=1 to run against a plugged-in Security Key")
	}
	devs, err := OpenHMACDevices(PromptPIN)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseAll(devs)
	if len(devs) == 0 {
		t.Fatal("no FIDO2 hmac-secret authenticators over USB HID")
	}
	dev := devs[0]
	if !dev.Info().HMACSecret {
		t.Fatal("expected hmac-secret")
	}
	enr, err := dev.Enroll()
	if err != nil {
		t.Fatal(err)
	}
	got, err := dev.Derive(enr.CredentialID, enr.Salt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(got, enr.Secret) {
		t.Fatal("live derive mismatch")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
