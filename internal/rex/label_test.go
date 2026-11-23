package rex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRex installs a rex executable that logs each call and keeps one block
// label in a file. describeShape selects the describe_block result format.
func fakeRex(t *testing.T, label, describeShape string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := `#!/bin/sh
dir=$(dirname "$0")
echo "$*" >> "$dir/calls"
case "$5" in
session.describe_block)
	[ -f "$dir/fail-describe" ] && { echo "Error: boom" >&2; exit 1; }
	label=$(cat "$dir/label")
	case "` + describeShape + `" in
	nested) printf '{"block":{"label":"%s"}}\n' "$label" ;;
	none) echo '{"revision":1}' ;;
	*) printf '{"label":"%s"}\n' "$label" ;;
	esac ;;
session.set_block_label)
	printf '%s' "$6" | sed -n 's/.*"label":"\([^"]*\)".*/\1/p' > "$dir/label"
	echo '{"revision":2}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "label"), []byte(label), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REX_SESSION", "session:s1")
	t.Setenv("REX_BLOCK", "block:b1")
	t.Setenv("REX_BIN_DIR", dir)
	t.Setenv("NSSH_REX", "")
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func TestLabelPaneSetsAndRestores(t *testing.T) {
	for _, shape := range []string{"flat", "nested"} {
		t.Run(shape, func(t *testing.T) {
			dir := fakeRex(t, "shell", shape)
			restore := LabelPane("acm-core1\x1b")
			restore()
			if got := readFile(t, filepath.Join(dir, "label")); got != "shell" {
				t.Fatalf("label after restore = %q, want shell", got)
			}
			calls := readFile(t, filepath.Join(dir, "calls"))
			want := strings.Join([]string{
				`api call -s session:s1 session.describe_block {"block_id":"block:b1"}`,
				`api call -s session:s1 session.set_block_label {"block_id":"block:b1","label":"acm-core1"}`,
				`api call -s session:s1 session.describe_block {"block_id":"block:b1"}`,
				`api call -s session:s1 session.set_block_label {"block_id":"block:b1","label":"shell"}`,
			}, "\n")
			if calls != want {
				t.Fatalf("calls =\n%s\nwant\n%s", calls, want)
			}
		})
	}
}

func TestLabelPaneKeepsOperatorRename(t *testing.T) {
	dir := fakeRex(t, "shell", "flat")
	restore := LabelPane("acm-core1")
	// Wait for the label to be set, then simulate an operator rename.
	for readFile(t, filepath.Join(dir, "label")) != "acm-core1" {
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(dir, "label"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore()
	if got := readFile(t, filepath.Join(dir, "label")); got != "mine" {
		t.Fatalf("label = %q, want operator rename kept", got)
	}
}

func TestLabelPaneSkipsWhenLabelUnreadable(t *testing.T) {
	for _, tt := range []struct{ name, shape string }{{"no label field", "none"}, {"describe fails", "fail"}} {
		t.Run(tt.name, func(t *testing.T) {
			dir := fakeRex(t, "shell", tt.shape)
			if tt.shape == "fail" {
				if err := os.WriteFile(filepath.Join(dir, "fail-describe"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			LabelPane("acm-core1")()
			if calls := readFile(t, filepath.Join(dir, "calls")); strings.Contains(calls, "set_block_label") {
				t.Fatalf("set a label it could not restore: %s", calls)
			}
		})
	}
}

func TestLabelPaneOutsideRex(t *testing.T) {
	dir := fakeRex(t, "shell", "flat")
	t.Setenv("NSSH_REX", "0")
	LabelPane("acm-core1")()
	t.Setenv("NSSH_REX", "")
	t.Setenv("REX_BIN_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // hide a standard macOS Rex install
	LabelPane("acm-core1")()
	if calls := readFile(t, filepath.Join(dir, "calls")); calls != "" {
		t.Fatalf("called rex outside Rex or without an executable: %s", calls)
	}
}
