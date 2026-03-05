package microfts2

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CRC: crc-Chunker.md

// RunChunker executes a chunking command for a file and returns byte offsets
// defining chunk boundaries. The command receives the filepath as an argument
// and outputs one byte offset per line on stdout.
func RunChunker(cmd, filepath string) ([]int64, error) {
	c := exec.Command("sh", "-c", cmd+` "$1"`, "--", filepath)
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("chunker %q: %w", cmd, err)
	}
	var offsets []int64
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		off, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("chunker output: invalid offset %q: %w", line, err)
		}
		offsets = append(offsets, off)
	}
	return offsets, scanner.Err()
}
