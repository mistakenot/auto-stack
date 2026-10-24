#!/usr/bin/env bash
# herdr-worker.sh — launch background coding agents under herdr, the same way
# every time. Bundled with the delegate / delegate-task skills.
#
# Two launch shapes, chosen by the caller:
#
#   worker   a fresh git worktree on its own branch, in its own Space nested
#            under the repo's primary Space; tab + agent named work-<slug>
#   planner  the primary checkout, which must be on main/master, as a new tab in
#            the repo's primary Space; named plan-<slug>
#
# --repo may be any path in the repo, including a linked worktree: launches
# always resolve the primary checkout first, so placement never varies.
#
#   herdr-worker.sh worker  --slug S --prompt TEXT [--repo PATH] [--agent claude|codex]
#                           [--branch NAME] [--base REF] [--timeout MS]
#   herdr-worker.sh planner --slug S --prompt TEXT [--repo PATH] [--agent claude|codex]
#                           [--timeout MS]
#   herdr-worker.sh verify  --name work-S|plan-S [--agent claude|codex]
#   herdr-worker.sh list    [--repo PATH]
#   herdr-worker.sh reap    --name work-S|plan-S [--force]
#   herdr-worker.sh prompt  --name work-S|plan-S --prompt TEXT     # follow-up prompt
#
# Each subcommand prints one JSON object on stdout; exit 0 means ok:true. On
# failure the object is {"ok":false,"error":"<code>",...} with whatever the
# caller needs to act on it (pane snapshot, argv, herdr's error). The script
# never answers a dialog on the agent's behalf: interstitials are reported.
#
# Requires herdr >= 0.8.2, jq, git.

set -euo pipefail

MIN_HERDR="0.8.2"
START_TIMEOUT_MS=90000
KICKOFF_TIMEOUT_MS=20000
DEFAULT_BASE="origin/main"

# --- helpers -----------------------------------------------------------------

die() { # die <code> [jq --arg k v ...]
  local code="$1"; shift
  jq -cn --arg error "$code" "$@" '{ok:false,error:$error} + ($ARGS.named | del(.error))'
  exit 1
}

need() { command -v "$1" >/dev/null 2>&1 || die missing_dependency --arg dependency "$1"; }

flags_for() {
  local f
  case "$1" in
    claude) f="--dangerously-skip-permissions" ;;
    codex)  f="--dangerously-bypass-approvals-and-sandbox" ;;
    *) die unsupported_kind --arg kind "$1" --arg supported "claude codex" ;;
  esac
  echo "$f"
}

herdr_json() { # run herdr, always hand back JSON (herdr puts its errors on stdout)
  local out
  if ! out=$(herdr "$@" 2>&1); then
    echo "$out" | jq -e . >/dev/null 2>&1 && { echo "$out"; return 0; }
    die herdr_failed --arg herdr_args "$*" --arg output "$out"
  fi
  echo "$out"
}

herdr_err_code() { echo "$1" | jq -r '.error.code // empty' 2>/dev/null || true; }

snapshot() { herdr pane read "$1" --source visible --format text 2>/dev/null | grep -v '^[[:space:]]*$' | tail -n 25 || true; }

agent_by_name() { herdr agent list | jq -c --arg n "$1" '.result.agents[] | select(.name==$n)'; }

process_cmdlines() { herdr pane process-info --pane "$1" | jq -r '.result.process_info.foreground_processes[]?.cmdline'; }
process_cwd()      { herdr pane process-info --pane "$1" | jq -r '.result.process_info.foreground_processes[0]?.cwd // empty'; }

is_linked_worktree() { # <path>
  local gd cd
  gd=$(git -C "$1" rev-parse --git-dir 2>/dev/null) || return 1
  cd=$(git -C "$1" rev-parse --git-common-dir 2>/dev/null) || return 1
  [ "$(cd "$1" && realpath "$gd")" != "$(cd "$1" && realpath "$cd")" ]
}

# herdr groups a worktree Space under its repo's primary Space only when the
# worktree is created from the primary checkout; from a linked worktree it fails
# with linked_worktree_source. So every launch resolves the primary first.
repo_source() { # <path in repo> -> {source_checkout_path, source_workspace_id?}
  local src
  src=$(herdr worktree list --cwd "$1" 2>/dev/null | jq -c '.result.source // empty' || true)
  [ -n "$src" ] || die not_a_herdr_repo --arg repo "$1" --arg hint "herdr worktree list could not resolve this repo"
  echo "$src"
}

