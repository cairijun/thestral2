package main

import (
	"context"
	"flag"

	"net/http"
	_ "net/http/pprof"
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
