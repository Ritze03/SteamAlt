// Package steam reads the Steam install on disk: VDF files, the Steam root,
// accounts, per-account game folders and installed-game names. Stdlib only.
package steam

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Node is one KeyValues entry. A block ("key" { ... }) has Children != nil
// (possibly empty) and no Value; a plain pair has Value and nil Children.
// The synthetic root returned by ParseVDF has an empty Key and holds the
// file's top-level entries as Children.
type Node struct {
	Key      string
	Value    string
	Children []*Node
}

// IsBlock reports whether n is a { ... } block rather than a string pair.
func (n *Node) IsBlock() bool { return n != nil && n.Children != nil }

// Get returns the first direct child whose key equals key, ignoring case
// (Steam treats keys case-insensitively). Nil-safe: returns nil if absent.
func (n *Node) Get(key string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if strings.EqualFold(c.Key, key) {
			return c
		}
	}
	return nil
}

// Find follows path through nested children with Get. Nil-safe.
func (n *Node) Find(path ...string) *Node {
	for _, k := range path {
		n = n.Get(k)
	}
	return n
}

// Str returns the string value at path, or "" if the path is missing or is a block.
func (n *Node) Str(path ...string) string {
	if t := n.Find(path...); t != nil && !t.IsBlock() {
		return t.Value
	}
	return ""
}

const maxVDFDepth = 100

// ParseVDF parses KeyValues text: quoted or bare keys and values, nested
// {} blocks, backslash escapes inside quotes, and // line comments. A leading
// UTF-8 BOM is skipped. Input that is not valid UTF-8 or is malformed
// returns an error (never panics).
func ParseVDF(data []byte) (*Node, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return nil, errors.New("vdf: not valid UTF-8")
	}
	p := &vdfParser{s: string(data)}
	kids, err := p.block(0)
	if err != nil {
		return nil, err
	}
	return &Node{Children: kids}, nil
}

// readVDF reads and parses a VDF file.
func readVDF(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := ParseVDF(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

type vdfTok int

const (
	tokEOF vdfTok = iota
	tokStr
	tokOpen
	tokClose
)

type vdfParser struct {
	s string
	i int
}

func (p *vdfParser) errf(format string, a ...any) error {
	line := 1 + strings.Count(p.s[:min(p.i, len(p.s))], "\n")
	return fmt.Errorf("vdf: line %d: %s", line, fmt.Sprintf(format, a...))
}

func (p *vdfParser) next() (vdfTok, string, error) {
	// Skip whitespace and // comments.
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c <= ' ' {
			p.i++
		} else if c == '/' && p.i+1 < len(p.s) && p.s[p.i+1] == '/' {
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		} else {
			break
		}
	}
	if p.i >= len(p.s) {
		return tokEOF, "", nil
	}
	switch c := p.s[p.i]; c {
	case '{':
		p.i++
		return tokOpen, "", nil
	case '}':
		p.i++
		return tokClose, "", nil
	case '"':
		p.i++
		var b strings.Builder
		for p.i < len(p.s) {
			c := p.s[p.i]
			switch {
			case c == '"':
				p.i++
				return tokStr, b.String(), nil
			case c == '\\' && p.i+1 < len(p.s):
				switch e := p.s[p.i+1]; e {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case '\\', '"':
					b.WriteByte(e)
				default: // unknown escape: keep it verbatim
					b.WriteByte('\\')
					b.WriteByte(e)
				}
				p.i += 2
			default:
				b.WriteByte(c)
				p.i++
			}
		}
		return 0, "", p.errf("unterminated string")
	}
	// Bare token: runs until whitespace, a brace or a quote.
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c <= ' ' || c == '{' || c == '}' || c == '"' {
			break
		}
		p.i++
	}
	return tokStr, p.s[start:p.i], nil
}

// block parses entries until the matching '}' (depth > 0) or EOF (depth 0).
func (p *vdfParser) block(depth int) ([]*Node, error) {
	if depth > maxVDFDepth {
		return nil, p.errf("nesting too deep")
	}
	nodes := []*Node{}
	for {
		k, key, err := p.next()
		if err != nil {
			return nil, err
		}
		switch k {
		case tokEOF:
			if depth > 0 {
				return nil, p.errf("unexpected end of file, missing '}'")
			}
			return nodes, nil
		case tokClose:
			if depth == 0 {
				return nil, p.errf("unexpected '}'")
			}
			return nodes, nil
		case tokOpen:
			return nil, p.errf("unexpected '{'")
		}
		k, val, err := p.next()
		if err != nil {
			return nil, err
		}
		switch k {
		case tokStr:
			nodes = append(nodes, &Node{Key: key, Value: val})
		case tokOpen:
			kids, err := p.block(depth + 1)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, &Node{Key: key, Children: kids})
		default:
			return nil, p.errf("key %q has no value", key)
		}
	}
}
