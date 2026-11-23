package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/rex"
	"golang.org/x/term"
)

var (
	paneHoldInput  io.Reader = os.Stdin
	paneHoldOutput io.Writer = os.Stderr
	paneHoldActive           = func() bool { return rex.InPane() && term.IsTerminal(int(os.Stdin.Fd())) }
)

// holdFailedPane keeps a pane nssh launched in Rex open after nssh fails, so
// the operator can read the error before Rex closes the pane with the
// process. It waits for Enter. A remote session's own exit status is not a
// failure, so it closes the pane as usual.
func holdFailedPane(err error) {
	if err == nil || !paneHoldActive() {
		return
	}
	var exitErr *exit.ExitError
	if errors.As(err, &exitErr) && exitErr.Remote {
		return
	}
	_, _ = fmt.Fprint(paneHoldOutput, "nssh: press Enter to close this pane")
	_, _ = bufio.NewReader(paneHoldInput).ReadString('\n')
}
