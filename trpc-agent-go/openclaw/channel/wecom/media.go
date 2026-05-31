package wecom

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
)

const (
	defaultMediaDownloadTimeout = 15 * time.Second
	defaultMediaPrefetchTTL     = 5 * time.Minute
	defaultMediaSnapshotTTL     = time.Hour

	defaultAttachmentName = "attachment"
	imageUploadNamePrefix = "image_"

	mediaSnapshotDirName     = "media_cache"
	mediaSnapshotDataExt     = ".bin"
	mediaSnapshotMetaExt     = ".json"
	mediaSnapshotTempPattern = "media-snapshot-*"

	mimeTypeOctetStream = "application/octet-stream"
	mimeTypePDF         = "application/pdf"
	mimeTypeZIP         = "application/zip"
	mimeTypeJPEG        = "image/jpeg"
	mimeTypePNG         = "image/png"
	mimeTypeGIF         = "image/gif"
	mimeTypeWEBP        = "image/webp"

	mimeTypeXLSX = "application/vnd.openxmlformats-" +
		"officedocument.spreadsheetml.sheet"
	mimeTypeDOCX = "application/vnd.openxmlformats-" +
		"officedocument.wordprocessingml.document"
	mimeTypePPTX = "application/vnd.openxmlformats-" +
		"officedocument.presentationml.presentation"

	imageFormatJPEG = "jpeg"
	imageFormatPNG  = "png"
	imageFormatGIF  = "gif"
	imageFormatWEBP = "webp"

	duplicateFilenameSuffixSeparator = "-"

	mediaSchemeHTTP  = "http"
	mediaSchemeHTTPS = "https"

	wecomMediaHostQPic     = "wework.qpic.cn"
	wecomMediaHostQYAPI    = "qyapi.weixin.qq.com"
	wecomMediaHostAIBotImg = "ww-aibot-img"

	errMediaURLHostMissing = "wecom media: missing download host"
	errMediaURLFragment    = "wecom media: fragments are not allowed"
	errMediaURLUserInfo    = "wecom media: userinfo is not allowed"
)

type fetchedMedia struct {
	contentType string
	filename    string
	data        []byte
}

type validatedMediaURL struct {
	value *url.URL
}

func (u validatedMediaURL) String() string {
	if u.value == nil {
		return ""
	}
	return u.value.String()
}

func (u validatedMediaURL) requestURL() string {
	return u.String()
}

type contentPartDecryptHint struct {
	AESKey string
}

