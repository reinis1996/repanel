//go:build linux

package system

import "syscall"

// oNoFollow makes os.OpenFile refuse to traverse a symlink in the final path
// component. The panel writes files as root, so without it a tenant could win
// the race between a jail check and the write by planting a symlink at the
// target name (e.g. via FTP) pointing outside their web space.
const oNoFollow = syscall.O_NOFOLLOW
