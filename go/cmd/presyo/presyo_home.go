package main

import "os"

// defaultPresyoHome returns $PRESYO_HOME, else the user's home
// directory (the service account's $HOME on the ingest host), else ".".
// No account name or path of any server is baked into the binary.
func defaultPresyoHome() string {
	if h := os.Getenv("PRESYO_HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}
