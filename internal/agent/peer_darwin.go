//go:build darwin

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
// On macOS, we use LOCAL_PEERCRED to get the peer's credentials.
func VerifyPeer(conn *net.UnixConn) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}

	var cred *unix.Xucred
	var credErr error

	err = rawConn.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
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
