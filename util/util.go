package util

import (
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

// GetConfigDir get config directory , default is ~/.zcn/
func GetDefaultConfigDir() (configDir string) {

	configDir = filepath.Join(GetHomeDir(), ".zcn")

	if err := os.MkdirAll(configDir, 0744); err != nil {
		panic(err)
	}
	return
}

// GetHomeDir Find home directory.
func GetHomeDir() (homeDir string) {
	var err error
	if homeDir, err = homedir.Dir(); err != nil {
		panic(err)
	}
	return
}
