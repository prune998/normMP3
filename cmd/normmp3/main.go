package main

import (
	"flag"
	"fmt"

	"github.com/prune998/normMP3/internal/ui"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "affiche la version et quitte")
	flag.Parse()
	if *showVersion {
		fmt.Println("normMP3", version)
		return
	}
	ui.Run(version)
}
