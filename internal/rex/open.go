package rex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Placement setting values.
const (
	SessionCurrent   = "current"
	SplitAuto        = "auto"
	SplitSideBySide  = "side-by-side"
	SplitStacked     = "stacked"
	paneEnv          = "NSSH_REX_PANE"
	terminalFlavor   = "com.superlogical.terminal.shell"
	autoSplitColumns = 160
)

// Placement controls where Open puts hosts.
type Placement struct {
	Session     string // session label, or SessionCurrent
	PanesPerTab int    // 1, 2, or 4
	Split       string // SplitAuto, SplitSideBySide, or SplitStacked
}

// Host is one host to open: its pane label and the argv its pane runs.
type Host struct {
	Name string
	Argv []string
}

// Opened reports what Open did.
type Opened struct {
	Session  string   // session label
	Tabs     []string // labels of new tabs, in order
	Reused   []string // hosts whose live pane was focused instead
	Selected bool     // the app switched to the result
}

// ForceOpen reports whether a Rex palette action asked nssh to open its
// targets in Rex panes even without a terminal (NSSH_REX_OPEN=1).
func ForceOpen() bool { return os.Getenv("NSSH_REX_OPEN") == "1" }

// InPane reports whether this process runs in a pane nssh launched. Such a
// pane never opens another pane.
func InPane() bool { return os.Getenv(paneEnv) == "1" }

// PaneArgv returns the argv a launched pane runs: exe --target TARGET, marked
// as a launched pane. Rex starts it directly, with no shell.
func PaneArgv(exe, target string) []string {
	return []string{"/usr/bin/env", paneEnv + "=1", exe, "--target", target}
}

// CanSelect reports whether the app accepts actions from the CLI, which needs
// Remote Control. It selects the invoking session, which the app already
// shows, so the probe has no visible effect.
func (c *Client) CanSelect(ctx context.Context) bool {
	return c.selectSession(ctx, c.session, "") == nil
}

func (c *Client) selectSession(ctx context.Context, session, window string) error {
	args := map[string]string{"session_id": session}
	if window != "" {
		args["window_id"] = window
	}
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, "do", "session.select", "--args", string(data))
	return err
}

type viewBlock struct {
	BlockID   string `json:"block_id"`
	Label     string `json:"label"`
	Placement string `json:"placement"`
	WindowID  string `json:"window_id"`
}

// Open places hosts in the configured session: live panes for a host are
// reused, the rest are grouped into new tabs. It never closes, moves, or adds
// panes to an existing tab. The app is switched to the first new tab, or to
// the first reused pane when nothing new opened.
func (c *Client) Open(ctx context.Context, p Placement, hosts []Host) (Opened, error) {
	label := p.Session
	if label == "" {
		label = App
	}
	result := Opened{Session: label}
	session := ""
	if label == SessionCurrent {
		session = c.session
	} else {
		var err error
		if session, err = c.findSession(ctx, label); err != nil {
			return result, err
		}
	}

	var focus viewBlock
	remaining := hosts
	if session != "" {
		live, err := c.liveHosts(ctx, session, hosts)
		if err != nil {
			return result, err
		}
		remaining = nil
		for _, host := range hosts {
			if block, ok := live[strings.ToLower(host.Name)]; ok {
				result.Reused = append(result.Reused, host.Name)
				if focus.BlockID == "" {
					focus = block
				}
				continue
			}
			remaining = append(remaining, host)
		}
	}

	var windows []map[string]any
	direction := c.splitDirection(ctx, p.Split)
	for _, group := range groupHosts(remaining, p.PanesPerTab) {
		name := TabName(group)
		result.Tabs = append(result.Tabs, name)
		windows = append(windows, map[string]any{"window_label": name, "layout": layout(group, direction)})
	}

	type created struct {
		WindowID string   `json:"window_id"`
		BlockIDs []string `json:"block_ids"`
	}
	var first created
	switch {
	case len(windows) == 0:
	case session == "":
		var out struct {
			SessionID      string    `json:"session_id"`
			InitialWindows []created `json:"initial_windows"`
		}
		if err := c.callJSON(ctx, "", "session.create", map[string]any{"label": label, "initial_windows": windows}, &out); err != nil {
			return result, err
		}
		session = out.SessionID
		if len(out.InitialWindows) > 0 {
			first = out.InitialWindows[0]
		}
	default:
		for i, window := range windows {
			window["focus"] = i == 0
			var out created
			if err := c.callJSON(ctx, session, "session.new_window", window, &out); err != nil {
				return result, err
			}
			if i == 0 {
				first = out
			}
		}
	}
	if len(first.BlockIDs) > 0 {
		focus = viewBlock{BlockID: first.BlockIDs[0], WindowID: first.WindowID}
	}
	if focus.BlockID == "" {
		return result, nil
	}
	// Focusing a block makes its window active in the session, which
	// session.create would otherwise leave on its last window.
	// Placement already succeeded, so a focus failure is not an error.
	if _, err := c.Call(ctx, session, "session.focus_block", map[string]any{"block_id": focus.BlockID}); err != nil {
		slog.Debug("rex focus failed", "err", err)
	}
	result.Selected = c.selectSession(ctx, session, focus.WindowID) == nil
	return result, nil
}

func (c *Client) findSession(ctx context.Context, label string) (string, error) {
	var list struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Label     string `json:"label"`
		} `json:"sessions"`
	}
	if err := c.callJSON(ctx, "", "session.list", nil, &list); err != nil {
		return "", err
	}
	for _, s := range list.Sessions {
		if strings.EqualFold(s.Label, label) {
			return s.SessionID, nil
		}
	}
	return "", nil
}

