package eval

import (
	"bufio"
	"fmt"
	"os"

	"github.com/goccy/go-json"
)

func LoadJSONL[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	items := make([]T, 0)
	scanner := bufio.NewScanner(file)

	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	line := 0
	for scanner.Scan() {
		line++

		if len(scanner.Bytes()) == 0 {
			continue
		}

		var item T
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, fmt.Errorf("decode %s line %d: %w", path, line, err)
		}

		items = append(items, item)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
