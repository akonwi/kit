//go:build darwin

package workspace

import (
	"io/fs"
	"syscall"
)

func hostFileIdentity(info fs.FileInfo) (uint64, uint64, int64) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Dev), stat.Ino, stat.Ctimespec.Sec*1_000_000_000 + stat.Ctimespec.Nsec
	}
	return 0, 0, 0
}
