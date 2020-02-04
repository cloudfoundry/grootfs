package mount

import (
	"os"

	"golang.org/x/sys/unix"
)

type RootfulUnmounter struct {
}

func (u RootfulUnmounter) Unmount(path string) error {
	err := unix.Unmount(path, unix.MNT_DETACH)
	// do not error if unmountPath does not exist or is not a mountpoint
	if os.IsNotExist(err) || err == unix.EINVAL {
		return nil
	}
	return err
}
