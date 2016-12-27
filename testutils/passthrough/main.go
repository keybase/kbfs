package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

var (
	options passthroughOptions

	rootCmd *cobra.Command
)

func init() {
	rootCmd = &cobra.Command{
		Use:   os.Args[0],
		Short: "",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			run(options)
		},
	}

	rootCmd.PersistentFlags().StringVarP(&options.mountPoint, "mountPoint", "m", "/tmp/passphrase-mnt", "mount point")
	rootCmd.PersistentFlags().StringVarP(&options.rootDir, "rootDir", "r", "/tmp/passphrase-root", "the root dir that passthrough maps the mount point to")
	rootCmd.PersistentFlags().StringVarP(&options.rootDir, "logFile", "l", "/tmp/passphrase.log", "path to log file")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
