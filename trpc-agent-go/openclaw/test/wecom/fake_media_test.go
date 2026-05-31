package wecome2e_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/png"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type fakeMediaServer struct {
	server           *http.Server
	listener         net.Listener
	containerBaseURL string
	mu               sync.Mutex
	counter          int
	assets           map[string]fakeMediaAsset
	hits             map[string]int
}

type fakeMediaAsset struct {
	filename    string
	contentType string
	data        []byte
}

func newFakeMediaServer(
	containerHost string,
) (*fakeMediaServer, error) {
	server := &fakeMediaServer{
		assets: make(map[string]fakeMediaAsset),
		hits:   make(map[string]int),
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		server.mu.Lock()
		asset, ok := server.assets[r.URL.Path]
		if ok {
			server.hits[r.URL.Path]++
		}
		server.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.contentType)
		w.Header().Set(
			"Content-Disposition",
			fmt.Sprintf("inline; filename=%q", asset.filename),
		)
		_, _ = w.Write(asset.data)
	})}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	server.server = httpServer
	server.listener = listener
	server.containerBaseURL = fmt.Sprintf("http://%s:%d", containerHost, port)
	return server, nil
}

func (s *fakeMediaServer) close() {
	if s == nil {
		return
	}
	if s.server != nil {
		_ = s.server.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

func (s *fakeMediaServer) registerAsset(
	t *testing.T,
	filename string,
	contentType string,
	data []byte,
) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	path := fmt.Sprintf("/asset-%d/%s", s.counter, filename)
	s.assets[path] = fakeMediaAsset{
		filename:    filename,
		contentType: contentType,
		data:        append([]byte(nil), data...),
	}
	return s.containerBaseURL + path
}

func (s *fakeMediaServer) hitCount(rawURL string) int {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[parsed.Path]
}

func mustCreateOCRImage(t *testing.T, text string) []byte {
	t.Helper()
	base := image.NewRGBA(image.Rect(0, 0, 240, 40))
	imagedraw.Draw(
		base,
		base.Bounds(),
		&image.Uniform{C: image.White},
		image.Point{},
		imagedraw.Src,
	)
	drawer := &font.Drawer{
		Dst:  base,
		Src:  image.Black,
		Face: basicfont.Face7x13,
		Dot:  fixed.P(10, 24),
	}
	drawer.DrawString(text)
	scaled := scaleImageNearest(base, 8)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, scaled))
	return buf.Bytes()
}

func mustCreateSolidColorImage(
	t *testing.T,
	width int,
	height int,
	fill color.Color,
) []byte {
	t.Helper()
	require.Positive(t, width)
	require.Positive(t, height)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	imagedraw.Draw(
		img,
		img.Bounds(),
		&image.Uniform{C: fill},
		image.Point{},
		imagedraw.Src,
	)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func scaleImageNearest(src *image.RGBA, factor int) *image.RGBA {
	if factor <= 1 {
		clone := image.NewRGBA(src.Bounds())
		copy(clone.Pix, src.Pix)
		return clone
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(
		0,
		0,
		bounds.Dx()*factor,
		bounds.Dy()*factor,
	))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			c := src.RGBAAt(bounds.Min.X+x, bounds.Min.Y+y)
			for yy := 0; yy < factor; yy++ {
				for xx := 0; xx < factor; xx++ {
					dst.SetRGBA(x*factor+xx, y*factor+yy, c)
				}
			}
		}
	}
	return dst
}

func mustCreateTextPDF(t *testing.T, lines ...string) []byte {
	t.Helper()
	return mustCreateMultiPageTextPDF(t, lines)
}

