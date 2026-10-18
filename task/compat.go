// Package task preserves the legacy tools import path.
//
// Deprecated: use github.com/yurika0211/luckyagent-orchestrator/task.
package task

import core "github.com/yurika0211/luckyagent-orchestrator/task"

type Budget = core.Budget
type CleanupResult = core.CleanupResult
type CostSnapshot = core.CostSnapshot
type Event = core.Event
type EventBus = core.EventBus
type EventObserver = core.EventObserver
type EventType = core.EventType
type FileStore = core.FileStore
type ListFilter = core.ListFilter
type MainAgentObservation = core.MainAgentObservation
type Mode = core.Mode
type Outcome = core.Outcome
type Policy = core.Policy
type PolicyDecision = core.PolicyDecision
type PolicyRequest = core.PolicyRequest
type Record = core.Record
type RetentionPolicy = core.RetentionPolicy
type RuntimeRef = core.RuntimeRef
type Source = core.Source
type Status = core.Status
type Store = core.Store

const EventCancelled = core.EventCancelled
const EventChildCreated = core.EventChildCreated
const EventCompleted = core.EventCompleted
const EventCreated = core.EventCreated
const EventFailed = core.EventFailed
const EventOutcomeRecorded = core.EventOutcomeRecorded
const EventPlanned = core.EventPlanned
const EventProgress = core.EventProgress
const EventStarted = core.EventStarted
const EventToolUsed = core.EventToolUsed
const ModeAuto = core.ModeAuto
const ModeAutonomyQueue = core.ModeAutonomyQueue
const ModeDebate = core.ModeDebate
const ModeParallel = core.ModeParallel
const ModePipeline = core.ModePipeline
const ModeSingle = core.ModeSingle
const SourceAutonomy = core.SourceAutonomy
const SourceGateway = core.SourceGateway
const SourceHTTP = core.SourceHTTP
const SourceTool = core.SourceTool
const StatusBlocked = core.StatusBlocked
const StatusCancelled = core.StatusCancelled
const StatusCompleted = core.StatusCompleted
const StatusFailed = core.StatusFailed
const StatusPending = core.StatusPending
const StatusRunning = core.StatusRunning

var EvaluatePolicy = core.EvaluatePolicy
var NewEventBus = core.NewEventBus
var NewFileStore = core.NewFileStore
var ReduceObservation = core.ReduceObservation
