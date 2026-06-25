package config

import _ "embed"

// ExampleConfig contains the commented initial configuration template.
// This is copied during `nssh self init` if no config exists.
//
//go:embed example_config.yaml
var ExampleConfig string
