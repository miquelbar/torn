package ui

import (
	"syscall"
	"unsafe"
)

// makeRaw switches the terminal behind fd into raw mode (no echo, no line
// buffering) and returns a restore function. Darwin implementation using
// TIOCGETA/TIOCSETA ioctls.
func makeRaw(fd uintptr) (func(), error) {
	var termios syscall.Termios
	if err := ioctl(fd, syscall.TIOCGETA, unsafe.Pointer(&termios)); err != nil {
		return nil, err
	}

	saved := termios
	termios.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	termios.Cc[syscall.VMIN] = 1
	termios.Cc[syscall.VTIME] = 0

	if err := ioctl(fd, syscall.TIOCSETA, unsafe.Pointer(&termios)); err != nil {
		return nil, err
	}

	restore := func() {
		_ = ioctl(fd, syscall.TIOCSETA, unsafe.Pointer(&saved))
	}
	return restore, nil
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
