//go:build windows

package postprocessor

import "errors"

// freeBytes : non implémenté sous Windows, le contrôle d'espace est alors ignoré.
func freeBytes(string) (uint64, error) { return 0, errors.New("not supported") }