// liveHosts maps lowercased host names to placed panes in session that carry
// the host's label and still run nssh.
func (c *Client) liveHosts(ctx context.Context, session string, hosts []Host) (map[string]viewBlock, error) {
	wanted := map[string]bool{}
	for _, host := range hosts {
		wanted[strings.ToLower(host.Name)] = true
	}
	var list struct {
		Blocks []viewBlock `json:"blocks"`
	}
	if err := c.callJSON(ctx, session, "session.list_blocks", nil, &list); err != nil {
		return nil, err
	}
	live := map[string]viewBlock{}
	for _, block := range list.Blocks {
		name := strings.ToLower(block.Label)
		if !wanted[name] || block.Placement != "placed" || live[name].BlockID != "" {
			continue
		}
		if c.runsNSSH(ctx, session, block.BlockID) {
			live[name] = block
		}
	}
	return live, nil
}

// runsNSSH reports whether a terminal block's program is a running nssh.
func (c *Client) runsNSSH(ctx context.Context, session, block string) bool {
	var process struct {
		Child *struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"child"`
		LastExit *struct{} `json:"last_exit"`
	}
	args := map[string]any{"block_id": block, "args": map[string]any{}}
	if err := c.callJSON(ctx, session, "com.superlogical.terminal.process", args, &process); err != nil {
		return false
	}
	if process.LastExit != nil || process.Child == nil {
		return false
	}
	return process.Child.Name == "nssh" || filepath.Base(process.Child.Path) == "nssh"
}

// splitDirection resolves the split setting to a layout direction: auto puts
// two panes side by side when the invoking pane is at least 160 columns wide.
func (c *Client) splitDirection(ctx context.Context, split string) string {
	switch split {
	case SplitSideBySide:
		return "horizontal"
	case SplitStacked:
		return "vertical"
	}
	var size struct {
		Columns int `json:"columns"`
	}
	args := map[string]any{"block_id": c.block, "args": map[string]any{}}
	if c.block != "" && c.callJSON(ctx, c.session, "com.superlogical.terminal.size", args, &size) == nil && size.Columns >= autoSplitColumns {
		return "horizontal"
	}
	return "vertical"
}

func groupHosts(hosts []Host, perTab int) [][]Host {
	if perTab != 1 && perTab != 4 {
		perTab = 2
	}
	var groups [][]Host
	for len(hosts) > 0 {
		n := min(perTab, len(hosts))
		groups = append(groups, hosts[:n])
		hosts = hosts[n:]
	}
	return groups
}

// layout builds a session.new_window layout: one pane, two split in
// direction, or up to four in a grid with the first row on top.
func layout(group []Host, direction string) map[string]any {
	switch len(group) {
	case 1:
		return pane(group[0])
	case 2:
		return split(direction, pane(group[0]), pane(group[1]))
	case 3:
		return split("vertical", split("horizontal", pane(group[0]), pane(group[1])), pane(group[2]))
	default:
		return split("vertical",
			split("horizontal", pane(group[0]), pane(group[1])),
			split("horizontal", pane(group[2]), pane(group[3])))
	}
}

func pane(host Host) map[string]any {
	options := map[string]any{"shell": "none", "command": host.Argv}
	if cwd, err := os.Getwd(); err == nil {
		options["cwd"] = cwd
	}
	return map[string]any{"block": map[string]any{
		"flavor": terminalFlavor, "label": clean(host.Name, maxLabelBytes), "options": options,
	}}
}

func split(direction string, before, after map[string]any) map[string]any {
	return map[string]any{"split": map[string]any{"direction": direction, "before": before, "after": after}}
}

// TabName names a tab after its hosts, using the REPL suffix grammar when
// they share a prefix: acm-core(1,2). A domain every name shares is dropped,
// so acm-core1.lab.local and acm-core2.lab.local also give acm-core(1,2).
// Names with no shared prefix are joined with " + ".
func TabName(group []Host) string {
	names := make([]string, len(group))
	for i, host := range group {
		names[i] = clean(host.Name, maxLabelBytes)
	}
	if len(names) == 1 {
		return names[0]
	}
	names = trimSharedDomain(names)
	prefix := names[0]
	for _, name := range names[1:] {
		for !strings.HasPrefix(name, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	// Do not split a run of digits: core10 and core11 become core(10,11).
	for prefix != "" && isDigit(prefix[len(prefix)-1]) {
		prefix = prefix[:len(prefix)-1]
	}
	suffixes := make([]string, len(names))
	for i, name := range names {
		suffixes[i] = name[len(prefix):]
		if suffixes[i] == "" {
			prefix = ""
		}
	}
	if prefix == "" {
		return clean(strings.Join(names, " + "), maxLabelBytes)
	}
	return clean(fmt.Sprintf("%s(%s)", prefix, strings.Join(suffixes, ",")), maxLabelBytes)
}

// trimSharedDomain drops the trailing dot-separated labels that every name
// shares, keeping at least one label of each name.
func trimSharedDomain(names []string) []string {
	suffix := names[0]
	for _, name := range names[1:] {
		for !strings.HasSuffix(name, suffix) {
			suffix = suffix[1:]
		}
	}
	dot := strings.IndexByte(suffix, '.')
	if dot < 0 {
		return names
	}
	suffix = suffix[dot:]
	trimmed := make([]string, len(names))
	for i, name := range names {
		trimmed[i] = strings.TrimSuffix(name, suffix)
		if trimmed[i] == "" || strings.HasSuffix(trimmed[i], ".") {
			return names
		}
	}
	return trimmed
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
