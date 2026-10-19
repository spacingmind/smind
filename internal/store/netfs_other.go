//go:build !linux && !darwin && !windows

package store

func networkFS(string) bool { return false }
