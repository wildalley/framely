package translate

import (
	"context"
	"os"
	"testing"
)

// Downloads the published models; set FRAMELY_MODEL_DOWNLOAD_DIR to run.
func TestPublishedModelsDownloadAndVerify(t *testing.T) {
	root := os.Getenv("FRAMELY_MODEL_DOWNLOAD_DIR")
	if root == "" {
		t.Skip("set FRAMELY_MODEL_DOWNLOAD_DIR to download the published models")
	}
	for _, m := range Models {
		if err := Download(context.Background(), nil, root, m, nil); err != nil {
			t.Fatal(m.Direction, err)
		}
		if !Installed(root, m) {
			t.Fatal(m.Direction, "not installed")
		}
	}
}
