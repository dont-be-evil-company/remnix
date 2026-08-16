//go:build cgo && piv

package piv

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"

	pivlib "github.com/go-piv/piv-go/v2/piv"
)

type HardwareToken struct {
	serial string
	yk     *pivlib.YubiKey
	slot   pivlib.Slot
	pub    crypto.PublicKey
}

func (h *HardwareToken) Serial() string { return h.serial }
func (h *HardwareToken) Label() string  { return "yubikey-" + h.serial }
func (h *HardwareToken) Public() (crypto.PublicKey, error) {
	return h.pub, nil
}

func (h *HardwareToken) Decrypt(ciphertext []byte) ([]byte, error) {
	priv, err := h.yk.PrivateKey(h.slot, h.pub, pivlib.KeyAuth{PIN: pivlib.DefaultPIN})
	if err != nil {
		return nil, fmt.Errorf("yubikey private key: %w", err)
	}
	type oaepDecrypter interface {
		Decrypt(rand io.Reader, msg []byte, opts crypto.DecrypterOpts) ([]byte, error)
	}
	d, ok := priv.(oaepDecrypter)
	if !ok {
		dec, ok := priv.(crypto.Decrypter)
		if !ok {
			return nil, fmt.Errorf("yubikey key does not support decrypt")
		}
		return dec.Decrypt(rand.Reader, ciphertext, nil)
	}
	switch h.pub.(type) {
	case *rsa.PublicKey:
		return d.Decrypt(rand.Reader, ciphertext, &rsa.OAEPOptions{Hash: crypto.SHA256, Label: []byte("syncsh-piv")})
	default:
		return d.Decrypt(rand.Reader, ciphertext, nil)
	}
}

type HardwareFactory struct{}

func (HardwareFactory) List() ([]Token, error) {
	cards, err := pivlib.Cards()
	if err != nil {
		return nil, Annotate(fmt.Errorf("list yubikeys: %w", err))
	}
	var out []Token
	for _, card := range cards {
		yk, err := pivlib.Open(card)
		if err != nil {
			return nil, Annotate(err)
		}
		serial, err := yk.Serial()
		if err != nil {
			_ = yk.Close()
			return nil, Annotate(err)
		}
		slot := pivlib.SlotKeyManagement
		pub, err := slotPublic(yk, slot)
		if err != nil {
			_ = yk.Close()
			continue
		}
		out = append(out, &HardwareToken{
			serial: fmt.Sprintf("%d", serial),
			yk:     yk,
			slot:   slot,
			pub:    pub,
		})
	}
	return out, nil
}

func slotPublic(yk *pivlib.YubiKey, slot pivlib.Slot) (crypto.PublicKey, error) {
	if cert, err := yk.Attest(slot); err == nil {
		return cert.PublicKey, nil
	}
	cert, err := yk.Certificate(slot)
	if err != nil {
		return nil, err
	}
	return cert.PublicKey, nil
}

func (HardwareFactory) Generate(slotHint string) (Token, error) {
	cards, err := pivlib.Cards()
	if err != nil {
		return nil, Annotate(fmt.Errorf("list yubikeys: %w", err))
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("no yubikey present; plug it in and start pcscd (sudo systemctl start pcscd)")
	}
	yk, err := pivlib.Open(cards[0])
	if err != nil {
		return nil, Annotate(err)
	}
	slot := pivlib.SlotKeyManagement
	if _, err := yk.Attest(slot); err == nil {
		return nil, fmt.Errorf("PIV slot 9d already has a key; refusing to overwrite")
	}
	pub, err := yk.GenerateKey(pivlib.DefaultManagementKey, slot, pivlib.Key{
		Algorithm:   pivlib.AlgorithmRSA2048,
		PINPolicy:   pivlib.PINPolicyOnce,
		TouchPolicy: pivlib.TouchPolicyNever,
	})
	if err != nil {
		_ = yk.Close()
		return nil, fmt.Errorf("generate piv key: %w", err)
	}
	serial, err := yk.Serial()
	if err != nil {
		_ = yk.Close()
		return nil, err
	}
	return &HardwareToken{serial: fmt.Sprintf("%d", serial), yk: yk, slot: slot, pub: pub}, nil
}
