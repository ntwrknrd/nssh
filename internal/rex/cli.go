package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
// detected or no rex executable resolves. $REX_BIN_DIR/rex is preferred.
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
		path, err := exec.LookPath("rex")
		if err != nil {
			return nil
		}
		bin = path
	}
	return &Client{bin: bin, session: os.Getenv("REX_SESSION"), block: os.Getenv("REX_BLOCK")}
}

// Call runs `rex api call -s SESSION METHOD ARGS` and returns its JSON output.
func (c *Client) Call(ctx context.Context, method string, args map[string]any) ([]byte, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, "api", "call", "-s", c.session, method, string(data))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rex %s: %w: %s", method, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// BlockLabel returns the current label of a block. The session.describe_block
// result shape is not documented, so a top-level or block-level label field is
// accepted; anything else is an error.
func (c *Client) BlockLabel(ctx context.Context, block string) (string, error) {
	out, err := c.Call(ctx, "session.describe_block", map[string]any{"block_id": block})
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
	_, err := c.Call(ctx, "session.set_block_label", map[string]any{"block_id": block, "label": label})
	return err
}
