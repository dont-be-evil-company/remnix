package fido2

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"

	"github.com/telesma-app/ctap/authenticator"
	"github.com/telesma-app/ctap/backend/hid"
	"github.com/telesma-app/ctap/cose"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/extension"
	"github.com/telesma-app/ctap/protocol"
	"github.com/telesma-app/ctap/webauthn"
	ghid "github.com/telesma-app/hid"
)

type hidDevice struct {
	dev    *authenticator.Device
	info   Info
	prompt PINPrompt
	pin    string
}

func List() ([]Info, error) {
	var out []Info
	for di, err := range hid.Devices(context.Background()) {
		if err != nil {
			return nil, Annotate(err)
		}
		d, err := openHID(di, nil)
		if err != nil {
			out = append(out, Info{
				Path:    hidPath(di),
				Product: hidProduct(di),
			})
			continue
		}
		out = append(out, d.Info())
		_ = d.Close()
	}
	return out, nil
}

func OpenHMACDevices(prompt PINPrompt) ([]Device, error) {
	var out []Device
	var last error
	for di, err := range hid.Devices(context.Background()) {
		if err != nil {
			last = err
			break
		}
		d, err := openHID(di, prompt)
		if err != nil {
			last = err
			continue
		}
		if !d.Info().HMACSecret {
			_ = d.Close()
			continue
		}
		out = append(out, d)
	}
	if len(out) == 0 && last != nil {
		return nil, Annotate(last)
	}
	return out, nil
}

func CloseAll(devs []Device) {
	for _, d := range devs {
		if d != nil {
			_ = d.Close()
		}
	}
}

func openHID(di *ghid.DeviceInfo, prompt PINPrompt) (*hidDevice, error) {
	path := hidPath(di)
	ctx := context.Background()
	transport, err := hid.Open(ctx, path)
	if err != nil {
		return nil, Annotate(fmt.Errorf("open fido2 hid %s: %w", path, err))
	}
	dev, err := authenticator.New(ctx, transport)
	if err != nil {
		_ = transport.Close()
		return nil, Annotate(fmt.Errorf("open fido2 hid %s: %w", path, err))
	}
	gi, ok := dev.GetInfoCached()
	if !ok {
		gi, err = dev.GetInfo(ctx)
		if err != nil {
			_ = dev.Close()
			return nil, Annotate(fmt.Errorf("open fido2 hid %s: %w", path, err))
		}
	}
	h := &hidDevice{
		dev:    dev,
		prompt: prompt,
		info: Info{
			Path:       path,
			Product:    hidProduct(di),
			AAGUID:     gi.AAGUID.String(),
			HMACSecret: slices.Contains(gi.Extensions, extension.ExtensionIdentifierHMACSecret),
			PINSet:     gi.Options[protocol.OptionClientPIN],
		},
	}
	if h.info.Product == "" {
		h.info.Product = "FIDO2 security key"
	}
	return h, nil
}

func hidPath(di *ghid.DeviceInfo) string {
	if di == nil {
		return ""
	}
	return di.Path
}

func hidProduct(di *ghid.DeviceInfo) string {
	if di == nil {
		return ""
	}
	if di.ProductStr != "" {
		return di.ProductStr
	}
	return fmt.Sprintf("%04x:%04x", di.VendorID, di.ProductID)
}

func (h *hidDevice) Info() Info { return h.info }

func (h *hidDevice) Close() error {
	if h.dev == nil {
		return nil
	}
	return h.dev.Close()
}

func (h *hidDevice) Enroll() (Enrollment, error) {
	if !h.info.HMACSecret {
		return Enrollment{}, fmt.Errorf("authenticator %s does not support hmac-secret", h.info.Product)
	}
	credID, aaguid, err := h.makeCredential()
	if err != nil {
		return Enrollment{}, err
	}
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return Enrollment{}, err
	}
	secret, err := h.Derive(credID, salt)
	if err != nil {
		return Enrollment{}, err
	}
	return Enrollment{
		CredentialID: credID,
		Salt:         salt,
		Secret:       secret,
		AAGUID:       aaguid,
		Product:      h.info.Product,
		RPID:         RPID,
	}, nil
}