func mustCreateMultiPageTextPDF(t *testing.T, pages ...[]string) []byte {
	t.Helper()
	require.NotEmpty(t, pages)
	contents := make([]string, 0, len(pages))
	for _, lines := range pages {
		require.NotEmpty(t, lines)
		var content bytes.Buffer
		_, err := content.WriteString("BT\n/F1 16 Tf\n72 720 Td\n")
		require.NoError(t, err)
		for i, line := range lines {
			if i > 0 {
				_, err = content.WriteString("0 -24 Td\n")
				require.NoError(t, err)
			}
			_, err = fmt.Fprintf(&content, "(%s) Tj\n", escapePDFText(line))
			require.NoError(t, err)
		}
		_, err = content.WriteString("ET\n")
		require.NoError(t, err)
		contents = append(contents, content.String())
	}
	fontObjectNumber := 3 + len(contents)*2
	kids := make([]string, 0, len(contents))
	objectMap := map[int]string{
		1: "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
	}
	for index, content := range contents {
		pageObjectNumber := 3 + index*2
		contentObjectNumber := pageObjectNumber + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObjectNumber))
		objectMap[pageObjectNumber] = fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>\nendobj\n", pageObjectNumber, fontObjectNumber, contentObjectNumber)
		objectMap[contentObjectNumber] = fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", contentObjectNumber, len(content), content)
	}
	objectMap[2] = fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", strings.Join(kids, " "), len(kids))
	objectMap[fontObjectNumber] = fmt.Sprintf("%d 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n", fontObjectNumber)
	objects := make([]string, 0, len(objectMap))
	for objectNumber := 1; objectNumber <= fontObjectNumber; objectNumber++ {
		object, ok := objectMap[objectNumber]
		require.True(t, ok)
		objects = append(objects, object)
	}
	var buf bytes.Buffer
	var err error
	_, err = buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	require.NoError(t, err)
	offsets := make([]int, 0, len(objects)+1)
	offsets = append(offsets, 0)
	for _, object := range objects {
		offsets = append(offsets, buf.Len())
		_, err = buf.WriteString(object)
		require.NoError(t, err)
	}
	xrefOffset := buf.Len()
	_, err = fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	require.NoError(t, err)
	_, err = buf.WriteString("0000000000 65535 f \n")
	require.NoError(t, err)
	for _, offset := range offsets[1:] {
		_, err = fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
		require.NoError(t, err)
	}
	_, err = fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	require.NoError(t, err)
	verifyPDFPagesText(t, buf.Bytes(), pages...)
	return buf.Bytes()
}

func escapePDFText(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}

func verifyPDFText(t *testing.T, data []byte, expectedLines ...string) {
	t.Helper()
	verifyPDFPagesText(t, data, expectedLines)
}

func readPDFPagesText(t *testing.T, data []byte) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	fileHandle, reader, err := pdf.Open(path)
	require.NoError(t, err)
	defer fileHandle.Close()
	fonts := make(map[string]*pdf.Font)
	pages := make([]string, 0, reader.NumPage())
	for pageIndex := 1; pageIndex <= reader.NumPage(); pageIndex++ {
		page := reader.Page(pageIndex)
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; ok {
				continue
			}
			font := page.Font(name)
			fonts[name] = &font
		}
		text, err := page.GetPlainText(fonts)
		require.NoError(t, err)
		pages = append(pages, text)
	}
	return pages
}

func verifyPDFPagesText(t *testing.T, data []byte, expectedPages ...[]string) {
	t.Helper()
	pages := readPDFPagesText(t, data)
	require.Len(t, pages, len(expectedPages))
	for pageIndex, expectedLines := range expectedPages {
		for _, expectedLine := range expectedLines {
			require.Contains(t, pages[pageIndex], expectedLine)
		}
	}
}

func readPDFText(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	fileHandle, reader, err := pdf.Open(path)
	require.NoError(t, err)
	defer fileHandle.Close()
	textReader, err := reader.GetPlainText()
	require.NoError(t, err)
	plain, err := io.ReadAll(textReader)
	require.NoError(t, err)
	return string(plain)
}

func decodeImageData(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return img
}
