//go:build windows

package postprocessor

import "errors"

// freeBytes: not implemented on Windows, the space check is skipped there.
func freeBytes(string) (uint64, error) { return 0, errors.New("not supported") }
