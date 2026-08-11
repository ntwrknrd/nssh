package config

import (
	"reflect"
	"testing"
)

func TestMergeSSHAppliesOverrideRules(t *testing.T) {
	base := SSHHostConfig{
		Compatibility: SSHCompatibility{HostKey: "ssh-rsa"},
		Options: SSHOptions{
			"IdentityFile": configTestOptionItems("~/.ssh/default"),
			"LogLevel":     NewSSHOptionString("ERROR"),
			"SetEnv":       NewSSHOptionMap(map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}),
			"TCPKeepAlive": NewSSHOptionBool(true),
		},
	}
	override := SSHHostConfig{
		Compatibility: SSHCompatibility{Kex: "diffie-hellman-group14-sha1", PublicKey: "ssh-rsa"},
		Options: SSHOptions{
			"IdentityFile": configTestOptionItems("~/.ssh/host"),
			"LogLevel":     NewSSHOptionString("DEBUG"),
			"SetEnv":       NewSSHOptionMap(map[string]string{"TERM": "screen-256color"}),
		},
	}

	got := MergeSSH(base, override)

	if got.Compatibility.HostKey != "ssh-rsa" || got.Compatibility.Kex != "diffie-hellman-group14-sha1" || got.Compatibility.PublicKey != "ssh-rsa" {
		t.Fatalf("Compatibility = %#v", got.Compatibility)
	}
	want := SSHOptions{
		"IdentityFile": configTestOptionItems("~/.ssh/default", "~/.ssh/host"),
		"LogLevel":     NewSSHOptionString("DEBUG"),
		"SetEnv":       NewSSHOptionMap(map[string]string{"TERM": "screen-256color", "COLORTERM": "truecolor"}),
		"TCPKeepAlive": NewSSHOptionBool(true),
	}
	if !reflect.DeepEqual(got.Options, want) {
		t.Fatalf("Options = %#v", got.Options)
	}
}

func configTestOptionItems(values ...string) SSHOptionValue {
	return NewSSHOptionItems(values...)
}
