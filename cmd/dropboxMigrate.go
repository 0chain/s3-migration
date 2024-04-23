/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/0chain/s3migration/util"
	"github.com/spf13/cobra"
)

	var (
		cfgDropbox = ""
		dropboxMigrateCmd = &cobra.Command{
		Use:   "dropboxmigration",
		Short: "Migrate data from Dropbox to dStorage",
		Long: `dropboxMigrate is a command-line utility that facilitates the migration of files and data
		from Dropbox to a decentralized storage allocation on the 0chain network. It leverages the
		Dropbox SDK for Go to interact with Dropbox and the 0chain gosdk for communication with the
		0chain network.`,
			Run: func(cmd *cobra.Command, args []string) {
				// Implementation for the migration process from Dropbox to dStorage
				fmt.Println("Migration from Dropbox to dStorage started...")
				startMigration()
				// TODO: Add the logic for Dropbox to dStorage migration
			},
	}
)



func init() {
	cobra.OnInitialize(initDropboxConfig)
	rootCmd.AddCommand(dropboxMigrateCmd)
	dropboxMigrateCmd.PersistentFlags().String("directory", "", "DirectoryName")
	dropboxMigrateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	dropboxMigrateCmd.Flags().StringVar(&configDir, "configDir", util.GetDefaultConfigDir(), "configuration directory")
	dropboxMigrateCmd.Flags().StringVar(&cfgDropbox, "dropbox", "dropbox.yaml", "config file")
}



func startMigration() {
	fmt.Printf("Intializing...")
	
}


func initDropboxConfig() {
	fmt.Println("Dropbox Migration in line...")
	fmt.Println(cfgDropbox)
	_, err :=loadDropboxFile(filepath.Join(configDir, cfgDropbox))
	if err != nil {
			// Handle the error appropriately
			log.Fatalf("Failed to load Dropbox config: %v", err)
		}
		fmt.Printf("config directory + %v\n", cfgDropbox)

	}