//go:build !cgo || !piv

package piv

import "fmt"

type HardwareFactory struct{}

func (HardwareFactory) List() ([]Token, error) {
	return nil, Annotate(fmt.Errorf("yubikey support is not available in this build"))
}

func (HardwareFactory) Generate(slotHint string) (Token, error) {
	return nil, Annotate(fmt.Errorf("yubikey support is not available in this build"))
}
