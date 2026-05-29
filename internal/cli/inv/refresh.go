package inv

import (
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func newConfigOnlyRunner(parser *sshconfig.Parser) inventory.RemoteRunner {
	return remoteexec.NewSSHRunner(func(host string) (*remoteexec.HostInfo, error) {
		entry, err := parser.FindHost(host)
		if err != nil || entry == nil {
			return &remoteexec.HostInfo{Target: host, Hostname: host}, err
		}
		return &remoteexec.HostInfo{Target: host, Hostname: entry.HostName, Username: entry.User()}, nil
	})
}
