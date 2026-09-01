//go:build windows

package win32

import "runtime"

func runtimeLockOSThread()   { runtime.LockOSThread() }
func runtimeUnlockOSThread() { runtime.UnlockOSThread() }
