package cron

// This file contains the durable result side of scheduled jobs.  The scheduler
// owns when a job runs; ResultStore owns the account-visible Task,
// Execution and Delivery records.  Keeping the two stores separate lets the
// scheduler remain useful to non-HTTP callers while preventing a notification
// from becoming the source of truth for an execution.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrResultNotFound   = errors.New("cron result not found")
	ErrTaskPaused       = errors.New("cron task is paused")
	ErrTaskDeleted      = errors.New("cron task is deleted")
	ErrExecutionClaimed = errors.New("cron execution already claimed")
	ErrInvalidResult    = errors.New("invalid cron result")
)

type TaskStatus string

const (
	TaskActive  TaskStatus = "active"
	TaskPaused  TaskStatus = "paused"
	TaskDeleted TaskStatus = "deleted"
)

type ExecutionStatus string

const (
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionSucceeded ExecutionStatus = "succeeded"
	ExecutionFailed    ExecutionStatus = "failed"
)

type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryRetrying  DeliveryStatus = "retrying"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryCancelled DeliveryStatus = "cancelled"
)

// Task is the account-level scheduled task.  Channels names are stable
// logical channels (for example "android" and "telegram"), not devices.
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name,omitempty"`
	Description string     `json:"description,omitempty"`
	Schedule    string     `json:"schedule,omitempty"`
	Status      TaskStatus `json:"status"`
	NextRunAt   time.Time  `json:"next_run_at,omitempty"`
	Channels    []string   `json:"channels,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Execution struct {
	ID          string          `json:"id"`
	TaskID      string          `json:"task_id"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at,omitempty"`
	Status      ExecutionStatus `json:"status"`
	Result      string          `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	RetryCount  int             `json:"retry_count,omitempty"`
}

type Delivery struct {
	ID          string         `json:"id"`
	ExecutionID string         `json:"execution_id"`
	Channel     string         `json:"channel"`
	Status      DeliveryStatus `json:"status"`
	Attempts    int            `json:"attempts,omitempty"`
	LastError   string         `json:"last_error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Device is a client capability registration.  Cursor is local-device state;
// it is intentionally not used to mutate account-level Delivery status.
type Device struct {
	ID                   string    `json:"id"`
	Channel              string    `json:"channel"`
	FCMToken             string    `json:"fcm_token,omitempty"`
	NotificationsEnabled bool      `json:"notifications_enabled"`
	ChannelEnabled       bool      `json:"channel_enabled"`
	RegisteredAt         time.Time `json:"registered_at"`
	CursorAt             time.Time `json:"cursor_at,omitempty"`
	CursorExecutionID    string    `json:"cursor_execution_id,omitempty"`
	LastCapabilityAt     time.Time `json:"last_capability_at,omitempty"`
}

type ResultState struct {
	Version    int         `json:"version"`
	Tasks      []Task      `json:"tasks,omitempty"`
	Executions []Execution `json:"executions,omitempty"`
	Deliveries []Delivery  `json:"deliveries,omitempty"`
	Devices    []Device    `json:"devices,omitempty"`
}

// ResultStore is safe for concurrent workers in one process.  The execution
// ID is derived from (task_id, scheduled_at), so a restart or retry cannot
// create a second row for the same planned slot.
type ResultStore struct {
	mu    sync.Mutex
	path  string
	state ResultState
}

func NewResultStore(path string) (*ResultStore, error) {
	s := &ResultStore{path: strings.TrimSpace(path), state: ResultState{Version: 1}}
	if s.path == "" {
		return s, nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cron result store: %w", err)
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("parse cron result store: %w", err)
	}
	if s.state.Version == 0 {
		s.state.Version = 1
	}
	return s, nil
}

func NewMemoryResultStore() *ResultStore {
	s, _ := NewResultStore("")
	return s
}

func (s *ResultStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *ResultStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create cron result store dir: %w", err)
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cron result store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".cron-results-*.tmp")
	if err != nil {
		return fmt.Errorf("create cron result temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod cron result temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write cron result temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync cron result temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cron result temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace cron result store: %w", err)
	}
	return nil
}

