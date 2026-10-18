// Package autonomy preserves the legacy tools import path.
//
// Deprecated: use github.com/yurika0211/luckyagent-orchestrator/autonomy.
package autonomy

import core "github.com/yurika0211/luckyagent-orchestrator/autonomy"

type AgentExecutor = core.AgentExecutor
type AutonomyConfig = core.AutonomyConfig
type AutonomyKit = core.AutonomyKit
type AutonomyStatus = core.AutonomyStatus
type Execution = core.Execution
type HeartbeatConfig = core.HeartbeatConfig
type HeartbeatEngine = core.HeartbeatEngine
type HeartbeatEvent = core.HeartbeatEvent
type HeartbeatMode = core.HeartbeatMode
type LoopConfig = core.LoopConfig
type LoopResult = core.LoopResult
type Operation = core.Operation
type PoolConfig = core.PoolConfig
type PoolStats = core.PoolStats
type QueueTask = core.QueueTask
type RunPolicy = core.RunPolicy
type StableSessionExecutor = core.StableSessionExecutor
type TaskPriority = core.TaskPriority
type TaskQueue = core.TaskQueue
type TaskState = core.TaskState
type ToolDefinitions = core.ToolDefinitions
type Worker = core.Worker
type WorkerConfig = core.WorkerConfig
type WorkerInfo = core.WorkerInfo
type WorkerPool = core.WorkerPool
type WorkerResult = core.WorkerResult
type WorkerState = core.WorkerState

const HeartbeatPassive = core.HeartbeatPassive
const HeartbeatProactive = core.HeartbeatProactive
const PriorityCritical = core.PriorityCritical
const PriorityHigh = core.PriorityHigh
const PriorityLow = core.PriorityLow
const PriorityNormal = core.PriorityNormal
const TaskBlocked = core.TaskBlocked
const TaskDone = core.TaskDone
const TaskInProgress = core.TaskInProgress
const TaskReady = core.TaskReady
const WorkerBusy = core.WorkerBusy
const WorkerIdle = core.WorkerIdle
const WorkerStopped = core.WorkerStopped
const WorkerStopping = core.WorkerStopping

var ErrBlocked = core.ErrBlocked
var ErrStaleExecution = core.ErrStaleExecution
var ErrYield = core.ErrYield

var DefaultAutonomyConfig = core.DefaultAutonomyConfig
var DefaultHeartbeatConfig = core.DefaultHeartbeatConfig
var DefaultPoolConfig = core.DefaultPoolConfig
var DefaultRunPolicy = core.DefaultRunPolicy
var DefaultWorkerLoopConfig = core.DefaultWorkerLoopConfig
var NewAutonomyKit = core.NewAutonomyKit
var NewExecution = core.NewExecution
var NewHeartbeatEngine = core.NewHeartbeatEngine
var NewTaskQueue = core.NewTaskQueue
var NewToolDefinitions = core.NewToolDefinitions
var NewWorker = core.NewWorker
var NewWorkerPool = core.NewWorkerPool
var ParseAcceptanceCriteria = core.ParseAcceptanceCriteria
var ParseTaskPriority = core.ParseTaskPriority
