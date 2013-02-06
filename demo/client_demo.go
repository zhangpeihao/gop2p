package main

import (
	"flag"
	"fmt"
	"github.com/zhangpeihao/gop2p"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	programName = "gop2p client"
	version     = "0.1.1"
)

var (
	serverAddr *string = flag.String("ServerAddress", "127.0.0.1:9000", "The p2p server address.")
	localAddr  *string = flag.String("LocalAddress", "", "The local address.")
	cid        *string = flag.String("ClientID", "1", "The Client ID.")
)

type TestHandler struct {
}

func (handler *TestHandler) ReceivedCommand(pack *gop2p.Package) {
	log.Printf("ReceivedCommand: %+v\r\n", *pack)
	if !pack.IsResponse {
		g_client.ResponseSuccess(pack)
	}
}

func (handler *TestHandler) ReceivedData(conn gop2p.Conn, pack *gop2p.Package) {
	log.Printf("ReceivedData: %+v\r\n", *pack)
	if !pack.IsResponse {
		conn.ResponseSuccess(pack)
	}
}

var (
	g_client gop2p.Client
)

func sendTestData(conn gop2p.Conn) {
	ticker := time.Tick(3 * time.Second)
	for _ = range ticker {
		conn.SendData([]byte("Test"))
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	testHandler := TestHandler{}
	g_client, err := gop2p.NewClient(*cid, *serverAddr, *localAddr, &testHandler)
	if err != nil {
		log.Println("NewClient err:", err)
		os.Exit(-1)
	}
	defer g_client.Close()

	peers := g_client.Peers()
	if peers == nil {
		log.Println("Get peers failed")
		os.Exit(-1)
	}
	var testid string
	for peerid, peer := range peers {
		fmt.Printf("[%s] - %s\r\n", peerid, peer.Addr())
		if peerid != *cid && len(peerid) != 0 {
			testid = peerid
		}
	}
	if len(testid) == 0 {
		log.Println("No peers")
	} else {
		log.Printf("Connect peer: %s", testid)
		conn, err := g_client.DialP2P(testid)
		if err != nil {
			log.Println("DialP2P err:", err)
			os.Exit(-1)
		}
		go sendTestData(conn)
	}
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT)
	_ = <-ch
	fmt.Printf("Exit normally\n")
}
