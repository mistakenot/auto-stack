#!/usr/bin/env python3
"""Prototype for `auto search session html`.

Given a root session id, recursively build a nested work-graph model
(parent -> sub-agents) and emit a single self-contained HTML file that
reads top to bottom with drill-down / drill-up.

This mirrors what the Go command will do: query the index, build a JSON
model, render it into an embedded template.
"""
import sqlite3, json, sys, html, re

DB = '/home/vscode/.auto/search/default.sqlite'
TRUNC = 4000  # per-message content cap embedded in the doc

db = sqlite3.connect(DB)
db.row_factory = sqlite3.Row


def norm(s):
    return re.sub(r'\s+', ' ', (s or '')).strip()


def get_session(sid):
    r = db.execute("SELECT * FROM sessions WHERE session_id=?", (sid,)).fetchone()
    return dict(r) if r else None


def children(sid):
    rows = db.execute(
        "SELECT * FROM sessions WHERE parent_session_id=? ORDER BY first_message_at",
        (sid,)).fetchall()
    return [dict(r) for r in rows]


def messages(sid):
    rows = db.execute(
        "SELECT * FROM messages WHERE session_id=? ORDER BY message_index", (sid,)
    ).fetchall()
    return [dict(r) for r in rows]


def clip(s, n=TRUNC):
    s = s or ''
    if len(s) <= n:
        return s, False
    return s[:n], True


def first_line(s, n=120):
    s = norm(s)
    return s[:n] + ('…' if len(s) > n else '')


def tool_summary(m):
    t = m['tool_name']
    ti = m.get('tool_input') or ''
    try:
        d = json.loads(ti)
    except Exception:
        d = {}
    if t == 'Bash':
        return first_line(m.get('bash_command') or d.get('command') or '', 200)
    if t in ('Read', 'Write', 'Edit', 'Glob', 'NotebookEdit'):
        p = m.get('tool_file_path') or d.get('file_path') or d.get('path') or d.get('pattern') or ''
        return p
    if t == 'Skill':
        return m.get('skill_name') or d.get('command') or d.get('skill') or ''
    if t in ('TaskCreate', 'TaskUpdate'):
        return first_line(d.get('subagent_type') or d.get('description') or d.get('prompt') or json.dumps(d), 120)
    if t == 'ToolSearch':
        return first_line(d.get('query') or '', 120)
    if t == 'Grep':
        return first_line(d.get('pattern') or '', 120)
    if t == 'Agent':
        return d.get('description') or first_line(d.get('prompt') or '', 120)
    # fallback: first scalar field
    return first_line(json.dumps(d) if d else m.get('content') or '', 120)


