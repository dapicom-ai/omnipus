package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/taskstore"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

const (
	defaultMaxConcurrentTasksPerAgent = 3
	maxTaskDepth                      = 10
)

// TaskExecutor runs queued tasks by dispatching them to agent sessions.
type TaskExecutor struct {
	agentLoop     *AgentLoop
	store         *taskstore.TaskStore
	mu            sync.Mutex
	running       map[string]context.CancelFunc
	maxConcurrent int
}

// newTaskExecutor creates a TaskExecutor.
func newTaskExecutor(al *AgentLoop, store *taskstore.TaskStore) *TaskExecutor {
	return &TaskExecutor{
		agentLoop:     al,
		store:         store,
		running:       make(map[string]context.CancelFunc),
		maxConcurrent: defaultMaxConcurrentTasksPerAgent,
	}
}

// ExecuteTask starts executing the task identified by taskID.
// It updates the task's status to "running" and dispatches it to the agent in a goroutine.
func (te *TaskExecutor) ExecuteTask(ctx context.Context, taskID string) error {
	task, err := te.store.Get(taskID)
	if err != nil {
		return fmt.Errorf("task_executor: get task %q: %w", taskID, err)
	}
	if task.Status != "queued" {
		return fmt.Errorf("task_executor: task %q is %s, not queued", taskID, task.Status)
	}

	registry := te.agentLoop.GetRegistry()
	if _, ok := registry.GetAgent(task.AgentID); !ok {
		// Agent not found — fail the task rather than silently dropping it.
		slog.Error("task_executor: agent not found, failing task", "task_id", taskID, "agent_id", task.AgentID)
		te.failTask(taskID, fmt.Sprintf("agent %q not found", task.AgentID))
		return fmt.Errorf("task_executor: agent %q not found", task.AgentID)
	}

	// Count running tasks for this specific agent via the store.


	runningTasks, err := te.store.List(taskstore.TaskFilter{Status: "running", AgentID: task.AgentID})
	if err != nil {
		return fmt.Errorf("task_executor: list running tasks for agent %q: %w", task.AgentID, err)
	}
	if len(runningTasks) >= te.maxConcurrent {
		return fmt.Errorf("task_executor: concurrency limit reached for agent %q (%d running)", task.AgentID, len(runningTasks))
	}

	// Transition to running.
	now := time.Now().UTC()
	updated, err := te.store.Update(taskID, taskstore.TaskPatch{
		Status:    ptrStr("running"),
		StartedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("task_executor: update task %q to running: %w", taskID, err)
	}
	task = updated

	taskCtx, cancel := context.WithCancel(ctx)
	te.mu.Lock()
	te.running[taskID] = cancel
	te.mu.Unlock()

	go te.runTask(taskCtx, task, cancel)
	return nil
}

// runTask executes the agent prompt and updates the task on completion.
func (te *TaskExecutor) runTask(ctx context.Context, task *taskstore.TaskEntity, cancel context.CancelFunc) {
	defer cancel()
	defer func() {
		te.mu.Lock()
		delete(te.running, task.ID)
		te.mu.Unlock()
	}()

	// Inject the agent ID into the tool context used during this task session.
	taskCtx := tools.WithAgentID(ctx, task.AgentID)

	sessionKey := fmt.Sprintf("agent:%s:task:%s", task.AgentID, task.ID)
	prompt := te.buildPrompt(task)

	resp, err := te.agentLoop.processTaskDirect(taskCtx, task.AgentID, prompt, sessionKey)
	if err != nil {
		slog.Error("task_executor: agent execution failed", "task_id", task.ID, "agent_id", task.AgentID, "error", err)
		te.failTask(task.ID, fmt.Sprintf("execution error: %v", err))
		return
	}

	// Check whether the agent already called task_update (task status is terminal).
	current, lerr := te.store.Get(task.ID)
	if lerr != nil {
		slog.Warn("task_executor: could not re-read task after execution", "task_id", task.ID, "error", lerr)
		return
	}
	if current.Status == "completed" || current.Status == "failed" {
		// Agent already called task_update which fired onTaskComplete via the tool callback.
		// Do not fire again to avoid duplicate parent notifications.
		return
	}

	// Agent did not call task_update — auto-complete.
	now := time.Now().UTC()
	result := resp
	if result == "" {
		result = "Task completed"
	}
	final, uerr := te.store.Update(task.ID, taskstore.TaskPatch{
		Status:      ptrStr("completed"),
		Result:      &result,
		CompletedAt: &now,
	})
	if uerr != nil {
		slog.Error("task_executor: auto-complete task failed", "task_id", task.ID, "error", uerr)
		return
	}
	te.onTaskComplete(final)
}

// buildPrompt constructs the prompt sent to the agent for a task.
func (te *TaskExecutor) buildPrompt(task *taskstore.TaskEntity) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", task.Title))
	if task.Prompt != "" {
		sb.WriteString(task.Prompt)
		sb.WriteString("\n\n")
	}
	sb.WriteString(fmt.Sprintf("Priority: %d (1=highest, 5=lowest)\n", task.Priority))
	sb.WriteString(fmt.Sprintf("Task ID: %s\n\n", task.ID))
	sb.WriteString("When you have finished this task, call `task_update` with:\n")
	sb.WriteString(fmt.Sprintf("  task_id: %q\n", task.ID))
	sb.WriteString("  status: \"completed\" (or \"failed\" if unsuccessful)\n")
	sb.WriteString("  result: a brief summary of what was accomplished\n")
	return sb.String()
}

