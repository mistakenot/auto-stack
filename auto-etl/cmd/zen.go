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
}

func init() {
	rootCmd.AddCommand(zenCmd)
}

var zenCmd = &cobra.Command{
	Use:    "zen",
	Short:  "Print a random piece of ETL wisdom",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(zenKoans))))
		fmt.Println(zenKoans[n.Int64()])
	},
}
