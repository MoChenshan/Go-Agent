package wecom

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
)

const (
	attachmentNoteStart = "[User-facing attachment labels: "
	attachmentNoteArrow = " => "
	attachmentNoteSep   = "; "
	attachmentNoteEnd   = ". Prefer these labels instead of " +
		"synthetic fallback filenames in the final reply.]"
	attachmentInputNotePrefix = "[Attachment handling: "
	attachmentInputNoteSuffix = "]"
	rasterImageInputNote      = "Raster images from this turn are " +
		"already available as image inputs. Do not treat " +
		"png/jpg/jpeg/gif/webp uploads as text documents; " +
		"if you need text from them, use vision or " +
		"OCR/image tooling."
	imageCompanionInputNote = "If the same upload appears as " +
		"both image and file parts, prefer the image part " +
		"as the primary input unless the user explicitly " +
		"asks for the raw file."

	hintReceivedFiles = "已收到 %d 个附件，正在读取内容..."
	hintPreparedFiles = "已读取 %d 个附件，正在准备处理..."

	replyLabelPDFPrefix        = "个上传的 PDF"
	replyLabelAttachmentPrefix = "个上传的附件"
	replyLabelCurrentPDF       = "当前上传的 PDF"
	replyLabelCurrentAttach    = "当前上传的附件"
	replyLabelOrdinalPrefix    = "第 "
	replyLabelOrdinalMid       = " "
)

type attachmentDisplayAlias struct {
	name  string
	label string
}

type replyUXProfile struct {
	attachmentCount   int
	rasterImageCount  int
	hasImageCompanion bool
	aliases           []attachmentDisplayAlias
}

func buildReplyUXProfile(
	parts []gwproto.ContentPart,
) replyUXProfile {
	profile := replyUXProfile{}
	type orderedFile struct {
		order int
		file  *gwproto.FilePart
	}
	files := make([]orderedFile, 0, len(parts))
	fileCount := 0

	for _, part := range parts {
		switch part.Type {
		case gwproto.PartTypeImage, gwproto.PartTypeAudio:
			profile.attachmentCount++
			if part.Type == gwproto.PartTypeImage {
				profile.rasterImageCount++
			}
		case gwproto.PartTypeFile:
			if isImageUploadCompanionName(part.File) {
				profile.hasImageCompanion = true
				continue
			}
			profile.attachmentCount++
			fileCount++
			files = append(files, orderedFile{
				order: fileCount,
				file:  part.File,
			})
		}
	}

	for _, ordered := range files {
		alias := buildAttachmentDisplayAlias(
			ordered.file,
			ordered.order,
			fileCount,
		)
		if alias.name == "" {
			continue
		}
		profile.aliases = append(profile.aliases, alias)
	}

	sort.SliceStable(profile.aliases, func(i, j int) bool {
		return len(profile.aliases[i].name) >
			len(profile.aliases[j].name)
	})
	return profile
}

func buildReplyUXPromptNotes(
	profile replyUXProfile,
) string {
	notes := make([]string, 0, 2)
	notes = appendPromptNote(
		notes,
		currentTurnAttachmentNote(profile.attachmentCount),
	)
	notes = appendPromptNote(
		notes,
		buildAttachmentDisplayNote(profile.aliases),
	)
	notes = appendPromptNote(
		notes,
		buildAttachmentInputNote(profile),
	)
	return strings.Join(notes, runtimePromptNoteSeparator)
}

func buildAttachmentInputNote(
	profile replyUXProfile,
) string {
	if profile.rasterImageCount <= 0 {
		return ""
	}

	parts := []string{rasterImageInputNote}
	if profile.hasImageCompanion {
		parts = append(parts, imageCompanionInputNote)
	}
	return attachmentInputNotePrefix +
		strings.Join(parts, " ") +
		attachmentInputNoteSuffix
}

