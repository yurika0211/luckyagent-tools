package tool

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yurika0211/luckyagent-tools/sandbox"
)

const (
	shellBackgroundOutputLimit = 10000
	maxShellBackgroundTasks    = 10
	shellBackgroundIDPrefix    = "shell-"
)

// shellBackgroundTask is one command that returned before it finished.
// Timeout does not kill it. Output is snapshotted; the on-disk copy is the
// same buffer the waiter appends to.
type shellBackgroundTask struct {
	id       string
	command  string
	cmd      *exec.Cmd
	started  time.Time
	done     chan struct{}
	waitErr  error
	finished time.Time

	mu     sync.Mutex
	stdout bytes.Buffer
	stderr bytes.Buffer
}

type shellBackgroundRegistry struct {
	mu    sync.Mutex
	seq   int
	tasks map[string]*shellBackgroundTask
}

var shellBackground = &shellBackgroundRegistry{tasks: map[string]*shellBackgroundTask{}}

func (t *shellBackgroundTask) running() bool {
	select {
	case <-t.done:
		return false
	default:
		return true
	}
}

func (t *shellBackgroundTask) snapshot() (output string, running bool, exitErr error) {
	t.mu.Lock()
	output = formatShellOutput(t.stdout.String(), t.stderr.String(), nil)
	t.mu.Unlock()
	running = t.running()
	if !running {
		exitErr = t.waitErr
		if exitErr != nil {
			output += fmt.Sprintf("\n[exit code: %v]", exitErr)
		}
	}
	if len(output) > shellBackgroundOutputLimit {
		output = output[:shellBackgroundOutputLimit] + "\n... (truncated)"
	}
	return output, running, exitErr
}

func (r *shellBackgroundRegistry) start(cmd *exec.Cmd, command string) (*shellBackgroundTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	running := 0
	for _, task := range r.tasks {
		if task.running() {
			running++
		}
	}
	if running >= maxShellBackgroundTasks {
		return nil, fmt.Errorf("too many background terminal tasks (max %d)", maxShellBackgroundTasks)
	}
	r.seq++
	task := &shellBackgroundTask{
		id:      shellBackgroundIDPrefix + strconv.Itoa(r.seq),
		command: command,
		cmd:     cmd,
		started: time.Now(),
		done:    make(chan struct{}),
	}
	stdout := &lockedBuffer{buf: &task.stdout, mu: &task.mu}
	stderr := &lockedBuffer{buf: &task.stderr, mu: &task.mu}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	configureShellProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r.tasks[task.id] = task
	go func() {
		err := cmd.Wait()
		task.mu.Lock()
		task.waitErr = err
		task.finished = time.Now()
		task.mu.Unlock()
		close(task.done)
	}()
	return task, nil
}

func (r *shellBackgroundRegistry) get(id string) (*shellBackgroundTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil {
		return nil, fmt.Errorf("unknown background terminal task %q", id)
	}
	return task, nil
}

// lockedBuffer serializes writes from the process with snapshot reads.
type lockedBuffer struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func formatShellOutput(stdout, stderr string, waitErr error) string {
	output := stdout
	if stderr != "" {
		output += "\n[stderr]\n" + stderr
	}
	if waitErr != nil {
		output += fmt.Sprintf("\n[exit code: %v]", waitErr)
	}
	return output
}

func shellBoolArg(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			return true
		}
	case float64:
		return v != 0
	}
	return false
}

func shellBackgroundStart(snapshot *sandbox.Snapshot, args map[string]any, command, fullCommand, workdir string, env map[string]string) (string, error) {
	var cmd *exec.Cmd
	var err error
	if snapshot != nil {
		cmd, err = snapshot.Command(fullCommand, workdir, env)
	} else {
		cmd, err = buildShellCommand(fullCommand)
	}
	if err != nil {
		return "", err
	}
	if workdir != "" && snapshot == nil {
		cmd.Dir = workdir
	}
	task, err := shellBackground.start(cmd, command)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task_id=%s\nbackground terminal started\ncommand: %s", task.id, command), nil
}

// shellBackgroundWait snapshots one task. waitSeconds 0 returns immediately.
// A positive wait returns when the process exits or the wait elapses. It does
// not kill a task that is still running.
func shellBackgroundWait(id string, waitSeconds int) (string, error) {
	task, err := shellBackground.get(id)
	if err != nil {
		return "", err
	}
	if waitSeconds > 0 {
		timer := time.NewTimer(time.Duration(waitSeconds) * time.Second)
		defer timer.Stop()
		select {
		case <-task.done:
		case <-timer.C:
		}
	}
	output, running, _ := task.snapshot()
	state := "exited"
	if running {
		state = "running"
	}
	return fmt.Sprintf("task_id=%s\nstate=%s\n%s", task.id, state, output), nil
}

func shellBackgroundKill(id string) (string, error) {
	task, err := shellBackground.get(id)
	if err != nil {
		return "", err
	}
	if !task.running() {
		output, _, _ := task.snapshot()
		return fmt.Sprintf("task_id=%s\nstate=exited\n%s", task.id, output), nil
	}
	signalShellTerm(task.cmd)
	select {
	case <-task.done:
	case <-time.After(time.Second):
		killShellProcessGroup(task.cmd)
		<-task.done
	}
	output, _, _ := task.snapshot()
	return fmt.Sprintf("task_id=%s\nstate=killed\n%s", task.id, output), nil
}

func handleShellOutput(args map[string]any) (string, error) {
	id, _ := args["task_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("task_id is required")
	}
	return shellBackgroundWait(id, shellTimeoutArg(args))
}

func handleShellKill(args map[string]any) (string, error) {
	id, _ := args["task_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("task_id is required")
	}
	return shellBackgroundKill(id)
}

// resetShellBackgroundTasks drops finished bookkeeping between tests. Live
// processes are killed so a test cannot leak a sleep into the next one.
func resetShellBackgroundTasks() {
	shellBackground.mu.Lock()
	tasks := shellBackground.tasks
	shellBackground.tasks = map[string]*shellBackgroundTask{}
	shellBackground.seq = 0
	shellBackground.mu.Unlock()
	for _, task := range tasks {
		if task.running() {
			killShellProcessGroup(task.cmd)
			select {
			case <-task.done:
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func shellTaskAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return shellProcessAlive(cmd.Process.Pid)
}
