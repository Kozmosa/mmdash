package article

const artifactFolderTimestampLayout = "20060102T150405.000000Z"

func articleBuildArtifactFolder(build Build) []string {
	timestamp := build.CreatedAt.UTC().Format(artifactFolderTimestampLayout)
	switch build.BuildKind {
	case BuildFormal:
		if build.CommitSHA == "" {
			return nil
		}
		return []string{"article", "build", build.CommitSHA + "_" + timestamp}
	case BuildPreview:
		return []string{"article", "draft", timestamp}
	case BuildTemplateTest:
		return []string{"article", "template", timestamp}
	default:
		return nil
	}
}
