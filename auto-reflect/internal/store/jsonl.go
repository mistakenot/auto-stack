package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func AppendJSONLine(path string, payload any) error {
	if path == "" {
		return errors.New("jsonl path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPermission); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode jsonl payload: %w", err)
	}
	data = append(data, '\n')

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open jsonl file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire file lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()

	if err := writeAll(file, data); err != nil {
		return fmt.Errorf("append jsonl record: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync jsonl file: %w", err)
	}
	return nil
}

func ReadJSONLines(path string, handle func(lineNumber int, line []byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	reader := bufio.NewReader(file)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("read jsonl line: %w", readErr)
		}
		if len(line) > 0 {
			lineNumber++
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				if err := handle(lineNumber, line); err != nil {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	return nil
}

func writeAll(file *os.File, data []byte) error {
	written := 0
	for written < len(data) {
		n, err := file.Write(data[written:])
		if err != nil {
			return err
		}
		written += n
	}
	return nil
}