func (h *hidDevice) makeCredential() (credID []byte, aaguid string, err error) {
	userID := make([]byte, 16)
	if _, err := rand.Read(userID); err != nil {
		return nil, "", err
	}
	token, err := h.authToken(protocol.PermissionMakeCredential)
	if err != nil {
		return nil, "", err
	}
	resp, err := h.dev.MakeCredential(
		context.Background(),
		token,
		[]byte(`{"type":"webauthn.create","origin":"remnix"}`),
		credential.PublicKeyCredentialRpEntity{ID: RPID, Name: "remnix"},
		credential.PublicKeyCredentialUserEntity{
			ID:          userID,
			Name:        "remnix",
			DisplayName: "remnix",
		},
		[]credential.PublicKeyCredentialParameters{{
			Type:      credential.PublicKeyCredentialTypePublicKey,
			Algorithm: cose.AlgorithmES256,
		}},
		nil,
		&webauthn.CreateAuthenticationExtensionsClientInputs{
			CreateHMACSecretInputs: &webauthn.CreateHMACSecretInputs{HMACCreateSecret: true},
		},
		map[protocol.Option]bool{protocol.OptionUserPresence: true},
		0,
		nil,
	)
	if err != nil {
		return nil, "", fmt.Errorf("fido2 makeCredential: %w", err)
	}
	if resp.AuthData == nil || resp.AuthData.AttestedCredentialData == nil {
		return nil, "", fmt.Errorf("fido2 makeCredential: missing credential data")
	}
	if resp.ExtensionOutputs != nil && resp.ExtensionOutputs.CreateHMACSecretOutputs != nil &&
		!resp.ExtensionOutputs.HMACCreateSecret {
		return nil, "", fmt.Errorf("fido2 makeCredential: hmac-secret was not enabled")
	}
	id := resp.AuthData.AttestedCredentialData.CredentialID
	if len(id) == 0 {
		return nil, "", fmt.Errorf("fido2 makeCredential: empty credential id")
	}
	return id, resp.AuthData.AttestedCredentialData.AAGUID.String(), nil
}

func (h *hidDevice) Derive(credID, salt []byte) ([]byte, error) {
	if len(salt) != SaltSize {
		return nil, fmt.Errorf("fido2 hmac salt must be %d bytes", SaltSize)
	}
	token, err := h.authToken(protocol.PermissionGetAssertion)
	if err != nil {
		return nil, err
	}
	allow := []credential.PublicKeyCredentialDescriptor{{
		Type:       credential.PublicKeyCredentialTypePublicKey,
		ID:         credID,
		Transports: []credential.AuthenticatorTransport{credential.AuthenticatorTransportUSB},
	}}
	var secret []byte
	var last error
	for assertion, err := range h.dev.GetAssertion(
		context.Background(),
		token,
		RPID,
		[]byte(`{"type":"webauthn.get","origin":"remnix"}`),
		allow,
		&webauthn.GetAuthenticationExtensionsClientInputs{
			GetHMACSecretInputs: &webauthn.GetHMACSecretInputs{
				HMACGetSecret: webauthn.HMACGetSecretInput{Salt1: salt},
			},
		},
		map[protocol.Option]bool{protocol.OptionUserPresence: true},
	) {
		if err != nil {
			last = err
			break
		}
		if assertion.ExtensionOutputs != nil && assertion.ExtensionOutputs.GetHMACSecretOutputs != nil {
			out := assertion.ExtensionOutputs.HMACGetSecret.Output1
			if len(out) == SecretSize {
				secret = out
				break
			}
			last = fmt.Errorf("fido2 hmac-secret: unexpected output length %d", len(out))
		}
	}
	if secret != nil {
		return secret, nil
	}
	if last != nil {
		return nil, fmt.Errorf("fido2 getAssertion: %w", last)
	}
	return nil, fmt.Errorf("fido2 getAssertion: no hmac-secret output")
}

func (h *hidDevice) authToken(perm protocol.Permission) ([]byte, error) {
	gi, err := h.dev.GetInfo(context.Background())
	if err != nil {
		return nil, fmt.Errorf("fido2 getInfo: %w", err)
	}
	if !gi.Options[protocol.OptionClientPIN] {
		return nil, nil
	}
	return h.pinToken(perm)
}

func (h *hidDevice) pinToken(perm protocol.Permission) ([]byte, error) {
	if h.pin == "" {
		prompt := h.prompt
		if prompt == nil {
			prompt = PromptPIN
		}
		pin, err := prompt(h.info)
		if err != nil {
			return nil, err
		}
		h.pin = pin
	}
	tok, err := h.dev.GetPinUvAuthTokenUsingPIN(context.Background(), h.pin, perm, RPID)
	if err != nil {
		h.pin = ""
		return nil, fmt.Errorf("fido2 pin: %w", err)
	}
	return tok, nil
}
