package model

import sharedmodel "github.com/mistakenot/auto-shared/model"

// ParquetSessionRow is an alias for the canonical AgentSession parquet schema.
type ParquetSessionRow = sharedmodel.AgentSession

// ParquetMessageRow is an alias for the canonical AgentMessage parquet schema.
type ParquetMessageRow = sharedmodel.AgentMessage
