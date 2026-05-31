// Package client for nfs rpc
package client

import (
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/rpc"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/nfs/transport"
)

// Client encapsulates NFS v3 protocol operations for remote file system access.
// It provides high-level file operations on top of the go-nfs-client library.
type Client struct {
	mount      *transport.Mount
	target     *transport.Target
	exportPath string
}

// Config holds the configuration for NFS connection.
type Config struct {
	ServerIP   string // NFS server address ip
	ServerPort int    // NFS server port
	ExportPath string // NFS export path, e.g., "/data/skills-workspace"
}

// NewNFSClient creates a new NFS client by parsing the NFS endpoint and establishing connection.
// Example: nfs://10.0.0.1:2049/usr/local/app/nfs_root
func NewNFSClient(endpoint string) (*Client, error) {
	cfg, err := ParseNFSEndpoint(endpoint)
	if err != nil {
		return nil, errors.WithMessage(err, "parse NFS endpoint")
	}

	mount, err := transport.DialMount(cfg.ServerIP, cfg.ServerPort)
	if err != nil {
		return nil, errors.WithMessagef(err, "dial NFS mount at %s:%d", cfg.ServerIP, cfg.ServerPort)
	}

	auth := rpc.NewAuthUnix("trpc-agent-go", 500, 1000)
	target, err := mount.Mount(cfg.ExportPath, auth.Auth())
	if err != nil {
		_ = mount.Close()
		return nil, errors.WithMessagef(err, "mount NFS export %s", cfg.ExportPath)
	}

	return &Client{
		mount:      mount,
		target:     target,
		exportPath: cfg.ExportPath,
	}, nil
}

// ParseNFSEndpoint parses an NFS endpoint in the format "nfs://host:port/path" into server address and export path.
// Examples:
//   - nfs://10.0.0.1:2049/usr/local/app/nfs_root -> ServerIP: "10.0.0.1", ServerPort: "2049", ExportPath: "/usr/local/app/nfs_root"
func ParseNFSEndpoint(endpoint string) (Config, error) {
	if !strings.HasPrefix(endpoint, "nfs://") {
		return Config{}, errors.Errorf(
			"invalid NFS endpoint format: %s, expected nfs://host:port/path", endpoint)
	}

	urlPart := strings.TrimPrefix(endpoint, "nfs://")
	parts := strings.SplitN(urlPart, "/", 2)
	if len(parts) != 2 {
		return Config{}, errors.Errorf(
			"invalid NFS endpoint format: %s, expected nfs://host:port/path", endpoint)
	}

	serverAddr := parts[0]
	exportPath := "/" + parts[1]

	if serverAddr == "" || exportPath == "" {
		return Config{}, errors.Errorf("invalid NFS endpoint: server or path is empty")
	}

	ip, port, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return Config{}, errors.WithMessagef(err, "invalid NFS server address: %s", serverAddr)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return Config{}, errors.WithMessagef(err, "invalid NFS server port: %s", port)
	}

	return Config{
		ServerIP:   ip,
		ServerPort: portNum,
		ExportPath: exportPath,
	}, nil
}

// MkdirAll creates a directory path recursively on the NFS server.
// It's similar to os.MkdirAll but operates on remote NFS filesystem.
func (c *Client) MkdirAll(dirPath string) error {
	relPath := c.toRelativePath(dirPath)
	relPath = path.Clean(relPath)
	if relPath == "." || relPath == "/" || relPath == "" {
		return nil
	}

	_, _, err := c.target.Lookup(relPath)
	if err == nil {
		return nil
	}

	parent := path.Dir(relPath)
	if parent != "." && parent != "/" && parent != "" {
		if err := c.MkdirAll(path.Join(c.exportPath, parent)); err != nil {
			return err
		}
	}

	_, err = c.target.Mkdir(relPath, 0o755)
	if err != nil && !isExistError(err) {
		return errors.WithMessagef(err, "do nfs mkdir %s", dirPath)
	}

	return nil
}

// WriteFile writes data to a file on the NFS server.
// It creates parent directories if they don't exist and overwrites the file if it exists.
func (c *Client) WriteFile(filePath string, data []byte, perm os.FileMode) error {
	relPath := c.toRelativePath(filePath)
	relPath = path.Clean(relPath)

	dir := path.Dir(relPath)
	if dir != "." && dir != "/" && dir != "" {
		if err := c.MkdirAll(path.Join(c.exportPath, dir)); err != nil {
			return errors.WithMessage(err, "create parent directories")
		}
	}

	file, err := c.target.OpenFile(relPath, perm)
	if err != nil {
		return errors.WithMessagef(err, "open file %s", filePath)
	}
	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		return errors.WithMessagef(err, "write file %s", filePath)
	}

	return nil
}

// ReadFile reads the entire content of a file from the NFS server.
func (c *Client) ReadFile(filePath string) ([]byte, error) {
	relPath := c.toRelativePath(filePath)
	relPath = path.Clean(relPath)

	file, err := c.target.Open(relPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "open file %s", filePath)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.WithMessagef(err, "read file %s", filePath)
	}

	return data, nil
}

// ReadFileLimited reads up to maxBytes from a file on the NFS server.
// If the file is larger than maxBytes, only the first maxBytes are returned.
// This prevents reading large files entirely into memory.
func (c *Client) ReadFileLimited(filePath string, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return []byte{}, nil
	}

	relPath := c.toRelativePath(filePath)
	relPath = path.Clean(relPath)

	file, err := c.target.Open(relPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "open file %s", filePath)
	}
	defer file.Close()

	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, errors.WithMessagef(err, "read file %s", filePath)
	}

	return buf[:n], nil
}

