// Package client provides the HTTP client implementation for interacting with
// the tRAG (https://ai.woa.com/#/trag) API. It handles function execution,
// file operations, and response processing.
package client

import "encoding/json"

// TragResponse represents the standard response from TRAG API
type TragResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	TraceID string          `json:"traceId"`
	Data    json.RawMessage `json:"data"`
}

// IsSuccess checks if the response indicates success
func (r *TragResponse) IsSuccess() bool {
	return r.Code == 0
}

// FunctionStatus represents the status of a function execution
type FunctionStatus string

// Function execution status constants.
const (
	// FunctionStatusSuccess indicates the function completed successfully
	FunctionStatusSuccess FunctionStatus = "success"
	// FunctionStatusFailed indicates the function failed during execution
	FunctionStatusFailed FunctionStatus = "failed"
	// FunctionStatusInit indicates the function is initialized but not yet started
	FunctionStatusInit FunctionStatus = "init"
	// FunctionStatusRunning indicates the function is currently executing
	FunctionStatusRunning FunctionStatus = "running"
	// FunctionStatusTimeout indicates the function exceeded its execution time limit
	FunctionStatusTimeout FunctionStatus = "timeout"
)

// IsFinished checks if the function execution has finished
func (s FunctionStatus) IsFinished() bool {
	return s == FunctionStatusSuccess || s == FunctionStatusFailed || s == FunctionStatusTimeout
}

// FunctionStatusData represents the status response data
type FunctionStatusData struct {
	Status FunctionStatus `json:"status"`
}

// FunctionDefinition represents a function's metadata
type FunctionDefinition struct {
	ToolsID       int            `json:"toolsId"`
	ToolsVersion  int            `json:"toolsVersion"`
	ToolVersionID string         `json:"toolVersionId"`
	FunctionName  string         `json:"functionName"`
	FullName      string         `json:"fullName"`
	ParameterType map[string]any `json:"parameterType"`
	ReturnType    map[string]any `json:"returnType"`
	Description   string         `json:"description"`
}

// FunctionMetaResponse represents the response for function meta query
type FunctionMetaResponse struct {
	ToolsID   int              `json:"toolsId"`
	Version   int              `json:"version"`
	Functions []map[string]any `json:"functions"`
}

// FileSource represents the source of a file
type FileSource string

// File source constants.
const (
	// FileSourceUserUpload indicates the file was uploaded by a user
	FileSourceUserUpload FileSource = "userUpload"
	// FileSourceUserOutput indicates the file was generated as output
	FileSourceUserOutput FileSource = "userOutput"
)

// FileContentType represents the MIME type of a file.
// These constants map to standard MIME types used for file upload/download operations.
type FileContentType string

