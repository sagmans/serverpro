package credentials

import (
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/privatefile"
)

type setFile struct {
	Set
	Project string `json:"project"`
}

func loadJSON(path string) (Set, error) {
	var file setFile
	if err := privatefile.ReadJSON(path, &file, "credentials"); err != nil {
		return Set{}, err
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