func buildAttachmentDisplayAlias(
	file *gwproto.FilePart,
	order int,
	total int,
) attachmentDisplayAlias {
	if file == nil {
		return attachmentDisplayAlias{}
	}
	name := cleanFilename(file.Filename)
	if !isSyntheticAttachmentName(name) {
		return attachmentDisplayAlias{}
	}
	return attachmentDisplayAlias{
		name:  name,
		label: userFacingAttachmentLabel(order, total, file),
	}
}

func userFacingAttachmentLabel(
	order int,
	total int,
	file *gwproto.FilePart,
) string {
	if total <= 1 {
		if isPDFFilePart(file) {
			return replyLabelCurrentPDF
		}
		return replyLabelCurrentAttach
	}

	prefix := replyLabelOrdinalPrefix +
		fmt.Sprintf("%d", order) +
		replyLabelOrdinalMid
	if isPDFFilePart(file) {
		return prefix + replyLabelPDFPrefix
	}
	return prefix + replyLabelAttachmentPrefix
}

func isPDFFilePart(file *gwproto.FilePart) bool {
	if file == nil {
		return false
	}
	if strings.EqualFold(file.Format, mimeTypePDF) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(
		strings.TrimSpace(file.Filename),
	))
	return ext == ".pdf"
}

func isImageUploadCompanionName(file *gwproto.FilePart) bool {
	if file == nil {
		return false
	}
	name := cleanFilename(file.Filename)
	if name == "" {
		return false
	}
	if !strings.HasPrefix(name, imageUploadNamePrefix) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".png", ".gif", ".webp":
	default:
		return false
	}
	indexPart := strings.TrimSuffix(name, ext)
	indexPart = strings.TrimPrefix(indexPart, imageUploadNamePrefix)
	if indexPart == "" {
		return false
	}
	_, err := strconv.Atoi(indexPart)
	return err == nil
}

func isSyntheticAttachmentName(name string) bool {
	base := cleanFilename(name)
	if base == "" {
		return false
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == defaultAttachmentName {
		return true
	}
	prefix := defaultAttachmentName +
		duplicateFilenameSuffixSeparator
	if !strings.HasPrefix(stem, prefix) {
		return false
	}
	index := strings.TrimPrefix(stem, prefix)
	if index == "" {
		return false
	}
	_, err := strconv.Atoi(index)
	return err == nil
}

func buildAttachmentDisplayNote(
	aliases []attachmentDisplayAlias,
) string {
	if len(aliases) == 0 {
		return ""
	}

	parts := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if alias.name == "" || alias.label == "" {
			continue
		}
		parts = append(
			parts,
			alias.name+attachmentNoteArrow+alias.label,
		)
	}
	if len(parts) == 0 {
		return ""
	}

	return attachmentNoteStart +
		strings.Join(parts, attachmentNoteSep) +
		attachmentNoteEnd
}

func rewriteReplyContentWithProfile(
	profile replyUXProfile,
	content string,
) string {
	rewritten := content
	for _, alias := range profile.aliases {
		if alias.name == "" || alias.label == "" {
			continue
		}
		rewritten = strings.ReplaceAll(
			rewritten,
			alias.name,
			alias.label,
		)
	}
	return rewritten
}

func initialReplyHintContent(
	processingMessage string,
	contentParts []gwproto.ContentPart,
) string {
	attachmentCount := countTurnAttachments(contentParts)
	switch {
	case hasImageContentPart(contentParts):
		return defaultImageProcessingText
	case attachmentCount > 1:
		return fmt.Sprintf(hintReceivedFiles, attachmentCount)
	case hasAttachmentContentPart(contentParts):
		return defaultAttachmentReadText
	default:
		return strings.TrimSpace(processingMessage)
	}
}

func preGatewayReplyHintContent(profile replyUXProfile) string {
	if profile.attachmentCount <= 0 {
		return ""
	}
	return fmt.Sprintf(
		hintPreparedFiles,
		profile.attachmentCount,
	)
}
