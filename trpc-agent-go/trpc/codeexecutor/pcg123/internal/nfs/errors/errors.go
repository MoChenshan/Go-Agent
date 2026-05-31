// Package errors provides NFS v3 protocol error definitions and error handling utilities.
package errors

import (
	"errors"
	"os"
)

const (
	NFS3Ok             = 0     // NFS3_OK - Success
	NFS3ErrPerm        = 1     // NFS3ERR_PERM - Not owner
	NFS3ErrNoEnt       = 2     // NFS3ERR_NOENT - No such file or directory
	NFS3ErrIO          = 5     // NFS3ERR_IO - I/O error
	NFS3ErrNXIO        = 6     // NFS3ERR_NXIO - No such device or address
	NFS3ErrAcces       = 13    // NFS3ERR_ACCES - Permission denied
	NFS3ErrExist       = 17    // NFS3ERR_EXIST - File exists
	NFS3ErrXDev        = 18    // NFS3ERR_XDEV - Attempt to do a cross-device hard link
	NFS3ErrNoDev       = 19    // NFS3ERR_NODEV - No such device
	NFS3ErrNotDir      = 20    // NFS3ERR_NOTDIR - Not a directory
	NFS3ErrIsDir       = 21    // NFS3ERR_ISDIR - Is a directory
	NFS3ErrInval       = 22    // NFS3ERR_INVAL - Invalid argument
	NFS3ErrFBig        = 27    // NFS3ERR_FBIG - File too large
	NFS3ErrNoSpc       = 28    // NFS3ERR_NOSPC - No space left on device
	NFS3ErrROFS        = 30    // NFS3ERR_ROFS - Read-only file system
	NFS3ErrMLink       = 31    // NFS3ERR_MLINK - Too many hard links
	NFS3ErrNameTooLong = 63    // NFS3ERR_NAMETOOLONG - File name too long
	NFS3ErrNotEmpty    = 66    // NFS3ERR_NOTEMPTY - Directory not empty
	NFS3ErrDQuot       = 69    // NFS3ERR_DQUOT - Disk quota exceeded
	NFS3ErrStale       = 70    // NFS3ERR_STALE - Invalid file handle
	NFS3ErrRemote      = 71    // NFS3ERR_REMOTE - Too many levels of remote in path
	NFS3ErrBadHandle   = 10001 // NFS3ERR_BADHANDLE - Illegal NFS file handle
	NFS3ErrNotSync     = 10002 // NFS3ERR_NOT_SYNC - Synchronize request required
	NFS3ErrBadCookie   = 10003 // NFS3ERR_BAD_COOKIE - Cookie is stale
	NFS3ErrNotSupp     = 10004 // NFS3ERR_NOTSUPP - Operation not supported
	NFS3ErrTooSmall    = 10005 // NFS3ERR_TOOSMALL - Buffer or request is too small
	NFS3ErrServerFault = 10006 // NFS3ERR_SERVERFAULT - An error occurred on the server
	NFS3ErrBadType     = 10007 // NFS3ERR_BADTYPE - Type not supported by server
)

var errToName = map[uint32]string{
	NFS3Ok:             "NFS3_OK",
	NFS3ErrPerm:        "NFS3ERR_PERM",
	NFS3ErrNoEnt:       "NFS3ERR_NOENT",
	NFS3ErrIO:          "NFS3ERR_IO",
	NFS3ErrNXIO:        "NFS3ERR_NXIO",
	NFS3ErrAcces:       "NFS3ERR_ACCES",
	NFS3ErrExist:       "NFS3ERR_EXIST",
	NFS3ErrXDev:        "NFS3ERR_XDEV",
	NFS3ErrNoDev:       "NFS3ERR_NODEV",
	NFS3ErrNotDir:      "NFS3ERR_NOTDIR",
	NFS3ErrIsDir:       "NFS3ERR_ISDIR",
	NFS3ErrInval:       "NFS3ERR_INVAL",
	NFS3ErrFBig:        "NFS3ERR_FBIG",
	NFS3ErrNoSpc:       "NFS3ERR_NOSPC",
	NFS3ErrROFS:        "NFS3ERR_ROFS",
	NFS3ErrMLink:       "NFS3ERR_MLINK",
	NFS3ErrNameTooLong: "NFS3ERR_NAMETOOLONG",
	NFS3ErrNotEmpty:    "NFS3ERR_NOTEMPTY",
	NFS3ErrDQuot:       "NFS3ERR_DQUOT",
	NFS3ErrStale:       "NFS3ERR_STALE",
	NFS3ErrRemote:      "NFS3ERR_REMOTE",
	NFS3ErrBadHandle:   "NFS3ERR_BADHANDLE",
	NFS3ErrNotSync:     "NFS3ERR_NOT_SYNC",
	NFS3ErrBadCookie:   "NFS3ERR_BAD_COOKIE",
	NFS3ErrNotSupp:     "NFS3ERR_NOTSUPP",
	NFS3ErrTooSmall:    "NFS3ERR_TOOSMALL",
	NFS3ErrServerFault: "NFS3ERR_SERVERFAULT",
	NFS3ErrBadType:     "NFS3ERR_BADTYPE",
}

// NFS3Error converts an NFS v3 error number to a Go error.
// It returns nil for NFS3Ok, standard Go errors for common cases,
// or an Error struct for other NFS-specific errors.
func NFS3Error(errno uint32) error {
	switch errno {
	case NFS3Ok:
		return nil
	case NFS3ErrPerm:
		return os.ErrPermission
	case NFS3ErrExist:
		return os.ErrExist
	case NFS3ErrNoEnt:
		return os.ErrNotExist
	default:
		if errStr, ok := errToName[errno]; ok {
			return &Error{
				ErrorNum:    errno,
				ErrorString: errStr,
			}
		}

		return os.ErrInvalid
	}
}

// Error represents an NFS v3 protocol error with error number and description.
type Error struct {
	ErrorNum    uint32
	ErrorString string
}

func (err *Error) Error() string { return err.ErrorString }

// IsNotDirError reports whether err is an NFS error indicating that a path is not a directory (NFS3ERR_NOTDIR).
func IsNotDirError(err error) bool {
	var nfsErr *Error
	if ok := errors.As(err, &nfsErr); !ok {
		return false
	}

	if nfsErr.ErrorNum == NFS3ErrNotDir {
		return true
	}

	return false
}
