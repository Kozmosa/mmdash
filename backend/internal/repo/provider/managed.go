package provider

import "context"

const managedCanonicalRemote = "managed://mmdash"

// Managed represents a repository whose authoritative bare Git data is
// created and owned by the Repo module below REPO_STORAGE_ROOT. The adapter
// performs no filesystem mutation; Runtime initializes the repository only
// after the tested setting is connected.
type Managed struct{}

func (Managed) Test(_ context.Context, config Config) (Connection, error) {
	branches := map[string]string{
		config.CodeBranch:    zeroSHA,
		config.ArticleBranch: zeroSHA,
		config.ResultBranch:  zeroSHA,
	}
	return Connection{
		Branches:           branches,
		CanonicalRemoteURL: managedCanonicalRemote,
		DefaultBranch:      config.CodeBranch,
		DisplayName:        "mmdash managed repository",
		FetchURL:           "bare.git",
		Provider:           "managed",
	}, nil
}

const zeroSHA = "0000000000000000000000000000000000000000"
