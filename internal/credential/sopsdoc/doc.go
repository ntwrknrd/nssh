package sopsdoc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Runner interface {
	Run(ctx context.Context, env []string, args ...string) ([]byte, error)
}

type CLIRunner struct {
	Command        string
	NonInteractive bool
}

type Document struct {
	data map[string]any
}

func Decrypt(ctx context.Context, runner Runner, file, ageKeyFile, ageKeyCommand string) (*Document, error) {
	file = expandPath(strings.TrimSpace(file))
	if file == "" {
		return nil, fmt.Errorf("sops-age file is required")
	}
	if runner == nil {
		runner = CLIRunner{Command: "sops"}
	}
	env := []string(nil)
	if strings.TrimSpace(ageKeyFile) != "" {
		env = append(env, "SOPS_AGE_KEY_FILE="+expandPath(ageKeyFile))
	}
	if command := strings.TrimSpace(ageKeyCommand); command != "" {
		// A configured command owns this provider's explicit identity source.
		// Clear inherited private identities and key-file references only in
		// the SOPS child, never in the caller's process environment.
		env = append(env, "SOPS_AGE_KEY=", "SOPS_AGE_KEY_FILE=", "SOPS_AGE_KEY_CMD="+command)
	}
	out, err := runner.Run(ctx, env, "--decrypt", "--output-type", "json", file)
	if err != nil {
		// Recognize SOPS' missing-key diagnostic without exposing provider output,
		// which can contain secret material from an identity command.
		if bytes.Contains(out, []byte("Failed to get the data key required to decrypt")) {
			return nil, fmt.Errorf("decrypt SOPS file %s: decryption key unavailable; check the age identity via age_key_file, age_key_command or SOPS_AGE_KEY_CMD: %w", file, err)
		}
		return nil, fmt.Errorf("decrypt SOPS file %s: %w", file, err)
	}
	doc, err := Parse(out)
	if err != nil {
		return nil, fmt.Errorf("parse decrypted SOPS file %s: %w", file, err)
	}
	return doc, nil
}

func Parse(data []byte) (*Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("invalid decrypted SOPS JSON")
	}
	return &Document{data: root}, nil
}

func (d *Document) Lookup(path string) (string, bool, error) {
	if d == nil {
		return "", false, nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, nil
	}
	parts := strings.Split(path, ".")
	var current any = d.data
	for _, part := range parts {
		if part == "" {
			return "", false, fmt.Errorf("SOPS key path %q has an empty component", path)
		}
		m, ok := current.(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("SOPS key path %q parent is not an object", path)
		}
		next, ok := m[part]
		if !ok {
			return "", false, nil
		}
		current = next
	}
	value, ok := current.(string)
	if !ok {
		return "", false, fmt.Errorf("SOPS key path %q is not a string", path)
	}
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func (r CLIRunner) Run(ctx context.Context, env []string, args ...string) ([]byte, error) {
	command := strings.TrimSpace(r.Command)
	if command == "" {
		command = "sops"
	}
	cmd := exec.CommandContext(ctx, command, args...)
	if r.NonInteractive {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	return out, nil
}

func expandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
