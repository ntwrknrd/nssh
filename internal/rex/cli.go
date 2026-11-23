package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// callTimeout bounds each rex CLI call so Rex never delays a connection
// noticeably.
const callTimeout = 2 * time.Second

// Client calls Rex server methods through the rex CLI, the stable surface.
type Client struct {
	bin     string
	session string
	block   string
}

// NewClient returns a client for the invoking pane, or nil when Rex is not
// detected or no rex executable resolves. $REX_BIN_DIR/rex is preferred, then
// rex on PATH, then a standard macOS install.
func NewClient() *Client {
	if !Detected() {
		return nil
	}
	bin := ""
	if dir := os.Getenv("REX_BIN_DIR"); dir != "" {
		candidate := filepath.Join(dir, "rex")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			bin = candidate
		}
	}
	if bin == "" {
		if path, err := exec.LookPath("rex"); err == nil {
			bin = path
		}
	}
	if bin == "" {
		bin = installedRex()
	}
	if bin == "" {
		return nil
	}
	return &Client{bin: bin, session: os.Getenv("REX_SESSION"), block: os.Getenv("REX_BLOCK")}
}

// installedRex returns the rex executable of a standard macOS install, which
// Rex palette actions need: the server's environment has no REX_BIN_DIR and
// may not have rex on PATH.
func installedRex() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, "Library", "Application Support", "com.superlogical.rex",
		"RexServer", "Versions", "Current", "Rex Server.app", "Contents", "MacOS", "rex")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// Call runs `rex api call -s SESSION METHOD ARGS` and returns its JSON output.
// An empty session omits -s for server-wide methods.
func (c *Client) Call(ctx context.Context, session, method string, args map[string]any) ([]byte, error) {
	if args == nil {
		args = map[string]any{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	argv := []string{"api", "call"}
	if session != "" {
		argv = append(argv, "-s", session)
	}
	return c.run(ctx, append(argv, method, string(data))...)
}

// run executes the rex CLI with argv and returns its standard output.
func (c *Client) run(ctx context.Context, argv ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	name := strings.Join(argv[:min(len(argv), 3)], " ")
	if len(argv) > 3 && argv[2] == "-s" {
		name = "api call " + argv[4]
	}
	slog.Debug("rex call", "call", name, "elapsed", time.Since(start).Round(time.Millisecond), "ok", err == nil)
	if err != nil {
		return nil, fmt.Errorf("rex %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// callJSON calls a method and decodes its result into out.
func (c *Client) callJSON(ctx context.Context, session, method string, args map[string]any, out any) error {
	data, err := c.Call(ctx, session, method, args)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("rex %s: %w", method, err)
	}
	return nil
}

// BlockLabel returns the current label of a block in the invoking session.
// session.describe_block reports it as block.label; a top-level label is
// also accepted.
func (c *Client) BlockLabel(ctx context.Context, block string) (string, error) {
	out, err := c.Call(ctx, c.session, "session.describe_block", map[string]any{"block_id": block})
	if err != nil {
		return "", err
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("rex session.describe_block: %w", err)
	}
	if label, ok := result["label"].(string); ok {
		return label, nil
	}
	if inner, ok := result["block"].(map[string]any); ok {
		if label, ok := inner["label"].(string); ok {
			return label, nil
		}
	}
	return "", fmt.Errorf("rex session.describe_block: no label in result")
}

// SetBlockLabel sets a block's label. An empty label restores Rex's default.
func (c *Client) SetBlockLabel(ctx context.Context, block, label string) error {
	_, err := c.Call(ctx, c.session, "session.set_block_label", map[string]any{"block_id": block, "label": label})
	return err
}
