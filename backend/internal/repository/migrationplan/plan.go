package migrationplan

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

type Step struct {
	Version string
	Name    string
	UpSQL   string
}

func LatestVersion(steps []Step) string {
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Version
}

func Validate(steps []Step) error {
	if len(steps) == 0 {
		return fmt.Errorf("migration plan is empty")
	}
	for index, step := range steps {
		if step.Version == "" {
			return fmt.Errorf("migration step %d has empty version", index)
		}
		if step.Name == "" {
			return fmt.Errorf("migration step %s has empty name", step.Version)
		}
		if strings.TrimSpace(step.UpSQL) == "" {
			return fmt.Errorf("migration step %s has empty sql", step.Version)
		}
		if index == 0 {
			continue
		}
		if steps[index-1].Version >= step.Version {
			return fmt.Errorf("migration versions must be strictly increasing: %s then %s", steps[index-1].Version, step.Version)
		}
	}
	return nil
}

func LoadSteps(fsys fs.FS, dir string) ([]Step, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migration dir %s: %w", dir, err)
	}
	steps := make([]Step, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".up.sql") {
			continue
		}
		version, name, err := parseFilename(filename)
		if err != nil {
			return nil, err
		}
		// embed.FS and other io/fs implementations always use slash-separated paths.
		// filepath.Join would inject backslashes on Windows and break reads.
		body, err := fs.ReadFile(fsys, path.Join(dir, filename))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", filename, err)
		}
		steps = append(steps, Step{
			Version: version,
			Name:    name,
			UpSQL:   string(body),
		})
	}
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].Version < steps[j].Version
	})
	if err := Validate(steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func parseFilename(filename string) (string, string, error) {
	trimmed := strings.TrimSuffix(filename, ".up.sql")
	version, name, ok := strings.Cut(trimmed, "_")
	if !ok || strings.TrimSpace(version) == "" || strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("invalid migration filename %q", filename)
	}
	return trimmed, name, nil
}
