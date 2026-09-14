//go:build android && !cgo

package local

import (
	"os"
	"strconv"
	"strings"
)

// statxAllowed reports whether the statx syscall may be issued on this platform.
//
// Android's app seccomp policy traps non-allowlisted syscalls with SIGSYS and
// kills the process, and statx was only added to the allowlist in Android 11
// (API 30), so statx is only used there. Without cgo the API level is read from
// the system property files as a best effort and statx is left off when it
// can't be determined.
func statxAllowed() bool {
	for _, path := range []string{"/system/build.prop", "/default.prop"} {
		if api := androidSDKFromPropFile(path); api > 0 {
			return api >= 30
		}
	}
	return false
}

// androidSDKFromPropFile returns ro.build.version.sdk from an Android property
// file, or 0 if it can't be read.
func androidSDKFromPropFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	const key = "ro.build.version.sdk="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key) {
			continue
		}
		api, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, key)))
		if err != nil {
			return 0
		}
		return api
	}
	return 0
}
