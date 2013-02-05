package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

const (
	programName = "gop2p server"
	version     = "0.0.1"
)

var (
	address *string = flag.String("Address", ":9000", "The bind address.")
)

var (
	g_listener *net.UDPConn
	g_exit     bool
	g_clients  map[string]net.Addr
)

func process(data []byte, addr net.Addr) {
	fmt.Printf("recv: %s\n", string(data))
	var msg map[string]string
	err := json.Unmarshal(data, &msg)
	if err != nil {
		fmt.Println("parse err:", err)
		return
	}
	cmd, found := msg["cmd"]
	if !found {
		fmt.Println("No cmd")
		return
	}
	var resp string
	switch cmd {
	case "reg":
		cid, _ := msg["cid"]
		if len(cid) == 0 {
			fmt.Println("No cid in reg message")
			break
		}
		g_clients[cid] = addr
		fmt.Printf("Reg[%s] from %s\r\n", cid, addr)
		resp = fmt.Sprintf(`{"cmd":"rreg","ecode":200,"addr":"%s"}`, addr.String())
	case "unreg":
		cid, _ := msg["cid"]
		if len(cid) == 0 {
			fmt.Println("No cid in unreg message")
			break
		}
		delete(g_clients, cid)
		resp = `{"cmd":"rreg","ecode":200}`
	case "list":
		resp = fmt.Sprintf(`{"cmd":"rlist","ecode":200,"clients":{`)
		first := true
		for cid, client_addr := range g_clients {
			if first {
				first = false
			} else {
				resp = resp + ","
			}
			resp = resp + fmt.Sprintf(`"%s":"%s"`, cid, client_addr.String())
		}
		resp = resp + "}}"
	case "ka":

	}
	if len(resp) == 0 {
		resp = fmt.Sprintf(`{"cmd":"r%s","ecode":400}`, cmd)
	}
	fmt.Println("resp:", resp)
	_, err = g_listener.WriteTo([]byte(resp), addr)
	if err != nil {
		log.Println("Write err:", err)
	}
}

func listen() {
	buf := make([]byte, 65507)
	for !g_exit {
		n, addr, err := g_listener.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		data := make([]byte, n)
		copy(data, buf)
		go process(data, addr)
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	udpAddr, err := net.ResolveUDPAddr("udp", *address)
	if err != nil {
		log.Fatal("Resolve address err:", err)
		os.Exit(-1)
	}

	g_listener, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatal("listen err:", err)
		os.Exit(-1)
	}

	g_clients = make(map[string]net.Addr)

	go listen()
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT)
	_ = <-ch
	g_exit = true
	g_listener.Close()
	fmt.Printf("Exit normally\n")
}