// Remove deletes a file from the NFS server.
// It returns nil if the file doesn't exist.
func (c *Client) Remove(filePath string) error {
	relPath := c.toRelativePath(filePath)
	relPath = path.Clean(relPath)

	err := c.target.Remove(relPath)
	if err != nil && !isNotExistError(err) {
		return errors.WithMessagef(err, "remove %s", filePath)
	}

	return nil
}

// RemoveAll removes a directory and all its contents from the NFS server.
// It returns nil if the path doesn't exist.
func (c *Client) RemoveAll(dirPath string) error {
	relPath := c.toRelativePath(dirPath)
	relPath = path.Clean(relPath)

	_, _, err := c.target.Lookup(relPath)
	if err != nil {
		if isNotExistError(err) {
			return nil
		}
		return errors.WithMessagef(err, "lookup %s", dirPath)
	}

	err = c.target.RemoveAll(relPath)
	if err != nil && !isNotExistError(err) {
		return errors.WithMessagef(err, "remove all %s", dirPath)
	}

	return nil
}

// Stat returns file information for a path on the NFS server.
func (c *Client) Stat(filePath string) (os.FileInfo, error) {
	relPath := c.toRelativePath(filePath)
	relPath = path.Clean(relPath)

	info, _, err := c.target.Lookup(relPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "stat %s", filePath)
	}

	return info, nil
}

// ReadDir reads the directory entries on the NFS server.
// It returns a slice of FileInfo for each entry (excluding "." and "..").
func (c *Client) ReadDir(dirPath string) ([]os.FileInfo, error) {
	relPath := c.toRelativePath(dirPath)
	relPath = path.Clean(relPath)

	entries, err := c.target.ReadDirPlus(relPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "read dir %s", dirPath)
	}

	infos := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}

		childPath := path.Join(relPath, name)
		info, _, err := c.target.Lookup(childPath)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}

	return infos, nil
}

// Glob returns the names of all files matching the pattern on the NFS server.
// The pattern syntax is the same as filepath.Match.
func (c *Client) Glob(pattern string) ([]string, error) {
	relPattern := c.toRelativePath(pattern)
	relPattern = path.Clean(relPattern)

	if !strings.Contains(relPattern, "*") && !strings.Contains(relPattern, "?") && !strings.Contains(relPattern, "[") {
		_, _, err := c.target.Lookup(relPattern)
		if err == nil {
			return []string{pattern}, nil
		}
		if isNotExistError(err) {
			return nil, nil
		}
		return nil, err
	}

	dir := relPattern
	for strings.Contains(dir, "*") || strings.Contains(dir, "?") || strings.Contains(dir, "[") {
		dir = path.Dir(dir)
	}

	if dir == "." {
		dir = "/"
	}

	var matches []string
	err := c.walkGlob(dir, relPattern, &matches)
	if err != nil {
		return nil, err
	}

	return matches, nil
}

// walkGlob recursively walks the NFS directory tree matching files against the glob pattern.
func (c *Client) walkGlob(currentPath, pattern string, matches *[]string) error {
	info, _, err := c.target.Lookup(currentPath)
	if err != nil {
		if isNotExistError(err) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		matched, _ := filepath.Match(pattern, currentPath)
		if matched {
			*matches = append(*matches, path.Join(c.exportPath, currentPath))
		}
		return nil
	}

	entries, err := c.target.ReadDirPlus(currentPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}

		fullPath := path.Join(currentPath, name)

		childInfo, _, err := c.target.Lookup(fullPath)
		if err != nil {
			continue
		}

		matched, _ := filepath.Match(pattern, fullPath)
		if matched && !childInfo.IsDir() {
			*matches = append(*matches, path.Join(c.exportPath, fullPath))
		}

		if childInfo.IsDir() {
			if err := c.walkGlob(fullPath, pattern, matches); err != nil {
				return err
			}
		}
	}

	return nil
}

// Close closes the NFS client connection and releases resources.
func (c *Client) Close() error {
	if c.target != nil {
		_ = c.target.Close()
	}
	if c.mount != nil {
		_ = c.mount.Close()
	}
	return nil
}

// HealthCheck verifies the existing NFS target connection with a real RPC.
func (c *Client) HealthCheck() error {
	if _, err := c.target.FSInfo(); err != nil {
		return errors.WithMessage(err, "nfs fsinfo")
	}
	return nil
}

// ExportPath returns the NFS export path configured for this client.
func (c *Client) ExportPath() string {
	return c.exportPath
}

// converts an absolute path to a path relative to the NFS export root.
// For example, if exportPath is "/usr/local/app/nfs_root" and fullPath is
// "/usr/local/app/nfs_root/ws_session-123/skills/test/file.txt", it returns
// "ws_session-123/skills/test/file.txt".
// If the path is already relative or does not start with exportPath, it returns the cleaned path as-is.
func (c *Client) toRelativePath(fullPath string) string {
	fullPath = path.Clean(fullPath)

	if strings.HasPrefix(fullPath, c.exportPath) {
		relPath := strings.TrimPrefix(fullPath, c.exportPath)
		relPath = strings.TrimPrefix(relPath, "/")
		if relPath == "" {
			return "/"
		}
		return relPath
	}

	return fullPath
}

// isExistError checks if the error indicates that a file or directory already exists.
func isExistError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "exist") || strings.Contains(err.Error(), "EEXIST")
}

// isNotExistError checks if the error indicates that a file or directory doesn't exist.
func isNotExistError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "no such") ||
		strings.Contains(errStr, "not exist") ||
		strings.Contains(errStr, "ENOENT") ||
		strings.Contains(errStr, "nfs3err=2")
}
