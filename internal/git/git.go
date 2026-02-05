package git

import (
	"fmt"
	"os"
	"os/exec"
)

func loadRepositoryPath() string {
	path, err := os.Getwd()

	if err != nil {
		fmt.Println("Error loading repository path:", err)
	}
	return path
}

func GetBranchStatus() (string, error) {
	out, err := exec.Command("git", "-C", loadRepositoryPath(), "branch", "--show-current").Output()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s", out), nil
}
