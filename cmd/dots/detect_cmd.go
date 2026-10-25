package main

import (
	"fmt"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/spf13/cobra"
)

var detectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect host operating system, architecture, and package managers",
	Run: func(cmd *cobra.Command, args []string) {
		sys := detector.Detect()

		fmt.Println("System Environment:")
		fmt.Printf("  OS:               %s\n", sys.OS)
		fmt.Printf("  Architecture:     %s\n", sys.Arch)
		if sys.Distro != detector.DistroUnknown {
			fmt.Printf("  Distribution:     %s\n", sys.Distro)
		}
		fmt.Printf("  Primary Manager:  %s\n", sys.PrimaryManager)

		var mgrNames []string
		for _, m := range sys.Managers {
			mgrNames = append(mgrNames, string(m))
		}
		if len(mgrNames) == 0 {
			mgrNames = []string{"none detected"}
		}
		fmt.Printf("  Available Tools:  %s\n", strings.Join(mgrNames, ", "))
	},
}

func init() {
	rootCmd.AddCommand(detectCmd)
}
