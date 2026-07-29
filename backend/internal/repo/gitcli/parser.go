package gitcli

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Branch is one fetched remote branch.
type Branch struct {
	CommitSHA string
	Name      string
}

// StatusEntry is one dirty worktree entry from porcelain v2.
type StatusEntry struct {
	Path         string
	PreviousPath string
	RecordType   string
	XY           string
}

// TreeEntry is one raw immutable Git tree entry.
type TreeEntry struct {
	Mode     string
	ObjectID string
	Path     string
	Size     *int64
	Type     string
}

// DiffEntry is one normalized name-status change.
type DiffEntry struct {
	Path         string
	PreviousPath string
	Status       string
}

// Worktree is one linked or detached Git worktree.
type Worktree struct {
	Bare     bool
	Branch   string
	Detached bool
	Head     string
	Path     string
	Prunable bool
}

// ParseBranches parses reviewed NUL-separated name/SHA pairs.
func ParseBranches(contents []byte) ([]Branch, error) {
	fields := nulFields(contents)
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("parse branches: incomplete record")
	}
	branches := make([]Branch, 0, len(fields)/2)
	for index := 0; index < len(fields); index += 2 {
		if ValidateBranch(fields[index]) != nil || ValidateFullSHA(fields[index+1]) != nil {
			return nil, fmt.Errorf("parse branches: invalid record")
		}
		branches = append(branches, Branch{Name: fields[index], CommitSHA: fields[index+1]})
	}
	return branches, nil
}

// ParseStatusPorcelainV2 parses -z output without splitting valid path whitespace.
func ParseStatusPorcelainV2(contents []byte) ([]StatusEntry, error) {
	records := nulFields(contents)
	entries := []StatusEntry{}
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" || strings.HasPrefix(record, "# ") {
			continue
		}
		switch record[0] {
		case '?', '!':
			if len(record) < 3 {
				return nil, fmt.Errorf("parse status: invalid untracked record")
			}
			entries = append(entries, StatusEntry{
				Path: record[2:], RecordType: record[:1], XY: record[:1],
			})
		case '1':
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 {
				return nil, fmt.Errorf("parse status: invalid ordinary record")
			}
			entries = append(entries, StatusEntry{
				Path: fields[8], RecordType: "1", XY: fields[1],
			})
		case '2':
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || index+1 >= len(records) {
				return nil, fmt.Errorf("parse status: invalid rename record")
			}
			index++
			entries = append(entries, StatusEntry{
				Path: fields[9], PreviousPath: records[index],
				RecordType: "2", XY: fields[1],
			})
		case 'u':
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 {
				return nil, fmt.Errorf("parse status: invalid unmerged record")
			}
			entries = append(entries, StatusEntry{
				Path: fields[10], RecordType: "u", XY: fields[1],
			})
		default:
			return nil, fmt.Errorf("parse status: unknown record")
		}
	}
	return entries, nil
}

// ParseTree parses `git ls-tree -z -l` output.
func ParseTree(contents []byte) ([]TreeEntry, error) {
	records := nulFields(contents)
	entries := make([]TreeEntry, 0, len(records))
	for _, record := range records {
		if record == "" {
			continue
		}
		header, path, found := splitOnce(record, "\t")
		if !found || path == "" {
			return nil, fmt.Errorf("parse tree: missing path")
		}
		fields := strings.Fields(header)
		if len(fields) != 4 || ValidateFullSHA(fields[2]) != nil {
			return nil, fmt.Errorf("parse tree: invalid header")
		}
		var size *int64
		if fields[3] != "-" {
			value, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil || value < 0 {
				return nil, fmt.Errorf("parse tree: invalid size")
			}
			size = &value
		}
		entries = append(entries, TreeEntry{
			Mode: fields[0], Type: fields[1], ObjectID: fields[2],
			Size: size, Path: path,
		})
	}
	return entries, nil
}

// ParseDiffNameStatus parses `git diff-tree --name-status -z` output.
func ParseDiffNameStatus(contents []byte) ([]DiffEntry, error) {
	fields := nulFields(contents)
	entries := []DiffEntry{}
	for index := 0; index < len(fields); {
		status := fields[index]
		index++
		if status == "" || index >= len(fields) {
			return nil, fmt.Errorf("parse diff: incomplete status")
		}
		if status[0] == 'R' || status[0] == 'C' {
			if index+1 >= len(fields) {
				return nil, fmt.Errorf("parse diff: incomplete rename")
			}
			entries = append(entries, DiffEntry{
				Status: status, PreviousPath: fields[index], Path: fields[index+1],
			})
			index += 2
			continue
		}
		entries = append(entries, DiffEntry{Status: status, Path: fields[index]})
		index++
	}
	return entries, nil
}

// ParseWorktrees parses `git worktree list --porcelain -z` output.
func ParseWorktrees(contents []byte) ([]Worktree, error) {
	fields := bytes.Split(contents, []byte{0})
	worktrees := []Worktree{}
	current := Worktree{}
	hasCurrent := false
	flush := func() error {
		if !hasCurrent {
			return nil
		}
		if current.Path == "" {
			return fmt.Errorf("parse worktree: missing path")
		}
		worktrees = append(worktrees, current)
		current = Worktree{}
		hasCurrent = false
		return nil
	}
	for _, raw := range fields {
		field := string(raw)
		if field == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		hasCurrent = true
		key, value, _ := splitOnce(field, " ")
		switch key {
		case "worktree":
			current.Path = value
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = value
		case "bare":
			current.Bare = true
		case "detached":
			current.Detached = true
		case "prunable":
			current.Prunable = true
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return worktrees, nil
}

func splitOnce(value, separator string) (string, string, bool) {
	index := strings.Index(value, separator)
	if index < 0 {
		return value, "", false
	}
	return value[:index], value[index+len(separator):], true
}

func nulFields(contents []byte) []string {
	raw := bytes.Split(contents, []byte{0})
	if len(raw) > 0 && len(raw[len(raw)-1]) == 0 {
		raw = raw[:len(raw)-1]
	}
	fields := make([]string, len(raw))
	for index, value := range raw {
		fields[index] = string(value)
	}
	return fields
}