check_prereqs() {
  need herdr; need jq; need git
  local v; v=$(herdr --version 2>/dev/null | awk '{print $2}')
  [ "$(printf '%s\n%s\n' "$MIN_HERDR" "$v" | sort -V | head -n1)" = "$MIN_HERDR" ] \
    || die herdr_too_old --arg installed "$v" --arg required "$MIN_HERDR"
  herdr agent list >/dev/null 2>&1 \
    || die herdr_server_unreachable --arg hint "start the herdr server from a plain terminal, not from inside an agent session"
}

check_integration() { # <kind>
  local line; line=$(herdr integration status 2>/dev/null | grep -E "^$1:" || true)
  case "$line" in
    ""|*"not installed"*) die integration_missing --arg kind "$1" --arg fix "herdr integration install $1" ;;
    *outdated*) echo "warning: herdr integration for $1 is outdated: $line" >&2 ;;
  esac
}

# verify_worker <name> <kind> [expected_cwd] [expected_role] -> JSON; 0 iff ok
verify_worker() {
  local name="$1" kind="$2" expected_cwd="${3:-}" expected_role="${4:-}"
  local rec pane flag cmdlines cwd role reasons=() flags_ok=false cwd_ok=true role_ok=true
  rec=$(agent_by_name "$name")
  [ -n "$rec" ] || { jq -cn --arg name "$name" '{ok:false,name:$name,reasons:["no live agent with that name"]}'; return 1; }
  pane=$(echo "$rec" | jq -r .pane_id)
  flag=$(flags_for "$kind")
  cmdlines=$(process_cmdlines "$pane")
  if [ -z "$cmdlines" ] || ! echo "$cmdlines" | grep -qE "(^|/)(claude|codex|node)( |$)"; then
    reasons+=("pane is not running an agent process")
  elif echo "$cmdlines" | grep -q -- "--permission-mode auto"; then
    reasons+=("launched with --permission-mode auto: its classifier blocks gh pr merge; reap and respawn")
  elif echo "$cmdlines" | grep -q -- "--resume" && ! echo "$cmdlines" | grep -qF -- "$flag"; then
    reasons+=("resumed by a herdr server restart without the permission flag")
  elif ! echo "$cmdlines" | grep -qF -- "$flag"; then
    reasons+=("argv lacks $flag (manual approval mode)")
  else
    flags_ok=true
  fi
  cwd=$(process_cwd "$pane")
  if [ -n "$expected_cwd" ] && [ "$(realpath "$cwd" 2>/dev/null || echo "$cwd")" != "$(realpath "$expected_cwd")" ]; then
    cwd_ok=false; reasons+=("cwd is $cwd, expected $expected_cwd")
  fi
  if is_linked_worktree "$cwd"; then role=worker; else role=planner; fi
  if [ -n "$expected_role" ] && [ "$role" != "$expected_role" ]; then
    role_ok=false; reasons+=("running as $role (by checkout), expected $expected_role")
  fi
  local ok=false; [ ${#reasons[@]} -eq 0 ] && ok=true
  jq -cn --arg name "$name" --arg pane "$pane" --arg kind "$kind" --arg cwd "$cwd" --arg role "$role" \
        --argjson ok "$ok" --argjson flags_ok "$flags_ok" --argjson cwd_ok "$cwd_ok" --argjson role_ok "$role_ok" \
        --arg cmdline "$(echo "$cmdlines" | head -n1)" \
        --argjson reasons "$(printf '%s\n' "${reasons[@]:-}" | jq -R . | jq -sc 'map(select(length>0))')" \
        --argjson status "$(echo "$rec" | jq '{agent_status, interactive_ready, workspace_id, tab_id}')" \
        '{ok:$ok,name:$name,pane_id:$pane,kind:$kind,cwd:$cwd,role:$role,cmdline:$cmdline,
          checks:{flags:$flags_ok,cwd:$cwd_ok,role:$role_ok},reasons:$reasons} + $status'
  $ok
}

# do_prompt <name> <text> <kickoff_timeout_ms> -> JSON; 0 iff the prompt landed
do_prompt() {
  local name="$1" text="$2" kt="$3" rec pane seq_before sent code seq_after waited
  rec=$(agent_by_name "$name")
  [ -n "$rec" ] || { jq -cn --arg n "$name" '{ok:false,error:"no_such_worker",name:$n}'; return 1; }
  pane=$(echo "$rec" | jq -r .pane_id); seq_before=$(echo "$rec" | jq -r .state_change_seq)

  sent=$(herdr agent prompt "$name" "$text" 2>&1 || true); code=$(herdr_err_code "$sent")
  if [ "$code" = agent_prompt_stalled ]; then      # documented: retry exactly once
    sleep 2; sent=$(herdr agent prompt "$name" "$text" 2>&1 || true); code=$(herdr_err_code "$sent")
  fi
  case "$code" in
    "") ;;
    agent_blocked)
      jq -cn --arg n "$name" --arg pane "$pane" --arg snap "$(snapshot "$pane")" \
        '{ok:false,error:"agent_blocked",name:$n,pane_id:$pane,snapshot:$snap,
          hint:"the agent is at a dialog; answer it with agent send-keys per references/herdr/unblock-worker.md, then prompt again"}'
      return 1 ;;
    *)
      jq -cn --arg n "$name" --arg pane "$pane" --arg code "$code" --arg raw "$sent" --arg snap "$(snapshot "$pane")" \
        '{ok:false,error:("prompt_"+$code),name:$n,pane_id:$pane,herdr:$raw,snapshot:$snap}'
      return 1 ;;
  esac

  # Kickoff evidence: `working` observed, or the state sequence moved (a short
  # prompt can finish before we look).
  waited=$(herdr agent wait "$name" --until working --timeout "$kt" 2>&1 || true)
  seq_after=$(agent_by_name "$name" | jq -r '.state_change_seq // 0')
  if echo "$waited" | jq -e '.error' >/dev/null 2>&1 && [ "$seq_after" -le "$seq_before" ]; then
    jq -cn --arg n "$name" --arg pane "$pane" --arg snap "$(snapshot "$pane")" \
      '{ok:false,error:"kickoff_not_observed",name:$n,pane_id:$pane,snapshot:$snap,
        hint:"the prompt may not have landed; read the pane before retrying"}'
    return 1
  fi
  jq -cn --arg n "$name" --argjson status "$(agent_by_name "$name" | jq '{agent_status,state_change_seq}')" \
    '{ok:true,name:$n,kicked_off:true} + $status'
}

