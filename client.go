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
	Addr() net.Addr
}

type Client interface {
	Request(msgId uint16, data []byte, addr net.Addr) (resp *Package, err error)
	Response(msgId uint16, seqId uint32, data []byte, addr net.Addr)
	ResponseSuccess(req *Package)
	ResponseError(req *Package, ecode int)
	DialP2P(peerid string) (conn Conn, err error)
	Peers() map[string]Peer
	ID() string
	LocalAddr() string
	Close()
}

type Conn interface {
	SendData(data []byte)
	ResponseSuccess(req *Package)
	ResponseError(req *Package, ecode int)
	Peer() Peer
	PeerID() string
	RemoteAddr() net.Addr
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
	peer   Peer
	client *client
}

////////////////////////////////////////////////////////////
// peer functions
func (p *peer) ID() string {
	return p.id
}

func (p *peer) Addr() net.Addr {
	return p.addr
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
		respMap:   make(map[uint32]chan *Package),
		conns:     make(map[string]Conn),
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

func (cl *client) Request(msgId uint16, data []byte, addr net.Addr) (resp *Package, err error) {
	// 1. Send command
	pack := Package{
		SeqID:      NextSeqID(),
		MsgID:      msgId,
		Data:       data,
		Addr:       addr,
		IsResponse: false,
	}
	respChan := make(chan *Package, 2)
	cl.respMap[pack.SeqID] = respChan
	cl.sendQueue <- &pack

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

func (cl *client) ResponseSuccess(req *Package) {
	pack := &Package{
		MsgID:      req.MsgID,
		SeqID:      req.SeqID,
		Data:       COMMON_RESP_SUCCESS,
		Addr:       req.Addr,
		IsResponse: true,
	}
	cl.sendQueue <- pack
}
func (cl *client) ResponseError(req *Package, ecode int) {
	pack := &Package{
		MsgID:      req.MsgID,
		SeqID:      req.SeqID,
		Data:       []byte(fmt.Sprintf(`{"ecode":%s}`, ecode)),
		Addr:       req.Addr,
		IsResponse: true,
	}
	cl.sendQueue <- pack
}

func (cl *client) Response(msgId uint16, seqId uint32, data []byte, addr net.Addr) {
	pack := &Package{
		MsgID:      msgId,
		SeqID:      seqId,
		Data:       data,
		Addr:       addr,
		IsResponse: true,
	}
	if pack.SeqID == 0 {
		pack.SeqID = NextSeqID()
	}
	cl.sendQueue <- pack
}

func (cl *client) Send(pack *Package) {
	cl.sendQueue <- pack
}

func (cl *client) DialP2P(peerid string) (conn Conn, err error) {
	// 1. Send TO_CONNECT command to P2P server
	resp, err := cl.Request(MSGID_CONNECT, []byte(fmt.Sprintf(`{"peerid":"%s"}`, peerid)), cl.serverAddr)
	if err != nil {
		return nil, err
	}
	var connectResp ConnectResponse
	err = json.Unmarshal(resp.Data, &connectResp)
	if err != nil {
		return nil, err
	}
	if connectResp.Ecode != ERROR_CODE_SUCCESS {
		return nil, errors.New(fmt.Sprintf("Response %d", connectResp.Ecode))
	}
	// 2. Create new connection
	conn, err = newP2PConn(peerid, connectResp.Addr, cl)
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
	log.Println("Closing...")
	_, _ = cl.Request(MSGID_UNREG, []byte(fmt.Sprintf(`{"id":"%s"}`, cl.id)), cl.serverAddr)
	cl.exit = true
	cl.c.Close()
}

func (cl *client) reg() error {
	resp, err := cl.Request(MSGID_REG, []byte(fmt.Sprintf(`{"id":"%s"}`, cl.id)), cl.serverAddr)
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
	resp, err := cl.Request(MSGID_LIST, nil, cl.serverAddr)
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
		MsgID:      MSGID_KEEPALIVE,
		Addr:       cl.serverAddr,
		IsResponse: false,
	}
	ticker := time.Tick(30 * time.Second)
	for _ = range ticker {
		if cl.exit {
			break
		}
		keepaliveReq.SeqID = NextSeqID()
		cl.Send(&keepaliveReq)
	}
}

func (cl *client) sendLoop() {
	for !cl.exit {
		select {
		case pack := <-cl.sendQueue:
			cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := pack.Send(cl.c)
			if err != nil {
				log.Println("Send error:", err)
				opErr, ok := err.(*net.OpError)
				if ok && opErr.Timeout() {
					continue
				} else {
					// Todo: reconnect
					log.Println("Send error:", err)
					break
				}
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
			opErr, ok := err.(*net.OpError)
			if ok && opErr.Timeout() {
				continue
			} else {
				log.Printf("Read error:", err)
				break
			}
		}
		pack, err := ParsePackage(buf, addr)
		if err != nil {
			log.Printf("ParsePackage(% 0x) error: %s\r\n", buf, err)
			continue
		}
		addrString := addr.String()
		if pack.IsResponse {
			respChan, found := cl.respMap[pack.SeqID]
			if found {
				respChan <- pack
				continue
			}
		}
		if addrString == serverAddr {
			// Server connection
			if !pack.IsResponse {
				// Server request
				switch pack.MsgID {
				case MSGID_TO_CONNECT:
					// Connected by a peer
					var req ToConnectRequest
					err := json.Unmarshal(pack.Data, &req)
					if err != nil {
						log.Println("MSGID_TO_CONNECT - Parse error:", err)
						cl.Response(MSGID_TO_CONNECT, pack.SeqID, COMMON_RESP_FAILED, cl.serverAddr)
						continue
					}
					go newP2PConn(req.ID, req.Address, cl)
					cl.Response(MSGID_TO_CONNECT, pack.SeqID, COMMON_RESP_SUCCESS, cl.serverAddr)
				default:
					cl.handler.ReceivedCommand(pack)
				}
			} else {
				cl.handler.ReceivedCommand(pack)
			}
		} else {
			findconn, found := cl.conns[addrString]
			if found {
				cl.handler.ReceivedData(findconn, pack)
			} else {
				// Package from unknown peer, should retry
				log.Printf("########## Peer address: %s\r\n", addrString)
				cl.Response(pack.MsgID, pack.SeqID, COMMON_RESP_RETRY, addr)
			}
		}
	}
}

////////////////////////////////////////////////////////////
// client functions
func newP2PConn(peerid, peeraddr string, cl *client) (Conn, error) {
	peerAddr, err := net.ResolveUDPAddr("udp", peeraddr)
	if err != nil {
		return nil, err
	}
	peer := &peer{
		id:   peerid,
		addr: peerAddr,
	}
	c := conn{
		peer:   peer,
		client: cl,
	}
	cl.conns[peeraddr] = &c

	reqData := []byte(fmt.Sprintf(`{"peerid":"%s"}`, cl.id))
LOOP:
	for i := 0; ; i++ {
		resp, err := cl.Request(MSGID_TRY_CONNECT, reqData, peerAddr)
		if err != nil {
			if i == 3 {
				delete(cl.conns, peeraddr)
				return nil, err
			}
		} else {
			var connectResp ConnectResponse
			err = json.Unmarshal(resp.Data, &connectResp)
			if err != nil {
				delete(cl.conns, peeraddr)
				return nil, err
			}
			switch connectResp.Ecode {
			case ERROR_CODE_SUCCESS:
				break LOOP
			case ERROR_CODE_RETRY:
				time.Sleep(time.Duration(i*2) * time.Second)
				continue LOOP
			default:
				continue LOOP
			}
		}
	}
	return &c, nil
}
func (co *conn) SendData(data []byte) {
	pack := Package{
		MsgID:      MSGID_DATA,
		SeqID:      NextSeqID(),
		Data:       data,
		IsResponse: false,
		Addr:       co.peer.Addr(),
	}
	co.client.Send(&pack)
}

func (co *conn) ResponseSuccess(req *Package) {
	pack := Package{
		MsgID:      req.MsgID,
		SeqID:      req.SeqID,
		Data:       COMMON_RESP_SUCCESS,
		Addr:       req.Addr,
		IsResponse: true,
	}
	co.client.Send(&pack)
}
func (co *conn) ResponseError(req *Package, ecode int) {
	pack := Package{
		MsgID:      req.MsgID,
		SeqID:      req.SeqID,
		Data:       []byte(fmt.Sprintf(`{"ecode":%s}`, ecode)),
		Addr:       req.Addr,
		IsResponse: true,
	}
	co.client.Send(&pack)
}

func (co *conn) Peer() Peer {
	return co.peer
}
func (co *conn) PeerID() string {
	return co.peer.ID()
}
func (co *conn) RemoteAddr() net.Addr {
	return co.peer.Addr()
}
