package rpc

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/util"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/xdr"
)

// Transport provides multiplexed request-response over a single TCP connection.
// Multiple goroutines can call RoundTrip concurrently; responses are routed
// by XID (the first 4 bytes of each message) via a background reader goroutine.
type Transport struct {
	conn *tcpTransport

	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[uint32]chan roundTripResult
	closed    bool
	closeErr  error
	done      chan struct{}
	closeOnce sync.Once
}

type roundTripResult struct {
	res io.ReadSeeker
	err error
}

func newTransport(conn *tcpTransport) *Transport {
	t := &Transport{
		conn:    conn,
		pending: make(map[uint32]chan roundTripResult),
		done:    make(chan struct{}),
	}
	go t.readLoop()
	return t
}

// continuously reads responses and dispatches them by XID.
func (t *Transport) readLoop() {
	for {
		select {
		case <-t.done:
			return
		default:
		}

		res, err := t.conn.recv()
		if err != nil {
			t.doClose(err)
			return
		}

		respXid, err := xdr.ReadUint32(res)
		if err != nil {
			util.Errorf("malformed xid frame: %s", err)
			continue
		}

		t.mu.Lock()
		ch, ok := t.pending[respXid]
		if ok {
			delete(t.pending, respXid)
		}
		t.mu.Unlock()

		if !ok {
			continue
		}

		ch <- roundTripResult{res: res}
	}
}

// RoundTrip sends data over the connection and waits for the response
// with the matching XID. The XID is extracted from the first 4 bytes
// of data (XDR big-endian uint32). The returned reader is positioned
// after the XID (already consumed for routing).
func (t *Transport) RoundTrip(data []byte) (io.ReadSeeker, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("rpc: data too short to contain XID")
	}
	reqXid := binary.BigEndian.Uint32(data[:4])

	ch := make(chan roundTripResult, 1)

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, t.closeErr
	}
	t.pending[reqXid] = ch
	t.mu.Unlock()

	t.writeMu.Lock()
	_, err := t.conn.Write(data)
	t.writeMu.Unlock()
	if err != nil {
		t.removePending(reqXid)
		return nil, err
	}

	result := <-ch
	if result.err != nil {
		return nil, result.err
	}
	return result.res, nil
}

func (t *Transport) doClose(err error) {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.closeErr = err
		for id, ch := range t.pending {
			ch <- roundTripResult{err: err}
			delete(t.pending, id)
		}
		t.mu.Unlock()

		close(t.done)
		t.conn.Close()
	})
}

// Close closes the transport and its underlying TCP connection.
func (t *Transport) Close() error {
	t.doClose(fmt.Errorf("rpc transport closed"))
	return nil
}

func (t *Transport) removePending(id uint32) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

// SetTimeout sets the read/write timeout for the underlying connection.
func (t *Transport) SetTimeout(d time.Duration) {
	t.conn.SetTimeout(d)
}
