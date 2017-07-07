package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"net/http"
	_ "net/http/pprof"

	"github.com/richardtsai/thestral2/lib"
	"github.com/richardtsai/thestral2/tools"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func printUsage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage: thestral2 [tool] [arguments]\n\n")
	_, _ = fmt.Fprintf(os.Stderr, "Main arguments:\n")
	flag.PrintDefaults()
	_, _ = fmt.Fprintln(os.Stderr)
	tools.PrintUsage()
	os.Exit(0)
}

func main() {
	flag.Usage = printUsage
	tools.Init()
	if len(os.Args) > 1 && os.Args[1][0] != '-' { // run tools
		tools.Run(os.Args[1], os.Args[1:])
		return
	}

	configFile := flag.String("c", "", "configuration file")
	flag.Parse()

	if *configFile == "" {
		printUsage()
	}

	config, err := lib.ParseConfigFile(*configFile)
	if err != nil {
		panic(err)
	}

	app, err := NewThestralApp(*config)
	if err != nil {
		panic(err)
	}

	if config.Misc.PProfAddr != "" {
		go func() {
			e := http.ListenAndServe(config.Misc.PProfAddr, nil)
			if e != nil {
				panic(e)
			}
		}()
	}

	if err = app.Run(context.Background()); err != nil {
		panic(err)
	}
}
