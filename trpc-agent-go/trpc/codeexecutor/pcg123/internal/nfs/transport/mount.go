package transport

import (
	"errors"
	"fmt"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/rpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/xdr"
)

const (
	MountProg = 100005 // Mount program number
	MountVers = 3      // Mount protocol version 3

	MountProc3Null   = 0 // NULL procedure
	MountProc3MNT    = 1 // MNT procedure - mount a filesystem
	MountProc3UMNT   = 3 // UMNT procedure - unmount a filesystem
	MountProc3Export = 5 // EXPORT procedure - list exports

	MNT3Ok             = 0     // no error
	MNT3ErrPerm        = 1     // Not owner
	MNT3ErrNoEnt       = 2     // No such file or directory
	MNT3ErrIO          = 5     // I/O error
	MNT3ErrAcces       = 13    // Permission denied
	MNT3ErrNotDir      = 20    // Not a directory
	MNT3ErrInval       = 22    // Invalid argument
	MNT3ErrNameTooLong = 63    // Filename too long
	MNT3ErrNotSupp     = 10004 // Operation not supported
	MNT3ErrServerFault = 10006 // A failure on the server
)

// Mount represents a connection to the NFS mount daemon.
// It provides methods to mount and unmount NFS filesystems.
type Mount struct {
	*rpc.Client
	auth    rpc.Auth
	dirPath string
	Addr    string // Server address
	Port    int    // Server port
}

// Unmount unmounts the previously mounted filesystem.
func (m *Mount) Unmount() error {
	type umount struct {
		rpc.Header
		Dirpath string
	}

	_, err := m.Call(&umount{
		rpc.Header{
			RPCVersion: 2,
			Prog:       MountProg,
			Version:    MountVers,
			Proc:       MountProc3UMNT,
			// Weirdly, the spec calls for AUTH_UNIX or better, but AUTH_NULL
			// works here on a linux NFS kernel server.  Follow the spec
			// anyway.
			Cred: m.auth,
			Verf: rpc.AuthNull,
		},
		m.dirPath,
	})
	if err != nil {
		return err
	}

	return nil
}

// Mount mounts an NFS filesystem at the given directory path.
// It returns a Target that can be used for file operations on the mounted filesystem.
func (m *Mount) Mount(dirpath string, auth rpc.Auth) (*Target, error) {
	type mount struct {
		rpc.Header
		Dirpath string
	}

	res, err := m.Call(&mount{
		rpc.Header{
			RPCVersion: 2,
			Prog:       MountProg,
			Version:    MountVers,
			Proc:       MountProc3MNT,
			Cred:       auth,
			Verf:       rpc.AuthNull,
		},
		dirpath,
	})
	if err != nil {
		return nil, err
	}

	mountstat3, err := xdr.ReadUint32(res)
	if err != nil {
		return nil, err
	}

	switch mountstat3 {
	case MNT3Ok:
		fh, err := xdr.ReadOpaque(res)
		if err != nil {
			return nil, err
		}

		_, _ = xdr.ReadUint32List(res)

		m.dirPath = dirpath
		m.auth = auth

		vol, err := NewTarget(m.Addr, m.Port, auth, fh, dirpath)
		if err != nil {
			return nil, err
		}

		return vol, nil

	case MNT3ErrPerm:
		return nil, errors.New("MNT3ERR_PERM")
	case MNT3ErrNoEnt:
		return nil, errors.New("MNT3ERR_NOENT")
	case MNT3ErrIO:
		return nil, errors.New("MNT3ERR_IO")
	case MNT3ErrAcces:
		return nil, errors.New("MNT3ERR_ACCES")
	case MNT3ErrNotDir:
		return nil, errors.New("MNT3ERR_NOTDIR")
	case MNT3ErrNameTooLong:
		return nil, errors.New("MNT3ERR_NAMETOOLONG")
	}
	return nil, fmt.Errorf("unknown mount stat: %d", mountstat3)
}

// DialMount creates a new Mount client connected to the NFS mount daemon
// at the specified address and port.
func DialMount(addr string, port int) (*Mount, error) {
	client, err := DialService(addr, port)
	if err != nil {
		return nil, err
	}

	return &Mount{
		Client: client,
		Addr:   addr,
		Port:   port,
	}, nil
}