# --- launch (shared by worker / planner) --------------------------------------

# launch <role> <name> <kind> <pane> <workspace> <tab> <expected_cwd> <prompt> <timeout> <extra-json>
launch() {
  local role="$1" name="$2" kind="$3" pane="$4" workspace="$5" tab="$6" expected_cwd="$7" prompt="$8" timeout="$9" extra="${10}"
  local flag started code verdict kickoff
  flag=$(flags_for "$kind")

  herdr tab rename "$tab" "$name" >/dev/null 2>&1 || true   # sidebar shows work-/plan- prefix

  # Pattern A only: no argv prompt, so `agent start` returns as soon as it is ready.
  started=$(herdr_json agent start "$name" --kind "$kind" --pane "$pane" --timeout "$timeout" -- "$flag")
  code=$(herdr_err_code "$started")
  case "$code" in
    "") ;;
    timeout)
      # Usually alive with the name unbound: rebind rather than respawn.
      if process_cmdlines "$pane" | grep -qF -- "$flag"; then
        herdr agent rename "$pane" "$name" >/dev/null 2>&1 || true
      else
        die start_timeout --arg name "$name" --arg pane "$pane" --arg workspace "$workspace" \
            --arg snapshot "$(snapshot "$pane")" --arg hint "no flagged agent in the pane; reap and retry"
      fi ;;
    agent_not_ready)
      die agent_not_ready --arg name "$name" --arg pane "$pane" --arg workspace "$workspace" \
          --arg snapshot "$(snapshot "$pane")" \
          --arg hint "startup interstitial: answer it deliberately with agent send-keys (references/herdr/unblock-worker.md), then run the verify and prompt subcommands for $name" ;;
    *) die start_failed --argjson herdr_error "$(echo "$started" | jq .error)" --arg name "$name" --arg pane "$pane" --arg workspace "$workspace" ;;
  esac

  if ! verdict=$(verify_worker "$name" "$kind" "$expected_cwd" "$role"); then
    die verify_failed --argjson verdict "$verdict" --arg workspace "$workspace" \
        --arg hint "reap and relaunch; permission mode cannot be changed in place"
  fi

  if ! kickoff=$(do_prompt "$name" "$prompt" "$KICKOFF_TIMEOUT_MS"); then
    jq -cn --argjson v "$verdict" --argjson k "$kickoff" --arg ws "$workspace" --argjson extra "$extra" \
      '{ok:false,error:"kickoff_failed",launched:true,workspace_id:$ws,verify:$v,kickoff:$k} + $extra'
    exit 1
  fi

  jq -cn --argjson v "$verdict" --argjson k "$kickoff" --arg ws "$workspace" --arg tab "$tab" --arg role "$role" --argjson extra "$extra" \
    '{ok:true,role:$role,name:$v.name,kind:$v.kind,pane_id:$v.pane_id,workspace_id:$ws,tab_id:$tab,cwd:$v.cwd,
      cmdline:$v.cmdline,verified:$v.checks,kickoff:$k,
      follow:{read:("herdr agent read "+$v.name+" --source recent --lines 120"),
              status:("herdr agent get "+$v.name),
              reap:($ENV.SELF+" reap --name "+$v.name)}} + $extra'
}

