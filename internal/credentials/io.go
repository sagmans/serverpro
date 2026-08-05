package credentials

import (
	"os"

	"github.com/sagmans/serverpro/internal/privatefile"
)

func loadJSON(path string) (Set, error) {
	var creds Set
	if err := privatefile.ReadJSON(path, &creds, "credentials"); err != nil {
		return Set{}, err
	}
	creds.normalizeNamespace()
	return creds, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
