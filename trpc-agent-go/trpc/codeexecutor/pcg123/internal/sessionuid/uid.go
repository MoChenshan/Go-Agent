// Package sessionuid derives a deterministic Linux uid/gid pair for a
// Skill execution from its workspace ID. Used by NFSRuntime to enforce
// per-session POSIX isolation on the shared NFS workspace volume: each
// session's workspace is chgrp'd to the derived gid with mode 2770 +
// setgid bit, and the bash wrapper drops privileges to (uid, gid) before
// exec'ing the user command.
//
// The derivation is intentionally stateless and deterministic so the same
// session ID always yields the same uid/gid across process restarts and
// sandbox reconnects, eliminating the need to persist a pool.
//
// The same numeric value is used for both uid and gid; the per-session
// range is well above any system or service uid/gid (see MinUID below)
// so collisions with real users or groups are impossible.
package sessionuid

import (
	"fmt"
	"hash/crc32"
)

const (
	// MinUID is the lowest uid/gid this package returns. Picked above any
	// system / service / typical user uid (Linux conventions reserve
	// 0–999 for system, 1000–65535 for typical users) and clear of the
	// special NFS squash uid 65534 ("nobody").
	MinUID uint32 = 100_000

	// RangeSize is the size of the uid/gid window. Allocate returns
	// values in [MinUID, MinUID+RangeSize). The window is intentionally
	// wide so that pairwise collisions are vanishingly rare at any
	// realistic concurrency level on a single sandbox:
	//
	//   collision probability ≈ N² / (2 * RangeSize)
	//
	// With RangeSize = 2·10⁹:
	//   - 1 000 concurrent sessions → ~2.5 × 10⁻⁴
	//   - 10 000 concurrent sessions → ~2.5 × 10⁻²
	//
	// MinUID + RangeSize stays well below 2³² − 1 (uid_t = -1 reserved),
	// so the result is always a valid Linux uid/gid.
	RangeSize uint32 = 2_000_000_000
)

// Allocate returns a deterministic uid/gid for the given session ID.
// Same id always yields the same value; different ids collide with
// probability ~1/RangeSize. Callers use the same returned value for
// both the uid and the gid of the session.
//
// An empty id (which should never happen in normal flow because
// NFSRuntime always passes a non-empty workspace ID) returns MinUID.
func Allocate(id string) uint32 {
	if id == "" {
		return MinUID
	}
	h := crc32.ChecksumIEEE([]byte(id))
	return MinUID + h%RangeSize
}

// Username returns the canonical Linux username/group name for the
// session id. Same scheme as Allocate: deterministic and stable across
// process restarts; a different id collides only when its derived
// uid/gid also collides.
//
// The numeric suffix (matching the value Allocate returns) keeps the
// user name and group name aligned 1:1 with the uid/gid, and avoids
// any chance of clashing with real account names like "mqq" or "root".
func Username(id string) string {
	return fmt.Sprintf("pcg123_%d", Allocate(id))
}
