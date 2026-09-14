//go:build android && cgo

package local

/*
#include <stdlib.h>
#include <sys/system_properties.h>

// rclone_android_sdk returns the Android API level or 0 if it can't be read.
static int rclone_android_sdk(void) {
	char buf[PROP_VALUE_MAX];
	if (__system_property_get("ro.build.version.sdk", buf) <= 0) {
		return 0;
	}
	return atoi(buf);
}
*/
import "C"

// statxAllowed reports whether the statx syscall may be issued on this platform.
//
// Android's app seccomp policy traps non-allowlisted syscalls with SIGSYS and
// kills the process, and statx was only added to the allowlist in Android 11
// (API 30), so statx is only used there. Older releases use fstatat instead,
// which has no birth time.
func statxAllowed() bool {
	return C.rclone_android_sdk() >= 30
}
