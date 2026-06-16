//go:build unix

package connector

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

func RenderSSHOptions(opts config.SSHHostConfig, sshVerbose int) []string {
	args := []string{"-F", "none"}
	if sshVerbose > 3 {
		sshVerbose = 3
	}
	if sshVerbose > 0 {
		args = append(args, "-"+strings.Repeat("v", sshVerbose))
	}
	appendO := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, "-o", key+"="+value)
		}
	}
	if opts.IdentitiesOnly != nil {
		appendO("IdentitiesOnly", yesNo(*opts.IdentitiesOnly))
	}
	appendO("IdentityAgent", opts.IdentityAgent.Path)
	for _, file := range opts.IdentityFiles {
		if strings.TrimSpace(file) != "" {
			args = append(args, "-i", file)
		}
	}
	for _, file := range opts.CertificateFiles {
		appendO("CertificateFile", file)
	}
	appendO("ProxyJump", opts.ProxyJump)
	appendO("ProxyCommand", opts.ProxyCommand)
	if opts.ForwardAgent != nil {
		appendO("ForwardAgent", yesNo(*opts.ForwardAgent))
	}
	for _, forward := range opts.LocalForwards {
		appendO("LocalForward", renderForward(forward))
	}
	for _, forward := range opts.RemoteForwards {
		appendO("RemoteForward", renderForward(forward))
	}
	if len(opts.SetEnv) > 0 {
		keys := make([]string, 0, len(opts.SetEnv))
		for key := range opts.SetEnv {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		values := make([]string, 0, len(keys))
		for _, key := range keys {
			values = append(values, key+"="+opts.SetEnv[key])
		}
		appendO("SetEnv", strings.Join(values, " "))
	}
	appendO("RemoteCommand", opts.RemoteCommand)
	appendDuration := func(key string, d config.Duration) {
		if d.Duration() > 0 {
			appendO(key, fmt.Sprintf("%d", int(d.Duration().Seconds())))
		}
	}
	appendDuration("ServerAliveInterval", opts.ServerAliveInterval)
	if opts.ServerAliveCountMax > 0 {
		appendO("ServerAliveCountMax", fmt.Sprintf("%d", opts.ServerAliveCountMax))
	}
	appendDuration("ConnectTimeout", opts.ConnectionTimeout)
	appendO("ControlMaster", opts.ControlMaster)
	appendDuration("ControlPersist", opts.ControlPersist)
	appendO("ControlPath", opts.ControlPath)
	appendList := func(key string, values []string) {
		if len(values) > 0 {
			appendO(key, strings.Join(values, ","))
		}
	}
	appendList("Ciphers", opts.Ciphers)
	appendList("MACs", opts.MACs)
	appendList("KexAlgorithms", opts.KexAlgorithms)
	appendList("HostKeyAlgorithms", opts.HostKeyAlgorithms)
	appendList("PubkeyAcceptedAlgorithms", opts.PubkeyAcceptedAlgorithms)
	if len(opts.Options) > 0 {
		keys := make([]string, 0, len(opts.Options))
		for key := range opts.Options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			appendO(key, opts.Options[key])
		}
	}
	return args
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func renderForward(f config.Forward) string {
	bind := strings.TrimSpace(f.Bind)
	target := strings.TrimSpace(f.Target)
	if bind == "" {
		return target
	}
	if target == "" {
		return bind
	}
	return bind + " " + target
}
