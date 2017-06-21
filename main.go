package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"net/http"
	_ "net/http/pprof"
)

func main() {
	configFile := flag.String("c", "", "configuration file")
	printVersion := flag.Bool("v", false, "print version")
	flag.Parse()

	if *printVersion {
		fmt.Printf("%s version: %s\n", os.Args[0], ThestralVersion)
		os.Exit(0)
	}
	if *configFile == "" {
		panic("a configuration file must be specified")
	}

	config, err := ParseConfigFile(*configFile)
	if err != nil {
		panic(err)
	}

	app, err := NewThestralApp(*config)
	if err != nil {
		panic(err)
	}

	if config.Misc.PProfAddr != "" {
		go func() {
			err := http.ListenAndServe(config.Misc.PProfAddr, nil)
			if err != nil {
				panic(err)
			}
		}()
	}

	if err = app.Run(context.Background()); err != nil {
		panic(err)
	}
}
