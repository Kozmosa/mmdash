package experiment

const artifactFolderTimestampLayout = "20060102T150405.000000Z"

func experimentArtifactFolder(item Experiment) []string {
	if item.SourceCommit == "" {
		return nil
	}
	return []string{
		"experiment",
		item.SourceCommit + "_" + item.CreatedAt.UTC().Format(artifactFolderTimestampLayout),
	}
}
