//go:build !darwin && !linux

package ui

// makeRaw is unsupported on this platform; the picker falls back to
// line-based selection when raw mode cannot be enabled.
func makeRaw(fd uintptr) (func(), error) {
	return nil, errRawUnsupported
}
