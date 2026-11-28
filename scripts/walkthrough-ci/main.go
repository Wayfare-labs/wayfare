package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const annotation = "walkthrough-ci"

var commands = map[string][]string{
	"make test": {"make", "test"},
	"go run ./cmd/wayfared -verify-store -data ./data": {"go", "run", "./cmd/wayfared", "-verify-store", "-data", "./data"},
	"go run ./cmd/hop-analysis -snapshots ./testdata/snapshots": {
		"go", "run", "./cmd/hop-analysis", "-snapshots", "./testdata/snapshots",
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	doc, err := os.Open("docs/first-15-minutes.md")
	if err != nil {
		return fmt.Errorf("open walkthrough: %w", err)
	}
	defer doc.Close()

	extracted, err := extractCommands(doc)
	if err != nil {
		return err
	}
	if len(extracted) == 0 {
		return fmt.Errorf("walkthrough contains no annotated commands")
	}
	for _, line := range extracted {
		fmt.Printf("==> %s\n", line)
		cmd := exec.Command(commands[line][0], commands[line][1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("walkthrough command %q failed: %w", line, err)
		}
	}
	return nil
}

func extractCommands(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	var (
		result      []string
		inFence     bool
		annotated   bool
		openingSize int
		lineNo      int
		block       []string
	)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		fenceSize := len(line) - len(strings.TrimLeft(line, "`"))
		if !inFence {
			if fenceSize < 3 {
				continue
			}
			info := strings.TrimSpace(line[fenceSize:])
			annotated = strings.Contains(info, annotation)
			if annotated && (fenceSize != 3 || info != "bash "+annotation) {
				return nil, fmt.Errorf("line %d: annotated fence must be exactly ```bash %s", lineNo, annotation)
			}
			inFence, openingSize = true, fenceSize
			block = nil
			continue
		}

		if fenceSize >= openingSize && strings.TrimSpace(line[fenceSize:]) == "" {
			if annotated && (fenceSize != openingSize || line != "```") {
				return nil, fmt.Errorf("line %d: annotated fence must close with ```", lineNo)
			}
			if annotated {
				if len(block) == 0 {
					return nil, fmt.Errorf("line %d: annotated command block is empty", lineNo)
				}
				result = append(result, block...)
			}
			inFence, annotated, openingSize = false, false, 0
			block = nil
			continue
		}
		if !annotated {
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := commands[line]; !ok {
			return nil, fmt.Errorf("line %d: unsupported walkthrough command %q", lineNo, line)
		}
		block = append(block, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read walkthrough: %w", err)
	}
	if inFence && annotated {
		return nil, fmt.Errorf("walkthrough ends inside an annotated command block")
	}
	return result, nil
}
