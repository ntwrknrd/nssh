//go:build linux

package agent

import (
	"errors"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// VerifyPeer checks that the connecting process has the same UID as this process.
// This prevents other users from connecting to the agent socket.
//
// On Linux, we use SO_PEERCRED to get the peer's credentials.
func VerifyPeer(conn *net.UnixConn) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}

	var cred *unix.Ucred
	var credErr error

	err = rawConn.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}

	if cred.Uid != uint32(os.Getuid()) {
		return errors.New("peer UID mismatch: connection rejected")
	}

	return nil
}
