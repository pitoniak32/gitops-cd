package system

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Source says where a merged value comes from: File:Line of the winning key, or a Note for
// values no file sets (defaults, forced values).
type Source struct {
	File string
	Line int
	Note string
}

func (s Source) String() string {
	if s.File == "" {
		return s.Note
	}
	return fmt.Sprintf("%s:%d", s.File, s.Line)
}

func keySource(file string, k *yaml.Node) Source {
	if k == nil {
		return Source{File: file}
	}
	return Source{File: file, Line: k.Line}
}

// src of a setting in this layer: its key's file:line, or the layer's note when no file backs it
func (l *layer) src(path ...string) Source {
	if l.file == "" {
		return Source{Note: l.note}
	}
	k, _ := getKV(l.node, path...)
	return keySource(l.file, k)
}