// onTaskComplete handles post-completion logic: parent notification.
func (te *TaskExecutor) onTaskComplete(task *taskstore.TaskEntity) {
	if task.ParentTaskID == "" {
		return
	}

	siblings, err := te.store.List(taskstore.TaskFilter{ParentTaskID: task.ParentTaskID})
	if err != nil {
		slog.Warn("task_executor: could not list siblings", "parent_id", task.ParentTaskID, "error", err)
		return
	}
	for _, s := range siblings {
		if s.Status == "queued" || s.Status == "running" {
			return
		}
	}

	// All siblings done — notify the parent agent.
	parent, err := te.store.Get(task.ParentTaskID)
	if err != nil {
		slog.Warn("task_executor: could not load parent task", "parent_id", task.ParentTaskID, "error", err)
		return
	}
	if parent.Status != "running" {
		return
	}

	summary := te.buildChildSummary(siblings)
	sessionKey := fmt.Sprintf("agent:%s:task:%s", parent.AgentID, parent.ID)
	followUp := fmt.Sprintf("All child tasks of task %q have completed.\n\n%s", parent.ID, summary)

	go func() {
		_, ferr := te.agentLoop.processTaskDirect(context.Background(), parent.AgentID, followUp, sessionKey)
		if ferr != nil {
			slog.Warn("task_executor: parent follow-up failed", "parent_id", parent.ID, "error", ferr)
		}
	}()
}

// buildChildSummary produces a markdown summary of all child task results.
func (te *TaskExecutor) buildChildSummary(children []taskstore.TaskEntity) string {
	var sb strings.Builder
	sb.WriteString("## Child Task Results\n\n")
	for _, c := range children {
		sb.WriteString(fmt.Sprintf("- **%s** (status: %s)", c.Title, c.Status))
		if c.Result != "" {
			sb.WriteString(fmt.Sprintf(": %s", c.Result))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// failTask marks a task as failed with the given reason.
func (te *TaskExecutor) failTask(taskID, reason string) {
	now := time.Now().UTC()
	if _, err := te.store.Update(taskID, taskstore.TaskPatch{
		Status:      ptrStr("failed"),
		Result:      &reason,
		CompletedAt: &now,
	}); err != nil {
		slog.Error("task_executor: could not mark task failed", "task_id", taskID, "error", err)
	}
}

// Stop cancels all running task goroutines.
func (te *TaskExecutor) Stop() {
	te.mu.Lock()
	defer te.mu.Unlock()
	for _, cancel := range te.running {
		cancel()
	}
	te.running = make(map[string]context.CancelFunc)
}

// CheckQueuedTasks picks up the highest-priority queued task per agent and starts it.
// Called by the heartbeat service.
func (te *TaskExecutor) CheckQueuedTasks(ctx context.Context) {
	queued, err := te.store.List(taskstore.TaskFilter{Status: "queued"})
	if err != nil {
		slog.Warn("task_executor: check queued tasks: list failed", "error", err)
		return
	}
	if len(queued) == 0 {
		return
	}

	// Group by agent_id; pick the first (lowest priority number = highest priority) per agent.
	byAgent := make(map[string]*taskstore.TaskEntity)
	for i := range queued {
		t := &queued[i]
		if _, exists := byAgent[t.AgentID]; !exists {
			byAgent[t.AgentID] = t
		}
	}

	for _, t := range byAgent {
		if err := te.ExecuteTask(ctx, t.ID); err != nil {
			slog.Warn("task_executor: heartbeat: could not start task", "task_id", t.ID, "error", err)
		}
	}
}

// ptrStr returns a pointer to s.
func ptrStr(s string) *string { return &s }