parse_launch_args() { # sets globals: slug prompt repo kind branch base timeout
  slug=""; prompt=""; repo=""; kind="claude"; branch=""; base="$DEFAULT_BASE"; timeout="$START_TIMEOUT_MS"
  while [ $# -gt 0 ]; do
    case "$1" in
      --slug) slug="$2"; shift 2 ;;
      --prompt) prompt="$2"; shift 2 ;;
      --repo) repo="$2"; shift 2 ;;
      --agent|--kind) kind="$2"; shift 2 ;;
      --branch) branch="$2"; shift 2 ;;
      --base) base="$2"; shift 2 ;;
      --timeout) timeout="$2"; shift 2 ;;
      *) die bad_argument --arg argument "$1" ;;
    esac
  done
  [ -n "$slug" ] || die missing_argument --arg argument "--slug"
  [ -n "$prompt" ] || die missing_argument --arg argument "--prompt"
  [[ "$slug" =~ ^[a-z0-9][a-z0-9_-]{0,26}$ ]] \
    || die bad_slug --arg slug "$slug" --arg rule "lowercase letters, digits, - or _; at most 27 chars (the work-/plan- prefix uses the rest)"
  [ -n "$repo" ] || repo=$(git rev-parse --show-toplevel 2>/dev/null) || die missing_argument --arg argument "--repo (not inside a git repo)"
  repo=$(realpath "$repo")
  git -C "$repo" rev-parse --show-toplevel >/dev/null 2>&1 || die not_a_repo --arg repo "$repo"
  flags_for "$kind" >/dev/null
}

cmd_worker() {
  local slug prompt repo kind branch base timeout
  parse_launch_args "$@"
  local name="work-$slug"; [ -n "$branch" ] || branch="task/$slug"
  check_prereqs; check_integration "$kind"
  [ -z "$(agent_by_name "$name")" ] || die name_in_use --arg name "$name" --argjson agent "$(agent_by_name "$name")"

  # One worker per branch. A second one means two PRs and a merge conflict.
  local existing
  existing=$(herdr worktree list --cwd "$repo" 2>/dev/null | jq -r --arg b "$branch" '.result.worktrees[]? | select(.branch==$b) | .path' || true)
  if [ -n "$existing" ]; then
    die worktree_exists --arg branch "$branch" --arg path "$existing" \
        --argjson agents "$(herdr agent list | jq -c --arg p "$existing" '[.result.agents[] | select(.cwd==$p) | {name,pane_id,agent_status}]')" \
        --arg hint "a worker may already be on this task; do not launch a second one"
  fi
  git -C "$repo" fetch -q origin 2>/dev/null || true   # workers branch from origin/main

  # Always from the primary checkout, so the worker nests under the repo's Space.
  repo=$(repo_source "$repo" | jq -r .source_checkout_path)
  local created pane workspace tab wt_path
  created=$(herdr_json worktree create --cwd "$repo" --branch "$branch" --base "$base" --label "$name" --no-focus)
  [ -z "$(herdr_err_code "$created")" ] || die worktree_create_failed --argjson herdr_error "$(echo "$created" | jq .error)" --arg branch "$branch" --arg base "$base"
  pane=$(echo "$created" | jq -r .result.root_pane.pane_id)
  workspace=$(echo "$created" | jq -r .result.workspace.workspace_id)
  tab=$(echo "$created" | jq -r .result.tab.tab_id)
  wt_path=$(echo "$created" | jq -r .result.worktree.path)

  launch worker "$name" "$kind" "$pane" "$workspace" "$tab" "$wt_path" "$prompt" "$timeout" \
    "$(jq -cn --arg repo "$repo" --arg wt "$wt_path" --arg br "$branch" --arg base "$base" '{repo:$repo,worktree_path:$wt,branch:$br,base:$base}')"
}

