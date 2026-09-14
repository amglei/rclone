# Local fork package recipe for termux-packages — copy this file to
# packages/rclone/build.sh in a termux-packages clone to build the fork as a
# Termux package. See ANDROID_WINDOWS_TIMES.md ("Termux-Paket mit Docker
# bauen") for the full build procedure.

TERMUX_PKG_HOMEPAGE=https://rclone.org/
TERMUX_PKG_DESCRIPTION="rsync for cloud storage"
TERMUX_PKG_LICENSE="MIT"
TERMUX_PKG_MAINTAINER="@termux"
TERMUX_PKG_VERSION="1.75.1"
TERMUX_PKG_SRCURL=file:///home/builder/termux-packages/sources/rclone
TERMUX_PKG_SHA256=SKIP_CHECKSUM

termux_step_make_install() {
	cd $TERMUX_PKG_SRCDIR

	termux_setup_golang

	mkdir -p .gopath/src/github.com/rclone
	ln -sf "$PWD" .gopath/src/github.com/rclone/rclone
	export GOPATH="$PWD/.gopath"

	go build -v -ldflags "-X github.com/rclone/rclone/fs.Version=v${TERMUX_PKG_VERSION}-termux-local.1" -tags noselfupdate -o rclone

	# XXX: Fix read-only files which prevents removal of src dir.
	chmod u+w -R .

	cp rclone $TERMUX_PREFIX/bin/rclone
	mkdir -p $TERMUX_PREFIX/share/man/man1/
	cp rclone.1 $TERMUX_PREFIX/share/man/man1/
}
