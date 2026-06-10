package tui

import "testing"

func TestBuildSplitDiffRowsEqualLines(t *testing.T) {
	rows := buildSplitDiffRows([]string{"alpha", "bravo"}, []string{"alpha", "bravo"})

	want := []splitDiffRow{
		{leftLineNo: 1, rightLineNo: 1, leftText: "alpha", rightText: "alpha", leftKind: diffEqual, rightKind: diffEqual},
		{leftLineNo: 2, rightLineNo: 2, leftText: "bravo", rightText: "bravo", leftKind: diffEqual, rightKind: diffEqual},
	}
	assertSplitDiffRows(t, rows, want)
}

func TestBuildSplitDiffRowsLeftOnlyLines(t *testing.T) {
	rows := buildSplitDiffRows([]string{"alpha", "left-only", "bravo"}, []string{"alpha", "bravo"})

	want := []splitDiffRow{
		{leftLineNo: 1, rightLineNo: 1, leftText: "alpha", rightText: "alpha", leftKind: diffEqual, rightKind: diffEqual},
		{leftLineNo: 2, leftText: "left-only", leftKind: diffLeftOnly},
		{leftLineNo: 3, rightLineNo: 2, leftText: "bravo", rightText: "bravo", leftKind: diffEqual, rightKind: diffEqual},
	}
	assertSplitDiffRows(t, rows, want)
}

func TestBuildSplitDiffRowsRightOnlyLines(t *testing.T) {
	rows := buildSplitDiffRows([]string{"alpha", "bravo"}, []string{"alpha", "right-only", "bravo"})

	want := []splitDiffRow{
		{leftLineNo: 1, rightLineNo: 1, leftText: "alpha", rightText: "alpha", leftKind: diffEqual, rightKind: diffEqual},
		{rightLineNo: 2, rightText: "right-only", rightKind: diffRightOnly},
		{leftLineNo: 2, rightLineNo: 3, leftText: "bravo", rightText: "bravo", leftKind: diffEqual, rightKind: diffEqual},
	}
	assertSplitDiffRows(t, rows, want)
}

func TestBuildSplitDiffRowsChangedLines(t *testing.T) {
	rows := buildSplitDiffRows([]string{"alpha", "left", "bravo"}, []string{"alpha", "right", "bravo"})

	want := []splitDiffRow{
		{leftLineNo: 1, rightLineNo: 1, leftText: "alpha", rightText: "alpha", leftKind: diffEqual, rightKind: diffEqual},
		{leftLineNo: 2, rightLineNo: 2, leftText: "left", rightText: "right", leftKind: diffChanged, rightKind: diffChanged},
		{leftLineNo: 3, rightLineNo: 3, leftText: "bravo", rightText: "bravo", leftKind: diffEqual, rightKind: diffEqual},
	}
	assertSplitDiffRows(t, rows, want)
}

func TestBuildSplitDiffRowsLargeInputUsesIndexFallback(t *testing.T) {
	left := make([]string, 501)
	right := make([]string, 501)
	for i := range left {
		left[i] = "same"
		right[i] = "same"
	}
	left[250] = "left"
	right[250] = "right"

	rows := buildSplitDiffRows(left, right)

	if len(rows) != 501 {
		t.Fatalf("rows = %d, want 501", len(rows))
	}
	got := rows[250]
	want := splitDiffRow{
		leftLineNo:  251,
		rightLineNo: 251,
		leftText:    "left",
		rightText:   "right",
		leftKind:    diffChanged,
		rightKind:   diffChanged,
	}
	if got != want {
		t.Fatalf("fallback row = %#v, want %#v", got, want)
	}
}

func TestBuildSplitDiffRowsLargeInputAlignsUniqueConfigAnchors(t *testing.T) {
	var left []string
	var right []string
	for i := 0; i < 520; i++ {
		left = append(left, "common prefix")
		right = append(right, "common prefix")
	}
	left = append(left,
		"username admin secret sha512 left-hash",
		"username neteng secret sha512 left-hash",
		"username test secret sha512 left-only",
	)
	right = append(right,
		"username admin secret sha512 right-hash",
	)
	anchors := []string{
		"hardware counter feature subinterface out",
		"hardware counter feature subinterface in",
		"hardware counter feature vlan-interface out",
		"hardware counter feature vlan-interface in",
	}
	left = append(left, anchors...)
	right = append(right, anchors...)

	rows := buildSplitDiffRows(left, right)

	for _, anchor := range anchors {
		row, ok := findSplitDiffRow(rows, anchor)
		if !ok {
			t.Fatalf("missing anchor row %q in %#v", anchor, rows)
		}
		if row.leftText != anchor || row.rightText != anchor || row.leftKind != diffEqual || row.rightKind != diffEqual {
			t.Fatalf("anchor row = %#v, want equal", row)
		}
	}
}

func findSplitDiffRow(rows []splitDiffRow, text string) (splitDiffRow, bool) {
	for _, row := range rows {
		if row.leftText == text || row.rightText == text {
			return row, true
		}
	}
	return splitDiffRow{}, false
}

func assertSplitDiffRows(t *testing.T, got, want []splitDiffRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("rows = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}
