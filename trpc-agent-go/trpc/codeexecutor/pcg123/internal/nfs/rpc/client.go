package rpc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync/atomic"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/util"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/xdr"
)

const (
	MsgAccepted = iota // RPC message was accepted
	MsgDenied          // RPC message was denied
)

const (
	Success      = iota // RPC call succeeded
	ProgUnavail         // Program number unavailable
	ProgMismatch        // Program version mismatch
	ProcUnavail         // Procedure number unavailable
	GarbageArgs         // Garbage arguments (XDR decode error)
	SystemErr           // System error on server
)

const (
	RpcMismatch = iota // RPC version mismatch
)

// xid is the transaction ID counter for RPC calls
var xid uint32

func init() {
	// seed the XID (which is set by the client)
	xid = rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()
}

// Client represents an RPC client that communicates over a multiplexed TCP transport.
// Multiple goroutines can call Call() concurrently.
type Client struct {
	transport *Transport
}

// message represents an RPC message with XID and message type.
type message struct {
	Xid     uint32
	Msgtype uint32
	Body    any
}

// DialTCP creates a new RPC client that connects to the specified address via TCP.
// The network parameter should be "tcp" or "tcp4" or "tcp6".
// ldr is the local address to bind to (can be nil for automatic selection).
// addr should be in the format "host:port".
func DialTCP(network string, ldr *net.TCPAddr, addr string) (*Client, error) {
	a, err := net.ResolveTCPAddr(network, addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP(a.Network(), ldr, a)
	if err != nil {
		return nil, err
	}

	t := &tcpTransport{
		r:  bufio.NewReader(conn),
		wc: conn,
	}

	return &Client{transport: newTransport(t)}, nil
}

// Close closes the RPC client and its underlying TCP connection.
func (c *Client) Close() error {
	return c.transport.Close()
}

// SetTimeout sets the read/write timeout for the underlying connection.
func (c *Client) SetTimeout(d time.Duration) {
	c.transport.SetTimeout(d)
}

// Call invokes an RPC method and returns the response reader.
// Multiple goroutines can call this concurrently on the same Client.
func (c *Client) Call(call any) (io.ReadSeeker, error) {
	retries := 1

	msg := &message{
		Xid:  atomic.AddUint32(&xid, 1),
		Body: call,
	}

retry:
	w := new(bytes.Buffer)
	if err := xdr.Write(w, msg); err != nil {
		return nil, err
	}

	res, err := c.transport.RoundTrip(w.Bytes())
	if err != nil {
		return nil, err
	}

	// XID already consumed by Transport, parse the rest of the response header
	mtype, err := xdr.ReadUint32(res)
	if err != nil {
		return nil, err
	}

	if mtype != 1 {
		return nil, fmt.Errorf("message as not a reply: %d", mtype)
	}

	status, err := xdr.ReadUint32(res)
	if err != nil {
		return nil, err
	}

	switch status {
	case MsgAccepted:

		// padding
		_, err = xdr.ReadUint32(res)
		if err != nil {
			return nil, err
		}

		opaqueLen, err := xdr.ReadUint32(res)
		if err != nil {
			return nil, err
		}

		_, err = res.Seek(int64(opaqueLen), io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		acceptStatus, _ := xdr.ReadUint32(res)

		switch acceptStatus {
		case Success:
			return res, nil
		case ProgUnavail:
			return nil, fmt.Errorf("rpc: PROG_UNAVAIL - server does not recognize the program number")
		case ProgMismatch:
			return nil, fmt.Errorf("rpc: PROG_MISMATCH - program version does not exist on the server")
		case ProcUnavail:
			return nil, fmt.Errorf("rpc: PROC_UNAVAIL - unrecognized procedure number")
		case GarbageArgs:
			// emulate Linux behaviour for GARBAGE_ARGS
			if retries > 0 {
				util.Debugf("Retrying on GARBAGE_ARGS per linux semantics")
				retries--
				goto retry
			}

			return nil, fmt.Errorf("rpc: GARBAGE_ARGS - rpc arguments cannot be XDR decoded")
		case SystemErr:
			return nil, fmt.Errorf("rpc: SYSTEM_ERR - unknown error on server")
		default:
			return nil, fmt.Errorf("rpc: unknown accepted status error: %d", acceptStatus)
		}

	case MsgDenied:
		rejectStatus, _ := xdr.ReadUint32(res)
		switch rejectStatus {
		case RpcMismatch:

		default:
			return nil, fmt.Errorf("rejectedStatus was not valid: %d", rejectStatus)
		}

	default:
		return nil, fmt.Errorf("rejectedStatus was not valid: %d", status)
	}

	panic("unreachable")
}
