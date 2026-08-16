package fido2

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// PINPrompt asks for a FIDO PIN. The PIN must not be persisted.
type PINPrompt func(info Info) (string, error)

func PromptPIN(info Info) (string, error) {
	name := info.Product
	if name == "" {
		name = info.Path
	}
	if name == "" {
		name = "security key"
	}
	fmt.Fprintf(os.Stderr, "Enter FIDO PIN for %s (not stored): ", name)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("FIDO PIN is set; run this command in a terminal so the PIN can be entered (it is never stored)")
	}
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", fmt.Errorf("empty FIDO PIN")
	}
	return string(b), nil
}
