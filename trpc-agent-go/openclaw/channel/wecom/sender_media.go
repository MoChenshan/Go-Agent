package wecom

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	replyMediaChunkSize = 512 * 1024

	replyFileMaxBytes  = 20 * 1024 * 1024
	replyImageMaxBytes = 2 * 1024 * 1024
	replyVoiceMaxBytes = 2 * 1024 * 1024
	replyVideoMaxBytes = 10 * 1024 * 1024

	replyVoiceExt = ".amr"
	replyVideoExt = ".mp4"
)

type replyMediaLimitError struct {
	Filename   string
	MsgType    string
	HintedType string
	Size       int
	Limit      int
}

type replyMediaEmptyError struct {
	Filename string
}

type replyMediaChunkError struct {
	Filename string
	Chunks   int
}

type aibotMediaRef struct {
	MediaID string `json:"media_id,omitempty"`
}

type aibotVideoRef struct {
	MediaID     string `json:"media_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type wsUploadMediaInitBody struct {
	Type        string `json:"type"`
	Filename    string `json:"filename"`
	TotalSize   int    `json:"total_size"`
	TotalChunks int    `json:"total_chunks"`
	MD5         string `json:"md5,omitempty"`
}

type wsUploadMediaInitAck struct {
	UploadID string `json:"upload_id,omitempty"`
}

type wsUploadMediaChunkBody struct {
	UploadID   string `json:"upload_id"`
	ChunkIndex int    `json:"chunk_index"`
	Base64Data string `json:"base64_data"`
}

type wsUploadMediaFinishBody struct {
	UploadID string `json:"upload_id"`
}

type wsUploadMediaFinishAck struct {
	Type      string `json:"type,omitempty"`
	MediaID   string `json:"media_id,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

type localReplyMedia struct {
	MsgType     string
	Filename    string
	Data        []byte
	Title       string
	Description string
	SourceExt   string
	HintedType  string
}

type localReplyMediaOptions struct {
	Filename   string
	ForceVoice bool
}

func (s *aibotWebSocketSender) SendLocalFile(
	ctx context.Context,
	_ string,
	path string,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	requester, ok := s.writer.(wsRequestWriter)
	if !ok {
		return fmt.Errorf(
			"wecom websocket sender: media upload not supported",
		)
	}

	media, err := loadLocalReplyMedia(path)
	if err != nil {
		return err
	}
	log.InfofContext(
		ctx,
		"wecom websocket: upload local file msgtype=%s "+
			"filename=%q bytes=%d",
		media.MsgType,
		media.Filename,
		len(media.Data),
	)

	mediaID, err := uploadLocalReplyMedia(
		ctx,
		requester,
		media,
	)
	if err != nil {
		return err
	}
	return s.sendUploadedReplyMedia(ctx, media, mediaID)
}

func loadLocalReplyMedia(path string) (localReplyMedia, error) {
	return loadLocalReplyMediaWithOptions(path, localReplyMediaOptions{})
}

func loadLocalReplyMediaFile(
	file occhannel.OutboundFile,
) (localReplyMedia, error) {
	return loadLocalReplyMediaWithOptions(file.Path, localReplyMediaOptions{
		Filename:   file.Name,
		ForceVoice: file.AsVoice,
	})
}

func loadLocalReplyMediaWithOptions(
	path string,
	opts localReplyMediaOptions,
) (localReplyMedia, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return localReplyMedia{}, fmt.Errorf(
			"wecom websocket sender: empty reply file path",
		)
	}
	cleaned := filepath.Clean(trimmedPath)
	data, err := os.ReadFile(cleaned)
	if err != nil {
		return localReplyMedia{}, fmt.Errorf(
			"wecom websocket sender: read reply file: %w",
			err,
		)
	}

	media := classifyLocalReplyMediaWithOptions(
		cleaned,
		data,
		opts,
	)
	if err := validateLocalReplyMedia(media); err != nil {
		return localReplyMedia{}, err
	}
	return media, nil
}

func classifyLocalReplyMedia(
	path string,
	data []byte,
) localReplyMedia {
	return classifyLocalReplyMediaWithOptions(
		path,
		data,
		localReplyMediaOptions{},
	)
}

