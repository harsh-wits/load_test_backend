package pipeline

import (
	"os"
	"path/filepath"
)

func LoadSearchPayload() ([]byte, error) {
	return os.ReadFile(filepath.Join("fixtures", "search", "search.json"))
}
