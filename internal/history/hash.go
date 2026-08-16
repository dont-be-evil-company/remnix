package history

import "crypto/sha256"

func HashCommand(command string) []byte {
	sum := sha256.Sum256([]byte(command))
	return sum[:]
}
