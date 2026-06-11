package model

import sharedmodel "github.com/mistakenot/auto-shared/model"

// Type aliases — existing code in auto-etl that references model.AgentMessage,
// model.AgentSession, etc. continues to compile without changes.
type AgentMessage = sharedmodel.AgentMessage
type AgentSession = sharedmodel.AgentSession
type MessageRole = sharedmodel.MessageRole
type PartitionKey = sharedmodel.PartitionKey

const (
	SchemaVersion             = sharedmodel.SchemaVersion
	DefaultTruncateMaxChars   = sharedmodel.DefaultTruncateMaxChars
	IntentTruncateMaxChars    = sharedmodel.IntentTruncateMaxChars
	DefaultTranscriptMaxChars = sharedmodel.DefaultTranscriptMaxChars
)

const (
	RoleUser      = sharedmodel.RoleUser
	RoleAssistant = sharedmodel.RoleAssistant
	RoleTool      = sharedmodel.RoleTool
	RoleSystem    = sharedmodel.RoleSystem
	RoleThinking  = sharedmodel.RoleThinking
)

var WeekPartition = sharedmodel.WeekPartition
var MonthPartition = sharedmodel.MonthPartition

// TransformedRows holds the output of the transform step.
type TransformedRows struct {
	Messages []sharedmodel.AgentMessage
	Sessions []sharedmodel.AgentSession
}
