package main

import (
	"context"
	"flag"
)

func main() {
	configFile := flag.String("c", "", "configuration file")
	flag.Parse()
	if *configFile == "" {
		panic("a configuration file must be specified")
	}

	config, err := ParseConfigFile(*configFile)
	if err != nil {
		panic(err)
	}

	if config.Misc.DumpStats != nil {
		go RunStatsDumper(*config.Misc.DumpStats)
	}

	app, err := NewThestralApp(*config)
	if err != nil {
		panic(err)
	}

	if err = app.Run(context.Background()); err != nil {
		panic(err)
	}
}
