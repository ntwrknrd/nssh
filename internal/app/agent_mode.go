package app

import (
	"os"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
)

func runAgentDaemon() {
	dataPipe := os.NewFile(3, "data-pipe")
	readyPipe := os.NewFile(4, "ready-pipe")

	signalError := func(msg string) {
		if readyPipe != nil {
			_, _ = readyPipe.WriteString("err:" + msg + "\n")
			_ = readyPipe.Close()
		}
		os.Exit(1)
	}

	if dataPipe == nil || readyPipe == nil {
		signalError("missing pipe file descriptors")
	}

	if os.Getenv("NSSH_AGENT_CACHE_ONLY") == "1" {
		_ = dataPipe.Close()
		cfg, err := config.LoadDefault()
		if err != nil {
			signalError(err.Error())
		}
		agentCfg := agent.DefaultRuntimeConfig()
		agentCfg.Agent = &cfg.Agent
		agentCfg.Archive = &cfg.Logging.Session.Archive
		agentCfg.ReadyPipe = readyPipe
		if err := agent.Run(agent.NewCacheOnlyProvider(), agentCfg); err != nil {
			if readyPipe != nil {
				_, _ = readyPipe.WriteString("err:" + err.Error() + "\n")
				_ = readyPipe.Close()
			}
			os.Exit(1)
		}
		return
	}

	if os.Getenv("NSSH_AGENT_RUNTIME") == "1" {
		_ = dataPipe.Close()
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

	_ = dataPipe.Close()
	signalError("agent runtime mode is required")
}
