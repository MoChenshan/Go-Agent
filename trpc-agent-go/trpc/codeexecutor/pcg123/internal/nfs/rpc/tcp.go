package rpc

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"time"
)

// tcpTransport implements raw RPC transport over TCP with record marking.
// Concurrency safety is provided by Transport at the multiplexing level.
type tcpTransport struct {
	r       io.Reader // Buffered reader for incoming data
	wc      net.Conn  // Write connection
	timeout time.Duration
}

// recv receives and decodes an RPC message from the connection.
// It implements the TCP record marking standard defined in RFC 5531.
// The record marker is a 4-byte big-endian integer where the high bit
// indicates the last fragment and the remaining 31 bits are the fragment length.
func (t *tcpTransport) recv() (io.ReadSeeker, error) {
	if t.timeout != 0 {
		deadline := time.Now().Add(t.timeout)
		_ = t.wc.SetReadDeadline(deadline)
	}

	var hdr uint32
	if err := binary.Read(t.r, binary.BigEndian, &hdr); err != nil {
		return nil, err
	}

	buf := make([]byte, hdr&0x7fffffff)
	if _, err := io.ReadFull(t.r, buf); err != nil {
		return nil, err
	}

	return bytes.NewReader(buf), nil
}

// Write encodes and writes data with the TCP record marker prefix.
// It prepends a 4-byte big-endian record marker with the high bit set
// to indicate this is the last (and only) fragment.
func (t *tcpTransport) Write(buf []byte) (int, error) {
	var hdr = uint32(len(buf)) | 0x80000000
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, hdr)
	if t.timeout != 0 {
		deadline := time.Now().Add(t.timeout)
		_ = t.wc.SetWriteDeadline(deadline)
	}
	n, err := t.wc.Write(append(b, buf...))

	return n, err
}

// Close closes the TCP connection.
func (t *tcpTransport) Close() error {
	return t.wc.Close()
}

// SetTimeout sets the read/write timeout for the connection.
// A timeout of 0 means no timeout (blocking I/O).
func (t *tcpTransport) SetTimeout(d time.Duration) {
	t.timeout = d
	if d == 0 {
		var zeroTime time.Time
		_ = t.wc.SetDeadline(zeroTime)
	}
}
