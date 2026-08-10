package model

import "reflect"

type DiffOperation struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type DiffBlock struct {
	BlockID    string          `json:"block_id"`
	Type       string          `json:"type"`
	Change     string          `json:"change"`
	Block      Block           `json:"block"`
	Operations []DiffOperation `json:"operations"`
}

type Diff struct {
	QuestionID     string      `json:"question_id"`
	FromSnapshotID string      `json:"from_snapshot_id"`
	ToSnapshotID   string      `json:"to_snapshot_id"`
	Granularity    string      `json:"granularity"`
	Blocks         []DiffBlock `json:"blocks"`
}

type flatBlock struct {
	Block Block
}

// CompareSnapshots aligns stable Notion block IDs, then compares each matched
// block by Unicode code point. Adjacent operations of the same kind are always
// coalesced so Chinese and Latin edits render as readable runs.
func CompareSnapshots(from Snapshot, to Snapshot) Diff {
	left := flattenBlocks(from.Blocks)
	right := flattenBlocks(to.Blocks)
	pairs := alignBlocks(left, right)
	blocks := make([]DiffBlock, 0, len(pairs))
	for _, pair := range pairs {
		switch {
		case pair.left < 0:
			block := right[pair.right].Block
			blocks = append(blocks, DiffBlock{BlockID: block.ID, Type: block.Type, Change: "added", Block: block, Operations: nonEmptyOperation("added", block.Text)})
		case pair.right < 0:
			block := left[pair.left].Block
			blocks = append(blocks, DiffBlock{BlockID: block.ID, Type: block.Type, Change: "deleted", Block: block, Operations: nonEmptyOperation("deleted", block.Text)})
		default:
			before, after := left[pair.left].Block, right[pair.right].Block
			change := "modified"
			if reflect.DeepEqual(before, after) {
				change = "unchanged"
			}
			blocks = append(blocks, DiffBlock{BlockID: after.ID, Type: after.Type, Change: change, Block: after, Operations: characterDiff(before.Text, after.Text)})
		}
	}
	return Diff{QuestionID: from.QuestionID, FromSnapshotID: from.ID, ToSnapshotID: to.ID, Granularity: "character", Blocks: blocks}
}

func flattenBlocks(blocks []Block) []flatBlock {
	result := make([]flatBlock, 0, len(blocks))
	var visit func([]Block)
	visit = func(items []Block) {
		for _, block := range items {
			shallow := block
			shallow.Children = []Block{}
			result = append(result, flatBlock{Block: shallow})
			visit(block.Children)
		}
	}
	visit(blocks)
	return result
}

type blockPair struct{ left, right int }

func alignBlocks(left, right []flatBlock) []blockPair {
	rows, columns := len(left), len(right)
	lcs := make([][]int, rows+1)
	for index := range lcs {
		lcs[index] = make([]int, columns+1)
	}
	for i := rows - 1; i >= 0; i-- {
		for j := columns - 1; j >= 0; j-- {
			if left[i].Block.ID == right[j].Block.ID {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	result := make([]blockPair, 0, rows+columns)
	for i, j := 0, 0; i < rows || j < columns; {
		switch {
		case i < rows && j < columns && left[i].Block.ID == right[j].Block.ID:
			result = append(result, blockPair{left: i, right: j})
			i++
			j++
		case j < columns && (i == rows || lcs[i][j+1] > lcs[i+1][j]):
			result = append(result, blockPair{left: -1, right: j})
			j++
		default:
			result = append(result, blockPair{left: i, right: -1})
			i++
		}
	}
	return result
}

// characterDiff uses a longest-common-subsequence trace over Unicode code
// points. Model paragraphs are bounded by the Worker, making the quadratic
// per-block matrix predictable while avoiding byte or visual-line semantics.
func characterDiff(before, after string) []DiffOperation {
	left, right := []rune(before), []rune(after)
	rows, columns := len(left), len(right)
	if rows > 0 && columns > 0 && rows*columns > 4_000_000 {
		return boundedCharacterDiff(left, right)
	}
	lcs := make([][]int, rows+1)
	for index := range lcs {
		lcs[index] = make([]int, columns+1)
	}
	for i := rows - 1; i >= 0; i-- {
		for j := columns - 1; j >= 0; j-- {
			if left[i] == right[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	result := []DiffOperation{}
	appendRune := func(kind string, value rune) {
		if len(result) > 0 && result[len(result)-1].Kind == kind {
			result[len(result)-1].Text += string(value)
			return
		}
		result = append(result, DiffOperation{Kind: kind, Text: string(value)})
	}
	for i, j := 0, 0; i < rows || j < columns; {
		switch {
		case i < rows && j < columns && left[i] == right[j]:
			appendRune("unchanged", left[i])
			i++
			j++
		case j < columns && (i == rows || lcs[i][j+1] > lcs[i+1][j]):
			appendRune("added", right[j])
			j++
		default:
			appendRune("deleted", left[i])
			i++
		}
	}
	return result
}

// boundedCharacterDiff keeps very large paragraphs safe by retaining the
// exact common prefix and suffix and grouping the changed middle as one
// deletion and one addition. The granularity remains characters and never
// depends on visual line wrapping.
func boundedCharacterDiff(left, right []rune) []DiffOperation {
	prefix := 0
	for prefix < len(left) && prefix < len(right) && left[prefix] == right[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(left)-prefix && suffix < len(right)-prefix && left[len(left)-1-suffix] == right[len(right)-1-suffix] {
		suffix++
	}
	result := []DiffOperation{}
	appendText := func(kind string, value []rune) {
		if len(value) > 0 {
			result = append(result, DiffOperation{Kind: kind, Text: string(value)})
		}
	}
	appendText("unchanged", left[:prefix])
	appendText("deleted", left[prefix:len(left)-suffix])
	appendText("added", right[prefix:len(right)-suffix])
	if suffix > 0 {
		appendText("unchanged", left[len(left)-suffix:])
	}
	return result
}

func nonEmptyOperation(kind, text string) []DiffOperation {
	if text == "" {
		return []DiffOperation{}
	}
	return []DiffOperation{{Kind: kind, Text: text}}
}
