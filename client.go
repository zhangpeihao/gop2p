package gop2p

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

type DataHandler interface {
	ReceivedCommand(pack *Package)
	ReceivedData(conn Conn, pack *Package)
}

type Peer interface {
	ID() string
	Addr() string
}

type Client interface {
	Send(pack *Package) (resp *Package, err error)
	DialP2P(peerid string) (conn Conn, err error)
	Peers() map[string]Peer
	ID() string
	LocalAddr() string
	Close()
}

type Conn interface {
	Send(pack *Package) (err error)
	Peer() Peer
	PeerID() string
	RemoteAddr() string
}

type peer struct {
	id   string
	addr *net.UDPAddr
}

type client struct {
	id              string
	c               *net.UDPConn
	peers           map[string]Peer
	peersUpdateTime int64
	localAddr       *net.UDPAddr
	serverAddr      *net.UDPAddr
	exit            bool
	sendQueue       chan *Package
	respMap         map[uint32]chan *Package
	conns           map[string]Conn
	handler         DataHandler
}

type conn struct {
	peer Peer
}

////////////////////////////////////////////////////////////
// peer functions
func (p *peer) ID() string {
	return p.id
}

func (p *peer) Addr() string {
	return p.addr.String()
}

////////////////////////////////////////////////////////////
// client functions
func NewClient(id string, serverAddr string, localAddr string, handler DataHandler) (Client, error) {
	// 1. Connect P2P server
	var err error
	cl := client{
		id:        id,
		exit:      false,
		sendQueue: make(chan *Package, 1000),
		handler:   handler,
	}
	cl.localAddr, err = net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	cl.serverAddr, err = net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return nil, err
	}
	cl.c, err = net.ListenUDP("udp", cl.localAddr)
	if err != nil {
		return nil, err
	}
	go cl.sendLoop()
	go cl.readLoop()

	// 2. Register
	err = cl.reg()
	if err != nil {
		cl.Close()
		return nil, err
	}
	// 3. Get peer list
	err = cl.getPeers()
	if err != nil {
		cl.Close()
		return nil, err
	}
	// 4. Start keepalive
	go cl.keepalive()

	return &cl, nil
}

func (cl *client) Send(pack *Package) (resp *Package, err error) {
	// 1. Send command
	if pack.SeqID == 0 {
		pack.SeqID = NextSeqID()
	}
	respChan := make(chan *Package, 2)
	cl.respMap[pack.SeqID] = respChan
	cl.sendQueue <- pack

	// 2. Wait response
	select {
	case resp = <-respChan:
		break
	case <-time.After(5 * time.Second):
		err = errors.New("Timeout")
	}
	delete(cl.respMap, pack.SeqID) // Memory leak????
	return
}

func (cl *client) DialP2P(peerid string) (conn Conn, err error) {
	// 1. Send TO_CONNECT command to P2P server
	// 2. Get peer address
	// 3. Send TRY_CONNECT command to peer, retry until success or timeout
	return
}

func (cl *client) Peers() map[string]Peer {
	// if peer infomation timeout, get peers from servers
	return cl.peers
}

func (cl *client) ID() string {
	return cl.id
}

func (cl *client) LocalAddr() string {
	return cl.c.LocalAddr().String()
}

func (cl *client) Close() {
	cl.exit = true
	cl.c.Close()
}

func (cl *client) reg() error {
	pack := Package{
		MsgID: MSGID_REG,
		Data:  []byte(fmt.Sprintf(`{"id":"%s"}`, cl.id)),
	}
	resp, err := cl.Send(&pack)
	if err != nil {
		return err
	}
	var commonResp CommonResponse
	err = json.Unmarshal(resp.Data, &commonResp)
	if err != nil {
		return err
	}
	if commonResp.Ecode != ERROR_CODE_SUCCESS {
		return errors.New(fmt.Sprintf("Register response %d", commonResp.Ecode))
	}
	return nil
}

func (cl *client) getPeers() error {
	pack := Package{
		MsgID: MSGID_LIST,
	}
	resp, err := cl.Send(&pack)
	if err != nil {
		return err
	}
	var listResponse ListResponse
	err = json.Unmarshal(resp.Data, &listResponse)
	if err != nil {
		return err
	}
	if listResponse.Ecode != ERROR_CODE_SUCCESS {
		return errors.New(fmt.Sprintf("Get Peers response %d", listResponse.Ecode))
	}
	peers := make(map[string]Peer)
	for peerid, peerAddr := range listResponse.Clients {
		addr, err := net.ResolveUDPAddr("udp", peerAddr)
		if err != nil {
			log.Printf("Parse peer address %s error: %s\r\n", peerAddr, err.Error())
			continue
		}
		newPeer := peer{
			id:   peerid,
			addr: addr,
		}
		peers[peerid] = &newPeer
	}
	cl.peers = peers
	cl.peersUpdateTime = time.Now().Unix()

	return nil
}

func (cl *client) keepalive() {
	keepaliveReq := Package{
		MsgID: MSGID_KEEPALIVE,
	}
	ticker := time.Tick(30 * time.Second)
	for _ = range ticker {
		if cl.exit {
			break
		}
		keepaliveReq.SeqID = 0
		resp, err := cl.Send(&keepaliveReq)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}
		var commonResp CommonResponse
		err = json.Unmarshal(resp.Data, &commonResp)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}
		if commonResp.Ecode != ERROR_CODE_SUCCESS {
			time.Sleep(10 * time.Second)
			continue
		}
	}
}

func (cl *client) sendLoop() {
	for !cl.exit {
		select {
		case pack := <-cl.sendQueue:
			cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := pack.Send(cl.c)
			if err != nil {
				// Todo: reconnect
				log.Println("Send error:", err)
				break
			}
		case <-time.After(5 * time.Second):
			// Check to exit
		}
	}
}

func (cl *client) readLoop() {
	buf := make([]byte, 65507)
	serverAddr := cl.serverAddr.String()
	for !cl.exit {
		cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, addr, err := cl.c.ReadFromUDP(buf)
		if err != nil {
			// Todo: reconnect
			log.Println("Read error:", err)
			break
		}
		pack, err := ParsePackage(buf)
		if err != nil {
			log.Printf("ParsePackage(% 0x) error: %s\r\n", buf, err)
			continue
		}
		if pack.IsResponse {
			respChan, found := cl.respMap[pack.SeqID]
			if found {
				respChan <- pack
				continue
			}
		}
		addrString := addr.String()
		if addrString == serverAddr {
			cl.handler.ReceivedCommand(pack)
		} else {
			findconn, found := cl.conns[addrString]
			if found {
				cl.handler.ReceivedData(findconn, pack)
			} else {
				// New Connection
				newpeer := &peer{
					addr: addr,
				}
				newconn := &conn{
					peer: newpeer,
				}
				cl.conns[addrString] = newconn
				cl.handler.ReceivedData(newconn, pack)
			}
		}
	}
}

////////////////////////////////////////////////////////////
// client functions
func (co *conn) Send(pack *Package) (err error) {
	// 1. Send data
	return errors.New("Not implement")
}
func (co *conn) Peer() Peer {
	return co.peer
}
func (co *conn) PeerID() string {
	return co.peer.ID()
}
func (co *conn) RemoteAddr() string {
	return co.peer.Addr()
}
