package slots

const (
	TypeRecovery  = "recovery"
	TypeYubiKey   = "yubikey-piv"
	TypeFIDO2Hmac = "fido2-hmac"
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

type Slot struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	GenerationID string `json:"generation_id"`
	WrapParams   []byte `json:"wrap_params,omitempty"`
	WrappedSMK   []byte `json:"wrapped_smk"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	Label        string `json:"label,omitempty"`
}

func Active(slots []Slot) []Slot {
	var out []Slot
	for _, s := range slots {
		if s.Status == StatusActive {
			out = append(out, s)
		}
	}
	return out
}

func OfType(slots []Slot, typ string) []Slot {
	var out []Slot
	for _, s := range slots {
		if s.Type == typ && s.Status == StatusActive {
			out = append(out, s)
		}
	}
	return out
}
