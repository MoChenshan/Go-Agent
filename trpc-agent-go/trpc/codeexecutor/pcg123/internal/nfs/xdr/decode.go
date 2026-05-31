package xdr

import (
	"io"

	xdr "github.com/rasky/go-xdr/xdr2"
)

// Read decodes an XDR-encoded value from the reader into val.
func Read(r io.Reader, val any) error {
	_, err := xdr.Unmarshal(r, val)
	return err
}

// ReadUint32 reads a uint32 value from the XDR-encoded stream.
func ReadUint32(r io.Reader) (uint32, error) {
	var n uint32
	if err := Read(r, &n); err != nil {
		return n, err
	}

	return n, nil
}

// ReadOpaque reads an opaque byte array from the XDR-encoded stream.
// It first reads the length, then reads that many bytes.
func ReadOpaque(r io.Reader) ([]byte, error) {
	length, err := ReadUint32(r)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, length)
	if _, err = r.Read(buf); err != nil {
		return nil, err
	}

	return buf, nil
}

// ReadUint32List reads an array of uint32 values from the XDR-encoded stream.
// It first reads the array length, then reads that many uint32 values.
func ReadUint32List(r io.Reader) ([]uint32, error) {
	length, err := ReadUint32(r)
	if err != nil {
		return nil, err
	}

	buf := make([]uint32, length)

	for i := 0; i < int(length); i++ {
		buf[i], err = ReadUint32(r)
		if err != nil {
			return nil, err
		}
	}

	return buf, nil
}
