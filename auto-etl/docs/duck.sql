-- run with duckdb from the ./auto-etl directory
CREATE VIEW messages AS
  SELECT * FROM read_parquet('.tmp/output/messages/**/*.parquet');

CREATE VIEW sessions AS
  SELECT * FROM read_parquet('.tmp/output/sessions/**/*.parquet');

CREATE VIEW blobs AS
  SELECT * FROM read_parquet('.tmp/output/blobs/**/*.parquet');

-- SELECT tool_file_path, count(*) 
-- FROM read_parquet('.tmp/output/messages/**/*.parquet') 
-- where 
--   tool_name = 'Read' and 
--   tool_file_path like '/Users/charlie/src/mxre.io/train/%'
-- group by 1
-- order by 2 desc
-- limit 10


-- SELECT tool_file_path, count(*) as reads, count(DISTINCT session_id) as sessions
-- FROM read_parquet('.tmp/output/messages/**/*.parquet')
-- WHERE tool_name = 'Read'
--   AND tool_file_path LIKE '/Users/charlie/src/mxre.io/train/%'
-- GROUP BY 1
-- ORDER BY 2 DESC

-- Which files have the most unique versions (high churn)?
-- SELECT b.file_path,
--        count(*) as versions,
--        min(epoch_ms(m.timestamp)) as first_seen,
--        max(epoch_ms(m.timestamp)) as last_seen
-- FROM read_parquet('.tmp/output/blobs/**/*.parquet') b
-- JOIN read_parquet('.tmp/output/messages/**/*.parquet') m
--   ON b.agent_message_id = m.id
-- GROUP BY b.file_path
-- ORDER BY versions DESC
-- LIMIT 20

-- auto-doc POC
-- run from ./auto-etl directory: cat docs/docs.sql | duckdb
-- identifies doc files, scores them from session behaviour signals

-- CREATE VIEW messages AS
--   SELECT * FROM read_parquet('.tmp/output/messages/**/*.parquet');

-- CREATE VIEW sessions AS
--   SELECT * FROM read_parquet('.tmp/output/sessions/**/*.parquet');

-- CREATE VIEW blobs AS
--   SELECT * FROM read_parquet('.tmp/output/blobs/**/*.parquet');

-- ─────────────────────────────────────────────
-- 1. All doc files seen across sessions
--    Docs = .md, .txt, .rst, files under docs/ specs/ requirements/
-- ─────────────────────────────────────────────
CREATE VIEW doc_reads AS
SELECT
  m.session_id,
  m.tool_file_path,
  m.timestamp,
  m.id as message_id,
  s.workspace
FROM messages m
JOIN sessions s ON s.id = m.session_id
WHERE m.tool_name = 'Read'
  AND m.tool_file_path LIKE '/Users/charlie/src/mxre.io/train/%'
  AND (
    m.tool_file_path LIKE '%.md'
    OR m.tool_file_path LIKE '%.txt'
    OR m.tool_file_path LIKE '%.rst'
    OR m.tool_file_path LIKE '%/docs/%'
    OR m.tool_file_path LIKE '%/specs/%'
    OR m.tool_file_path LIKE '%/requirements/%'
    OR m.tool_file_path LIKE '%/planning/%'
    OR m.tool_file_path LIKE '%README%'
  );

-- ─────────────────────────────────────────────
-- 2. Doc access summary — most read docs
-- ─────────────────────────────────────────────
SELECT '=== MOST READ DOCS ===' as section, '' as tool_file_path, 0 as reads, 0 as sessions, 0.0 as reads_per_session
UNION ALL
SELECT
  '',
  tool_file_path,
  count(*)                        as reads,
  count(DISTINCT session_id)      as sessions,
  round(count(*)::float / count(DISTINCT session_id), 1) as reads_per_session
FROM doc_reads
GROUP BY tool_file_path
HAVING sessions > 1
ORDER BY reads DESC
LIMIT 20;

-- ─────────────────────────────────────────────
-- 3. Confusion signal — docs with high re-read rate per session
--    Re-reading = agent didn't get what it needed first time
-- ─────────────────────────────────────────────
SELECT '=== HIGH RE-READ RATE (confusion signal) ===' as info;

SELECT
  tool_file_path,
  count(*)                        as total_reads,
  count(DISTINCT session_id)      as sessions,
  round(count(*)::float / count(DISTINCT session_id), 1) as reads_per_session
FROM doc_reads
GROUP BY tool_file_path
HAVING sessions > 1
  AND reads_per_session > 2.0
ORDER BY reads_per_session DESC
LIMIT 20;

-- ─────────────────────────────────────────────
-- 4. Web search after doc read signal
--    Agent read a doc then immediately ran a web search = doc was insufficient
-- ─────────────────────────────────────────────
SELECT '=== DOCS FOLLOWED BY WEB SEARCH (insufficiency signal) ===' as info;

SELECT
  dr.tool_file_path,
  count(*) as times_followed_by_search
