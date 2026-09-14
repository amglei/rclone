//go:build linux && !android

package local

// statxAllowed reports whether the statx syscall may be issued on this platform.
func statxAllowed() bool {
	return true
}
