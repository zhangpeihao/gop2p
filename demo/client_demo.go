package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	programName = "gop2p client"
	version     = "0.0.1"
)

var (
	address   *string = flag.String("Address", "127.0.0.1:9000", "The p2p server address.")
	localAddr *string = flag.String("LocalAddress", "", "The local address.")
	cid       *string = flag.String("ClientID", "1", "The Client ID.")
)

var (
	g_conn *net.UDPConn
	g_lock sync.Mutex
)

type commandResp struct {
	Cmd   string `json:"cmd"`
	Ecode int    `json:"ecode"`
}

func send(msg string, addr *net.UDPAddr) (err error) {
	g_lock.Lock()
	g_conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if addr == nil {
		_, err = g_conn.Write([]byte(msg))
	} else {
		_, err = g_conn.WriteToUDP([]byte(msg), addr)
	}
	if err != nil {
		fmt.Println("Send error:", err)
	}
	g_lock.Unlock()
	return
}

func read(buf []byte) (n int, resp commandResp, err error) {
	g_conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err = g_conn.ReadFromUDP(buf)
	if err != nil {
		return
	}
	err = json.Unmarshal(buf[:n], &resp)
	if err != nil {
		return
	}
	return
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	udpAddr, err := net.ResolveUDPAddr("udp", "192.168.20.111:9000")
	if err != nil {
		log.Fatal("Resolve address err:", err)
		os.Exit(-1)
	}

	var localAddress *net.UDPAddr
	if len(*localAddr) > 0 {
		localAddress, err = net.ResolveUDPAddr("udp", *localAddr)
		if err != nil {
			log.Fatal("Resolve local address err:", err)
			os.Exit(-1)
		}
	}

	//	g_conn, err = net.DialUDP("udp", localAddress, udpAddr)
	g_conn, err = net.ListenUDP("udp", localAddress)
	if err != nil {
		log.Fatal("dial err:", err)
		os.Exit(-1)
	}
	defer g_conn.Close()
	//	file, err := g_conn.File()
	//	if err != nil {
	//		log.Fatal("Get file err:", err)
	//		os.Exit(-1)
	//	}
	//	syscall.Setsockopt(file.Fd(), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)

	// register
	err = send(fmt.Sprintf(`{"cmd":"reg","cid":"%s"}`, *cid), udpAddr)
	if err != nil {
		os.Exit(-1)
	}

	var resp commandResp
	buf := make([]byte, 65507)

	_, resp, err = read(buf)
	if err != nil {
		fmt.Println("Register err:", err)
		os.Exit(-1)
	}
	if resp.Ecode != 200 {
		fmt.Printf("Register failed:%d\n", resp.Ecode)
		os.Exit(-1)
	}
	fmt.Printf("resp:\r\n%+v\r\n", resp)

	udpAddr, err = net.ResolveUDPAddr("udp", "192.168.20.211:9000")
	if err != nil {
		log.Fatal("Resolve address err:", err)
		os.Exit(-1)
	}
	err = send(fmt.Sprintf(`{"cmd":"reg","cid":"%s"}`, *cid), udpAddr)
	if err != nil {
		os.Exit(-1)
	}
	_, resp, err = read(buf)
	if err != nil {
		fmt.Println("Register err:", err)
		os.Exit(-1)
	}
	if resp.Ecode != 200 {
		fmt.Printf("Register failed:%d\n", resp.Ecode)
		os.Exit(-1)
	}
	fmt.Printf("resp:\r\n%+v\r\n", resp)

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT)
	_ = <-ch
	fmt.Printf("Exit normally\n")

	// Reuse connect
}
