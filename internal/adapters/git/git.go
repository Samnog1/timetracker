package git

import (
	"os"
	"os/exec"
	"strings"
)

type LocalGitRepository struct {
	path string
}

func NewLocalGitRepository() (*LocalGitRepository, error) {
	path, err := loadRepositoryPath()
	if err != nil {
		return nil, err
	}
	return &LocalGitRepository{path: path}, nil
}

func loadRepositoryPath() (string, error) {
	path, err := os.Getwd()

	if err != nil {
		return "", err
	}
	return path, nil
}

func (localGit *LocalGitRepository) GetBranchStatus() (string, error) {
	out, err := exec.Command("git", "-C", localGit.path, "branch", "--show-current").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
