// Package transport implements NFS v3 protocol types and utilities.
package transport

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/user"
	"syscall"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/rpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/util"
)

const (
	Nfs3Prog = 100003 // NFS program number
	Nfs3Vers = 3      // NFS version 3

	NFSProc3Lookup      = 3  // LOOKUP - Lookup filename in directory
	NFSProc3Readlink    = 5  // READLINK - Read symbolic link
	NFSProc3Read        = 6  // READ - Read from file
	NFSProc3Write       = 7  // WRITE - Write to file
	NFSProc3Create      = 8  // CREATE - Create a file
	NFSProc3Mkdir       = 9  // MKDIR - Create a directory
	NFSProc3Remove      = 12 // REMOVE - Remove a file
	NFSProc3RmDir       = 13 // RMDIR - Remove a directory
	NFSProc3ReadDirPlus = 17 // READDIRPLUS - Read directory with attributes
	NFSProc3FSInfo      = 19 // FSINFO - Get file system information
	NFSProc3Commit      = 21 // COMMIT - Commit cached data to stable storage

	NF3Reg  = 1 // Regular file
	NF3Dir  = 2 // Directory
	NF3Blk  = 3 // Block special device
	NF3Chr  = 4 // Character special device
	NF3Lnk  = 5 // Symbolic link
	NF3Sock = 6 // Socket
	NF3FIFO = 7 // Named pipe (FIFO)
)

// Diropargs3 represents directory operation arguments (directory handle + filename).
type Diropargs3 struct {
	FH       []byte // File handle of the parent directory
	Filename string // Name of the file or directory
}

// Sattr3 represents settable file attributes in NFS v3.
type Sattr3 struct {
	Mode  SetMode // File mode/permissions
	UID   SetUID  // User ID
	GID   SetUID  // Group ID
	Size  SetSize // File size
	Atime SetTime // Access time
	Mtime SetTime // Modification time
}

// SetMode is a union for optionally setting the file mode.
type SetMode struct {
	SetIt bool   `xdr:"union"`       // Whether to set the mode
	Mode  uint32 `xdr:"unioncase=1"` // File mode bits
}

// SetUID is a union for optionally setting the user ID.
type SetUID struct {
	SetIt bool   `xdr:"union"`       // Whether to set the UID
	UID   uint32 `xdr:"unioncase=1"` // User ID value
}

// SetSize is a union for optionally setting the file size.
type SetSize struct {
	SetIt bool   `xdr:"union"`       // Whether to set the size
	Size  uint64 `xdr:"unioncase=1"` // File size in bytes
}

// TimeHow specifies how to set file timestamps.
type TimeHow int

const (
	DontChange      TimeHow = iota // DONT_CHANGE - Don't modify the timestamp
	SetToServerTime                // SET_TO_SERVER_TIME - Use server's current time
	SetToClientTime                // SET_TO_CLIENT_TIME - Use client-provided time
)

// SetTime is a union for optionally setting file timestamps.
type SetTime struct {
	SetIt TimeHow  `xdr:"union"`       // How to set the time
	Time  NFS3Time `xdr:"unioncase=2"` // Time value (when SetIt is SetToClientTime)
}

// NFS3Time represents an NFS v3 timestamp with seconds and nanoseconds.
type NFS3Time struct {
	Seconds  uint32 // Seconds since epoch
	Nseconds uint32 // Nanoseconds
}

// Fattr represents NFS v3 file attributes.
type Fattr struct {
	Type                uint32    // File type (NF3Reg, NF3Dir, etc.)
	FileMode            uint32    // File mode/permissions
	Nlink               uint32    // Number of hard links
	UID                 uint32    // User ID
	GID                 uint32    // Group ID
	Filesize            uint64    // File size in bytes
	Used                uint64    // Space used in bytes
	SpecData            [2]uint32 // Special device data
	FSID                uint64    // File system ID
	Fileid              uint64    // File ID
	Atime, Mtime, Ctime NFS3Time  // Access, modification, and change times
}

// Name returns the file name (empty for Fattr as it doesn't contain name).
func (f *Fattr) Name() string {
	return ""
}

// Size returns the file size.
func (f *Fattr) Size() int64 {
	return int64(f.Filesize)
}

// Mode returns the file mode.
func (f *Fattr) Mode() os.FileMode {
	return os.FileMode(f.FileMode)
}

// ModTime returns the modification time.
func (f *Fattr) ModTime() time.Time {
	return time.Unix(int64(f.Mtime.Seconds), int64(f.Mtime.Nseconds))
}

// IsDir returns whether the file is a directory.
func (f *Fattr) IsDir() bool {
	return f.Type == NF3Dir
}

// Sys returns system-specific file information.
func (f *Fattr) Sys() any {
	return nil
}

