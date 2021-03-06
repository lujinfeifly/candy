package main

import (
	"flag"

	"github.com/dearcode/candy/server/gate"
	"github.com/dearcode/candy/server/util"
)

func main() {
	host := flag.String("p", "0.0.0.0:9000", "listen host")
	master := flag.String("m", "0.0.0.0:9001", "master host")
	store := flag.String("s", "0.0.0.0:9004", "store host")
	version := flag.Bool("v", false, "print version")
	flag.Parse()

	if *version {
		util.PrintVersion()
		return
	}

	s := gate.NewGate(*host, *master, *store)
	if err := s.Start(); err != nil {
		println(err.Error())
	}

}
