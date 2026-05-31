package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStreamTableWritesRowsBeforeClose(t *testing.T) {
	var out bytes.Buffer
	table := NewStreamTable("Host", "Issue").
		WithColumnWidths(10, 10).
		WithWriter(&out)

	table.AddRow("edge01", "stale-dns")

	beforeClose := out.String()
	if !strings.Contains(beforeClose, "edge01") {
		t.Fatalf("row was not written before close:\n%s", beforeClose)
	}
	if strings.Contains(beforeClose, "╯") {
		t.Fatalf("stream table closed before Close:\n%s", beforeClose)
	}

	table.Close()
	afterClose := out.String()
	if !strings.Contains(afterClose, "╯") {
		t.Fatalf("stream table did not close:\n%s", afterClose)
	}
}

func TestRenderTablesSideBySideStringJoinsTablesHorizontally(t *testing.T) {
	left := NewTable("Group", "Total")
	left.AddRow("custcbb", "1,090")
	right := NewTable("Provider", "Hosts")
	right.AddRow("netbox-prod", "1,089")

	rendered, _ := renderTablesSideBySideString(left, right, 4)

	if !strings.Contains(rendered, "custcbb") || !strings.Contains(rendered, "netbox-prod") {
		t.Fatalf("side-by-side render missing table content:\n%s", rendered)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "custcbb") && strings.Contains(line, "netbox-prod") {
			return
		}
	}
	t.Fatalf("expected table rows to render on the same line:\n%s", rendered)
}

func TestRenderTitledTablesSideBySideStringLabelsEachTable(t *testing.T) {
	left := NewTable("Group")
	left.AddRow("custcbb")
	right := NewTable("local", "netbox-prod")
	right.AddRow("1", "1,089")

	rendered, _ := renderTitledTablesSideBySideString("Groups", left, "Provider counts", right, 4)

	if !strings.Contains(rendered, "Groups") || !strings.Contains(rendered, "Provider counts") {
		t.Fatalf("side-by-side render missing titles:\n%s", rendered)
	}
}

func TestTableWithMinWidthExpandsRenderedWidth(t *testing.T) {
	table := NewTable("Name")
	table.AddRow("x")

	natural := table.Width()
	table.WithMinWidth(natural + 12)

	if got := table.Width(); got != natural+12 {
		t.Fatalf("width = %d, want %d", got, natural+12)
	}
	for _, line := range strings.Split(table.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lipgloss.Width(line) < natural+12 {
			t.Fatalf("line width = %d, want at least %d: %q", lipgloss.Width(line), natural+12, line)
		}
	}
}

func TestTableWithMinWidthPreservesVisibleCellText(t *testing.T) {
	table := NewTable("Name", "Value").WithMinWidth(60)
	table.AddRow("provider", "provider_local.conf")

	rendered := table.String()
	for _, want := range []string{"provider", "provider_local.conf"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered table missing %q:\n%s", want, rendered)
		}
	}
}
