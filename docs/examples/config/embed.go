package configexample

import _ "embed"

// ExampleConfig contains the user-facing example configuration file.
//
//go:embed config.example.toml
var ExampleConfig string
