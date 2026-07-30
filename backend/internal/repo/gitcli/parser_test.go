package gitcli

import "testing"

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestNULParsersPreserveSpecialPaths(t *testing.T) {
	branches, err := ParseBranches([]byte("main\x00" + shaA + "\x00paper\x00" + shaB + "\x00"))
	if err != nil || len(branches) != 2 || branches[0].Name != "main" {
		t.Fatalf("parse branches: %+v, %v", branches, err)
	}

	tree, err := ParseTree([]byte(
		"100644 blob " + shaA + " 12\t目录/file name\n.md\x00" +
			"040000 tree " + shaB + " -\tsubdir\x00",
	))
	if err != nil || len(tree) != 2 ||
		tree[0].Path != "目录/file name\n.md" ||
		tree[0].Size == nil || *tree[0].Size != 12 ||
		tree[1].Size != nil {
		t.Fatalf("parse tree: %+v, %v", tree, err)
	}

	diff, err := ParseDiffNameStatus([]byte(
		"M\x00file name\x00R100\x00old\nname\x00new\nname\x00",
	))
	if err != nil || len(diff) != 2 ||
		diff[1].PreviousPath != "old\nname" ||
		diff[1].Path != "new\nname" {
		t.Fatalf("parse diff: %+v, %v", diff, err)
	}
}

func TestStatusAndWorktreeParsers(t *testing.T) {
	status, err := ParseStatusPorcelainV2([]byte(
		"1 .M N... 100644 100644 100644 " + shaA + " " + shaB + " file name\x00" +
			"2 R. N... 100644 100644 100644 " + shaA + " " + shaB + " R100 new name\x00old name\x00" +
			"? untracked\nname\x00",
	))
	if err != nil || len(status) != 3 ||
		status[1].Path != "new name" ||
		status[1].PreviousPath != "old name" {
		t.Fatalf("parse status: %+v, %v", status, err)
	}

	worktrees, err := ParseWorktrees([]byte(
		"worktree /repo\x00HEAD " + shaA + "\x00bare\x00\x00" +
			"worktree /repo/worktrees/code\x00HEAD " + shaB +
			"\x00branch refs/heads/mmdash/code\x00\x00",
	))
	if err != nil || len(worktrees) != 2 ||
		!worktrees[0].Bare ||
		worktrees[1].Branch != "refs/heads/mmdash/code" {
		t.Fatalf("parse worktrees: %+v, %v", worktrees, err)
	}

	legacyWorktrees, err := ParseWorktrees([]byte(
		"worktree C:/repo\r\nHEAD " + shaA + "\r\nbare\r\n\r\n" +
			"worktree \"C:/repo/worktrees/line\\nname\"\r\nHEAD " + shaB +
			"\r\nbranch refs/heads/mmdash/code\r\n\r\n",
	))
	if err != nil || len(legacyWorktrees) != 2 ||
		legacyWorktrees[1].Path != "C:/repo/worktrees/line\nname" ||
		legacyWorktrees[1].Branch != "refs/heads/mmdash/code" {
		t.Fatalf("parse legacy worktrees: %+v, %v", legacyWorktrees, err)
	}
}
