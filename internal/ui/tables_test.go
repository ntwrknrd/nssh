package ui

import (
	"bytes"
	"strings"
	"testing"
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
