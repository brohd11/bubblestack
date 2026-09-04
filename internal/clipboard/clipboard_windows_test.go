//go:build windows

package clipboard

import (
	"syscall"
	"testing"
)

func TestGlobalUnlockZeroWithoutLastErrorIsSuccess(t *testing.T) {
	if err := globalUnlockError(0, syscall.Errno(0)); err != nil {
		t.Fatalf("normal GlobalUnlock result reported an error: %v", err)
	}
	if err := globalUnlockError(0, syscall.Errno(5)); err == nil {
		t.Fatal("GlobalUnlock failure should retain a nonzero Win32 error")
	}
	if err := globalUnlockError(1, syscall.Errno(5)); err != nil {
		t.Fatalf("nonzero GlobalUnlock result reported an error: %v", err)
	}
}
