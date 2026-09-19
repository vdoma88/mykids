//go:build !windows

package main

import "github.com/vdoma88/mykids/agents/windows/internal/winsvc"

func stopService() error { return winsvc.ErrNotWindows }
