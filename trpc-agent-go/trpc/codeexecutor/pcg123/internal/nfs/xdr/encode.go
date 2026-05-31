package xdr

import (
	"io"

	xdr "github.com/rasky/go-xdr/xdr2"
)

// Write encodes val as XDR and writes it to w.
func Write(w io.Writer, val any) error {
	_, err := xdr.Marshal(w, val)
	return err
}
