package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

func TestHostListOutputProcess(t *testing.T) {
	binary := buildBinary(t)
	for _, mode := range []string{"pipe", "terminal", "no-color"} {
		t.Run(mode, func(t *testing.T) {
			f := newHostListFixture(t, binary)
			script := "#!/bin/sh\ncase \"$*\" in *'show env power'*) printf 'Power    Input\\n------ -------\\n1        73.0W\\n';; esac\n"
			if err := os.WriteFile(filepath.Join(f.dir, "bin", "ssh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := f.command(t, "alpha, beta", "show env power")
			cmd.Env = append(cmd.Env, "TERM=xterm-256color")
			if mode == "no-color" {
				cmd.Env = append(cmd.Env, "NO_COLOR=1")
			}
			var output []byte
			if mode == "pipe" {
				var err error
				output, err = cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %s", err, output)
				}
			} else {
				terminal, err := pty.Start(cmd)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = terminal.Close() }()
				output, _ = io.ReadAll(terminal)
				if err = cmd.Wait(); err != nil {
					t.Fatalf("%v: %s", err, output)
				}
			}
			text := strings.ReplaceAll(ansi.Strip(string(output)), "\r\n", "\n")
			for _, want := range []string{"Command: show env power", "2 hosts", "[alpha] OK", "[beta] OK", "\nPower    Input\n", "Results", "alpha : ok=1  failed=0  canceled=0\nbeta  : ok=1  failed=0  canceled=0"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q:\n%s", want, text)
				}
			}
			if strings.Index(text, "[alpha] OK") > strings.Index(text, "[beta] OK") {
				t.Fatalf("hosts printed out of order: %s", text)
			}
			if strings.Contains(text, "alpha-user@") || strings.Contains(text, "completed") {
				t.Errorf("redundant identity/status:\n%s", text)
			}
			colored := regexp.MustCompile(`\x1b\[[0-9;]*m`).Match(output)
			if colored != (mode == "terminal") {
				t.Errorf("mode=%s colored=%v output=%q", mode, colored, output)
			}
		})
	}
}
