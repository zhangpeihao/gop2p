package main

import (
	"flag"
	"fmt"
	"github.com/zhangpeihao/gop2p"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const (
	programName = "gop2p server"
	version     = "0.1.1"
)

var (
	address *string = flag.String("Address", ":9000", "The bind address.")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	server, err := gop2p.NewServer(*address)
	if err != nil {
		log.Println("NewServer error:", err)
		os.Exit(-1)
	}
	defer server.Close()
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT)
	_ = <-ch
	fmt.Printf("Exit normally\n")
}
