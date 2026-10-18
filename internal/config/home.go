package config

import (
	"os"
	"path/filepath"
)

const dirName = ".smind"

// Dir returns the smind home directory, without creating anything:
// $SMIND_HOME if non-empty, else ~/.smind.
func Dir() string {
	if v := os.Getenv("SMIND_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dirName
	}
	return DirFor(home)
}

// DirFor is Dir's resolution for the home directory of a given user, ignoring
// $SMIND_HOME (the caller decides whether that applies).
func DirFor(home string) string {
	return filepath.Join(home, dirName)
}

// Path returns the config file location inside Dir().
func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}
