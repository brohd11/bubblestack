//go:build windows

// Package clipboard wraps the repository's clipboard backend and corrects its Windows
// read path. The upstream v0.1.4 implementation treats GlobalUnlock's normal zero return
// as failure even when GetLastError is NO_ERROR.
package clipboard

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	upstream "github.com/atotto/clipboard"
)

const cfUnicodeText = 13

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	isClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	openClipboard              = user32.NewProc("OpenClipboard")
	closeClipboard             = user32.NewProc("CloseClipboard")
	getClipboardData           = user32.NewProc("GetClipboardData")

	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
)

// ReadAll reads CF_UNICODETEXT from the Windows clipboard. GlobalUnlock returning zero
// means the object is now fully unlocked; it is an error only when GetLastError is nonzero.
func ReadAll() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	available, _, _ := isClipboardFormatAvailable.Call(cfUnicodeText)
	if available == 0 {
		return "", nil
	}
	if err := waitOpenClipboard(); err != nil {
		return "", err
	}
	defer func() {
		_, _, _ = closeClipboard.Call() // preserve the read result
	}()

	handle, _, callErr := getClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return "", win32Error("get clipboard data", callErr)
	}
	locked, _, callErr := globalLock.Call(handle)
	if locked == 0 {
		return "", win32Error("lock clipboard data", callErr)
	}
	text := syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(locked))[:])
	unlocked, _, callErr := globalUnlock.Call(handle)
	if err := globalUnlockError(unlocked, callErr); err != nil {
		return "", err
	}
	return text, nil
}

// The upstream write path already distinguishes GlobalUnlock's zero-success case.
func WriteAll(text string) error { return upstream.WriteAll(text) }

func waitOpenClipboard() error {
	deadline := time.Now().Add(time.Second)
	var last error
	for time.Now().Before(deadline) {
		opened, _, callErr := openClipboard.Call(0)
		if opened != 0 {
			return nil
		}
		last = win32Error("open clipboard", callErr)
		time.Sleep(time.Millisecond)
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("open clipboard: timed out")
}

func win32Error(action string, err error) error {
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return fmt.Errorf("%s failed", action)
	}
	if err == nil {
		return fmt.Errorf("%s failed", action)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func globalUnlockError(result uintptr, err error) error {
	if result != 0 {
		return nil
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return nil
	}
	return win32Error("unlock clipboard data", err)
}
