package main

import (
	"fmt"
	"os"
)

var (
	userliToken     string
	userliBaseURL   string
	aliasListenAddr string
)

func main() {
	userliBaseURL = os.Getenv("USERLI_BASE_URL")
	if userliBaseURL == "" {
		userliBaseURL = "http://localhost:8000"
	}

	userliToken = os.Getenv("USERLI_TOKEN")
	if userliToken == "" {
		fmt.Println("USERLI_TOKEN is required")
		os.Exit(1)
	}

	aliasListenAddr = os.Getenv("ALIAS_LISTEN_ADDR")
	if aliasListenAddr == "" {
		aliasListenAddr = ":10001"
	}

	userli := NewUserli(userliToken, userliBaseURL)
	alias := NewAlias(aliasListenAddr, userli)

	alias.Listen()
}
