package mcper

import (
	"bufio"
	"io"
	"regexp"
)

// scrubPatterns redacts cap tokens and bearer secrets from log lines.
// Defense-in-depth: PR 5 also stops logging raw stdio frames when caps
// are present, but the scrubber catches residual leaks via plugin stderr
// or error messages built from upstream response bodies.
var scrubPatterns = []*regexp.Regexp{
	// Headers: redact everything to end-of-line so "Authorization: Bearer X" loses X.
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*).*`),
	regexp.MustCompile(`(?i)(x-mcper-cap\s*[:=]\s*).*`),
	regexp.MustCompile(`(?i)("mcper_cap"\s*:\s*)"[^"]+"`),
	regexp.MustCompile(`(?i)("mcper_auth_token"\s*:\s*)"[^"]+"`),
	regexp.MustCompile(`(?i)(MCPER_CAP=)\S+`),
	regexp.MustCompile(`(?i)(MCPER_AUTH_TOKEN=)\S+`),
	// JWT detector: three base64url segments separated by dots. Redact ALL three.
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{4,}\.eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}`),
	// Standalone "Bearer X" anywhere (defense after header strip).
	regexp.MustCompile(`(?i)\bbearer\s+\S+`),
}

// ScrubBearerTokens replaces likely secrets with "<REDACTED>" sentinels.
// Used by RunModuleWithLogging when MCPER_USE_CAP_PROXY is on.
func ScrubBearerTokens(line string) string {
	// Two passes: first JWT/bearer (whole-token), then header-shaped patterns.
	// Header patterns need ${1} to keep the key visible; JWT/bearer redact entirely.
	for i, p := range scrubPatterns {
		switch i {
		case 0, 1, 2, 3, 4, 5:
			line = p.ReplaceAllString(line, "${1}<REDACTED>")
		default:
			line = p.ReplaceAllString(line, "<REDACTED>")
		}
	}
	return line
}

// ScrubWriter wraps an io.Writer with the scrubber applied line-by-line.
type ScrubWriter struct {
	inner io.Writer
	buf   *bufio.Scanner
	pipe  *io.PipeWriter
}

// NewScrubWriter returns a writer that scrubs each line written to it
// before forwarding to `inner`. Buffer up to 1 MiB per line.
func NewScrubWriter(inner io.Writer) io.WriteCloser {
	pr, pw := io.Pipe()
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	sw := &ScrubWriter{inner: inner, buf: scanner, pipe: pw}
	go func() {
		for sw.buf.Scan() {
			_, _ = inner.Write([]byte(ScrubBearerTokens(sw.buf.Text()) + "\n"))
		}
	}()
	return &writerCloser{Writer: pw, closer: pw}
}

type writerCloser struct {
	io.Writer
	closer io.Closer
}

func (wc *writerCloser) Close() error { return wc.closer.Close() }
