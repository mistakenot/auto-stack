package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitCodeFile is the fixed name of the exit-code file inside a run's RunDir.
// It is the completion contract every adapter shares: the launch wrapper writes
// it atomically on exit, and Status reads it before asking the manager about
// liveness.
const ExitCodeFile = "exit-code"

// LaunchSpec describes the wrapper script WriteLaunchScript produces. The inner
// command (the bash/claude composition) is supplied by the caller as Command;
// the wrapper owns only the completion contract, so exit semantics are
// identical across tmux, ntm and herdr.
type LaunchSpec struct {
	// RunDir is the run's runtime directory. It is exported to the script as
	// $RUN_DIR and holds the exit-code file.
	RunDir string
	// WorkDir is the directory the script cd's into before running Command.
	WorkDir string
	// Command is the inner command line to execute (e.g.
	// `bash -lc "$(cat "$RUN_DIR/command.txt")"`). It may reference $RUN_DIR.
	Command string
	// OutputPath is the file the script tees combined stdout/stderr into.
	OutputPath string
	// ExitPath is the file the script writes the exit code into atomically. It
	// must be <RunDir>/exit-code so ReadExit can find it.
	ExitPath string
}

// WriteLaunchScript writes the wrapper script into RunDir and returns its path.
// The script uses `set -u`, cd's into WorkDir (exiting 97 if that fails), tees
// combined output into OutputPath, runs Command, and writes the exit code
// atomically (temp file then rename) so a healthy run never exposes a partial
// exit-code file.
func WriteLaunchScript(spec *LaunchSpec) (string, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return "", errors.New("launch command is empty")
	}
	// %q-quote the paths; the command is injected literally (the caller owns
	// it). The exit code is written to a .tmp sibling then renamed over
	// EXIT_FILE, which is atomic within a directory.
	body := fmt.Sprintf(`#!/usr/bin/env bash
set -u

RUN_DIR=%[1]q
WORK_DIR=%[2]q
OUTPUT_FILE=%[3]q
EXIT_FILE=%[4]q

exec > >(tee -a "$OUTPUT_FILE") 2>&1
cd "$WORK_DIR" || exit 97

%[5]s
code=$?
printf '%%s\n' "$code" > "$EXIT_FILE.tmp" && mv "$EXIT_FILE.tmp" "$EXIT_FILE"
exit "$code"
`, spec.RunDir, spec.WorkDir, spec.OutputPath, spec.ExitPath, spec.Command)

	path := filepath.Join(spec.RunDir, "launch.sh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { //nolint:gosec // launch script must be executable
		return "", fmt.Errorf("write launch script: %w", err)
	}
	return path, nil
}

// ReadExit reads the exit-code file in runDir. It reports:
//
//   - (code, true, nil)  — the file holds a parseable integer.
//   - (nil, true, nil)   — the file exists but is empty or malformed, i.e. the
//     run exited but no usable code was recorded (a legacy or interrupted
//     write). This is "exited, code unknown", never an error, so it can never
//     pin a run to running.
//   - (nil, false, nil)  — the file does not exist yet: no exit recorded.
//   - (nil, false, err)  — the file could not be read for another reason.
func ReadExit(runDir string) (code *int, exited bool, err error) {
	if strings.TrimSpace(runDir) == "" {
		return nil, false, nil
	}
	data, readErr := os.ReadFile(filepath.Join(runDir, ExitCodeFile))
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read exit code: %w", readErr)
	}
	n, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil {
		// An empty or malformed file means the run exited but recorded no
		// usable code (a legacy or interrupted write). That is "exited, code
		// unknown" — deliberately not a read error, so it can never pin a run
		// to running.
		return nil, true, nil //nolint:nilerr // malformed exit file is "exited, code unknown", not an error
	}
	return &n, true, nil
}
