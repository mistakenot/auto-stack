package runner

import "fmt"

func ScheduledRunName(runID int64, taskID string) string {
	return fmt.Sprintf("autowatch-run-%d--%s", runID, taskID)
}