func classifyLocalReplyMediaWithOptions(
	path string,
	data []byte,
	opts localReplyMediaOptions,
) localReplyMedia {
	sourceExt := strings.ToLower(filepath.Ext(path))
	filename := cleanReplyMediaFilename(opts.Filename, path, sourceExt)
	if filename == "" {
		filename = defaultAttachmentName
	}

	ext := strings.ToLower(filepath.Ext(filename))
	kindExt := ext
	if opts.ForceVoice && sourceExt == replyVoiceExt {
		kindExt = replyVoiceExt
	}
	media := localReplyMedia{
		MsgType:    MsgTypeFile,
		Filename:   filename,
		Data:       data,
		SourceExt:  sourceExt,
		HintedType: hintedReplyMediaType(kindExt),
	}
	switch {
	case isReplyImageExt(kindExt) && len(data) <= replyImageMaxBytes:
		media.MsgType = MsgTypeImage
	case kindExt == replyVoiceExt &&
		len(data) <= replyVoiceMaxBytes:
		media.MsgType = MsgTypeVoice
	case kindExt == replyVideoExt &&
		len(data) <= replyVideoMaxBytes:
		media.MsgType = MsgTypeVideo
		media.Title = strings.TrimSuffix(filename, ext)
	default:
		media.MsgType = MsgTypeFile
	}
	if strings.TrimSpace(media.Title) == "" {
		media.Title = strings.TrimSuffix(filename, ext)
	}
	return media
}

func cleanReplyMediaFilename(
	name string,
	path string,
	sourceExt string,
) string {
	filename := cleanFilename(name)
	if filename == "" {
		return cleanFilename(path)
	}
	if filepath.Ext(filename) == "" && strings.TrimSpace(sourceExt) != "" {
		return buildFilename(filename, strings.ToLower(sourceExt))
	}
	return filename
}

func isReplyImageExt(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".jpg", ".jpeg", ".png", ".gif":
		return true
	default:
		return false
	}
}

func hintedReplyMediaType(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	switch {
	case isReplyImageExt(ext):
		return MsgTypeImage
	case isReplyVoiceLikeExt(ext):
		return MsgTypeVoice
	case isReplyVideoLikeExt(ext):
		return MsgTypeVideo
	default:
		return MsgTypeFile
	}
}

func isReplyVoiceLikeExt(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case replyVoiceExt, ".mp3", ".wav", ".m4a", ".aac", ".ogg":
		return true
	default:
		return false
	}
}

func isReplyVideoLikeExt(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case replyVideoExt, ".mov", ".m4v", ".avi", ".mkv", ".webm":
		return true
	default:
		return false
	}
}

func validateLocalReplyMedia(media localReplyMedia) error {
	size := len(media.Data)
	if size <= 0 {
		return &replyMediaEmptyError{Filename: media.Filename}
	}

	limit := replyFileMaxBytes
	switch media.MsgType {
	case MsgTypeImage:
		limit = replyImageMaxBytes
	case MsgTypeVoice:
		limit = replyVoiceMaxBytes
	case MsgTypeVideo:
		limit = replyVideoMaxBytes
	}
	if size > limit {
		return &replyMediaLimitError{
			Filename:   media.Filename,
			MsgType:    media.MsgType,
			HintedType: media.HintedType,
			Size:       size,
			Limit:      limit,
		}
	}

	chunks := replyMediaChunkCount(size)
	if chunks > maxReplyMediaChunks {
		return &replyMediaChunkError{
			Filename: media.Filename,
			Chunks:   chunks,
		}
	}
	return nil
}

func (e *replyMediaLimitError) Error() string {
	if e == nil {
		return "wecom websocket sender: reply file exceeds size limit"
	}
	return fmt.Sprintf(
		"wecom websocket sender: reply file %q exceeds "+
			"size limit %d bytes for %s",
		e.Filename,
		e.Limit,
		e.MsgType,
	)
}

