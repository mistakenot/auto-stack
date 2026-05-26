---
name: graph-context
kind: service
model: sonnet
---

requires:
- project_dir: absolute path to the project directory
- question: the feature-planning question with context about what we're trying to do
- seed_files: newline-separated list of seed file paths relative to project root

ensures:
- graph_bundle: the full context pack output from the autograph CLI tool
- graph_token_count: the approximate token count of the graph context bundle
- graph_metadata: JSON with fields: tool_used, token_limit, seed_files, exit_code, stderr (if any)

## Instructions

You are running the `autograph code context` CLI tool to generate a context bundle. You must run this CLEAN — as if you were a developer invoking the tool from their terminal with no prior knowledge of the codebase.

### Steps

1. Read the `question` and `seed_files` inputs to understand the task.

2. Detect the project language:
   - If `go.mod` exists in `project_dir`: language is `go`
   - If `tsconfig.json` exists in `project_dir`: language is `typescript`

3. Run the autograph CLI to build the context pack. The binary is built from the auto-graph subproject. Run it like this:

   ```bash
   cd /home/vscode/src/auto-stack/auto-graph && go run ./cmd/autograph code context <project_dir> \
     --lang <language> \
     --token-limit 8000 \
     --file <seed_file_1> \
     --file <seed_file_2> \
     ...
   ```

   Use a token limit of 8000 tokens. This is a reasonable budget for an LLM context window slice.

4. Capture the full stdout output as the `context_bundle`.

5. Count the approximate tokens in the output (rough estimate: word_count * 1.3).

6. Record metadata about the run.

### Important

- Do NOT read any source files yourself. The CLI tool does all the work.
- Do NOT modify the output. Save it exactly as the tool produces it.
- If the tool errors, save the error output and note the failure in metadata.
- You are simulating a clean, no-context invocation of the tool.
