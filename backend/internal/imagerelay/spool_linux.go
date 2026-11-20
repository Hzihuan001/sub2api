package imagerelay

import (
	"errors"
	"math"
	"os"
	"syscall"
)

func lockSpool(root *os.Root) (*os.File, error) {
	file, err := spoolLockFile(root, 0)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("image relay spool could not obtain exclusive directory lock")
	}
	return file, nil
}

func unlockSpool(_ *os.Root, file *os.File) error {
	return file.Close()
}

func spoolFreeBytes(root *os.Root) (uint64, error) {
	dir, err := root.Open(".")
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	var stat syscall.Statfs_t
	err = syscall.Fstatfs(int(dir.Fd()), &stat)
	if err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 {
		return 0, errors.New("invalid filesystem block size")
	}
	size := uint64(stat.Bsize)
	blocks := uint64(stat.Bavail)
	if blocks > math.MaxUint64/size {
		return math.MaxUint64, nil
	}
	return blocks * size, nil
}