func (e *replyMediaLimitError) UserNote() string {
	if e == nil {
		return ""
	}
	name := strings.TrimSpace(e.Filename)
	if name == "" {
		name = "该附件"
	}
	base := name + " " + formatReplyMediaBytes(e.Size) +
		"，超过企微" + replyMediaTypeLabel(e.MsgType) +
		" " + formatReplyMediaBytes(e.Limit) + " 上限。"
	switch e.HintedType {
	case MsgTypeImage:
		return base + "如果要按图片回传，请先压缩到 2 MB 内。"
	case MsgTypeVoice:
		return base + "如果要按语音回传，请先转成 amr 且压到 2 MB 内。"
	case MsgTypeVideo:
		return base + "如果要按视频回传，请先转成 mp4 且压到 10 MB 内。"
	default:
		return base
	}
}

func (e *replyMediaEmptyError) Error() string {
	if e == nil {
		return "wecom websocket sender: reply file is empty"
	}
	return "wecom websocket sender: reply file is empty"
}

func (e *replyMediaEmptyError) UserNote() string {
	if e == nil {
		return ""
	}
	name := strings.TrimSpace(e.Filename)
	if name == "" {
		name = "该附件"
	}
	return name + " 是空文件，无法回传。"
}

func (e *replyMediaChunkError) Error() string {
	if e == nil {
		return "wecom websocket sender: reply file exceeds chunk limit"
	}
	return fmt.Sprintf(
		"wecom websocket sender: reply file %q needs %d "+
			"chunks, exceeds %d",
		e.Filename,
		e.Chunks,
		maxReplyMediaChunks,
	)
}

func (e *replyMediaChunkError) UserNote() string {
	if e == nil {
		return ""
	}
	name := strings.TrimSpace(e.Filename)
	if name == "" {
		name = "该附件"
	}
	return name + " 分片数超过企微上限，无法回传。"
}

func replyMediaTypeLabel(msgType string) string {
	switch strings.TrimSpace(msgType) {
	case MsgTypeImage:
		return "图片"
	case MsgTypeVoice:
		return "语音"
	case MsgTypeVideo:
		return "视频"
	default:
		return "普通文件"
	}
}

func formatReplyMediaBytes(size int) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func replyMediaChunkCount(size int) int {
	if size <= 0 {
		return 0
	}
	chunks := size / replyMediaChunkSize
	if size%replyMediaChunkSize != 0 {
		chunks++
	}
	return chunks
}

func uploadLocalReplyMedia(
	ctx context.Context,
	writer wsRequestWriter,
	media localReplyMedia,
) (string, error) {
	initAck, err := uploadLocalReplyMediaInit(
		ctx,
		writer,
		media,
	)
	if err != nil {
		return "", err
	}

	uploadID := strings.TrimSpace(initAck.UploadID)
	if uploadID == "" {
		return "", fmt.Errorf(
			"wecom websocket sender: empty upload_id",
		)
	}

	if err := uploadLocalReplyMediaChunks(
		ctx,
		writer,
		uploadID,
		media.Data,
	); err != nil {
		return "", err
	}

	finishAck, err := uploadLocalReplyMediaFinish(
		ctx,
		writer,
		uploadID,
	)
	if err != nil {
		return "", err
	}

	mediaID := strings.TrimSpace(finishAck.MediaID)
	if mediaID == "" {
		return "", fmt.Errorf(
			"wecom websocket sender: empty media_id",
		)
	}
	return mediaID, nil
}

func uploadLocalReplyMediaInit(
	ctx context.Context,
	writer wsRequestWriter,
	media localReplyMedia,
) (wsUploadMediaInitAck, error) {
	sum := md5.Sum(media.Data)
	frame := wsOutboundFrame{
		Command: wsCommandUploadMediaInit,
		Headers: wsFrameHeaders{
			ReqID: nextWSReqID(wsReqIDUploadInit),
		},
		Body: wsUploadMediaInitBody{
			Type:        media.MsgType,
			Filename:    media.Filename,
			TotalSize:   len(media.Data),
			TotalChunks: replyMediaChunkCount(len(media.Data)),
			MD5:         hex.EncodeToString(sum[:]),
		},
	}
	ack, err := writer.request(ctx, frame)
	if err != nil {
		return wsUploadMediaInitAck{}, err
	}
	var body wsUploadMediaInitAck
	if err := unmarshalWSFrameBody(ack, &body); err != nil {
		return wsUploadMediaInitAck{}, err
	}
	return body, nil
}

