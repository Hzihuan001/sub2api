//go:build !linux && !windows

package imagerelay

import (
	"errors"
	"os"
)

func lockSpool(root *os.Root) (*os.File, error) {
	file, err := spoolLockFile(root, os.O_EXCL)
	if errors.Is(err, os.ErrExist) {
		return nil, errors.New("image relay spool owner exists; verify it has exited before removing the ownership marker")
	}
	return file, err
}

func unlockSpool(root *os.Root, file *os.File) error {
	return errors.Join(file.Close(), root.Remove(spoolLockName))
}

func spoolFreeBytes(_ *os.Root) (uint64, error) {
	return 0, errors.New("image relay spool disk availability is unsupported on this platform")
}
