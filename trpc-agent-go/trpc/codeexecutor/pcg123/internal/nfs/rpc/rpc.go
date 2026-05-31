// Package rpc implements ONC RPC (Remote Procedure Call) protocol for NFS communication.
package rpc

import (
	"bytes"
	"math/rand"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/xdr"
)

// Header represents the RPC call header as defined in RFC 5531.
type Header struct {
	RPCVersion uint32 // RPC version number (must be 2)
	Prog       uint32 // Program number
	Version    uint32 // Program version number
	Proc       uint32 // Procedure number
	Cred       Auth   // Authentication credentials
	Verf       Auth   // Verification credential (typically AUTH_NULL)
}

// Auth represents RPC authentication credentials.
type Auth struct {
	Flavor uint32 // Authentication flavor (e.g., 1 for AUTH_UNIX)
	Body   []byte // Encoded authentication data
}

// AuthNull is the null authentication flavor (no authentication).
var AuthNull Auth

// AuthUnix represents UNIX-style authentication with user/group IDs.
type AuthUnix struct {
	Stamp   uint32 // Timestamp to make credentials unique
	Machine string // Machine hostname
	Uid     uint32 // User ID
	Gid     uint32 // Primary group ID
	GidLen  uint32 // Number of auxiliary GIDs (not used, always 1)
	GIDs    uint32 // Auxiliary GIDs (not used, always 0)
}

// NewAuthUnix creates a new AUTH_UNIX credential with the given machine name, UID, and GID.
func NewAuthUnix(machine string, uid, gid uint32) *AuthUnix {
	return &AuthUnix{
		Stamp:   rand.New(rand.NewSource(time.Now().UnixNano())).Uint32(),
		Machine: machine,
		Uid:     uid,
		Gid:     gid,
		GidLen:  1,
	}
}

// Auth converts an AuthUnix into an Auth opaque struct for XDR encoding.
func (a AuthUnix) Auth() Auth {
	w := new(bytes.Buffer)
	_ = xdr.Write(w, a)
	return Auth{1, w.Bytes()}
}
