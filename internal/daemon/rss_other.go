//go:build !linux

package daemon

func processRSS() (uint64, error) {
	return 0, nil
}
