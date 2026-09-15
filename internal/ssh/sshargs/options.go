// Package sshargs identifies OpenSSH options without interpreting shell text.
package sshargs

import (
	"fmt"
	"strings"
)

// Option is one option in a short-option word, such as -vJjump or -qp 2222.
type Option struct {
	Name  byte
	Value string
}

// TakesValue reports whether an OpenSSH short option consumes an argument.
func TakesValue(name byte) bool {
	return strings.ContainsRune("BDEFIJLOPQRSWbceilmopw", rune(name))
}

// Next reads one option word and its separate value, if any. The caller owns
// positional arguments and --. Values are opaque, even when they begin with -.
func Next(args []string) ([]Option, int, error) {
	if len(args) == 0 || len(args[0]) < 2 || args[0][0] != '-' || args[0][1] == '-' {
		return nil, 0, fmt.Errorf("expected an SSH option")
	}
	word := args[0]
	var options []Option
	for i := 1; i < len(word); i++ {
		name := word[i]
		option := Option{Name: name}
		if TakesValue(name) {
			if i+1 < len(word) {
				option.Value = word[i+1:]
				return append(options, option), 1, nil
			}
			if len(args) < 2 {
				return nil, 0, fmt.Errorf("option -%c requires a value", name)
			}
			option.Value = args[1]
			return append(options, option), 2, nil
		}
		if !strings.ContainsRune("1246ACGKMNTVXYZafgknqstvxy", rune(name)) {
			return nil, 0, fmt.Errorf("unknown SSH option: -%c", name)
		}
		options = append(options, option)
	}
	return options, 1, nil
}

// Walk visits options in order, stopping at --, a positional argument, an
// invalid option, or when visit returns false.
func Walk(args []string, visit func(Option) bool) {
	for len(args) > 0 {
		options, n, err := Next(args)
		if err != nil {
			return
		}
		for _, option := range options {
			if !visit(option) {
				return
			}
		}
		args = args[n:]
	}
}

// Split separates the internal option stream from its explicit command marker.
// An option argument equal to -- remains an argument, not a marker.
func Split(args []string) (options, command []string) {
	for i := 0; i < len(args); {
		if args[i] == "--" {
			return args[:i], args[i+1:]
		}
		_, n, err := Next(args[i:])
		if err != nil {
			break
		}
		i += n
	}
	return args, nil
}
