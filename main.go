package main

import (
	"embed"

	"github.com/junjieliang672/marmota/src/cmd"
)

var (
	//go:embed src/marmota_config/*
	content embed.FS
)

func main() {
	cmd.Run(content)
}
