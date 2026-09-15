//go:build linux

package workspace

import (
	"io/fs"
	"syscall"
)

func hostFileIdentity(info fs.FileInfo) (uint64, uint64, int64) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Dev), stat.Ino, stat.Ctim.Sec*1_000_000_000 + stat.Ctim.Nsec
	}
	return 0, 0, 0
}
