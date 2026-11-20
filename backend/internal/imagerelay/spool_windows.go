package imagerelay

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	spoolKernel32    = syscall.NewLazyDLL("kernel32.dll")
	spoolLockFileEx  = spoolKernel32.NewProc("LockFileEx")
	spoolGetDiskFree = spoolKernel32.NewProc("GetDiskFreeSpaceExW")
)

func lockSpool(root *os.Root) (*os.File, error) {
	file, err := spoolLockFile(root, 0)
	if err != nil {
		return nil, err
	}
	const lockExclusive = 2
	const lockFailImmediately = 1
	var position syscall.Overlapped
	ok, _, _ := spoolLockFileEx.Call(file.Fd(), lockExclusive|lockFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&position)))
	runtime.KeepAlive(file)
	if ok == 0 {
		file.Close()
		return nil, errors.New("image relay spool could not obtain exclusive directory lock")
	}
	return file, nil
}

func unlockSpool(_ *os.Root, file *os.File) error {
	return file.Close()
}

func spoolFreeBytes(root *os.Root) (uint64, error) {
	path, err := syscall.UTF16PtrFromString(root.Name())
	if err != nil {
		return 0, err
	}
	var free uint64
	ok, _, err := spoolGetDiskFree.Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&free)), 0, 0)
	if ok == 0 {
		return 0, err
	}
	return free, nil
}
