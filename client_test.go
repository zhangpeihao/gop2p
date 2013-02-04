package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

const (
	programName = "gop2p client"
	version     = "0.0.1"
)

var (
	address *string = flag.String("Address", "127.0.0.1:9000", "The p2p server address.")
	cid     *string = flag.String("ClientID", "1", "The Client ID.")
)

var (
	g_conn     *net.UDPConn
	g_msgQueue chan *string = make(chan *string, 100)
	g_lock     sync.Mutex
)

func printHelp() {
	fmt.Println("Command list:\r\nlist - List all clients\r\nconnect - Connect a peer client\r\nexit - Exit")
}

func send(msg string) (err error) {
	g_lock.Lock()
	g_conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = g_conn.Write([]byte(msg))
	if err != nil {
		fmt.Println("Send error:", err)
	}
	g_lock.Unlock()
	return
}

type commandResp struct {
	Cmd   string `json:"cmd"`
	Ecode int    `json:"ecode"`
}

type listResp struct {
	Clients map[string]string `json:"clients"`
}

type connectResp struct {
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

func keepalive() {
	ticker := time.Tick(30 * time.Second)
	for _ = range ticker {
		send(`{"cmd":"ka"}`)
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

	g_conn, err = net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Fatal("dial err:", err)
		os.Exit(-1)
	}
	defer g_conn.Close()

	// register
	err = send(fmt.Sprintf(`{"cmd":"reg","cid":"%s"}`, *cid))
	if err != nil {
		os.Exit(-1)
	}

	fmt.Println("    ++++ P2P Client ++++")
	prompt := "client> "
	var cmd string
	var resp commandResp
	var n int
	buf := make([]byte, 65507)

	n, resp, err = read(buf)
	if err != nil {
		fmt.Println("Register err:", err)
		os.Exit(-1)
	}
	if resp.Ecode != 200 {
		fmt.Printf("Register failed:%d\n", resp.Ecode)
		os.Exit(-1)
	}

	go keepalive()
LOOP:
	for {
		fmt.Print(prompt)
		_, err := fmt.Scanln(&cmd)
		if err != nil {
			printHelp()
			continue
		}
		switch cmd {
		case "exit":
			break LOOP
		case "list":
			send(`{"cmd":"list"}`)
		case "reg":
			send(fmt.Sprintf(`{"cmd":"reg","cid":"%s"}`, *cid))
		case "connect":
			fmt.Print("Peer ID: ")
			var peerId string
			_, err := fmt.Scanln(peerId)
			if err != nil {
				printHelp()
			}
			send(fmt.Sprintf(`{"cmd":"connect","peerid":"%s"}`, peerId))
		default:
			printHelp()
			continue
		}
		n, resp, err = read(buf)
		switch {
		case err != nil:
			fmt.Printf("Response Error: %s\r\n", err.Error())
		case resp.Ecode != 200:
			fmt.Printf("Command Failed: %s\r\n", string(buf[:n]))
		default:
			switch resp.Cmd {
			case "rreg":
				fmt.Println("Resigter OK!")
			case "rlist":
				var tresp listResp
				err = json.Unmarshal(buf[:n], &tresp)
				if err != nil {
					fmt.Println("Parse list response error:", string(buf[:n]))
				} else {
					for key, value := range tresp.Clients {
						if key != *cid {
							fmt.Printf("%s: %s\r\n", key, value)
						} else {
							fmt.Printf("(self)%s: %s\r\n", key, value)
						}
					}
				}
			case "rconnect":
				fmt.Println("Connect OK!")
			}
		}
	}

	// unregister
	_ = send(fmt.Sprintf(`{"cmd":"unreg","cid":"%s"}`, *cid))
}
