package git

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/joho/godotenv"
)

func loadRepositoryPath() string {
	err := godotenv.Load()
	if err != nil {
		panic("Error loading .env file")
	}
	repoPath := os.Getenv("REPO_PATH")
	return repoPath
}

func GetBranchStatus() (string, error) {
	out, err := exec.Command("git", "-C", loadRepositoryPath(), "branch", "--show-current").Output()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s", out), nil
}
