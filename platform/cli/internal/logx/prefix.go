// Package logx prefixes log lines of parallel units ([hub], [region-<r>]).
package logx

import (
	"bytes"
	"io"
	"sync"
)

type prefixWriter struct {
	mu     sync.Mutex
	w      io.Writer
	prefix []byte
	buf    []byte
}

// Prefix returns a line-buffered writer that writes each complete line to w with prefix in front.
// Safe for concurrent use; a trailing partial line is held until its newline arrives.
func Prefix(w io.Writer, prefix string) io.Writer {
	return &prefixWriter{w: w, prefix: []byte(prefix)}
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		line := make([]byte, 0, len(p.prefix)+i+1)
		line = append(append(line, p.prefix...), p.buf[:i+1]...)
		p.buf = p.buf[i+1:]
		if _, err := p.w.Write(line); err != nil {
			return len(b), err
		}
	}
}
