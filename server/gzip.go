package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// minGzipSize is the threshold below which response compression is skipped.
//
// Compressing tiny payloads wastes CPU and, because of gzip framing overhead
// (headers, Huffman tables, CRC32 trailer), can yield a payload larger than
// the uncompressed source. At 512 bytes, error envelopes, health probes, and
// small status messages pass through uncompressed, while index.html (~100 KB)
// and populated JSON responses are compressed.
const minGzipSize = 512

// withGzip compresses JSON and HTML responses with gzip when the client's
// Accept-Encoding header indicates support (issue #610).
//
// Vary: Accept-Encoding is unconditionally added to responses handled by this
// middleware so downstream caches (CDNs, browser caches, proxies) keep the
// compressed and uncompressed representations distinct. Tiny bodies below
// minGzipSize pass through uncompressed.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setVary(w.Header())
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gzw := &gzipResponseWriter{ResponseWriter: w}
		next.ServeHTTP(gzw, r)
		_ = gzw.finish()
	})
}

// setVary ensures Accept-Encoding is present in the Vary header without
// clobbering existing Vary values.
func setVary(h http.Header) {
	for _, v := range h.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}

// acceptsGzip returns true if the client's Accept-Encoding header permits gzip.
func acceptsGzip(r *http.Request) bool {
	ae := r.Header.Get("Accept-Encoding")
	if ae == "" {
		return false
	}
	for _, clause := range strings.Split(ae, ",") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		token, params, _ := strings.Cut(clause, ";")
		token = strings.TrimSpace(token)
		if !strings.EqualFold(token, "gzip") && token != "*" {
			continue
		}
		if params != "" {
			var disabled bool
			for _, param := range strings.Split(params, ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					val := strings.TrimSpace(strings.TrimPrefix(param, "q="))
					if q, err := strconv.ParseFloat(val, 64); err == nil && q <= 0 {
						disabled = true
						break
					}
				}
			}
			if disabled {
				continue
			}
		}
		return true
	}
	return false
}

// gzipResponseWriter delays writing headers until it knows whether the total
// response body exceeds minGzipSize. When the threshold is reached, it
// sets Content-Encoding: gzip, drops any uncompressed Content-Length, and
// streams compressed bytes through compress/gzip. Smaller bodies are sent
// uncompressed.
type gzipResponseWriter struct {
	http.ResponseWriter
	w           *gzip.Writer
	status      int
	wroteHeader bool
	compressing bool
	buf         bytes.Buffer
}

func isBodyAllowed(status int) bool {
	return status != http.StatusNoContent && status != http.StatusNotModified
}

// isStreamingContentType returns true for response types that must reach the
// client as the handler wrote them, un-buffered and uncompressed. Server-Sent
// Events are the current case: a client reads one event at a time, and
// compressing them would both delay delivery (gzip buffers until it has enough
// to compress) and strand bytes in the gzip writer until Close, which an open
// SSE stream never calls.
func isStreamingContentType(h http.Header) bool {
	ct := h.Get("Content-Type")
	if ct == "" {
		return false
	}
	mediaType, _, _ := strings.Cut(ct, ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "text/event-stream")
}

func (g *gzipResponseWriter) WriteHeader(statusCode int) {
	if g.wroteHeader || g.status != 0 {
		return
	}
	g.status = statusCode
	if g.Header().Get("Content-Encoding") != "" || !isBodyAllowed(statusCode) || isStreamingContentType(g.Header()) {
		g.ResponseWriter.WriteHeader(statusCode)
		g.wroteHeader = true
	}
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if g.wroteHeader && !g.compressing {
		return g.ResponseWriter.Write(p)
	}
	if g.status != 0 && !isBodyAllowed(g.status) {
		if !g.wroteHeader {
			g.ResponseWriter.WriteHeader(g.status)
			g.wroteHeader = true
		}
		return g.ResponseWriter.Write(p)
	}
	if g.Header().Get("Content-Encoding") != "" || isStreamingContentType(g.Header()) {
		if !g.wroteHeader {
			status := g.status
			if status == 0 {
				status = http.StatusOK
			}
			g.ResponseWriter.WriteHeader(status)
			g.wroteHeader = true
		}
		return g.ResponseWriter.Write(p)
	}
	if g.compressing {
		return g.w.Write(p)
	}
	if g.buf.Len()+len(p) >= minGzipSize {
		return g.startGzipAndWrite(p)
	}
	return g.buf.Write(p)
}

func (g *gzipResponseWriter) startGzipAndWrite(p []byte) (int, error) {
	g.compressing = true
	setVary(g.Header())
	if g.Header().Get("Content-Type") == "" {
		if g.buf.Len() > 0 {
			g.Header().Set("Content-Type", http.DetectContentType(g.buf.Bytes()))
		} else if len(p) > 0 {
			g.Header().Set("Content-Type", http.DetectContentType(p))
		}
	}
	g.Header().Del("Content-Length")
	g.Header().Set("Content-Encoding", "gzip")

	status := g.status
	if status == 0 {
		status = http.StatusOK
	}
	if !g.wroteHeader {
		g.ResponseWriter.WriteHeader(status)
		g.wroteHeader = true
	}

	g.w = gzip.NewWriter(g.ResponseWriter)
	if g.buf.Len() > 0 {
		if _, err := g.w.Write(g.buf.Bytes()); err != nil {
			return 0, err
		}
		g.buf.Reset()
	}
	if len(p) > 0 {
		return g.w.Write(p)
	}
	return 0, nil
}

func (g *gzipResponseWriter) finish() error {
	setVary(g.Header())
	if g.compressing {
		return g.w.Close()
	}
	status := g.status
	if status == 0 {
		status = http.StatusOK
	}
	if !g.wroteHeader {
		g.ResponseWriter.WriteHeader(status)
		g.wroteHeader = true
	}
	if g.buf.Len() > 0 {
		_, err := g.ResponseWriter.Write(g.buf.Bytes())
		return err
	}
	return nil
}

// Flush flushes buffered data or the active gzip writer, then forwards to
// the underlying ResponseWriter if it implements http.Flusher.
func (g *gzipResponseWriter) Flush() {
	if g.compressing {
		_ = g.w.Flush()
	} else if g.buf.Len() > 0 {
		_, _ = g.startGzipAndWrite(nil)
		if g.w != nil {
			_ = g.w.Flush()
		}
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return g.ResponseWriter
}
