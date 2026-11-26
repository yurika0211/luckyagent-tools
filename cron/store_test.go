package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStoreLoadSkipsMissedRunOnRestart(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "cron_jobs.json")
	staleNextRun := time.Now().Add(-1 * time.Hour).UnixMilli()
	data := `{
  "version": 1,
  "engine_running": false,
  "jobs": [
    {
      "id": "stale-job",
      "schedule_text": "每小时",
      "command": "echo stale",
      "mode": "shell",
      "next_run_unix": ` + fmt.Sprint(staleNextRun) + `,
      "metadata": {
        "mode": "shell",
        "command": "echo stale",
        "schedule_text": "每小时"
      }
    }
  ]
}`
	if err := os.WriteFile(storePath, []byte(data), 0600); err != nil {
		t.Fatalf("write json store: %v", err)
	}

	engine := NewEngine()
	store := NewStore(storePath)
	beforeLoad := time.Now()
	restored, err := store.Load(engine, func(job PersistedJob) (func() error, map[string]string, error) {
		return func() error { return nil }, map[string]string{
			"mode":          job.Mode,
			"command":       job.Command,
			"schedule_text": job.ScheduleText,
		}, nil
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if restored != 1 {
		t.Fatalf("expected 1 restored job, got %d", restored)
	}
	job, ok := engine.GetJob("stale-job")
	if !ok {
		t.Fatal("expected stale-job to be restored")
	}
	if !job.NextRun.After(beforeLoad) {
		t.Fatalf("expected stale next run to be rescheduled after load, got %v beforeLoad=%v", job.NextRun, beforeLoad)
	}
}

func TestStorePersistsClaimedSlotsWithNanosecondPrecision(t *testing.T) {
	planned := time.Now().Add(time.Hour).Truncate(time.Second).Add(123 * time.Millisecond)
	for _, fileName := range []string{"cron_jobs.json", "cron_jobs.md"} {
		t.Run(fileName, func(t *testing.T) {
			storePath := filepath.Join(t.TempDir(), fileName)
			engine := NewEngine()
			if err := engine.AddJobWithMeta("task", "Task", "", OnceSchedule{At: planned}, func() error { return nil }, map[string]string{
				"command":       "echo task",
				"mode":          "shell",
				"schedule_text": planned.Format("2006-01-02 15:04"),
			}); err != nil {
				t.Fatal(err)
			}
			if claimed, ok := engine.claimSlot("task"); !ok || !claimed.Equal(planned) {
				t.Fatalf("claim = %v, %t", claimed, ok)
			}

			store := NewStore(storePath)
			if err := store.Save(engine); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			data, err := os.ReadFile(storePath)
			if err != nil {
				t.Fatal(err)
			}
			var persisted PersistedState
			if filepath.Ext(storePath) == ".md" {
				persisted, err = parseMarkdownState(string(data))
			} else {
				err = json.Unmarshal(data, &persisted)
			}
			if err != nil {
				t.Fatalf("parse saved state: %v", err)
			}
			if len(persisted.Jobs) != 1 {
				t.Fatalf("saved jobs = %#v", persisted.Jobs)
			}
			job := persisted.Jobs[0]
			if !reflect.DeepEqual(job.ClaimedSlots, []int64{planned.UnixNano()}) {
				t.Fatalf("saved claimed slots = %v, want %d", job.ClaimedSlots, planned.UnixNano())
			}
			if job.NextRunUnixNano != planned.UnixNano() {
				t.Fatalf("saved next run nanos = %d, want %d", job.NextRunUnixNano, planned.UnixNano())
			}

			restored := NewEngine()
			count, err := store.Load(restored, func(job PersistedJob) (func() error, map[string]string, error) {
				return func() error { return nil }, map[string]string{"mode": job.Mode}, nil
			})
			if err != nil || count != 1 {
				t.Fatalf("Load() = %d, %v", count, err)
			}
			restoredJob, ok := restored.GetJob("task")
			if !ok || !restoredJob.NextRun.Equal(planned) {
				t.Fatalf("restored job = %#v, want next run %v", restoredJob, planned)
			}
			if _, ok := restored.claimSlot("task"); ok {
				t.Fatal("restored claimed slot was claimed a second time")
			}
		})
	}
}
