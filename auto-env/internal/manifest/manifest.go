package manifest

import (
	"os"
	"strings"
)

func Write(path string, files []string) error {
	content := strings.Join(files, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func Read(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
