package tui

import "sort"

type diffKind int

const (
	diffEqual diffKind = iota
	diffLeftOnly
	diffRightOnly
	diffChanged
)

type splitDiffRow struct {
	leftLineNo  int
	rightLineNo int
	leftText    string
	rightText   string
	leftKind    diffKind
	rightKind   diffKind
}

const maxSplitDiffCells = 250000

func buildSplitDiffRows(left, right []string) []splitDiffRow {
	if len(left)*len(right) > maxSplitDiffCells {
		return buildAnchoredSplitDiffRows(left, right)
	}
	matches := longestCommonSubsequenceMatches(left, right)
	rows := make([]splitDiffRow, 0, max(len(left), len(right)))
	leftPos, rightPos := 0, 0
	for _, match := range matches {
		rows = append(rows, changedSplitDiffRows(left, right, leftPos, match.left, rightPos, match.right)...)
		rows = append(rows, splitDiffRow{
			leftLineNo:  match.left + 1,
			rightLineNo: match.right + 1,
			leftText:    left[match.left],
			rightText:   right[match.right],
			leftKind:    diffEqual,
			rightKind:   diffEqual,
		})
		leftPos = match.left + 1
		rightPos = match.right + 1
	}
	rows = append(rows, changedSplitDiffRows(left, right, leftPos, len(left), rightPos, len(right))...)
	return rows
}

func buildAnchoredSplitDiffRows(left, right []string) []splitDiffRow {
	matches := uniqueCommonLineMatches(left, right)
	if len(matches) == 0 {
		return buildIndexAlignedSplitDiffRows(left, right)
	}
	rows := make([]splitDiffRow, 0, max(len(left), len(right)))
	leftPos, rightPos := 0, 0
	for _, match := range matches {
		rows = append(rows, splitDiffRowsInRange(left, right, leftPos, match.left, rightPos, match.right)...)
		rows = append(rows, splitDiffRow{
			leftLineNo:  match.left + 1,
			rightLineNo: match.right + 1,
			leftText:    left[match.left],
			rightText:   right[match.right],
			leftKind:    diffEqual,
			rightKind:   diffEqual,
		})
		leftPos = match.left + 1
		rightPos = match.right + 1
	}
	rows = append(rows, splitDiffRowsInRange(left, right, leftPos, len(left), rightPos, len(right))...)
	return rows
}

func splitDiffRowsInRange(left, right []string, leftStart, leftEnd, rightStart, rightEnd int) []splitDiffRow {
	rows := buildSplitDiffRows(left[leftStart:leftEnd], right[rightStart:rightEnd])
	for i := range rows {
		if rows[i].leftLineNo > 0 {
			rows[i].leftLineNo += leftStart
		}
		if rows[i].rightLineNo > 0 {
			rows[i].rightLineNo += rightStart
		}
	}
	return rows
}

func buildIndexAlignedSplitDiffRows(left, right []string) []splitDiffRow {
	height := max(len(left), len(right))
	rows := make([]splitDiffRow, 0, height)
	for i := 0; i < height; i++ {
		row := splitDiffRow{}
		if i < len(left) {
			row.leftLineNo = i + 1
			row.leftText = left[i]
		}
		if i < len(right) {
			row.rightLineNo = i + 1
			row.rightText = right[i]
		}
		switch {
		case i >= len(right):
			row.leftKind = diffLeftOnly
		case i >= len(left):
			row.rightKind = diffRightOnly
		case left[i] == right[i]:
			row.leftKind = diffEqual
			row.rightKind = diffEqual
		default:
			row.leftKind = diffChanged
			row.rightKind = diffChanged
		}
		rows = append(rows, row)
	}
	return rows
}

func changedSplitDiffRows(left, right []string, leftStart, leftEnd, rightStart, rightEnd int) []splitDiffRow {
	leftCount := leftEnd - leftStart
	rightCount := rightEnd - rightStart
	count := max(leftCount, rightCount)
	rows := make([]splitDiffRow, 0, count)
	for i := 0; i < count; i++ {
		row := splitDiffRow{}
		if i < leftCount {
			row.leftLineNo = leftStart + i + 1
			row.leftText = left[leftStart+i]
		}
		if i < rightCount {
			row.rightLineNo = rightStart + i + 1
			row.rightText = right[rightStart+i]
		}
		switch {
		case i < leftCount && i < rightCount:
			row.leftKind = diffChanged
			row.rightKind = diffChanged
		case i < leftCount:
			row.leftKind = diffLeftOnly
		default:
			row.rightKind = diffRightOnly
		}
		rows = append(rows, row)
	}
	return rows
}

func uniqueCommonLineMatches(left, right []string) []lcsMatch {
	leftCounts, leftIndexes := lineCountsAndIndexes(left)
	rightCounts, rightIndexes := lineCountsAndIndexes(right)
	candidates := make([]lcsMatch, 0)
	for line, leftIndex := range leftIndexes {
		if leftCounts[line] != 1 || rightCounts[line] != 1 {
			continue
		}
		rightIndex, ok := rightIndexes[line]
		if !ok {
			continue
		}
		candidates = append(candidates, lcsMatch{left: leftIndex, right: rightIndex})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].left < candidates[j].left
	})
	return increasingRightMatches(candidates)
}

func lineCountsAndIndexes(lines []string) (map[string]int, map[string]int) {
	counts := make(map[string]int, len(lines))
	indexes := make(map[string]int, len(lines))
	for i, line := range lines {
		counts[line]++
		indexes[line] = i
	}
	return counts, indexes
}

func increasingRightMatches(candidates []lcsMatch) []lcsMatch {
	if len(candidates) == 0 {
		return nil
	}
	previous := make([]int, len(candidates))
	for i := range previous {
		previous[i] = -1
	}
	tails := make([]int, 0, len(candidates))
	for i, candidate := range candidates {
		pos := sort.Search(len(tails), func(j int) bool {
			return candidates[tails[j]].right >= candidate.right
		})
		if pos > 0 {
			previous[i] = tails[pos-1]
		}
		if pos == len(tails) {
			tails = append(tails, i)
		} else {
			tails[pos] = i
		}
	}
	out := make([]lcsMatch, len(tails))
	for i, candidateIndex := len(tails)-1, tails[len(tails)-1]; i >= 0; i-- {
		out[i] = candidates[candidateIndex]
		candidateIndex = previous[candidateIndex]
	}
	return out
}

type lcsMatch struct {
	left  int
	right int
}

func longestCommonSubsequenceMatches(left, right []string) []lcsMatch {
	width := len(right) + 1
	table := make([]int, (len(left)+1)*width)
	at := func(i, j int) int {
		return i*width + j
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				table[at(i, j)] = table[at(i+1, j+1)] + 1
				continue
			}
			table[at(i, j)] = max(table[at(i+1, j)], table[at(i, j+1)])
		}
	}
	var matches []lcsMatch
	for i, j := 0, 0; i < len(left) && j < len(right); {
		switch {
		case left[i] == right[j]:
			matches = append(matches, lcsMatch{left: i, right: j})
			i++
			j++
		case table[at(i+1, j)] >= table[at(i, j+1)]:
			i++
		default:
			j++
		}
	}
	return matches
}