// Supported MIME types for file operations.
const (
	FileContentTypeAAC       FileContentType = "audio/aac"
	FileContentTypeABW       FileContentType = "application/x-abiword"
	FileContentTypeAPNG      FileContentType = "image/apng"
	FileContentTypeARC       FileContentType = "application/x-freearc"
	FileContentTypeASX       FileContentType = "video/x-ms-asf"
	FileContentTypeASF       FileContentType = "video/x-ms-asf"
	FileContentTypeAVIF      FileContentType = "image/avif"
	FileContentTypeAVI       FileContentType = "video/x-msvideo"
	FileContentTypeAZW       FileContentType = "application/vnd.amazon.ebook"
	FileContentTypeBIN       FileContentType = "application/octet-stream"
	FileContentTypeBMP       FileContentType = "image/bmp"
	FileContentTypeBZ        FileContentType = "application/x-bzip"
	FileContentTypeBZ2       FileContentType = "application/x-bzip2"
	FileContentTypeCDA       FileContentType = "application/x-cdf"
	FileContentTypeCSH       FileContentType = "application/x-csh"
	FileContentTypeCSS       FileContentType = "text/css"
	FileContentTypeCSV       FileContentType = "text/csv"
	FileContentTypeDOC       FileContentType = "application/msword"
	FileContentTypeDOCX      FileContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	FileContentTypeEOT       FileContentType = "application/vnd.ms-fontobject"
	FileContentTypeEPUB      FileContentType = "application/epub+zip"
	FileContentTypeFLV       FileContentType = "video/x-flv"
	FileContentTypeGZ        FileContentType = "application/x-gzip"
	FileContentTypeGZIP      FileContentType = "application/gzip"
	FileContentTypeGIF       FileContentType = "image/gif"
	FileContentTypeHTML      FileContentType = "text/html"
	FileContentTypeHTM       FileContentType = "text/html"
	FileContentTypeICO       FileContentType = "image/vnd.microsoft.icon"
	FileContentTypeICS       FileContentType = "text/calendar"
	FileContentTypeJAR       FileContentType = "application/java-archive"
	FileContentTypeJPG       FileContentType = "image/jpg"
	FileContentTypeJPEG      FileContentType = "image/jpeg"
	FileContentTypeJS        FileContentType = "text/javascript"
	FileContentTypeJSON      FileContentType = "application/json"
	FileContentTypeJSONLD    FileContentType = "application/ld+json"
	FileContentTypeMARKDOWN  FileContentType = "text/markdown"
	FileContentTypeMIDI      FileContentType = "audio/midi"
	FileContentTypeMID       FileContentType = "audio/midi"
	FileContentTypeMJS       FileContentType = "application/javascript"
	FileContentTypeMP3       FileContentType = "audio/mpeg"
	FileContentTypeMP4       FileContentType = "video/mp4"
	FileContentTypeM4A       FileContentType = "audio/x-m4a"
	FileContentTypeM4V       FileContentType = "video/x-m4v"
	FileContentTypeMNG       FileContentType = "audio/x-mng"
	FileContentTypeMOV       FileContentType = "video/quicktime"
	FileContentTypeMPEG      FileContentType = "video/mpeg"
	FileContentTypeMPKG      FileContentType = "application/vnd.apple.installer+xml"
	FileContentTypeODP       FileContentType = "application/vnd.oasis.opendocument.presentation"
	FileContentTypeODS       FileContentType = "application/vnd.oasis.opendocument.spreadsheet"
	FileContentTypeODT       FileContentType = "application/vnd.oasis.opendocument.text"
	FileContentTypeOGA       FileContentType = "audio/ogg"
	FileContentTypeOGG       FileContentType = "audio/ogg"
	FileContentTypeOGV       FileContentType = "video/ogg"
	FileContentTypeOGX       FileContentType = "application/ogg"
	FileContentTypeOPUS      FileContentType = "audio/ogg"
	FileContentTypeOTF       FileContentType = "font/otf"
	FileContentTypePNG       FileContentType = "image/png"
	FileContentTypePDF       FileContentType = "application/pdf"
	FileContentTypePHP       FileContentType = "application/x-httpd-php"
	FileContentTypePPT       FileContentType = "application/vnd.ms-powerpoint"
	FileContentTypePPTX      FileContentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	FileContentTypeRA        FileContentType = "audio/x-realaudio"
	FileContentTypeRAR       FileContentType = "application/vnd.rar"
	FileContentTypeRTF       FileContentType = "application/rtf"
	FileContentTypeSH        FileContentType = "application/x-sh"
	FileContentTypeSVG       FileContentType = "image/svg+xml"
	FileContentTypeTAR       FileContentType = "application/x-tar"
	FileContentTypeTIFF      FileContentType = "image/tiff"
	FileContentTypeTIF       FileContentType = "image/tiff"
	FileContentTypeTS        FileContentType = "video/mp2t"
	FileContentTypeTSV       FileContentType = "text/tab-separated-values"
	FileContentTypeTTF       FileContentType = "font/ttf"
	FileContentTypeTXT       FileContentType = "text/plain"
	FileContentTypeTEXT      FileContentType = "text/plain"
	FileContentTypeVSD       FileContentType = "application/vnd.visio"
	FileContentTypeWAV       FileContentType = "audio/wav"
	FileContentTypeWMV       FileContentType = "video/x-ms-wmv"
	FileContentTypeWEBA      FileContentType = "audio/webm"
	FileContentTypeWEBM      FileContentType = "video/webm"
	FileContentTypeWEBP      FileContentType = "image/webp"
	FileContentTypeWOFF      FileContentType = "font/woff"
	FileContentTypeWOFF2     FileContentType = "font/woff2"
	FileContentTypeXHTML     FileContentType = "application/xhtml+xml"
	FileContentTypeXLS       FileContentType = "application/vnd.ms-excel"
	FileContentTypeXLSX      FileContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	FileContentTypeXML       FileContentType = "application/xml"
	FileContentTypeXUL       FileContentType = "application/vnd.mozilla.xul+xml"
	FileContentTypeZIP       FileContentType = "application/zip"
	FileContentType3GPP      FileContentType = "video/3gpp"
	FileContentType3GP       FileContentType = "video/3gpp"
	FileContentTypeMULTIPART FileContentType = "multipart/form-data"
	FileContentTypeSTREAM    FileContentType = "application/octet-stream"
	FileContentTypeUNKNOWN   FileContentType = "unknown"
)

// FileInfo represents information about a file to be uploaded
type FileInfo struct {
	FileName   string          `json:"fileName"`
	FilePath   string          `json:"filePath"`
	FileType   FileContentType `json:"fileType"`
	FileSource FileSource      `json:"fileSource"`
}

// UploadFileRequest represents a request to get upload URL
type UploadFileRequest struct {
	Operator    string          `json:"operator"`
	FileName    string          `json:"fileName"`
	FileSize    int64           `json:"fileSize"`
	FileSource  FileSource      `json:"fileSource"`
	ContentType FileContentType `json:"contentType"`
}

// UploadFileResponse represents the response containing upload URL
type UploadFileResponse struct {
	FileID    string `json:"fileId"`
	UploadURL string `json:"uploadUrl"`
	FileName  string `json:"fileName"`
}

// DownloadFileResponse represents the response containing download URL
type DownloadFileResponse struct {
	FileID      string `json:"fileId"`
	DownloadURL string `json:"downloadUrl"`
	FileName    string `json:"fileName"`
}
