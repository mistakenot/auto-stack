module session_message_conformance

// Structural conformance model for auto-etl's normalized Session and Message
// datasets. Opaque signatures stand in for strings, timestamps, and IDs. The
// model focuses on relationships and multiplicities, not parsing or scalar
// validation.
//
// Policy captured here:
// - malformed source data should not crash the ETL run;
// - canonical Session/Message output should stay queryable;
// - malformed source shapes should be captured as DataErrors, suitable for a
//   separate diagnostic parquet dataset.

abstract sig Flag {}
one sig Yes, No extends Flag {}

abstract sig Role {}
one sig UserRole, AssistantRole, ToolRole, SystemRole, ThinkingRole extends Role {}

// Role vocabulary is intentionally open. New agent roles can be added without
// making the canonical model inconsistent.
sig FutureRole extends Role {}

abstract sig TimestampKind {}
one sig ZeroTimestamp, NonZeroTimestamp extends TimestampKind {}

abstract sig Severity {}
one sig WarningSeverity, ErrorSeverity extends Severity {}

abstract sig ErrorKind {}
one sig EmptySessionNotice,
        OrphanSubagentIgnored,
        MissingToolUse,
        DuplicateToolUseID,
        ZeroTimestampMessage
  extends ErrorKind {}

sig Host {}
sig ProjectPath {}
sig GitRemote {}
sig ModelName {}
sig SessionID {}
sig MessageIndex {}
sig ToolUseKey {}

sig Session {
  sid: one SessionID,
  host: one Host,
  projectPath: lone ProjectPath,
  gitRemote: lone GitRemote,
  modelName: lone ModelName,
  isSubagent: one Flag,
  parentSessionID: lone SessionID,
  firstMessageAt: one TimestampKind,
  lastMessageAt: one TimestampKind
}

sig Message {
  owner: one Session,
  host: one Host,
  projectPath: lone ProjectPath,
  gitRemote: lone GitRemote,
  modelName: lone ModelName,
  role: one Role,
  timestamp: one TimestampKind,
  idx: one MessageIndex,
  messageIsSubagent: one Flag,
  messageParentSessionID: lone SessionID,
  toolUseID: lone ToolUseKey,
  hasToolName: one Flag,
  hasContent: one Flag,
  hasToolInput: one Flag
}

sig DataError {
  kind: one ErrorKind,
  severity: one Severity,
  sourceSessionID: one SessionID,
  sourceMessageIndex: lone MessageIndex,
  sourceToolUseID: lone ToolUseKey
}

pred assistantToolUse[m: Message] {
  m.role = AssistantRole
  some m.toolUseID
}

pred toolResult[m: Message] {
  m.role = ToolRole
}

fact PersistedSessionShape {
  // The current transform skips Sessions whose FirstMessageAt is zero.
  all s: Session | s.firstMessageAt = NonZeroTimestamp
  all s: Session | s.lastMessageAt = NonZeroTimestamp

  // Parser/transform shape: parent Sessions have no parent_session_id;
  // Subagents do.
  all s: Session |
    (s.isSubagent = Yes iff some s.parentSessionID) and
    (s.isSubagent = No iff no s.parentSessionID)

  // Subagent rows use agentId as Session ID, not the parent Session ID.
  all s: Session | s.isSubagent = Yes implies s.sid != s.parentSessionID
}

fact CanonicalSessionIdentity {
  all disj a, b: Session | a.sid != b.sid
}

fact DenormalizedSessionFields {
  // makeBaseMessage copies these fields from the Session onto every Message.
  all m: Message | {
    m.host = m.owner.host
    m.projectPath = m.owner.projectPath
    m.gitRemote = m.owner.gitRemote
    m.modelName = m.owner.modelName
    m.messageIsSubagent = m.owner.isSubagent
    m.messageParentSessionID = m.owner.parentSessionID
  }
}

fact MessageIdentityShape {
  // AgentMessage.ID is Session.ID plus the Message index in Go. Model that as
  // no duplicate indexes within a Session.
  all disj a, b: Message |
    a.owner = b.owner implies a.idx != b.idx
}

fact RoleSpecificShape {
  all m: Message | {
    // tool_result rows carry a tool_use_id in well-formed source blocks.
    m.role = ToolRole implies some m.toolUseID

    // tool_use rows carry a tool name and input, but content is intentionally
    // empty in the current ETL mapping.
    assistantToolUse[m] implies {
      m.hasToolName = Yes
      m.hasToolInput = Yes
      m.hasContent = No
    }

    // Non-tool user/system/thinking/future-role rows do not need tool
    // metadata.
    m.role != AssistantRole and m.role != ToolRole implies {
      no m.toolUseID
      m.hasToolName = No
      m.hasToolInput = No
    }
  }
}

