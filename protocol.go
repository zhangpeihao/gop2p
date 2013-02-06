package gop2p

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync/atomic"
)

/*
Protocol message ID
*/
const (
	// Client to server
	MSGID_REG       = uint16(1)
	MSGID_LIST      = uint16(101)
	MSGID_CONNECT   = uint16(102)
	MSGID_KEEPALIVE = uint16(103)
	MSGID_UNREG     = uint16(104)

	// Server to client
	MSGID_TO_CONNECT = uint16(201)

	// Client to client
	MSGID_TRY_CONNECT = uint16(301)
	MSGID_DATA        = uint16(302)
)

/*
Error codes
*/
const (
	// 2XX
	ERROR_CODE_SUCCESS = http.StatusOK
	// 4XX
	ERROR_CODE_BAD_REQUEST = http.StatusBadRequest
	ERROR_CODE_NOT_FOUND   = http.StatusNotFound
	ERROR_CODE_RETRY       = 449
	// 5XX
	ERROR_CODE_FATAL_ERROR = http.StatusInternalServerError
)

/* 
Register request
*/
type RegRequest struct {
	ID string `json:"id"`
}

/* 
Connect request
*/
type ConnectRequest struct {
	PeerID string `json:"peerid"`
}

/* 
ToConnect request
*/
type ToConnectRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

/* 
Unregister request
*/
type UnregRequest struct {
	ID string `json:"id"`
}

/* 
TryConnect request
*/
type TryConnectRequest struct {
	ID string `json:"id"`
}

///////////////////////////////////////////////////////////////
// Response

/* 
Common response
*/
type CommonResponse struct {
	Ecode int `json:"ecode"`
}

/*
List response
*/
type ListResponse struct {
	Ecode   int               `json:"ecode"`
	Clients map[string]string `json:"clients"`
}

/* 
Connect response
*/
type ConnectResponse struct {
	Ecode  int    `json:"ecode"`
	PeerID string `json:"peerid"`
	Addr   string `json:"addr"`
}

var (
	COMMON_RESP_SUCCESS   = []byte(`{"ecode":200}`)
	COMMON_RESP_FAILED    = []byte(`{"ecode":400}`)
	COMMON_RESP_NOT_FOUND = []byte(`{"ecode":404}`)
	COMMON_RESP_RETRY     = []byte(`{"ecode":449}`)
)

///////////////////////////////////////////////////////////////
// Package
const (
	PACKAGE_HEAD_TAIL  uint16 = 0XA9E3
	PACKAGE_HEAD_LEN   uint16 = 8
	PACKAGE_MAX_LENGTH int    = 65535
)

var seqId uint32

/*
Package to send
*/
type Package struct {
	SeqID      uint32
	MsgID      uint16
	DataSize   uint16
	Data       []byte
	IsResponse bool
	Addr       net.Addr
}

/*
Parse data into Package object
*/
func ParsePackage(data []byte, addr net.Addr) (pack *Package, err error) {
	pack = &Package{
		Addr: addr,
	}
	buf := bytes.NewBuffer(data)
	err = binary.Read(buf, binary.LittleEndian, &(pack.SeqID))
	if err != nil {
		return nil, err
	}
	err = binary.Read(buf, binary.LittleEndian, &(pack.MsgID))
	if err != nil {
		return nil, err
	}
	if pack.MsgID&uint16(0X8000) != 0 {
		pack.MsgID = pack.MsgID & uint16(0X7FFF)
		pack.IsResponse = true
	}
	err = binary.Read(buf, binary.LittleEndian, &(pack.DataSize))
	if err != nil {
		return nil, err
	}
	if len(data) < (int)(pack.DataSize+PACKAGE_HEAD_LEN) {
		return nil, errors.New(fmt.Sprintf("Package size unmatch: len(data): %d, DataSize: %d\n", len(data), pack.DataSize))
	}
	pack.Data = make([]byte, pack.DataSize)
	copy(pack.Data, data[PACKAGE_HEAD_LEN:])
	if pack.DataSize > 0 {
		if pack.IsResponse {
			log.Printf("<<< Response (%s) {MsgID:%d, SeqID:%d, Data:%s}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID, string(pack.Data))
		} else {
			log.Printf("<<< Request  (%s) {MsgID:%d, SeqID:%d, Data:%s}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID, string(pack.Data))
		}
	} else {
		if pack.IsResponse {
			log.Printf("<<< Response (%s) {MsgID:%d, SeqID:%d, Data:nil}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID)
		} else {
			log.Printf("<<< Request  (%s) {MsgID:%d, SeqID:%d, Data:nil}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID)
		}
	}
	return
}

/*
Get next sequence number
*/
func NextSeqID() (id uint32) {
	for id == 0 {
		id = atomic.AddUint32(&seqId, 1)
	}
	return id
}

/*
Send package
*/
func (pack *Package) Send(conn *net.UDPConn) (n int, err error) {
	pack.DataSize = uint16(len(pack.Data))
	buf := new(bytes.Buffer)
	err = binary.Write(buf, binary.LittleEndian, pack.SeqID)
	if err != nil {
		return
	}
	if pack.IsResponse {
		t := uint16(uint16(0X8000) | pack.MsgID)
		err = binary.Write(buf, binary.LittleEndian, t)
		if err != nil {
			return
		}
	} else {
		err = binary.Write(buf, binary.LittleEndian, pack.MsgID)
		if err != nil {
			return
		}
	}
	err = binary.Write(buf, binary.LittleEndian, pack.DataSize)
	if err != nil {
		return
	}
	if pack.DataSize > 0 {
		if pack.IsResponse {
			log.Printf(">>> Response (%s): {MsgID:%d, SeqID:%d, Data:%s}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID, string(pack.Data))
		} else {
			log.Printf(">>> Request  (%s): {MsgID:%d, SeqID:%d, Data:%s}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID, string(pack.Data))
		}
		n, err = buf.Write(pack.Data)
		if err != nil {
			return
		}
		if n != int(pack.DataSize) {
			return 0, errors.New("Write buffer error")
		}
	} else {
		if pack.IsResponse {
			log.Printf(">>> Response (%s): {MsgID:%d, SeqID:%d, Data:nil}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID)
		} else {
			log.Printf(">>> Request  (%s): {MsgID:%d, SeqID:%d, Data:nil}\r\n", pack.Addr.String(), pack.MsgID, pack.SeqID)
		}
	}
	n, err = conn.WriteTo(buf.Bytes(), pack.Addr)
	if err != nil {
		return
	}
	if n != int(pack.DataSize+PACKAGE_HEAD_LEN) {
		return n, errors.New("Send failed!")
	}
	return
}
