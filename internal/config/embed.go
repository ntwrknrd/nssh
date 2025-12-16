package config

import _ "embed"

// ExampleConfig contains the example configuration file.
// This is copied during `nssh self init` if no config exists.
//
//go:embed example_config.toml
var ExampleConfig string
