package main

import "testing"

func TestIsUploadAction(t *testing.T) {
	for _, action := range []string{"default", "default\n", "upload"} {
		if !isUploadAction(action) {
			t.Errorf("expected %q to upload", action)
		}
	}
	for _, action := range []string{"", "later", "dismissed"} {
		if isUploadAction(action) {
			t.Errorf("did not expect %q to upload", action)
		}
	}
}