// wrap NFS v3 file attributes for name
type namedFileAttr struct {
	name string
	*Fattr
}

// Name returns the file name
func (f *namedFileAttr) Name() string {
	return f.name
}

// PostOpFH3 represents an optional file handle returned after an operation.
type PostOpFH3 struct {
	IsSet bool   `xdr:"union"`       // Whether the file handle is set
	FH    []byte `xdr:"unioncase=1"` // File handle value
}

// PostOpAttr represents optional file attributes returned after an operation.
type PostOpAttr struct {
	IsSet bool  `xdr:"union"`       // Whether attributes are set
	Attr  Fattr `xdr:"unioncase=1"` // File attributes
}

// EntryPlus represents a directory entry with attributes and file handle.
// It implements os.FileInfo interface.
type EntryPlus struct {
	FileId   uint64     // File ID
	FileName string     // File name
	Cookie   uint64     // Directory read cookie
	Attr     PostOpAttr // File attributes
	Handle   PostOpFH3  // File handle
}

// Name returns the file name.
func (e *EntryPlus) Name() string {
	return e.FileName
}

// Size returns the file size.
func (e *EntryPlus) Size() int64 {
	if !e.Attr.IsSet {
		return 0
	}

	return e.Attr.Attr.Size()
}

// Mode returns the file mode.
func (e *EntryPlus) Mode() os.FileMode {
	if !e.Attr.IsSet {
		return 0
	}

	return e.Attr.Attr.Mode()
}

// ModTime returns the modification time.
func (e *EntryPlus) ModTime() time.Time {
	if !e.Attr.IsSet {
		return time.Time{}
	}

	return e.Attr.Attr.ModTime()
}

// IsDir returns whether the entry is a directory.
func (e *EntryPlus) IsDir() bool {
	if !e.Attr.IsSet {
		return false
	}

	return e.Attr.Attr.IsDir()
}

// Sys returns system-specific file information (file ID).
func (e *EntryPlus) Sys() any {
	if !e.Attr.IsSet {
		return 0
	}

	return e.FileId
}

// WccData represents weak cache consistency data returned with operations.
type WccData struct {
	Before struct {
		IsSet bool     `xdr:"union"`       // Whether before attributes are set
		Size  uint64   `xdr:"unioncase=1"` // File size before operation
		MTime NFS3Time `xdr:"unioncase=1"` // Modification time before operation
		CTime NFS3Time `xdr:"unioncase=1"` // Change time before operation
	}
	After PostOpAttr // Attributes after operation
}

// FSInfo represents filesystem information returned by FSINFO operation.
type FSInfo struct {
	Attr       PostOpAttr // Filesystem attributes
	RTMax      uint32     // Maximum read transfer size
	RTPref     uint32     // Preferred read transfer size
	RTMult     uint32     // Read transfer size multiplier
	WTMax      uint32     // Maximum write transfer size
	WTPref     uint32     // Preferred write transfer size
	WTMult     uint32     // Write transfer size multiplier
	DTPref     uint32     // Preferred directory read transfer size
	Size       uint64     // Total filesystem size
	TimeDelta  NFS3Time   // Server time delta
	Properties uint32     // Filesystem properties flags
}

// DialService dials an RPC service at the specified address and port.
// It handles privileged and non-privileged port binding automatically.
func DialService(addr string, port int) (*rpc.Client, error) {
	client, err := dialService(addr, port)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func dialService(addr string, port int) (*rpc.Client, error) {
	var (
		ldr    *net.TCPAddr
		client *rpc.Client
	)

	usr, err := user.Current()

	// Unless explicitly configured, the target will likely reject connections
	// from non-privileged ports.
	if err == nil && usr.Uid == "0" {
		r1 := rand.New(rand.NewSource(time.Now().UnixNano()))

		var p int
		for {
			p = r1.Intn(1024)
			if p < 0 {
				continue
			}

			ldr = &net.TCPAddr{
				Port: p,
			}

			raddr := fmt.Sprintf("%s:%d", addr, port)
			util.Debugf("Connecting to %s", raddr)

			client, err = rpc.DialTCP("tcp", ldr, raddr)
			if err == nil {
				break
			}
			// bind error, try again
			if isAddrInUse(err) {
				continue
			}

			return nil, err
		}

		util.Debugf("using random port %d -> %d", p, port)
	} else {
		raddr := fmt.Sprintf("%s:%d", addr, port)
		util.Debugf("Connecting to %s from unprivileged port", raddr)

		client, err = rpc.DialTCP("tcp", ldr, raddr)
		if err != nil {
			return nil, err
		}
	}

	return client, nil
}

func isAddrInUse(err error) bool {
	var er *net.OpError
	if errors.As(err, &er) {
		var sysErr *os.SyscallError
		if errors.As(er.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.EADDRINUSE)
		}
	}

	return false
}
