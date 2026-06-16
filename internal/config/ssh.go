package config

type SSHHostConfig struct {
	ProxyJump                string            `yaml:"proxy_jump,omitempty"`
	ProxyCommand             string            `yaml:"proxy_command,omitempty"`
	IdentitiesOnly           *bool             `yaml:"identities_only,omitempty"`
	IdentityAgent            IdentityAgent     `yaml:"identity_agent,omitempty"`
	IdentityFiles            []string          `yaml:"identity_files,omitempty"`
	CertificateFiles         []string          `yaml:"certificate_files,omitempty"`
	ForwardAgent             *bool             `yaml:"forward_agent,omitempty"`
	LocalForwards            []Forward         `yaml:"local_forwards,omitempty"`
	RemoteForwards           []Forward         `yaml:"remote_forwards,omitempty"`
	SetEnv                   map[string]string `yaml:"set_env,omitempty"`
	RemoteCommand            string            `yaml:"remote_command,omitempty"`
	ServerAliveInterval      Duration          `yaml:"server_alive_interval,omitempty"`
	ServerAliveCountMax      int               `yaml:"server_alive_count_max,omitempty"`
	ConnectionTimeout        Duration          `yaml:"connection_timeout,omitempty"`
	ControlMaster            string            `yaml:"control_master,omitempty"`
	ControlPersist           Duration          `yaml:"control_persist,omitempty"`
	ControlPath              string            `yaml:"control_path,omitempty"`
	Ciphers                  []string          `yaml:"ciphers,omitempty"`
	MACs                     []string          `yaml:"macs,omitempty"`
	KexAlgorithms            []string          `yaml:"kex_algorithms,omitempty"`
	HostKeyAlgorithms        []string          `yaml:"host_key_algorithms,omitempty"`
	PubkeyAcceptedAlgorithms []string          `yaml:"pubkey_accepted_algorithms,omitempty"`
	Compat                   []string          `yaml:"compat,omitempty"`
	Options                  map[string]string `yaml:"options,omitempty"`
}

type IdentityAgent struct {
	Path string `yaml:"path,omitempty"`
}

type Forward struct {
	Bind   string `yaml:"bind"`
	Target string `yaml:"target"`
}
