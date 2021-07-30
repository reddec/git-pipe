package internal

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func ReadEnvFile(filename string) (map[string]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	ans := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}

		kv := strings.SplitN(line, "=", 2) //nolint:gomnd
		ans[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}

	return ans, scanner.Err()
}
