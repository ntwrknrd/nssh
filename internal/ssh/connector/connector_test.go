package connector

import "testing"

func TestSetEnvCopiesEnvironment(t *testing.T) {
	conn := NewConnector("edge01", "netops", nil, nil)
	env := []string{"SSH_ASKPASS=/tmp/nssh-askpass"}

	conn.SetEnv(env)
	env[0] = "SSH_ASKPASS=/tmp/changed"

	if len(conn.env) != 1 || conn.env[0] != "SSH_ASKPASS=/tmp/nssh-askpass" {
		t.Fatalf("env = %#v, want copied askpass env", conn.env)
	}
}