type mediaSnapshotMeta struct {
	ContentType string    `json:"content_type,omitempty"`
	Filename    string    `json:"filename,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

type mediaSnapshotStore struct {
	mu  sync.Mutex
	dir string
	ttl time.Duration
}

type prefetchedMediaEntry struct {
	ready chan struct{}

	media     fetchedMedia
	err       error
	expiresAt time.Time
}

type mediaPrefetcher struct {
	mu        sync.Mutex
	ttl       time.Duration
	entries   map[string]*prefetchedMediaEntry
	snapshots *mediaSnapshotStore
}

func newMediaPrefetcher(ttl time.Duration) *mediaPrefetcher {
	return newMediaPrefetcherWithSnapshotDir(ttl, "", 0)
}

func newMediaPrefetcherWithSnapshotDir(
	ttl time.Duration,
	snapshotDir string,
	snapshotTTL time.Duration,
) *mediaPrefetcher {
	if ttl <= 0 {
		ttl = defaultMediaPrefetchTTL
	}
	return &mediaPrefetcher{
		ttl:     ttl,
		entries: make(map[string]*prefetchedMediaEntry),
		snapshots: newMediaSnapshotStore(
			snapshotDir,
			snapshotTTL,
		),
	}
}

func newMediaSnapshotStore(
	dir string,
	ttl time.Duration,
) *mediaSnapshotStore {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultMediaSnapshotTTL
	}
	return &mediaSnapshotStore{
		dir: dir,
		ttl: ttl,
	}
}

func mediaPrefetchSnapshotDir(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(
		stateDir,
		sessionTrackerStoreDirName,
		mediaSnapshotDirName,
	)
}

func (p *mediaPrefetcher) loadSnapshot(
	key string,
) (fetchedMedia, bool) {
	if p == nil || p.snapshots == nil {
		return fetchedMedia{}, false
	}
	media, ok, err := p.snapshots.Load(key)
	if err != nil {
		return fetchedMedia{}, false
	}
	return media, ok
}

func (p *mediaPrefetcher) saveSnapshot(
	key string,
	media fetchedMedia,
) {
	if p == nil || p.snapshots == nil {
		return
	}
	if err := p.snapshots.Save(key, media); err != nil {
		return
	}
}

func (s *mediaSnapshotStore) Load(
	key string,
) (fetchedMedia, bool, error) {
	if s == nil {
		return fetchedMedia{}, false, nil
	}

	metaPath, dataPath, ok := s.pathsForKey(key)
	if !ok {
		return fetchedMedia{}, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	meta, found, err := s.readMeta(metaPath)
	if err != nil {
		_ = s.removeFiles(metaPath, dataPath)
		return fetchedMedia{}, false, err
	}
	if !found {
		return fetchedMedia{}, false, nil
	}
	if time.Now().After(meta.ExpiresAt) {
		_ = s.removeFiles(metaPath, dataPath)
		return fetchedMedia{}, false, nil
	}

	data, err := os.ReadFile(dataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = s.removeFiles(metaPath, dataPath)
			return fetchedMedia{}, false, nil
		}
		return fetchedMedia{}, false, err
	}
	return fetchedMedia{
		contentType: meta.ContentType,
		filename:    meta.Filename,
		data:        data,
	}, true, nil
}

func (s *mediaSnapshotStore) Save(
	key string,
	media fetchedMedia,
) error {
	if s == nil {
		return nil
	}

	metaPath, dataPath, ok := s.pathsForKey(key)
	if !ok {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, sessionTrackerStoreDirPerm); err != nil {
		return fmt.Errorf("wecom media snapshot mkdir: %w", err)
	}

	meta := mediaSnapshotMeta{
		ContentType: media.contentType,
		Filename:    media.filename,
		ExpiresAt:   time.Now().Add(s.ttl),
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("wecom media snapshot marshal: %w", err)
	}

	if err := s.writeFileLocked(dataPath, media.data); err != nil {
		return err
	}
	if err := s.writeFileLocked(metaPath, metaData); err != nil {
		return err
	}
	s.pruneExpiredLocked()
	return nil
}

func (s *mediaSnapshotStore) pathsForKey(
	key string,
) (string, string, bool) {
	if s == nil {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	name := mediaSnapshotKey(key)
	return filepath.Join(s.dir, name+mediaSnapshotMetaExt),
		filepath.Join(s.dir, name+mediaSnapshotDataExt),
		true
}

func mediaSnapshotKey(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func (s *mediaSnapshotStore) readMeta(
	metaPath string,
) (mediaSnapshotMeta, bool, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mediaSnapshotMeta{}, false, nil
		}
		return mediaSnapshotMeta{}, false, err
	}

	var meta mediaSnapshotMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return mediaSnapshotMeta{}, false, err
	}
	return meta, true, nil
}

func (s *mediaSnapshotStore) writeFileLocked(
	path string,
	data []byte,
) error {
	file, err := os.CreateTemp(s.dir, mediaSnapshotTempPattern)
	if err != nil {
		return fmt.Errorf("wecom media snapshot temp file: %w", err)
	}

	name := file.Name()
	defer func() {
		_ = os.Remove(name)
	}()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("wecom media snapshot write: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("wecom media snapshot close: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("wecom media snapshot rename: %w", err)
	}
	return nil
}

func (s *mediaSnapshotStore) pruneExpiredLocked() {
	if s == nil {
		return
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() ||
			filepath.Ext(entry.Name()) != mediaSnapshotMetaExt {
			continue
		}

		metaPath := filepath.Join(s.dir, entry.Name())
		meta, found, err := s.readMeta(metaPath)
		if err != nil || !found || now.After(meta.ExpiresAt) {
			dataPath := strings.TrimSuffix(
				metaPath,
				mediaSnapshotMetaExt,
			) + mediaSnapshotDataExt
			_ = s.removeFiles(metaPath, dataPath)
		}
	}
}

func (s *mediaSnapshotStore) removeFiles(paths ...string) error {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := os.Remove(path); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (c *Channel) materializeContentParts(
	ctx context.Context,
	parts []gwproto.ContentPart,
	decryptHints []contentPartDecryptHint,
) ([]gwproto.ContentPart, error) {
	if len(parts) == 0 {
		return nil, nil
	}

	startedAt := time.Now()
	resolved := make([]gwproto.ContentPart, 0, len(parts)*2)
	imageIndex := 0
	for i, part := range parts {
		decryptHint := contentPartDecryptHintAt(decryptHints, i)
		partStartedAt := time.Now()
		switch part.Type {
		case gwproto.PartTypeImage:
			image, err := c.materializeImagePart(
				ctx,
				part.Image,
				decryptHint,
			)
			if err != nil {
				return nil, err
			}
			part.Image = image
			resolved = append(resolved, part)
			logMaterializedPart(
				ctx,
				i,
				part,
				time.Since(partStartedAt),
			)
			companion := imageUploadCompanion(image, imageIndex)
			if companion != nil {
				resolved = append(resolved, gwproto.ContentPart{
					Type: gwproto.PartTypeFile,
					File: companion,
				})
			}
			imageIndex++
			continue
		case gwproto.PartTypeFile:
			file, err := c.materializeFilePart(
				ctx,
				part.File,
				decryptHint,
			)
			if err != nil {
				return nil, err
			}
			if image := imagePartFromFile(file); image != nil {
				imagePart := gwproto.ContentPart{
					Type:  gwproto.PartTypeImage,
					Image: image,
				}
				logMaterializedPart(
					ctx,
					i,
					imagePart,
					time.Since(partStartedAt),
				)
				resolved = append(resolved, imagePart)
				companion := imageUploadCompanion(
					image,
					imageIndex,
				)
				if companion != nil {
					resolved = append(
						resolved,
						gwproto.ContentPart{
							Type: gwproto.PartTypeFile,
							File: companion,
						},
					)
				}
				imageIndex++
				continue
			}
			part.File = file
		}
		logMaterializedPart(
			ctx,
			i,
			part,
			time.Since(partStartedAt),
		)
		resolved = append(resolved, part)
	}
	disambiguateResolvedFilenames(resolved)
	log.InfofContext(
		ctx,
		"wecom: materialized %d content parts into %d gateway "+
			"parts in %v",
		len(parts),
		len(resolved),
		time.Since(startedAt),
	)
	return resolved, nil
}

func disambiguateResolvedFilenames(parts []gwproto.ContentPart) {
	seen := make(map[string]int)
	for i := range parts {
		if parts[i].Type != gwproto.PartTypeFile ||
			parts[i].File == nil {
			continue
		}
		name := cleanFilename(parts[i].File.Filename)
		if name == "" {
			continue
		}
		seen[name]++
		if seen[name] == 1 {
			parts[i].File.Filename = name
			continue
		}
		parts[i].File.Filename = indexedFilename(
			name,
			seen[name],
		)
	}
}

func indexedFilename(name string, index int) string {
	if index <= 1 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		return name
	}
	return base + duplicateFilenameSuffixSeparator +
		fmt.Sprintf("%d", index) + ext
}

func imageUploadCompanion(
	image *gwproto.ImagePart,
	index int,
) *gwproto.FilePart {
	if image == nil || len(image.Data) == 0 {
		return nil
	}

	format := strings.TrimSpace(image.Format)
	if format == "" {
		return nil
	}
	mimeType := mimeTypeFromImageFormat(format)
	if mimeType == "" {
		return nil
	}
	return &gwproto.FilePart{
		Data:     image.Data,
		Filename: imageUploadName(index, format),
		Format:   mimeType,
	}
}

func imagePartFromFile(
	file *gwproto.FilePart,
) *gwproto.ImagePart {
	if file == nil || len(file.Data) == 0 {
		return nil
	}

	format := imageFormatFromMimeType(file.Format)
	if format == "" {
		return nil
	}

	return &gwproto.ImagePart{
		Data:   file.Data,
		Format: format,
	}
}

func imageUploadName(index int, format string) string {
	ext := extFromImageFormat(format)
	if ext == "" {
		return imageUploadNamePrefix + fmt.Sprintf("%d", index)
	}
	return imageUploadNamePrefix + fmt.Sprintf("%d%s", index, ext)
}

func (c *Channel) prefetchMessageMedia(msg WebhookMessage) {
	if c == nil || c.mediaPrefetch == nil {
		return
	}

	seen := make(map[string]struct{})
	for _, rawURL := range mediaURLsForPrefetch(
		msg,
		c.embedImageURL,
		c.embedFileURL,
	) {
		if _, ok := seen[rawURL]; ok {
			continue
		}
		seen[rawURL] = struct{}{}
		c.prefetchMediaURL(rawURL)
	}
}

func mediaURLsForPrefetch(
	msg WebhookMessage,
	embedImageURL bool,
	embedFileURL bool,
) []string {
	var urls []string
	appendURL := func(rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		urls = append(urls, rawURL)
	}

	switch msg.MsgType {
	case MsgTypeImage:
		if !embedImageURL {
			appendURL(msg.Image.URL)
		}
	case MsgTypeFile:
		if !embedFileURL {
			appendURL(msg.File.URL)
		}
	case MsgTypeMixed:
		for _, item := range msg.MixedMessage.MsgItem {
			switch item.MsgType {
			case MsgTypeImage:
				if !embedImageURL {
					appendURL(item.Image.URL)
				}
			case MsgTypeFile:
				if !embedFileURL {
					appendURL(item.File.URL)
				}
			}
		}
		for _, ref := range unknownMixedMediaRefs(msg) {
			appendURL(ref.URL)
		}
	}
	return urls
}

func (c *Channel) prefetchMediaURL(rawURL string) {
	if c == nil || c.mediaPrefetch == nil {
		return
	}
	c.mediaPrefetch.Start(
		rawURL,
		func() (fetchedMedia, error) {
			prefetchCtx, cancel := context.WithTimeout(
				context.Background(),
				defaultMediaDownloadTimeout,
			)
			defer cancel()
			return c.downloadMedia(prefetchCtx, rawURL)
		},
	)
}

func (p *mediaPrefetcher) Start(
	key string,
	loader func() (fetchedMedia, error),
) {
	if p == nil || loader == nil {
		return
	}
	if _, ok := p.loadSnapshot(key); ok {
		return
	}

	entry, shouldLoad := p.ensureEntry(key)
	if !shouldLoad {
		return
	}

	go func() {
		media, err := loader()
		p.complete(key, entry, media, err)
	}()
}

func (p *mediaPrefetcher) Get(
	ctx context.Context,
	key string,
	loader func(context.Context) (fetchedMedia, error),
) (fetchedMedia, error) {
	if p == nil || loader == nil {
		return fetchedMedia{}, errors.New("wecom media: nil prefetcher")
	}
	if media, ok := p.loadSnapshot(key); ok {
		return media, nil
	}

	entry, shouldLoad := p.ensureEntry(key)
	if shouldLoad {
		media, err := loader(ctx)
		p.complete(key, entry, media, err)
		return media, err
	}
	return p.wait(ctx, key, entry)
}

func (p *mediaPrefetcher) ensureEntry(
	key string,
) (*prefetchedMediaEntry, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}

	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.entries[key]; ok {
		if entry.expiresAt.IsZero() ||
			now.Before(entry.expiresAt) {
			return entry, false
		}
		delete(p.entries, key)
	}

	entry := &prefetchedMediaEntry{
		ready: make(chan struct{}),
	}
	p.entries[key] = entry
	return entry, true
}

func (p *mediaPrefetcher) complete(
	key string,
	entry *prefetchedMediaEntry,
	media fetchedMedia,
	err error,
) {
	if p == nil || entry == nil {
		return
	}

	p.mu.Lock()
	entry.media = media
	entry.err = err
	if err != nil {
		delete(p.entries, strings.TrimSpace(key))
	} else {
		entry.expiresAt = time.Now().Add(p.ttl)
		p.saveSnapshot(key, media)
	}
	close(entry.ready)
	p.mu.Unlock()
}

func (p *mediaPrefetcher) wait(
	ctx context.Context,
	key string,
	entry *prefetchedMediaEntry,
) (fetchedMedia, error) {
	if entry == nil {
		return fetchedMedia{},
			fmt.Errorf("wecom media: missing prefetch entry for %q", key)
	}

	select {
	case <-entry.ready:
		return entry.media, entry.err
	case <-ctx.Done():
		return fetchedMedia{}, ctx.Err()
	}
}

func (c *Channel) materializeImagePart(
	ctx context.Context,
	image *gwproto.ImagePart,
	decryptHint contentPartDecryptHint,
) (*gwproto.ImagePart, error) {
	if image == nil || strings.TrimSpace(image.URL) == "" {
		return image, nil
	}

	rawURL := strings.TrimSpace(image.URL)
	media, err := c.fetchMedia(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	data, format, err := c.resolveImageData(
		rawURL,
		media.data,
		decryptHint,
	)
	if err != nil {
		return nil, err
	}

	return &gwproto.ImagePart{
		Data:   data,
		Detail: image.Detail,
		Format: format,
	}, nil
}

func (c *Channel) materializeFilePart(
	ctx context.Context,
	file *gwproto.FilePart,
	decryptHint contentPartDecryptHint,
) (*gwproto.FilePart, error) {
	if file == nil || strings.TrimSpace(file.URL) == "" {
		return file, nil
	}

	rawURL := strings.TrimSpace(file.URL)
	media, err := c.fetchMedia(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	data, mimeType, ext, err := c.resolveFileData(
		rawURL,
		media,
		decryptHint,
	)
	if err != nil {
		return nil, err
	}

	filename := buildFilename(media.filename, ext)
	return &gwproto.FilePart{
		Data:     data,
		Filename: filename,
		Format:   mimeType,
	}, nil
}

func (c *Channel) fetchMedia(
	ctx context.Context,
	rawURL string,
) (fetchedMedia, error) {
	if c != nil && c.mediaPrefetch != nil {
		return c.mediaPrefetch.Get(
			ctx,
			rawURL,
			func(fetchCtx context.Context) (fetchedMedia, error) {
				return c.downloadMedia(fetchCtx, rawURL)
			},
		)
	}
	return c.downloadMedia(ctx, rawURL)
}

func (c *Channel) downloadMedia(
	ctx context.Context,
	rawURL string,
) (fetchedMedia, error) {
	startedAt := time.Now()
	parsedURL, err := c.validateMediaURL(rawURL)
	if err != nil {
		return fetchedMedia{}, err
	}

	client := c.mediaClient
	if client == nil {
		client = &http.Client{Timeout: defaultMediaDownloadTimeout}
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		parsedURL.requestURL(),
		nil,
	)
	if err != nil {
		return fetchedMedia{}, fmt.Errorf("wecom media request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fetchedMedia{}, fmt.Errorf("wecom media download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fetchedMedia{}, fmt.Errorf(
			"wecom media download: status %d",
			resp.StatusCode,
		)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fetchedMedia{}, fmt.Errorf("wecom media read: %w", err)
	}

	media := fetchedMedia{
		contentType: normalizedMediaType(
			resp.Header.Get("Content-Type"),
		),
		filename: mediaFilename(
			resp.Header.Get("Content-Disposition"),
			parsedURL.String(),
		),
		data: data,
	}
	log.InfofContext(
		ctx,
		"wecom: fetched media filename=%q content_type=%q bytes=%d "+
			"duration=%v",
		media.filename,
		media.contentType,
		len(media.data),
		time.Since(startedAt),
	)
	return media, nil
}

func (c *Channel) validateMediaURL(
	rawURL string,
) (validatedMediaURL, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	parsedURL, err := url.Parse(trimmedURL)
	if err != nil {
		return validatedMediaURL{}, fmt.Errorf("wecom media url: %w", err)
	}
	if parsedURL.User != nil {
		return validatedMediaURL{}, errors.New(errMediaURLUserInfo)
	}
	if parsedURL.Fragment != "" {
		return validatedMediaURL{}, errors.New(errMediaURLFragment)
	}

	switch strings.ToLower(parsedURL.Scheme) {
	case mediaSchemeHTTP, mediaSchemeHTTPS:
	default:
		return validatedMediaURL{}, fmt.Errorf(
			"wecom media: unsupported scheme %q",
			parsedURL.Scheme,
		)
	}

	if parsedURL.Hostname() == "" {
		return validatedMediaURL{}, errors.New(errMediaURLHostMissing)
	}

	validator := defaultMediaURLValidator
	if c != nil && c.mediaURLValidator != nil {
		validator = c.mediaURLValidator
	}
	if err := validator(parsedURL); err != nil {
		return validatedMediaURL{}, err
	}
	return validatedMediaURL{value: parsedURL}, nil
}

func defaultMediaURLValidator(parsedURL *url.URL) error {
	host := parsedURL.Hostname()
	if isWecomMediaHost(host) {
		return nil
	}
	return fmt.Errorf("wecom media: untrusted download host %q", host)
}

func logMaterializedPart(
	ctx context.Context,
	index int,
	part gwproto.ContentPart,
	duration time.Duration,
) {
	switch part.Type {
	case gwproto.PartTypeImage:
		if part.Image == nil {
			return
		}
		log.InfofContext(
			ctx,
			"wecom: materialized part index=%d type=%s format=%q "+
				"bytes=%d duration=%v",
			index,
			part.Type,
			part.Image.Format,
			len(part.Image.Data),
			duration,
		)
	case gwproto.PartTypeFile:
		if part.File == nil {
			return
		}
		log.InfofContext(
			ctx,
			"wecom: materialized part index=%d type=%s filename=%q "+
				"format=%q bytes=%d duration=%v",
			index,
			part.Type,
			part.File.Filename,
			part.File.Format,
			len(part.File.Data),
			duration,
		)
	}
}

func (c *Channel) resolveImageData(
	rawURL string,
	data []byte,
	decryptHint contentPartDecryptHint,
) ([]byte, string, error) {
	if format, ok := detectImageFormat(data); ok {
		return data, format, nil
	}

	decrypted, err := c.decryptMediaPayload(
		rawURL,
		data,
		decryptHint,
	)
	if err != nil {
		return nil, "", fmt.Errorf("wecom image decrypt: %w", err)
	}

	format, ok := detectImageFormat(decrypted)
	if !ok {
		return nil, "", fmt.Errorf(
			"wecom image: decrypted payload is not an image",
		)
	}
	return decrypted, format, nil
}

func (c *Channel) resolveFileData(
	rawURL string,
	media fetchedMedia,
	decryptHint contentPartDecryptHint,
) ([]byte, string, string, error) {
	rawMimeType, rawExt := detectFileType(
		media.data,
		media.contentType,
		media.filename,
	)
	if !IsWecomFileURL(rawURL) {
		return media.data, rawMimeType, rawExt, nil
	}

	decrypted, err := c.decryptMediaPayload(
		rawURL,
		media.data,
		decryptHint,
	)
	if err != nil {
		if strings.TrimSpace(decryptHint.AESKey) != "" {
			return nil, "", "", fmt.Errorf(
				"wecom file decrypt: %w",
				err,
			)
		}
		if rawMimeType == mimeTypeOctetStream && rawExt == "" {
			return nil, "", "", fmt.Errorf(
				"wecom file decrypt: %w",
				err,
			)
		}
		return media.data, rawMimeType, rawExt, nil
	}

	decryptedMimeType, decryptedExt := detectFileType(
		decrypted,
		"",
		media.filename,
	)
	if shouldPreferDecryptedFile(rawMimeType, decryptedMimeType) {
		return decrypted, decryptedMimeType, decryptedExt, nil
	}
	return media.data, rawMimeType, rawExt, nil
}

func contentPartDecryptHintAt(
	hints []contentPartDecryptHint,
	index int,
) contentPartDecryptHint {
	if index < 0 || index >= len(hints) {
		return contentPartDecryptHint{}
	}
	return hints[index]
}

func (c *Channel) decryptMediaPayload(
	rawURL string,
	data []byte,
	decryptHint contentPartDecryptHint,
) ([]byte, error) {
	if !IsWecomFileURL(rawURL) {
		return nil, errors.New(
			"wecom media: unsupported encrypted payload",
		)
	}

	encodedAESKey := strings.TrimSpace(decryptHint.AESKey)
	if encodedAESKey != "" {
		return decryptFileWithEncodingAESKey(encodedAESKey, data)
	}
	if c.crypt == nil {
		return nil, errors.New(
			"wecom media: missing aeskey for websocket payload",
		)
	}
	return c.crypt.DecryptFile(data)
}

func shouldPreferDecryptedFile(
	rawMimeType string,
	decryptedMimeType string,
) bool {
	if decryptedMimeType == "" {
		return false
	}
	if rawMimeType == "" || rawMimeType == mimeTypeOctetStream {
		return true
	}
	if rawMimeType == mimeTypeZIP &&
		decryptedMimeType != mimeTypeZIP {
		return true
	}
	return false
}

func detectImageFormat(data []byte) (string, bool) {
	switch normalizedMediaType(http.DetectContentType(dataPrefix(data))) {
	case mimeTypeJPEG:
		return imageFormatJPEG, true
	case mimeTypePNG:
		return imageFormatPNG, true
	case mimeTypeGIF:
		return imageFormatGIF, true
	case mimeTypeWEBP:
		return imageFormatWEBP, true
	default:
		return "", false
	}
}

func detectFileType(
	data []byte,
	contentType string,
	filename string,
) (string, string) {
	mimeType := normalizedMediaType(contentType)
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))

	detectedMimeType, detectedExt := detectPayloadType(data)
	if shouldPreferDetectedType(mimeType, detectedMimeType) {
		mimeType = detectedMimeType
	}
	if detectedExt != "" {
		ext = detectedExt
	}

	if mimeType == "" && ext != "" {
		mimeType = mimeTypeFromExt(ext)
	}
	if mimeType == "" {
		mimeType = mimeTypeOctetStream
	}
	if ext == "" {
		ext = extFromMimeType(mimeType)
	}
	return mimeType, ext
}

func shouldPreferDetectedType(
	current string,
	detected string,
) bool {
	if detected == "" {
		return false
	}
	if current == "" || current == mimeTypeOctetStream {
		return true
	}
	if current == mimeTypeZIP && detected != mimeTypeZIP {
		return true
	}
	return false
}

func detectPayloadType(data []byte) (string, string) {
	if format, ok := detectImageFormat(data); ok {
		return mimeTypeFromImageFormat(format), extFromImageFormat(format)
	}

	if officeMimeType := detectOfficeMimeType(data); officeMimeType != "" {
		return officeMimeType, extFromMimeType(officeMimeType)
	}

	mimeType := normalizedMediaType(http.DetectContentType(dataPrefix(data)))
	if mimeType == "" {
		return "", ""
	}
	return mimeType, extFromMimeType(mimeType)
}

func detectOfficeMimeType(data []byte) string {
	readerAt := bytes.NewReader(data)
	archive, err := zip.NewReader(readerAt, int64(len(data)))
	if err != nil {
		return ""
	}

	for _, file := range archive.File {
		switch {
		case strings.HasPrefix(file.Name, "xl/"):
			return mimeTypeXLSX
		case strings.HasPrefix(file.Name, "word/"):
			return mimeTypeDOCX
		case strings.HasPrefix(file.Name, "ppt/"):
			return mimeTypePPTX
		}
	}
	return ""
}

func mediaFilename(
	contentDisposition string,
	rawURL string,
) string {
	if filename := filenameFromDisposition(contentDisposition); filename != "" {
		return filename
	}
	return filenameFromURL(rawURL)
}

func filenameFromDisposition(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	return cleanFilename(params["filename"])
}

func filenameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := cleanFilename(path.Base(parsed.Path))
	if filepath.Ext(base) == "" {
		return ""
	}
	return base
}

func cleanFilename(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	base := path.Base(trimmed)
	base = filepath.Base(base)
	switch base {
	case "", ".", "/", "\\":
		return ""
	default:
		return base
	}
}

func buildFilename(filename string, ext string) string {
	base := cleanFilename(filename)
	if base == "" {
		base = defaultAttachmentName
	}
	if ext == "" || strings.EqualFold(filepath.Ext(base), ext) {
		return base
	}
	if filepath.Ext(base) != "" {
		return base
	}
	return base + ext
}

func normalizedMediaType(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err != nil {
		return strings.ToLower(trimmed)
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func mimeTypeFromImageFormat(format string) string {
	switch format {
	case imageFormatJPEG:
		return mimeTypeJPEG
	case imageFormatPNG:
		return mimeTypePNG
	case imageFormatGIF:
		return mimeTypeGIF
	case imageFormatWEBP:
		return mimeTypeWEBP
	default:
		return ""
	}
}

func imageFormatFromMimeType(mimeType string) string {
	switch normalizedMediaType(mimeType) {
	case mimeTypeJPEG:
		return imageFormatJPEG
	case mimeTypePNG:
		return imageFormatPNG
	case mimeTypeGIF:
		return imageFormatGIF
	case mimeTypeWEBP:
		return imageFormatWEBP
	default:
		return ""
	}
}

func extFromImageFormat(format string) string {
	switch format {
	case imageFormatJPEG:
		return ".jpg"
	case imageFormatPNG:
		return ".png"
	case imageFormatGIF:
		return ".gif"
	case imageFormatWEBP:
		return ".webp"
	default:
		return ""
	}
}

func extFromMimeType(mimeType string) string {
	switch normalizedMediaType(mimeType) {
	case mimeTypePDF:
		return ".pdf"
	case mimeTypeJPEG:
		return ".jpg"
	case mimeTypePNG:
		return ".png"
	case mimeTypeGIF:
		return ".gif"
	case mimeTypeWEBP:
		return ".webp"
	case mimeTypeXLSX:
		return ".xlsx"
	case mimeTypeDOCX:
		return ".docx"
	case mimeTypePPTX:
		return ".pptx"
	default:
		return ""
	}
}

func mimeTypeFromExt(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".pdf":
		return mimeTypePDF
	case ".jpg", ".jpeg":
		return mimeTypeJPEG
	case ".png":
		return mimeTypePNG
	case ".gif":
		return mimeTypeGIF
	case ".webp":
		return mimeTypeWEBP
	case ".xlsx":
		return mimeTypeXLSX
	case ".docx":
		return mimeTypeDOCX
	case ".pptx":
		return mimeTypePPTX
	default:
		return ""
	}
}

func dataPrefix(data []byte) []byte {
	const sniffLen = 512
	if len(data) <= sniffLen {
		return data
	}
	return data[:sniffLen]
}