func (s *ResultStore) EnsureTask(task Task) (Task, error) {
	if s == nil || strings.TrimSpace(task.ID) == "" {
		return Task{}, ErrInvalidResult
	}
	now := time.Now().UTC()
	task.ID = strings.TrimSpace(task.ID)
	if task.Status == "" {
		task.Status = TaskActive
	}
	if task.Status != TaskActive && task.Status != TaskPaused && task.Status != TaskDeleted {
		return Task{}, ErrInvalidResult
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Tasks {
		if s.state.Tasks[i].ID != task.ID {
			continue
		}
		old := s.state.Tasks[i]
		if task.CreatedAt.IsZero() {
			task.CreatedAt = old.CreatedAt
		}
		if task.Status == TaskActive && old.Status == TaskDeleted {
			return Task{}, ErrTaskDeleted
		}
		task.UpdatedAt = now
		task.Channels = normalizeChannels(task.Channels, old.Channels)
		s.state.Tasks[i] = task
		return task, s.persistLocked()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	task.Channels = normalizeChannels(task.Channels, nil)
	s.state.Tasks = append(s.state.Tasks, task)
	return task, s.persistLocked()
}

func normalizeChannels(channels, fallback []string) []string {
	if len(channels) == 0 {
		channels = fallback
	}
	seen := make(map[string]struct{}, len(channels))
	out := make([]string, 0, len(channels))
	for _, channel := range channels {
		channel = strings.ToLower(strings.TrimSpace(channel))
		if channel == "" {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		out = append(out, channel)
	}
	if len(out) == 0 {
		out = []string{"android"}
	}
	sort.Strings(out)
	return out
}

func (s *ResultStore) Claim(taskID string, scheduledAt, now time.Time) (Execution, bool, error) {
	if s == nil || strings.TrimSpace(taskID) == "" || scheduledAt.IsZero() {
		return Execution{}, false, ErrInvalidResult
	}
	taskID = strings.TrimSpace(taskID)
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.taskLocked(taskID)
	if !ok {
		return Execution{}, false, ErrResultNotFound
	}
	switch task.Status {
	case TaskPaused:
		return Execution{}, false, ErrTaskPaused
	case TaskDeleted:
		return Execution{}, false, ErrTaskDeleted
	}
	executionID := executionIDFor(taskID, scheduledAt)
	for _, execution := range s.state.Executions {
		if execution.ID == executionID {
			return execution, false, nil
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	execution := Execution{ID: executionID, TaskID: taskID, ScheduledAt: scheduledAt.UTC(), StartedAt: now.UTC(), Status: ExecutionRunning}
	s.state.Executions = append(s.state.Executions, execution)
	if err := s.persistLocked(); err != nil {
		return Execution{}, false, err
	}
	return execution, true, nil
}

func executionIDFor(taskID string, scheduledAt time.Time) string {
	h := sha256.Sum256([]byte(taskID + "\x00" + scheduledAt.UTC().Format(time.RFC3339Nano)))
	return "cron-exec-" + hex.EncodeToString(h[:12])
}

func deliveryIDFor(executionID, channel string) string {
	h := sha256.Sum256([]byte(executionID + "\x00" + channel))
	return "cron-delivery-" + hex.EncodeToString(h[:12])
}

func (s *ResultStore) Complete(executionID string, result string, failed error, finishedAt time.Time) (Execution, error) {
	if s == nil || strings.TrimSpace(executionID) == "" {
		return Execution{}, ErrInvalidResult
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.executionIndexLocked(executionID)
	if idx < 0 {
		return Execution{}, ErrResultNotFound
	}
	execution := s.state.Executions[idx]
	if execution.Status != ExecutionRunning {
		return execution, nil
	}
	execution.FinishedAt = finishedAt.UTC()
	execution.Result = result
	if failed != nil {
		execution.Status = ExecutionFailed
		execution.Error = failed.Error()
	} else {
		execution.Status = ExecutionSucceeded
		execution.Error = ""
	}
	s.state.Executions[idx] = execution
	task, taskOK := s.taskLocked(execution.TaskID)
	if taskOK && task.Status != TaskDeleted {
		for _, channel := range normalizeChannels(task.Channels, nil) {
			id := deliveryIDFor(execution.ID, channel)
			if s.deliveryIndexLocked(id) >= 0 {
				continue
			}
			s.state.Deliveries = append(s.state.Deliveries, Delivery{ID: id, ExecutionID: execution.ID, Channel: channel, Status: DeliveryPending, CreatedAt: finishedAt.UTC(), UpdatedAt: finishedAt.UTC()})
		}
	}
	if err := s.persistLocked(); err != nil {
		return Execution{}, err
	}
	return execution, nil
}

func (s *ResultStore) GetTask(taskID string) (Task, bool) {
	if s == nil {
		return Task{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.taskLocked(strings.TrimSpace(taskID))
	return task, ok
}

func (s *ResultStore) ListTasks(includeDeleted bool) []Task {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.state.Tasks))
	for _, task := range s.state.Tasks {
		if !includeDeleted && task.Status == TaskDeleted {
			continue
		}
		task.Channels = append([]string(nil), task.Channels...)
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *ResultStore) ListExecutions(taskID string) []Execution {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := false
	if task, ok := s.taskLocked(strings.TrimSpace(taskID)); ok {
		deleted = task.Status == TaskDeleted
	}
	out := make([]Execution, 0)
	for _, execution := range s.state.Executions {
		if execution.TaskID == strings.TrimSpace(taskID) && !deleted {
			out = append(out, execution)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScheduledAt.After(out[j].ScheduledAt) })
	return out
}

func (s *ResultStore) GetExecution(executionID string) (Execution, bool) {
	if s == nil {
		return Execution{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.executionLocked(strings.TrimSpace(executionID))
}

func (s *ResultStore) ListDeliveries(executionID string) []Delivery {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Delivery, 0)
	for _, delivery := range s.state.Deliveries {
		if executionID == "" || delivery.ExecutionID == strings.TrimSpace(executionID) {
			out = append(out, delivery)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *ResultStore) RegisterDevice(device Device) (Device, error) {
	if s == nil || strings.TrimSpace(device.ID) == "" {
		return Device{}, ErrInvalidResult
	}
	device.ID = strings.TrimSpace(device.ID)
	device.Channel = strings.ToLower(strings.TrimSpace(device.Channel))
	if device.Channel == "" {
		device.Channel = "android"
	}
	if device.RegisteredAt.IsZero() {
		device.RegisteredAt = time.Now().UTC()
	}
	device.LastCapabilityAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Devices {
		if s.state.Devices[i].ID != device.ID {
			continue
		}
		device.CursorAt = s.state.Devices[i].CursorAt
		device.CursorExecutionID = s.state.Devices[i].CursorExecutionID
		device.RegisteredAt = s.state.Devices[i].RegisteredAt
		s.state.Devices[i] = device
		return device, s.persistLocked()
	}
	s.state.Devices = append(s.state.Devices, device)
	return device, s.persistLocked()
}

func (s *ResultStore) GetDevice(deviceID string) (Device, bool) {
	if s == nil {
		return Device{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, device := range s.state.Devices {
		if device.ID == strings.TrimSpace(deviceID) {
			return device, true
		}
	}
	return Device{}, false
}

// PendingForDevice returns the newest result eligible for this device.  It
// includes account-level delivered rows because another device's ACK must not
// suppress a local notification that has never been shown here.
func (s *ResultStore) PendingForDevice(deviceID string, limit int) ([]Delivery, error) {
	if s == nil || strings.TrimSpace(deviceID) == "" {
		return nil, ErrInvalidResult
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.deviceLocked(strings.TrimSpace(deviceID))
	if !ok {
		return nil, ErrResultNotFound
	}
	if limit <= 0 {
		limit = 1
	}
	type item struct {
		delivery Delivery
		exec     Execution
	}
	items := make([]item, 0)
	for _, delivery := range s.state.Deliveries {
		if delivery.Channel != device.Channel || (delivery.Status != DeliveryPending && delivery.Status != DeliveryRetrying) {
			continue
		}
		execution, exists := s.executionLocked(delivery.ExecutionID)
		if !exists {
			continue
		}
		task, exists := s.taskLocked(execution.TaskID)
		if !exists || task.Status == TaskDeleted || execution.Status == ExecutionRunning {
			continue
		}
		if !execution.ScheduledAt.After(device.CursorAt) && execution.ID == device.CursorExecutionID {
			continue
		}
		items = append(items, item{delivery: delivery, exec: execution})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].exec.ScheduledAt.After(items[j].exec.ScheduledAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]Delivery, 0, len(items))
	for _, item := range items {
		out = append(out, item.delivery)
	}
	return out, nil
}

func (s *ResultStore) Ack(deliveryID, deviceID string, at time.Time) (Delivery, error) {
	if s == nil || strings.TrimSpace(deliveryID) == "" || strings.TrimSpace(deviceID) == "" {
		return Delivery{}, ErrInvalidResult
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.deviceLocked(strings.TrimSpace(deviceID)); !ok {
		return Delivery{}, ErrResultNotFound
	}
	idx := s.deliveryIndexLocked(strings.TrimSpace(deliveryID))
	if idx < 0 {
		return Delivery{}, ErrResultNotFound
	}
	delivery := s.state.Deliveries[idx]
	if delivery.Status != DeliveryDelivered {
		delivery.Status = DeliveryDelivered
		delivery.Attempts++
		delivery.LastError = ""
		delivery.UpdatedAt = at.UTC()
		s.state.Deliveries[idx] = delivery
	}
	if execution, ok := s.executionLocked(delivery.ExecutionID); ok {
		for i := range s.state.Devices {
			if s.state.Devices[i].ID == strings.TrimSpace(deviceID) {
				if execution.ScheduledAt.After(s.state.Devices[i].CursorAt) || execution.ID == s.state.Devices[i].CursorExecutionID {
					s.state.Devices[i].CursorAt = execution.ScheduledAt
					s.state.Devices[i].CursorExecutionID = execution.ID
				}
				break
			}
		}
	}
	return delivery, s.persistLocked()
}

func (s *ResultStore) PauseTask(taskID string) error { return s.setTaskStatus(taskID, TaskPaused) }

func (s *ResultStore) ResumeTask(taskID string) error { return s.setTaskStatus(taskID, TaskActive) }

func (s *ResultStore) DeleteTask(taskID string) error {
	if s == nil {
		return ErrResultNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.taskIndexLocked(strings.TrimSpace(taskID))
	if idx < 0 {
		return ErrResultNotFound
	}
	task := s.state.Tasks[idx]
	task.Status = TaskDeleted
	task.UpdatedAt = time.Now().UTC()
	s.state.Tasks[idx] = task
	for i := range s.state.Deliveries {
		if s.state.Deliveries[i].Status == DeliveryPending || s.state.Deliveries[i].Status == DeliveryRetrying {
			if execution, ok := s.executionLocked(s.state.Deliveries[i].ExecutionID); ok && execution.TaskID == task.ID {
				s.state.Deliveries[i].Status = DeliveryCancelled
				s.state.Deliveries[i].UpdatedAt = task.UpdatedAt
			}
		}
	}
	return s.persistLocked()
}

func (s *ResultStore) setTaskStatus(taskID string, status TaskStatus) error {
	if s == nil {
		return ErrResultNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.taskIndexLocked(strings.TrimSpace(taskID))
	if idx < 0 {
		return ErrResultNotFound
	}
	if s.state.Tasks[idx].Status == TaskDeleted {
		return ErrTaskDeleted
	}
	s.state.Tasks[idx].Status = status
	s.state.Tasks[idx].UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

func (s *ResultStore) taskIndexLocked(id string) int {
	for i := range s.state.Tasks {
		if s.state.Tasks[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *ResultStore) taskLocked(id string) (Task, bool) {
	idx := s.taskIndexLocked(id)
	if idx < 0 {
		return Task{}, false
	}
	task := s.state.Tasks[idx]
	task.Channels = append([]string(nil), task.Channels...)
	return task, true
}

func (s *ResultStore) executionIndexLocked(id string) int {
	for i := range s.state.Executions {
		if s.state.Executions[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *ResultStore) executionLocked(id string) (Execution, bool) {
	idx := s.executionIndexLocked(id)
	if idx < 0 {
		return Execution{}, false
	}
	return s.state.Executions[idx], true
}

func (s *ResultStore) deliveryIndexLocked(id string) int {
	for i := range s.state.Deliveries {
		if s.state.Deliveries[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *ResultStore) deviceLocked(id string) (Device, bool) {
	for _, device := range s.state.Devices {
		if device.ID == id {
			return device, true
		}
	}
	return Device{}, false
}
