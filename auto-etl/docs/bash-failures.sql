-- run with: cat bash-failures.sql | duckdb
-- from the ./auto-etl directory

CREATE VIEW messages AS
  SELECT * FROM read_parquet('.tmp/output/messages/**/*.parquet');

-- ─────────────────────────────────────────────
-- 1. Find bash tool_use messages and pair with their tool_result
--    A failed command = tool_result content contains error signals
-- ─────────────────────────────────────────────
CREATE VIEW bash_pairs AS
SELECT
  cmd.session_id,
  cmd.id as cmd_message_id,
  cmd.bash_command,
  cmd.timestamp as cmd_ts,
  result.content as result_content,
  result.id as result_message_id,
  -- heuristic: check for common error patterns in tool result
  CASE WHEN result.content LIKE '%error%'
         OR result.content LIKE '%Error%'
         OR result.content LIKE '%ERROR%'
         OR result.content LIKE '%fatal%'
         OR result.content LIKE '%FATAL%'
         OR result.content LIKE '%panic%'
         OR result.content LIKE '%not found%'
         OR result.content LIKE '%No such file%'
         OR result.content LIKE '%command not found%'
         OR result.content LIKE '%permission denied%'
         OR result.content LIKE '%Exit code 1%'
         OR result.content LIKE '%Exit code 2%'
         OR result.content LIKE '%FAILED%'
         OR result.content LIKE '%failed%'
         OR result.content LIKE '%segfault%'
         OR result.content LIKE '%traceback%'
         OR result.content LIKE '%Traceback%'
         OR result.content LIKE '%exception%'
         OR result.content LIKE '%Exception%'
    THEN true ELSE false END as likely_failed
FROM messages cmd
JOIN messages result
  ON result.session_id = cmd.session_id
  AND result.role = 'tool'
  AND result.index = cmd.index + 1
WHERE cmd.tool_name = 'Bash'
  AND cmd.bash_command != '';

-- ─────────────────────────────────────────────
-- 2. Overall failure rate
-- ─────────────────────────────────────────────
SELECT '=== BASH COMMAND FAILURE RATE ===' as info;

SELECT
  count(*) as total_bash,
  sum(likely_failed::int) as failed,
  count(*) - sum(likely_failed::int) as succeeded,
  round(sum(likely_failed::int)::float / count(*) * 100, 1) as failure_pct
FROM bash_pairs;

-- ─────────────────────────────────────────────
-- 3. Most repeated failing commands
--    Same command failing across multiple sessions = systemic issue
-- ─────────────────────────────────────────────
SELECT '=== MOST COMMON FAILING COMMANDS ===' as info;

SELECT
  bash_command,
  count(*) as failures,
  count(DISTINCT session_id) as sessions
FROM bash_pairs
WHERE likely_failed
GROUP BY bash_command
ORDER BY failures DESC
LIMIT 20;

-- ─────────────────────────────────────────────
-- 4. Retry loops — same command run multiple times in a session
--    Indicates agent is stuck retrying something that keeps failing
-- ─────────────────────────────────────────────
SELECT '=== RETRY LOOPS (same command repeated in session) ===' as info;

SELECT
  bash_command,
  session_id,
  count(*) as attempts,
  sum(likely_failed::int) as failures
FROM bash_pairs
GROUP BY bash_command, session_id
HAVING count(*) >= 3 AND sum(likely_failed::int) >= 2
ORDER BY attempts DESC
LIMIT 20;

-- ─────────────────────────────────────────────
-- 5. Failure rate by command prefix (first word)
--    Which tools/binaries fail most often?
-- ─────────────────────────────────────────────
SELECT '=== FAILURE RATE BY COMMAND (first word) ===' as info;

SELECT
  split_part(trim(bash_command), ' ', 1) as cmd_prefix,
  count(*) as total,
  sum(likely_failed::int) as failed,
  round(sum(likely_failed::int)::float / count(*) * 100, 1) as failure_pct
FROM bash_pairs
WHERE bash_command != ''
GROUP BY cmd_prefix
HAVING total >= 5
ORDER BY failure_pct DESC
LIMIT 20;

-- ─────────────────────────────────────────────
-- 6. Sessions with highest failure rates
--    Which sessions had the agent struggling most?
-- ─────────────────────────────────────────────
SELECT '=== SESSIONS WITH MOST BASH FAILURES ===' as info;

SELECT
  session_id,
  count(*) as total_bash,
  sum(likely_failed::int) as failed,
  round(sum(likely_failed::int)::float / count(*) * 100, 1) as failure_pct
FROM bash_pairs
GROUP BY session_id
HAVING total_bash >= 5
ORDER BY failure_pct DESC
LIMIT 15;
