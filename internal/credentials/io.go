package credentials

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/privatefile"
)

type setFile struct {
	Set
	Project string `json:"project"`
}

// loadJSON reads one credential file through the private-file checks, then
// decrypts it (or accepts legacy plaintext) before the JSON schema rules run.
func loadJSON(path string) (Set, error) {
	body, err := privatefile.ReadPrivate(path, credentialFileKind)
	if err != nil {
		return Set{}, err
	}
	plaintext, err := decodeCredentialFile(path, body)
	if err != nil {
		return Set{}, err
	}
	var file setFile
	if err := json.Unmarshal(plaintext, &file); err != nil {
		return Set{}, fmt.Errorf("parse credentials %s: %w", path, err)
	}
	if file.Namespace != "" && file.Project != "" && file.Namespace != file.Project {
		return Set{}, fmt.Errorf("credentials namespace %q conflicts with legacy project %q", file.Namespace, file.Project)
	}
	if file.Namespace == "" {
		file.Namespace = file.Project
	}
	return file.Set, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
