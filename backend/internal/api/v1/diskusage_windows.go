//go:build windows

package apiv1

import "errors"

type diskInfo struct {
	Total uint64
	Used  uint64
	Free  uint64
}

func diskUsage(_ string) (diskInfo, error) {
	return diskInfo{}, errors.New("disk usage not supported on windows")
}
