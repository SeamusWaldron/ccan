package discovery

import (
	"os"
	"path/filepath"
	"strings"
)

// Project represents a discovered Claude Code project directory.
type Project struct {
	EncodedPath  string // directory name under ~/.claude/projects
	DecodedGuess string // best-effort decoded absolute path
	Dir          string // absolute path to the project directory
	SessionFiles []string
}

// DiscoverProjects walks projectsDir and returns all directories
// that contain at least one .jsonl file.
func DiscoverProjects(projectsDir string) ([]Project, error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	var projects []Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, e.Name())
		files, err := jsonlFiles(dir)
		if err != nil || len(files) == 0 {
			continue
		}
		projects = append(projects, Project{
			EncodedPath:  e.Name(),
			DecodedGuess: DecodePath(e.Name()),
			Dir:          dir,
			SessionFiles: files,
		})
	}
	return projects, nil
}

// jsonlFiles returns all .jsonl files in a directory (non-recursive).
func jsonlFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// DecodePath converts an encoded project directory name back to an
// approximate absolute path. The encoding replaces '/' with '-' and
// prefixes with '-', so '-Users-alice-Dev-project' → '/Users/alice/Dev/project'.
// This is a best-effort guess; the decoded value is never used as a unique key.
func DecodePath(encoded string) string {
	if strings.HasPrefix(encoded, "-") {
		return "/" + strings.ReplaceAll(encoded[1:], "-", "/")
	}
	return encoded
}