func uploadLocalReplyMediaChunks(
	ctx context.Context,
	writer wsRequestWriter,
	uploadID string,
	data []byte,
) error {
	for index, start := 0, 0; start < len(data); index++ {
		end := start + replyMediaChunkSize
		if end > len(data) {
			end = len(data)
		}
		frame := wsOutboundFrame{
			Command: wsCommandUploadMediaChunk,
			Headers: wsFrameHeaders{
				ReqID: nextWSReqID(wsReqIDUploadChunk),
			},
			Body: wsUploadMediaChunkBody{
				UploadID:   uploadID,
				ChunkIndex: index,
				Base64Data: base64.StdEncoding.EncodeToString(
					data[start:end],
				),
			},
		}
		if _, err := writer.request(ctx, frame); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func uploadLocalReplyMediaFinish(
	ctx context.Context,
	writer wsRequestWriter,
	uploadID string,
) (wsUploadMediaFinishAck, error) {
	frame := wsOutboundFrame{
		Command: wsCommandUploadMediaFinish,
		Headers: wsFrameHeaders{
			ReqID: nextWSReqID(wsReqIDUploadFinish),
		},
		Body: wsUploadMediaFinishBody{
			UploadID: uploadID,
		},
	}
	ack, err := writer.request(ctx, frame)
	if err != nil {
		return wsUploadMediaFinishAck{}, err
	}
	var body wsUploadMediaFinishAck
	if err := unmarshalWSFrameBody(ack, &body); err != nil {
		return wsUploadMediaFinishAck{}, err
	}
	return body, nil
}

func (s *aibotWebSocketSender) sendUploadedReplyMedia(
	ctx context.Context,
	media localReplyMedia,
	mediaID string,
) error {
	body := wsReplyBody{MsgType: media.MsgType}
	switch media.MsgType {
	case MsgTypeImage:
		body.Image = &aibotMediaRef{MediaID: mediaID}
	case MsgTypeVoice:
		body.Voice = &aibotMediaRef{MediaID: mediaID}
	case MsgTypeVideo:
		body.Video = &aibotVideoRef{
			MediaID:     mediaID,
			Title:       media.Title,
			Description: media.Description,
		}
	default:
		body.MsgType = MsgTypeFile
		body.File = &aibotMediaRef{MediaID: mediaID}
	}

	frame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body:    body,
	}
	return s.writer.send(ctx, frame)
}

func sendUploadedPushMedia(
	ctx context.Context,
	writer wsRequestWriter,
	target pushTarget,
	media localReplyMedia,
	mediaID string,
) error {
	if writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	body := buildPushMediaBody(target, media, mediaID)
	_, err := writer.request(ctx, wsOutboundFrame{
		Command: wsCommandSend,
		Headers: wsFrameHeaders{
			ReqID: nextWSReqID(wsReqIDSend),
		},
		Body: body,
	})
	return err
}

func buildPushMediaBody(
	target pushTarget,
	media localReplyMedia,
	mediaID string,
) wsSendBody {
	body := wsSendBody{
		ChatID:  target.ChatID,
		MsgType: media.MsgType,
	}
	switch media.MsgType {
	case MsgTypeImage:
		body.Image = &aibotMediaRef{MediaID: mediaID}
	case MsgTypeVoice:
		body.Voice = &aibotMediaRef{MediaID: mediaID}
	case MsgTypeVideo:
		body.Video = &aibotVideoRef{
			MediaID:     mediaID,
			Title:       media.Title,
			Description: media.Description,
		}
	default:
		body.MsgType = MsgTypeFile
		body.File = &aibotMediaRef{MediaID: mediaID}
	}
	return body
}

func unmarshalWSFrameBody(
	frame wsInboundFrame,
	target any,
) error {
	if len(frame.Body) == 0 {
		return fmt.Errorf("wecom websocket: empty ack body")
	}
	if err := json.Unmarshal(frame.Body, target); err != nil {
		return fmt.Errorf(
			"wecom websocket: unmarshal ack body: %w",
			err,
		)
	}
	return nil
}