def build(sid, depth=0, dispatch_label=None, seen=None):
    """Recursively build a node for a session."""
    seen = seen or set()
    if sid in seen:
        return None
    seen.add(sid)
    s = get_session(sid)
    if not s:
        return None
    msgs = messages(sid)

    # index tool results by tool_use_id
    results = {}
    for m in msgs:
        if m['role'] == 'tool' and m.get('tool_use_id'):
            results[m['tool_use_id']] = m

    # unmatched children, to be claimed by Agent dispatches in order
    kids = children(sid)
    used_kids = set()

    def claim_child(prompt):
        p = norm(prompt)
        # exact / prefix match on first_user_intent
        for k in kids:
            if k['session_id'] in used_kids:
                continue
            fi = norm(k.get('first_user_intent') or '')
            if fi and (fi.startswith(p[:200]) or p.startswith(fi[:200])):
                used_kids.add(k['session_id'])
                return k['session_id']
        # fallback: next unused in order
        for k in kids:
            if k['session_id'] not in used_kids:
                used_kids.add(k['session_id'])
                return k['session_id']
        return None

    events = []
    counts = {'bash': 0, 'file': 0, 'tool': 0, 'agent': 0, 'skill': 0, 'error': 0}

    for m in msgs:
        role = m['role']
        if role == 'thinking':
            body, tr = clip(m.get('content') or '')
            events.append({'kind': 'thinking', 'idx': m['message_index'],
                           'summary': first_line(m.get('content') or '', 100),
                           'body': body, 'truncated': tr, 'mid': m['message_id']})
            continue
        if role == 'user':
            c = m.get('content') or ''
            # skip pure harness noise lines but keep most
            body, tr = clip(c)
            events.append({'kind': 'user', 'idx': m['message_index'],
                           'summary': first_line(c, 140), 'body': body,
                           'truncated': tr, 'mid': m['message_id']})
            continue
        if role == 'assistant':
            t = m.get('tool_name')
            if not t:
                c = m.get('content') or ''
                if not c.strip():
                    continue
                body, tr = clip(c)
                events.append({'kind': 'assistant', 'idx': m['message_index'],
                               'summary': first_line(c, 140), 'body': body,
                               'truncated': tr, 'mid': m['message_id'],
                               'out_tokens': m.get('output_tokens') or 0})
                continue
            # tool_use row
            res = results.get(m.get('tool_use_id'))
            res_content = ''
            duration = 0
            is_err = False
            interrupted = False
            if res:
                res_content = res.get('content') or res.get('tool_use_result_json') or ''
                duration = res.get('duration_ms') or 0
                is_err = bool(res.get('is_error'))
                interrupted = bool(res.get('interrupted'))
            if t == 'Agent':
                counts['agent'] += 1
                try:
                    d = json.loads(m.get('tool_input') or '{}')
                except Exception:
                    d = {}
                child_id = claim_child(d.get('prompt') or '')
                child = build(child_id, depth + 1,
                              dispatch_label=d.get('description'), seen=seen) if child_id else None
                prompt_body, ptr = clip(d.get('prompt') or '', 6000)
                res_body, rtr = clip(res_content, 6000)
                events.append({'kind': 'agent', 'idx': m['message_index'],
                               'summary': d.get('description') or first_line(d.get('prompt') or '', 100),
                               'subagent_type': d.get('subagent_type') or '',
                               'prompt': prompt_body, 'prompt_trunc': ptr,
                               'result': res_body, 'result_trunc': rtr,
                               'duration': duration, 'mid': m['message_id'],
                               'child': child})
                continue
            # regular tool
            if t == 'Bash':
                counts['bash'] += 1
                if (res and res.get('bash_exit_code')) or is_err:
                    counts['error'] += 1
            elif t in ('Read', 'Write', 'Edit', 'Glob', 'NotebookEdit'):
                counts['file'] += 1
            elif t == 'Skill':
                counts['skill'] += 1
            else:
                counts['tool'] += 1
            inp, itr = clip(m.get('tool_input') or '', 4000)
            outp, otr = clip(res_content, 4000)
            events.append({'kind': 'tool', 'idx': m['message_index'],
                           'tool': t, 'summary': tool_summary(m),
                           'input': inp, 'input_trunc': itr,
                           'output': outp, 'output_trunc': otr,
                           'duration': duration, 'is_error': is_err,
                           'interrupted': interrupted, 'mid': m['message_id'],
                           'exit': (res.get('bash_exit_code') if res else 0)})
            continue
        # role == 'tool' standalone -> consumed via pairing; skip
    # Derive a human title for the root: prefer the slash-command invocation.
    title = ''
    if depth == 0:
        blob = ' '.join((m.get('content') or '') for m in msgs if m['role'] == 'user')
        # Pick the first real slash-command (skip harness no-ops like /clear),
        # then grab the command-args nearest after it.
        for mm in re.finditer(r'<command-name>/?([\w-]+)</command-name>', blob):
            cn = mm.group(1)
            if cn in ('clear', 'compact'):
                continue
            am = re.search(r'<command-args>([^<]*)</command-args>', blob[mm.end():mm.end() + 200])
            ca = am.group(1).strip() if am else ''
            title = '/' + cn + ((' ' + ca) if ca else '')
            break

    node = {
        'id': s['session_id'],
        'title': title,
        'intent': s.get('first_user_intent') or '',
        'subagent_name': s.get('subagent_name') or '',
        'dispatch_label': dispatch_label or '',
        'is_subagent': bool(s.get('is_subagent')),
        'workspace': s.get('workspace') or '',
        'git_remote': s.get('git_remote') or '',
        'model': s.get('model') or '',
        'first_ms': s.get('first_message_at') or 0,
        'last_ms': s.get('last_message_at') or 0,
        'duration_ms': (s.get('last_message_at') or 0) - (s.get('first_message_at') or 0),
        'total_tokens': s.get('total_tokens') or 0,
        'msg_count': len(msgs),
        'counts': counts,
        'depth': depth,
        'events': events,
    }
    return node


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else 'f2d1dabc-4b4c-467f-89a1-a4cc74d970b5'
    out = sys.argv[2] if len(sys.argv) > 2 else '/tmp/session.html'
    tree = build(root)
    data_json = json.dumps(tree, ensure_ascii=False)
    # Make the JSON safe to embed inside a <script> tag.
    data_json = data_json.replace('</', '<\\/')
    data_json = data_json.replace('\u2028', '\\u2028').replace('\u2029', '\\u2029')
    tmpl = open(sys.argv[3] if len(sys.argv) > 3 else 'template.html', encoding='utf-8').read()
    doc = tmpl.replace('/*__DATA__*/', 'window.__SESSION__ = ' + data_json + ';')
    with open(out, 'w', encoding='utf-8') as f:
        f.write(doc)
    print('wrote', out, 'bytes=', len(doc))


if __name__ == '__main__':
    main()