fact CanonicalOutputPolicy {
  // Empty Sessions are allowed. They should not crash ETL or break downstream
  // joins, so there is intentionally no "every Session has a Message" fact.

  // Orphan Subagents are not allowed in canonical Session output. If source
  // data contains one, auto-etl should ignore it and emit a DataError.
  all s: Session |
    s.isSubagent = Yes implies
      one p: Session | p.sid = s.parentSessionID and p.isSubagent = No

  // Canonical tool-result Messages must have exactly one matching assistant
  // tool-use Message in the same Session.
  all r: Message |
    toolResult[r] implies
      one u: Message |
        assistantToolUse[u] and
        u.owner = r.owner and
        u.toolUseID = r.toolUseID

  // Duplicate assistant tool-use IDs inside one Session are not allowed in
  // canonical output.
  all disj a, b: Message |
    assistantToolUse[a] and assistantToolUse[b] and a.owner = b.owner implies
      a.toolUseID != b.toolUseID

  // Canonical Message partitions should never be derived from a zero
  // timestamp.
  all m: Message | m.timestamp = NonZeroTimestamp
}

fact DataErrorPolicy {
  all d: DataError | {
    d.kind = EmptySessionNotice implies d.severity = WarningSeverity
    d.kind != EmptySessionNotice implies d.severity = ErrorSeverity

    d.kind = EmptySessionNotice implies no d.sourceMessageIndex and no d.sourceToolUseID
    d.kind = OrphanSubagentIgnored implies no d.sourceMessageIndex and no d.sourceToolUseID
    d.kind = MissingToolUse implies some d.sourceMessageIndex and some d.sourceToolUseID
    d.kind = DuplicateToolUseID implies some d.sourceToolUseID
    d.kind = ZeroTimestampMessage implies some d.sourceMessageIndex
  }
}

pred example {
  some Session
  some Message
}

pred emptySessionAllowed {
  some s: Session | no m: Message | m.owner = s
}

pred emptySessionCanBeNoticed {
  some s: Session |
    no m: Message | m.owner = s

  some d: DataError | {
    d.kind = EmptySessionNotice
    d.severity = WarningSeverity
  }
}

pred orphanSubagentCapturedAsError {
  some d: DataError | {
    d.kind = OrphanSubagentIgnored
    no s: Session | s.sid = d.sourceSessionID
  }
}

pred missingToolUseCapturedAsError {
  some d: DataError | {
    d.kind = MissingToolUse
    no r: Message | toolResult[r] and r.toolUseID = d.sourceToolUseID
  }
}

pred duplicateToolUseCapturedAsError {
  some d: DataError | d.kind = DuplicateToolUseID
}

pred zeroTimestampCapturedAsError {
  some d: DataError | d.kind = ZeroTimestampMessage
}

pred futureRoleAllowed {
  some m: Message | m.role in FutureRole
}

// Conformance checks for canonical output. These should be UNSAT: Alloy should
// not be able to find a counterexample inside the bounded scope.

assert CanonicalSubagentsHaveParents {
  all s: Session |
    s.isSubagent = Yes implies
      one p: Session | p.sid = s.parentSessionID and p.isSubagent = No
}

assert CanonicalToolResultsHaveExactlyOneUse {
  all r: Message |
    toolResult[r] implies
      one u: Message |
        assistantToolUse[u] and
        u.owner = r.owner and
        u.toolUseID = r.toolUseID
}

assert CanonicalToolUseIDsUniqueWithinSession {
  all disj a, b: Message |
    assistantToolUse[a] and assistantToolUse[b] and a.owner = b.owner implies
      a.toolUseID != b.toolUseID
}

assert CanonicalMessagesHaveNonZeroTimestamps {
  all m: Message | m.timestamp = NonZeroTimestamp
}

assert DataErrorsHaveExpectedSeverity {
  all d: DataError |
    (d.kind = EmptySessionNotice iff d.severity = WarningSeverity) and
    (d.kind != EmptySessionNotice iff d.severity = ErrorSeverity)
}

run example for 5
run emptySessionAllowed for 5
run emptySessionCanBeNoticed for 5
run orphanSubagentCapturedAsError for 5
run missingToolUseCapturedAsError for 5
run duplicateToolUseCapturedAsError for 5
run zeroTimestampCapturedAsError for 5
run futureRoleAllowed for 6

check CanonicalSubagentsHaveParents for 5
check CanonicalToolResultsHaveExactlyOneUse for 5
check CanonicalToolUseIDsUniqueWithinSession for 5
check CanonicalMessagesHaveNonZeroTimestamps for 5
check DataErrorsHaveExpectedSeverity for 5