FROM doc_reads dr
JOIN messages next_msg
  ON next_msg.session_id = dr.session_id
  AND next_msg.timestamp > dr.timestamp
  AND next_msg.timestamp < dr.timestamp + 60000  -- within 60 seconds
  AND (
    next_msg.tool_name = 'WebSearch'
    OR next_msg.tool_name LIKE '%search%'
    OR next_msg.tool_name LIKE '%web%'
  )
GROUP BY dr.tool_file_path
ORDER BY times_followed_by_search DESC
LIMIT 20;

-- ─────────────────────────────────────────────
-- 5. Session outcome by doc — which docs appear in successful vs abandoned sessions
--    Proxy for success: session has at least one Write/Edit after the doc read
-- ─────────────────────────────────────────────
SELECT '=== DOC SUCCESS VS ABANDON RATE ===' as info;

WITH doc_session_outcomes AS (
  SELECT
    dr.tool_file_path,
    dr.session_id,
    -- session is "successful" if there is a write after the doc read
    max(CASE WHEN m2.tool_name IN ('Write', 'Edit')
              AND m2.timestamp > dr.timestamp
             THEN 1 ELSE 0 END) as had_write_after
  FROM doc_reads dr
  LEFT JOIN messages m2 ON m2.session_id = dr.session_id
  GROUP BY dr.tool_file_path, dr.session_id
)
SELECT
  tool_file_path,
  count(*)                                    as sessions,
  sum(had_write_after)                        as sessions_with_write,
  count(*) - sum(had_write_after)             as sessions_without_write,
  round(sum(had_write_after)::float / count(*) * 100, 1) as success_pct
FROM doc_session_outcomes
GROUP BY tool_file_path
HAVING sessions > 2
ORDER BY success_pct ASC  -- worst first
LIMIT 20;

-- ─────────────────────────────────────────────
-- 6. Doc → code linkage
--    Which code files are written after reading each doc?
--    Reveals what each doc is actually used for
-- ─────────────────────────────────────────────
SELECT '=== DOC → CODE LINKAGE ===' as info;

SELECT
  dr.tool_file_path                           as doc,
  m2.tool_file_path                           as code_file_written,
  count(DISTINCT dr.session_id)               as sessions
FROM doc_reads dr
JOIN messages m2
  ON m2.session_id = dr.session_id
  AND m2.tool_name IN ('Write', 'Edit')
  AND m2.timestamp > dr.timestamp
  AND m2.tool_file_path NOT LIKE '%.md'
  AND m2.tool_file_path NOT LIKE '%.txt'
WHERE dr.tool_file_path LIKE '%.md'
GROUP BY doc, code_file_written
HAVING sessions > 1
ORDER BY doc, sessions DESC
LIMIT 50;

-- ─────────────────────────────────────────────
-- 7. Composite doc quality score
--    Higher = doc is helping. Lower = doc is hindering or ignored.
--
--    Score components:
--      success_rate   (0-1)  sessions with write after read
--      reread_penalty        penalise high reads_per_session
--      search_penalty        penalise frequent web searches after read
-- ─────────────────────────────────────────────
SELECT '=== DOC QUALITY SCORES ===' as info;

WITH outcomes AS (
  SELECT
    dr.tool_file_path,
    dr.session_id,
    max(CASE WHEN m2.tool_name IN ('Write', 'Edit')
              AND m2.timestamp > dr.timestamp
             THEN 1 ELSE 0 END) as had_write
  FROM doc_reads dr
  LEFT JOIN messages m2 ON m2.session_id = dr.session_id
  GROUP BY dr.tool_file_path, dr.session_id
),
reads_per AS (
  SELECT
    tool_file_path,
    count(*)                                              as total_reads,
    count(DISTINCT session_id)                            as sessions,
    round(count(*)::float / count(DISTINCT session_id), 2) as reads_per_session
  FROM doc_reads
  GROUP BY tool_file_path
),
scores AS (
  SELECT
    r.tool_file_path,
    r.sessions,
    r.total_reads,
    r.reads_per_session,
    round(avg(o.had_write) * 100, 1)                  as success_pct,
    -- composite: success rate minus penalties for confusion signals
    round(
      avg(o.had_write)                                 -- base: success rate (0-1)
      - least((r.reads_per_session - 1) * 0.1, 0.4)  -- penalty: re-reads above 1
    , 2) as quality_score
  FROM reads_per r
  JOIN outcomes o ON o.tool_file_path = r.tool_file_path
  WHERE r.sessions > 2
  GROUP BY r.tool_file_path, r.sessions, r.total_reads, r.reads_per_session
)
SELECT
  tool_file_path,
  sessions,
  total_reads,
  reads_per_session,
  success_pct,
  quality_score,
  CASE
    WHEN quality_score >= 0.7 THEN '✓ good'
    WHEN quality_score >= 0.4 THEN '~ ok'
    ELSE '✗ needs work'
  END as verdict
FROM scores
ORDER BY quality_score ASC;