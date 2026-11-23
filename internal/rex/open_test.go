package rex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServer installs a rex executable that logs each call as one line and
// answers api calls from resp/<method> files, or {} when none exists.
// `rex do` fails when a fail-select file exists.
func fakeServer(t *testing.T, responses map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
dir=$(dirname "$0")
printf '%s\n' "$*" >> "$dir/calls"
if [ "$1" = do ]; then
	[ -f "$dir/fail-select" ] && { echo "Error: refused" >&2; exit 1; }
	echo '{}'; exit 0
fi
shift 2
[ "$1" = -s ] && shift 2
if [ -f "$dir/resp/$1" ]; then cat "$dir/resp/$1"; else echo '{}'; fi
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "resp"), 0o755); err != nil {
		t.Fatal(err)
	}
	for method, body := range responses {
		if err := os.WriteFile(filepath.Join(dir, "resp", method), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REX_SESSION", "session:me")
	t.Setenv("REX_BLOCK", "block:me")
	t.Setenv("REX_BIN_DIR", dir)
	t.Setenv("NSSH_REX", "")
	return dir
}

func calls(t *testing.T, dir string) []string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// callArgs returns the decoded JSON argument of the first call to method.
func callArgs(t *testing.T, dir, method string) map[string]any {
	t.Helper()
	for _, line := range calls(t, dir) {
		if i := strings.Index(line, " "+method+" {"); i >= 0 {
			var args map[string]any
			if err := json.Unmarshal([]byte(line[i+len(method)+2:]), &args); err != nil {
				t.Fatalf("decode %s args: %v", method, err)
			}
			return args
		}
	}
	t.Fatalf("no %s call in %q", method, calls(t, dir))
	return nil
}

func hostsNamed(names ...string) []Host {
	hosts := make([]Host, len(names))
	for i, name := range names {
		hosts[i] = Host{Name: name, Argv: PaneArgv("/bin/nssh", name)}
	}
	return hosts
}

func TestOpenCreatesSessionWithSplitTabs(t *testing.T) {
	dir := fakeServer(t, map[string]string{
		"session.list":                   `{"sessions":[{"session_id":"session:other","label":"work"}]}`,
		"com.superlogical.terminal.size": `{"columns":200,"rows":50}`,
		"session.create":                 `{"session_id":"session:new","initial_windows":[{"window_id":"window:1","block_ids":["block:1","block:2"]},{"window_id":"window:2","block_ids":["block:3"]}]}`,
	})
	opened, err := NewClient().Open(t.Context(), Placement{Session: "nssh", PanesPerTab: 2, Split: SplitAuto}, hostsNamed("acm-core1", "acm-core2", "acm-core3"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opened.Tabs, " | "); got != "acm-core(1,2) | acm-core3" || !opened.Selected || len(opened.Reused) != 0 {
		t.Fatalf("opened = %+v", opened)
	}
	args := callArgs(t, dir, "session.create")
	windows := args["initial_windows"].([]any)
	first := windows[0].(map[string]any)
	split := first["layout"].(map[string]any)["split"].(map[string]any)
	if first["window_label"] != "acm-core(1,2)" || split["direction"] != "horizontal" || args["label"] != "nssh" {
		t.Fatalf("create args = %v", args)
	}
	block := split["before"].(map[string]any)["block"].(map[string]any)
	options := block["options"].(map[string]any)
	if block["label"] != "acm-core1" || options["shell"] != "none" ||
		strings.Join(toStrings(options["command"]), " ") != "/usr/bin/env NSSH_REX_PANE=1 /bin/nssh --target acm-core1" {
		t.Fatalf("pane = %v", block)
	}
	if got := callArgs(t, dir, "session.focus_block")["block_id"]; got != "block:1" {
		t.Fatalf("focused %v, want block:1", got)
	}
	last := calls(t, dir)[len(calls(t, dir))-1]
	if last != `do session.select --args {"session_id":"session:new","window_id":"window:1"}` {
		t.Fatalf("select = %q", last)
	}
}

func TestOpenReusesLivePanesAndAddsTabs(t *testing.T) {
	dir := fakeServer(t, map[string]string{
		"session.list":                      `{"sessions":[{"session_id":"session:nssh","label":"NSSH"}]}`,
		"session.list_blocks":               `{"blocks":[{"block_id":"block:a","label":"acm-core1","placement":"placed","window_id":"window:a"},{"block_id":"block:d","label":"acm-core2","placement":"detached"}]}`,
		"com.superlogical.terminal.process": `{"child":{"name":"nssh","pid":1,"user_id":501},"last_exit":null}`,
		"com.superlogical.terminal.size":    `{"columns":100,"rows":50}`,
		"session.new_window":                `{"window_id":"window:b","block_ids":["block:b"],"revision":3}`,
	})
	opened, err := NewClient().Open(t.Context(), Placement{Session: "nssh", PanesPerTab: 2}, hostsNamed("acm-core1", "acm-core2"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(opened.Reused, ",") != "acm-core1" || strings.Join(opened.Tabs, ",") != "acm-core2" {
		t.Fatalf("opened = %+v", opened)
	}
	args := callArgs(t, dir, "session.new_window")
	if args["focus"] != true || args["window_label"] != "acm-core2" {
		t.Fatalf("new_window args = %v", args)
	}
	if got := callArgs(t, dir, "session.focus_block")["block_id"]; got != "block:b" {
		t.Fatalf("focused %v, want the new tab", got)
	}
}

func TestOpenFocusesExistingWhenAllLive(t *testing.T) {
	dir := fakeServer(t, map[string]string{
		"session.list":                      `{"sessions":[{"session_id":"session:nssh","label":"nssh"}]}`,
		"session.list_blocks":               `{"blocks":[{"block_id":"block:a","label":"acm-core1","placement":"placed","window_id":"window:a"}]}`,
		"com.superlogical.terminal.process": `{"child":{"name":"nssh","pid":1,"user_id":501},"last_exit":null}`,
	})
	if err := os.WriteFile(filepath.Join(dir, "fail-select"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := NewClient().Open(t.Context(), Placement{}, hostsNamed("acm-core1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Tabs) != 0 || opened.Selected || strings.Join(opened.Reused, ",") != "acm-core1" {
		t.Fatalf("opened = %+v", opened)
	}
	if got := callArgs(t, dir, "session.focus_block")["block_id"]; got != "block:a" {
		t.Fatalf("focused %v", got)
	}
	for _, line := range calls(t, dir) {
		if strings.Contains(line, "new_window") || strings.Contains(line, "session.create") {
			t.Fatalf("created a pane for a live host: %s", line)
		}
	}
}

func TestOpenIgnoresPanesNotRunningNSSH(t *testing.T) {
	for name, process := range map[string]string{
		"shell":  `{"child":{"name":"zsh","pid":1,"user_id":501},"last_exit":null}`,
		"exited": `{"child":{"name":"nssh","pid":1,"user_id":501},"last_exit":{"exit_code":0}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := fakeServer(t, map[string]string{
				"session.list":                      `{"sessions":[{"session_id":"session:nssh","label":"nssh"}]}`,
				"session.list_blocks":               `{"blocks":[{"block_id":"block:a","label":"acm-core1","placement":"placed","window_id":"window:a"}]}`,
				"com.superlogical.terminal.process": process,
				"session.new_window":                `{"window_id":"window:b","block_ids":["block:b"]}`,
			})
			opened, err := NewClient().Open(t.Context(), Placement{}, hostsNamed("acm-core1"))
			if err != nil || len(opened.Reused) != 0 || len(opened.Tabs) != 1 {
				t.Fatalf("opened = %+v, %v", opened, err)
			}
			callArgs(t, dir, "session.new_window")
		})
	}
}

func TestOpenCurrentSession(t *testing.T) {
	dir := fakeServer(t, map[string]string{"session.new_window": `{"window_id":"window:b","block_ids":["block:b"]}`})
	if _, err := NewClient().Open(t.Context(), Placement{Session: SessionCurrent, Split: SplitStacked}, hostsNamed("a", "b")); err != nil {
		t.Fatal(err)
	}
	for _, line := range calls(t, dir) {
		if strings.Contains(line, "session.list ") {
			t.Fatalf("looked up a session for current: %s", line)
		}
	}
	if !strings.Contains(strings.Join(calls(t, dir), "\n"), "api call -s session:me session.new_window") {
		t.Fatalf("calls = %q", calls(t, dir))
	}
	split := callArgs(t, dir, "session.new_window")["layout"].(map[string]any)["split"].(map[string]any)
	if split["direction"] != "vertical" {
		t.Fatalf("stacked split = %v", split)
	}
}

func TestCanSelect(t *testing.T) {
	dir := fakeServer(t, nil)
	c := NewClient()
	if !c.CanSelect(t.Context()) {
		t.Fatal("CanSelect = false")
	}
	if last := calls(t, dir)[0]; last != `do session.select --args {"session_id":"session:me"}` {
		t.Fatalf("probe = %q", last)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-select"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if c.CanSelect(t.Context()) {
		t.Fatal("CanSelect = true when the app refuses actions")
	}
}

func TestGroupAndLayout(t *testing.T) {
	hosts := hostsNamed("a", "b", "c", "d", "e")
	for perTab, want := range map[int]string{1: "1 1 1 1 1", 2: "2 2 1", 4: "4 1", 3: "2 2 1"} {
		var sizes []string
		for _, group := range groupHosts(hosts, perTab) {
			sizes = append(sizes, string(rune('0'+len(group))))
		}
		if got := strings.Join(sizes, " "); got != want {
			t.Fatalf("panes_per_tab %d groups = %q, want %q", perTab, got, want)
		}
	}
	grid := layout(hosts[:4], "horizontal")["split"].(map[string]any)
	top := grid["before"].(map[string]any)["split"].(map[string]any)
	if grid["direction"] != "vertical" || top["direction"] != "horizontal" {
		t.Fatalf("grid = %v", grid)
	}
}

func TestTabName(t *testing.T) {
	tests := map[string]string{
		"acm-core1":           "acm-core1",
		"acm-core1,acm-core2": "acm-core(1,2)",
		"core10,core11":       "core(10,11)",
		"edge1,core2":         "edge1 + core2",
		"core,core2":          "core + core2",
		"pit2-a1,pit2-b1":     "pit2-(a1,b1)",
	}
	for in, want := range tests {
		if got := TabName(hostsNamed(strings.Split(in, ",")...)); got != want {
			t.Errorf("TabName(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestNewClientFindsInstalledRex(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "Library", "Application Support", "com.superlogical.rex", "RexServer", "Versions", "Current", "Rex Server.app", "Contents", "MacOS")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "rex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("REX_BIN_DIR", "")
	t.Setenv("REX_SESSION", "session:me")
	t.Setenv("REX_BLOCK", "block:me")
	t.Setenv("NSSH_REX", "")
	if c := NewClient(); c == nil || c.bin != filepath.Join(bin, "rex") {
		t.Fatalf("client = %+v", c)
	}
}

func TestInPaneAndArgv(t *testing.T) {
	t.Setenv("NSSH_REX_PANE", "1")
	if !InPane() {
		t.Fatal("InPane = false")
	}
	t.Setenv("NSSH_REX_PANE", "")
	if InPane() {
		t.Fatal("InPane = true")
	}
}

func toStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, len(items))
	for i, item := range items {
		out[i], _ = item.(string)
	}
	return out
}
