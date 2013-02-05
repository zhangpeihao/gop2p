package gop2p

import (
	"flag"
	"fmt"
	"testing"
)

var (
	serverAddr *string = flag.String("ServerAddress", "127.0.0.1:9000", "The p2p server address.")
	localAddr  *string = flag.String("LocalAddress", "", "The local address.")
	cid        *string = flag.String("ClientID", "1", "The Client ID.")
)

type TestHandler struct {
}

func (handler *TestHandler) ReceivedCommand(pack *Package) {
}

func (handler *TestHandler) ReceivedData(conn Conn, pack *Package) {
}

////////////////////////////////////////////////////////////
// Test functions
func TestClient(t *testing.T) {
	flag.Parse()

	testHandler := TestHandler{}
	client, err := NewClient(*cid, *serverAddr, *localAddr, &testHandler)
	if err != nil {
		t.Fatal("NewClient err:", err)
	}
	req := Package{
		MsgID: MSGID_KEEPALIVE,
	}
	_, err = client.Send(&req)
	if err != nil {
		t.Error("SendCommand keep-alive failed:", err)
	}
	peers := client.Peers()
	if peers == nil {
		t.Error("Get peers failed")
	}
	var testid string
	for peerid, peer := range peers {
		fmt.Printf("[%s] - %s\r\n", peerid, peer.Addr())
		if peerid != *cid && len(testid) != 0 {
			testid = peerid
		}
	}
	if len(testid) == 0 {
		t.Fatal("No peers")
	}
	conn, err := client.DialP2P(testid)
	if err != nil {
		t.Fatal("DialP2P err:", err)
	}

	req.SeqID = 0
	err = conn.Send(&req)
	if err != nil {
		t.Fatal("conn.Send err:", err)
	}
}