cmd_planner() {
  local slug prompt repo kind branch base timeout
  parse_launch_args "$@"
  [ -z "$branch" ] || die bad_argument --arg argument "--branch" --arg hint "planners run on main/master in the primary checkout"
  local name="plan-$slug"
  check_prereqs; check_integration "$kind"
  [ -z "$(agent_by_name "$name")" ] || die name_in_use --arg name "$name" --argjson agent "$(agent_by_name "$name")"

  # Planner invariants: primary checkout (resolved even from a worktree), on main or master.
  local src; src=$(repo_source "$repo")
  repo=$(echo "$src" | jq -r .source_checkout_path)
  local head; head=$(git -C "$repo" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
  case "$head" in main|master) ;; *) die not_on_main --arg repo "$repo" --arg branch "$head" --arg hint "check out main/master in the primary checkout first" ;; esac

  # A tab in the repo's primary Space; open that Space first if it is not open.
  # (workspace create per planner would add an unrelated top-level Space.)
  local created pane workspace tab parent
  parent=$(echo "$src" | jq -r '.source_workspace_id // empty')
  if [ -n "$parent" ]; then
    created=$(herdr_json tab create --workspace "$parent" --cwd "$repo" --label "$name" --no-focus)
  else
    created=$(herdr_json workspace create --cwd "$repo" --label "$(basename "$repo")" --no-focus)
  fi
  [ -z "$(herdr_err_code "$created")" ] || die planner_tab_create_failed --argjson herdr_error "$(echo "$created" | jq .error)"
  pane=$(echo "$created" | jq -r .result.root_pane.pane_id)
  workspace=$(echo "$created" | jq -r .result.root_pane.workspace_id)
  tab=$(echo "$created" | jq -r .result.root_pane.tab_id)

  launch planner "$name" "$kind" "$pane" "$workspace" "$tab" "$repo" "$prompt" "$timeout" \
    "$(jq -cn --arg repo "$repo" --arg br "$head" '{repo:$repo,branch:$br}')"
}

# --- prompt / verify / list / reap ------------------------------------------

cmd_prompt() {
  local name="" text="" kt="$KICKOFF_TIMEOUT_MS"
  while [ $# -gt 0 ]; do
    case "$1" in
      --name) name="$2"; shift 2 ;;
      --prompt) text="$2"; shift 2 ;;
      --kickoff-timeout) kt="$2"; shift 2 ;;
      *) die bad_argument --arg argument "$1" ;;
    esac
  done
  [ -n "$name" ] || die missing_argument --arg argument "--name"
  [ -n "$text" ] || die missing_argument --arg argument "--prompt"
  check_prereqs
  local out; out=$(do_prompt "$name" "$text" "$kt") || { echo "$out"; exit 1; }
  echo "$out"
}

cmd_verify() {
  local name="" kind=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --name) name="$2"; shift 2 ;;
      --agent|--kind) kind="$2"; shift 2 ;;
      *) die bad_argument --arg argument "$1" ;;
    esac
  done
  [ -n "$name" ] || die missing_argument --arg argument "--name"
  check_prereqs
  [ -n "$kind" ] || kind=$(agent_by_name "$name" | jq -r '.agent // empty')
  [ -n "$kind" ] || die no_such_worker --arg name "$name"
  local expected_role=""
  case "$name" in work-*) expected_role=worker ;; plan-*) expected_role=planner ;; esac
  local out; out=$(verify_worker "$name" "$kind" "" "$expected_role") || { echo "$out"; exit 1; }
  echo "$out"
}

