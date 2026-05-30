package app

import (
	"os"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
)

func runAgentDaemon() {
	readyPipe := os.NewFile(3, "ready-pipe")

	signalError := func(msg string) {
		if readyPipe != nil {
			_, _ = readyPipe.WriteString("err:" + msg + "\n")
			_ = readyPipe.Close()
		}
		os.Exit(1)
	}

	if readyPipe == nil {
		signalError("missing pipe file descriptors")
	}

	if os.Getenv("NSSH_AGENT_RUNTIME") == "1" {
		cfg, err := config.LoadDefault()
		if err != nil {
			signalError(err.Error())
		}
		agentCfg := agent.DefaultRuntimeConfig()
		agentCfg.Agent = &cfg.Agent
		agentCfg.Archive = &cfg.Logging.Session.Archive
		agentCfg.ReadyPipe = readyPipe
		if err := agent.Run(agent.NewConfiguredRuntimeProvider(cfg), agentCfg); err != nil {
			if readyPipe != nil {
				_, _ = readyPipe.WriteString("err:" + err.Error() + "\n")
				_ = readyPipe.Close()
			}
			os.Exit(1)
		}
		return
	}

	signalError("agent runtime mode is required")
}
