package gop2p

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

type Server interface {
	Close()
}

type server struct {
	c         *net.UDPConn
	exit      bool
	clients   map[string]net.Addr
	sendQueue chan *Package
}

func NewServer(addr string) (Server, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	c, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	s := &server{
		c:         c,
		exit:      false,
		clients:   make(map[string]net.Addr),
		sendQueue: make(chan *Package, 1000),
	}
	go s.listen()
	go s.sendLoop()
	return s, nil
}

func (s *server) Close() {
	s.exit = true
	s.c.Close()
}

func (s *server) findIDByAddr(addrstring string) string {
	for id, addr := range s.clients {
		if addr.String() == addrstring {
			return id
		}
	}
	return ""
}

func (s *server) send(pack *Package) {
	s.sendQueue <- pack
}

func (s *server) response(msgId uint16, seqId uint32, data []byte, addr net.Addr) {
	resp := Package{
		SeqID:      seqId,
		MsgID:      msgId,
		IsResponse: true,
		Data:       data,
		Addr:       addr,
	}
	s.send(&resp)
}

func (s *server) process(pack *Package) {
	if pack.IsResponse {
		switch pack.MsgID {
		case MSGID_TO_CONNECT:
			var commonResp CommonResponse
			err := json.Unmarshal(pack.Data, &commonResp)
			if err != nil {
				log.Println("MSGID_TO_CONNECT - Parse error:", err)
				break
			}
			if commonResp.Ecode != ERROR_CODE_SUCCESS {
				log.Printf("MSGID_TO_CONNECT - response: %d\r\n", commonResp.Ecode)
				break
			}
			log.Printf("MSGID_TO_CONNECT - OK (addr: %s)\r\n", pack.Addr.String())
		}
	} else {
		switch pack.MsgID {
		case MSGID_REG:
			var req RegRequest
			err := json.Unmarshal(pack.Data, &req)
			if err != nil {
				log.Println("MSGID_REG - Parse error:", err)
				s.response(MSGID_REG, pack.SeqID, COMMON_RESP_FAILED, pack.Addr)
				break
			}
			log.Printf("Client register: %+v\r\n", req)
			s.clients[req.ID] = pack.Addr
			s.response(MSGID_REG, pack.SeqID, COMMON_RESP_SUCCESS, pack.Addr)

		case MSGID_LIST:
			list := `{"ecode":200,"clients":{`
			first := true
			for cid, client_addr := range s.clients {
				if first {
					first = false
				} else {
					list = list + ","
				}
				list = list + fmt.Sprintf(`"%s":"%s"`, cid, client_addr.String())
			}
			list = list + "}}"

			s.response(MSGID_LIST, pack.SeqID, []byte(list), pack.Addr)

		case MSGID_CONNECT:
			var req ConnectRequest
			err := json.Unmarshal(pack.Data, &req)
			if err != nil {
				log.Println("MSGID_CONNECT - Parse error:", err)
				s.response(MSGID_CONNECT, pack.SeqID, COMMON_RESP_FAILED, pack.Addr)
				break
			}
			log.Printf("Connect: %+v\r\n", req)
			client, found := s.clients[req.PeerID]
			if !found {
				log.Println("MSGID_CONNECT - not found client:", req.PeerID)
				s.response(MSGID_CONNECT, pack.SeqID, COMMON_RESP_NOT_FOUND, pack.Addr)
				break
			}
			promoterID := s.findIDByAddr(pack.Addr.String())
			if len(promoterID) == 0 {
				log.Println("MSGID_CONNECT - not found promoter ID:", promoterID)
				s.response(MSGID_CONNECT, pack.SeqID, COMMON_RESP_NOT_FOUND, pack.Addr)
				break
			}
			log.Printf("promoterID: %s, pack.Addr.String(): %s\r\n", promoterID, pack.Addr.String())

			s.response(MSGID_CONNECT, pack.SeqID, []byte(fmt.Sprintf(`{"ecode":200,"peerid":"%s","addr":"%s"}`, req.PeerID, client.String())), pack.Addr)
			// To connect
			toConnectPack := Package{
				MsgID:      MSGID_TO_CONNECT,
				SeqID:      NextSeqID(),
				IsResponse: false,
				Addr:       client,
				Data:       []byte(fmt.Sprintf(`{"id":"%s","address":"%s"}`, promoterID, pack.Addr.String())),
			}
			s.send(&toConnectPack)
		case MSGID_KEEPALIVE:
			s.response(MSGID_KEEPALIVE, pack.SeqID, COMMON_RESP_SUCCESS, pack.Addr)
		case MSGID_UNREG:
			var req UnregRequest
			err := json.Unmarshal(pack.Data, &req)
			if err != nil {
				log.Println("MSGID_UNREG - Parse error:", err)
				s.response(MSGID_UNREG, pack.SeqID, COMMON_RESP_FAILED, pack.Addr)
				break
			}
			log.Printf("Client unregister: %+v\r\n", req)
			client, found := s.clients[req.ID]
			if found {
				if client.String() == pack.Addr.String() {
					delete(s.clients, req.ID)
				}
			}
			s.response(MSGID_UNREG, pack.SeqID, COMMON_RESP_SUCCESS, pack.Addr)
		}
	}
}

func (s *server) listen() {
	log.Println("listening ...")
	buf := make([]byte, 65507)
	for !s.exit {
		s.c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, addr, err := s.c.ReadFromUDP(buf)
		if err != nil {
			log.Println("Read error:", err)
			opErr, ok := err.(*net.OpError)
			if ok && opErr.Timeout() {
				continue
			} else {
				log.Println("Read error:", err, ", Addr:", addr)
				continue
			}
		}
		pack, err := ParsePackage(buf, addr)
		if err != nil {
			log.Printf("ParsePackage(% 0x) error: %s\r\n", buf, err)
			continue
		}
		go s.process(pack)
	}
}

func (s *server) sendLoop() {
	log.Println("sending ...")
	for !s.exit {
		select {
		case pack := <-s.sendQueue:
			s.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := pack.Send(s.c)
			if err != nil {
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
			log.Println("sending ...")
		}
	}
}
