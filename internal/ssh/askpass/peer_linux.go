//go:build linux

package askpass

import (
	"errors"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func verifyPeer(conn *net.UnixConn) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	if err := rawConn.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if cred.Uid != uint32(os.Getuid()) {
		return errors.New("askpass peer UID mismatch")
	}
	return nil
}
