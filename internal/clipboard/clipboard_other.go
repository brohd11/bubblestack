//go:build !windows

package clipboard

import upstream "github.com/atotto/clipboard"

func ReadAll() (string, error) { return upstream.ReadAll() }

func WriteAll(text string) error { return upstream.WriteAll(text) }
