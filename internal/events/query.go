package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func ReadTaskOutput(eventLogPath, taskID string) ([]string, error) {
	var lines []string

	for _, path := range logPaths(eventLogPath) {
		ll, err := extractOutput(path, taskID)
		if err != nil {
			continue
		}
		lines = append(lines, ll...)
	}

	return lines, nil
}

func extractOutput(path, taskID string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var ev struct {
			TaskID string                 `json:"task_id"`
			Event  string                 `json:"event"`
			Data   map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.TaskID != taskID || ev.Event != "output" {
			continue
		}
		if line, ok := ev.Data["line"].(string); ok {
			lines = append(lines, line)
		}
	}

	return lines, scanner.Err()
}

func logPaths(base string) []string {
	paths := []string{}
	for i := 3; i >= 1; i-- {
		p := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	paths = append(paths, base)
	return paths
}
