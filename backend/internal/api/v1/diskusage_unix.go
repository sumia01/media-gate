//go:build !windows

package apiv1

import "syscall"

type diskInfo struct {
	Total uint64
	Used  uint64
	Free  uint64
}

func diskUsage(path string) (diskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskInfo{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	return diskInfo{
		Total: total,
		Free:  free,
		Used:  total - free,
	}, nil
}
