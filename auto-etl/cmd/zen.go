package cmd

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/spf13/cobra"
)

var zenKoans = []string{
	"The best log is the one you never have to read.",
	"A pipeline that runs once is a script. A pipeline that runs twice is a system.",
	"Parquet is just a fancy way of saying 'I care about column order.'",
	"If your ETL is idempotent, you can sleep at night.",
	"The diff tells you what changed. The review tells you why it mattered.",
	"Normalize your data, but never your curiosity.",
	"A session without context is just a list of tool calls.",
	"The schema evolved. The data remained.",
	"Before enlightenment: parse JSON, handle errors. After enlightenment: parse JSON, handle errors.",
	"The empty partition contains all possibilities.",
	"What is the sound of one row inserting?",
	"To seek the bug, first become the bug.",
	"The wise engineer does not fear the nil pointer — they expect it.",
	"A thousand retries begin with a single timeout.",
	"There is no diff between the master branch and the feature branch — only the illusion of separation.",
	"When the rate limit strikes, the patient process drinks tea.",
	"The schema that can be described is not the eternal schema.",
	"In the beginner's mind there are many columns. In the expert's mind there are few.",
	"Do not chase the merged PR. Sit quietly, and the webhook will come to you.",
	"The token expires. The wisdom remains.",
	"You cannot step into the same stream twice, but you can replay the same offset.",
	"Attachment to mutable state is the root of all suffering.",
	"The backfill is the journey. The incremental is the destination.",
	"What was your original query before you were born?",
	"The goroutine that grasps at channels finds only deadlock.",
	"Ten thousand rows flow through the pipeline. Not one knows it is being transformed.",
	"The master debugger stares at the logs and sees nothing. Then sees everything.",
	"To understand recursion, first understand recursion.",
	"The data does not care about your schema. The schema does not care about your queries. Only the user suffers.",
	"An unread metric is indistinguishable from a metric that does not exist.",
	"The fastest query is the one you never run.",
}

func newZenCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "zen",
		Short:  "Print a random piece of ETL wisdom",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(zenKoans))))
			fmt.Println(zenKoans[n.Int64()])
		},
	}
}
