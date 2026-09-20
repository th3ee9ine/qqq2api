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
		newestTerminal.ID,
		oldestTerminal.ID,
	}, codexTurnStateCollectionTaskIDs(tasks))
	require.Empty(t, tasks[0].Events)
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