cmd_list() {
  local repo=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --repo) repo="$2"; shift 2 ;;
      *) die bad_argument --arg argument "$1" ;;
    esac
  done
  check_prereqs
  local wts="[]"
  if [ -n "$repo" ]; then
    repo=$(realpath "$repo")
    wts=$(herdr worktree list --cwd "$repo" 2>/dev/null | jq -c '[.result.worktrees[]? | {path,branch,is_linked_worktree}]' || echo "[]")
  fi
  herdr agent list | jq -c --arg repo "$repo" --argjson wts "$wts" '
    [ .result.agents[] | . as $a
      | ($wts | map(select(.path == $a.cwd)) | .[0]) as $wt
      | select($repo == "" or ($a.cwd | startswith($repo)) or ($wt != null))
      | {name, pane_id, workspace_id, agent, agent_status, interactive_ready, cwd,
         branch: ($wt.branch // null),
         role: (if ($a.name // "" | startswith("work-")) or ($wt.is_linked_worktree // false) then "worker"
                elif ($a.name // "" | startswith("plan-")) then "planner" else "unmanaged" end),
         title: .terminal_title_stripped} ]
    | {ok:true, repo:(if $repo=="" then null else $repo end), agents:.}'
}

cmd_reap() {
  local name="" force=false
  while [ $# -gt 0 ]; do
    case "$1" in
      --name) name="$2"; shift 2 ;;
      --force) force=true; shift ;;
      *) die bad_argument --arg argument "$1" ;;
    esac
  done
  [ -n "$name" ] || die missing_argument --arg argument "--name"
  check_prereqs
  local rec pane ws tab cwd
  rec=$(agent_by_name "$name"); [ -n "$rec" ] || die no_such_worker --arg name "$name"
  pane=$(echo "$rec" | jq -r .pane_id); ws=$(echo "$rec" | jq -r .workspace_id); tab=$(echo "$rec" | jq -r .tab_id); cwd=$(echo "$rec" | jq -r .cwd)
  [ "$(echo "$rec" | jq -r .agent_status)" != working ] || $force \
    || die worker_busy --arg name "$name" --arg hint "it is mid-turn; pass --force to reap anyway"

  if is_linked_worktree "$cwd"; then
    local dirty unpushed
    dirty=$(git -C "$cwd" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
    unpushed=$(git -C "$cwd" log --oneline '@{u}..HEAD' 2>/dev/null | wc -l | tr -d ' ') \
      || unpushed=$(git -C "$cwd" log --oneline "$DEFAULT_BASE..HEAD" 2>/dev/null | wc -l | tr -d ' ') \
      || unpushed=$(git -C "$cwd" log --oneline HEAD --not --remotes 2>/dev/null | wc -l | tr -d ' ')   # no origin/main
    if { [ "$dirty" -gt 0 ] || [ "${unpushed:-0}" -gt 0 ]; } && ! $force; then
      die unsaved_work --arg name "$name" --arg worktree "$cwd" --argjson dirty_files "$dirty" --argjson unpushed_commits "${unpushed:-0}" \
          --arg hint "push or discard first, or pass --force to lose it"
    fi
    local removed
    removed=$(herdr worktree remove --workspace "$ws" --force 2>&1 || true)   # worktree first, while $ws still names it
    herdr workspace close "$ws" >/dev/null 2>&1 || true                        # workspace_not_found here is success
    jq -cn --arg n "$name" --arg ws "$ws" --arg wt "$cwd" --arg removed "$removed" \
      '{ok:true,name:$n,workspace_id:$ws,worktree_path:$wt,removed_worktree:($removed|test("worktree_removed")),
        note:"branch left in place; delete it once merged or abandoned"}'
  else
    # The planner's tab lives in the repo's primary Space: close only the tab.
    herdr tab close "$tab" >/dev/null 2>&1 || herdr pane close "$pane" >/dev/null
    jq -cn --arg n "$name" --arg ws "$ws" --arg tab "$tab" '{ok:true,name:$n,workspace_id:$ws,tab_id:$tab,closed:"tab (planner, no worktree)"}'
  fi
}

# --- main --------------------------------------------------------------------

export SELF="$0"
sub="${1:-}"; shift || true
case "$sub" in
  worker) cmd_worker "$@" ;;
  planner) cmd_planner "$@" ;;
  prompt) cmd_prompt "$@" ;;
  verify) cmd_verify "$@" ;;
  list) cmd_list "$@" ;;
  reap) cmd_reap "$@" ;;
  ""|-h|--help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  *) die unknown_subcommand --arg subcommand "$sub" --arg usage "worker|planner|prompt|verify|list|reap" ;;
esac
