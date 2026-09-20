package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListCodexTurnStateCollectionTasksPutsUnfinishedTasksFirst(t *testing.T) {
	s := &OpenAIGatewayService{}
	createTask := func(accountID int64) *CodexTurnStateCollectionTask {
		task, _, err := s.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
			AccountID:    accountID,
			AccountName:  "account",
			RequestModel: "gpt-6-astra",
			OwnerModel:   "gpt-6-astra",
			Source:       CodexTurnStateCollectionSourceManual,
		})
		require.NoError(t, err)
		return task
	}

	oldestRunning := createTask(1)
	_, started := s.StartCodexTurnStateCollectionTask(oldestRunning.ID, CodexTurnStateCollectionTaskStageCollecting, 1)
	require.True(t, started)

	oldestTerminal := createTask(2)
	_, completed := s.CompleteCodexTurnStateCollectionTask(oldestTerminal.ID)
	require.True(t, completed)

	newestQueued := createTask(3)

	newestTerminal := createTask(4)
	_, failed := s.FailCodexTurnStateCollectionTask(newestTerminal.ID, "request_failed")
	require.True(t, failed)

	newestRunning := createTask(5)
	_, started = s.StartCodexTurnStateCollectionTask(newestRunning.ID, CodexTurnStateCollectionTaskStageCollecting, 1)
	require.True(t, started)

	tasks := s.ListCodexTurnStateCollectionTasks()
	require.Equal(t, []string{
		newestRunning.ID,
		newestQueued.ID,
		oldestRunning.ID,
	}, codexTurnStateCollectionTaskIDs(tasks))
	require.Empty(t, tasks[0].Events)
	_, exists := s.GetCodexTurnStateCollectionTask(oldestTerminal.ID)
	require.False(t, exists, "succeeded tasks must not be retained as an archive")
	_, exists = s.GetCodexTurnStateCollectionTask(newestTerminal.ID)
	require.False(t, exists, "failed tasks must not be retained as an archive")
}

func TestCodexTurnStateCollectionTaskTerminalRemovalReleasesCapacity(t *testing.T) {
	s := &OpenAIGatewayService{}
	s.openaiTurnStateTasksOnce.Do(func() {
		s.openaiTurnStateTasks = newCodexTurnStateCollectionTaskRegistry(1)
	})
	create := func() *CodexTurnStateCollectionTask {
		task, _, err := s.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
			AccountID:    42,
			AccountName:  "account",
			RequestModel: "gpt-6-astra",
			OwnerModel:   "gpt-6-astra",
			Source:       CodexTurnStateCollectionSourceManual,
		})
		require.NoError(t, err)
		return task
	}
	first := create()
	failed, ok := s.FailCodexTurnStateCollectionTask(first.ID, "request_failed")
	require.True(t, ok)
	require.False(t, failed.CanRetry, "terminal task IDs are not retained for task-center retries")
	second := create()
	require.NotEqual(t, first.ID, second.ID)
	require.Len(t, s.ListCodexTurnStateCollectionTasks(), 1)
}

func TestCodexTurnStateCollectionTaskRegistryIsProcessLocal(t *testing.T) {
	first := &OpenAIGatewayService{}
	task, _, err := first.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
		AccountID:    42,
		AccountName:  "account",
		RequestModel: "gpt-6-astra",
		OwnerModel:   "gpt-6-astra",
		Source:       CodexTurnStateCollectionSourceManual,
	})
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Len(t, first.ListCodexTurnStateCollectionTasks(), 1)

	// A fresh service represents a restarted process. Task diagnostics must not
	// be restored through the account repository or any other durable storage.
	restarted := &OpenAIGatewayService{}
	require.Empty(t, restarted.ListCodexTurnStateCollectionTasks())
	_, exists := restarted.GetCodexTurnStateCollectionTask(task.ID)
	require.False(t, exists)
}

func codexTurnStateCollectionTaskIDs(tasks []*CodexTurnStateCollectionTask) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
