//go:build !android

package main

func notificationsEnabled() bool     { return true }
func openNotificationSettings() bool { return false }
