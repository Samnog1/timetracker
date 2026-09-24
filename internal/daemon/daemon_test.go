package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockStateUsesOmarchyOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "omarchy")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	locked, err := lockState()
	if err != nil || !locked {
		t.Fatalf("locked = %v, err = %v", locked, err)
	}
}

func TestIsResumeAction(t *testing.T) {
	for _, action := range []string{"default", "default\n", "resume"} {
		if !isResumeAction(action) {
			t.Errorf("expected %q to resume", action)
		}
	}
	for _, action := range []string{"", "later", "dismissed"} {
		if isResumeAction(action) {
			t.Errorf("did not expect %q to resume", action)
		}
	}
}
