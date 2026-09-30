package pkgmgr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

var npmGroups = []string{"dependencies", "devDependencies"}

var npm = &Ecosystem{
	ID: "npm", File: "package.json", Groups: npmGroups,
	Parse: parseNPM, Apply: applyNPM,
}

// ordered is a JSON object that remembers key order so edits do not reshuffle
// the file.
type ordered struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseOrdered(b []byte) (*ordered, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("expected a JSON object")
	}
	o := &ordered{vals: map[string]json.RawMessage{}}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected content after the JSON object")
	}
	return o, nil
}

func (o *ordered) set(k string, raw json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = raw
}

func (o *ordered) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o *ordered) marshal(indent, prefix string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{")
	for i, k := range o.keys {
		if i > 0 {
			b.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		b.WriteString("\n" + prefix + indent)
		b.Write(kb)
		b.WriteString(": ")
		var v bytes.Buffer
		if err := json.Indent(&v, o.vals[k], prefix+indent, indent); err != nil {
			return nil, err
		}
		b.Write(v.Bytes())
	}
	if len(o.keys) > 0 {
		b.WriteString("\n" + prefix)
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

func indentOf(data []byte) string {
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(l) > 0 && (l[0] == '\t' || l[0] == ' ') {
			n := 0
			for n < len(l) && (l[n] == '\t' || l[n] == ' ') {
				n++
			}
			return string(l[:n])
		}
	}
	return "  "
}

func parseNPM(data []byte) ([]Dependency, error) {
	o, err := parseOrdered(data)
	if err != nil {
		return nil, &ParseError{"package.json", "not valid JSON: " + err.Error()}
	}
	var out []Dependency
	for _, g := range npmGroups {
		raw, ok := o.vals[g]
		if !ok {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, &ParseError{"package.json", g + " must be an object"}
		}
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			s, isStr := m[n].(string)
			out = append(out, Dependency{Name: n, Spec: s, Group: g, Editable: isStr})
		}
	}
	return out, nil
}

func applyNPM(data []byte, ops []Op) ([]byte, error) {
	if err := checkOps("npm", ops, npmGroups, npmName, npmSpec, false); err != nil {
		return nil, err
	}
	o, err := parseOrdered(data)
	if err != nil {
		return nil, &ParseError{"package.json", "not valid JSON: " + err.Error()}
	}
	groups := map[string]map[string]string{}
	load := func(g string) (map[string]string, error) {
		if m, ok := groups[g]; ok {
			return m, nil
		}
		m := map[string]string{}
		if raw, ok := o.vals[g]; ok {
			var any map[string]json.RawMessage
			if err := json.Unmarshal(raw, &any); err != nil {
				return nil, &ParseError{"package.json", g + " must be an object"}
			}
			for k, v := range any {
				var s string
				if json.Unmarshal(v, &s) != nil {
					continue // non-string entries are preserved below
				}
				m[k] = s
			}
		}
		groups[g] = m
		return m, nil
	}
	find := func(name string) string {
		for _, g := range npmGroups {
			m, _ := load(g)
			if _, ok := m[name]; ok {
				return g
			}
		}
		return ""
	}
	for _, op := range ops {
		cur := find(op.Name)
		switch op.Action {
		case "add":
			if cur != "" {
				return nil, fmt.Errorf("%s is already a dependency", op.Name)
			}
			g := op.Group
			if g == "" {
				g = "dependencies"
			}
			m, err := load(g)
			if err != nil {
				return nil, err
			}
			m[op.Name] = op.Spec
		case "update":
			if cur == "" {
				return nil, fmt.Errorf("%s is not a dependency", op.Name)
			}
			m, _ := load(cur)
			m[op.Name] = op.Spec
		case "remove":
			if cur == "" {
				return nil, fmt.Errorf("%s is not a dependency", op.Name)
			}
			m, _ := load(cur)
			delete(m, op.Name)
		}
	}
	for g, m := range groups {
		if len(m) == 0 && o.vals[g] == nil {
			continue
		}
		// Preserve non-string entries; rewrite string entries sorted (npm does too).
		var existing map[string]json.RawMessage
		if raw, ok := o.vals[g]; ok {
			_ = json.Unmarshal(raw, &existing)
		}
		merged := map[string]json.RawMessage{}
		for k, v := range existing {
			var s string
			if json.Unmarshal(v, &s) != nil {
				merged[k] = v
			}
		}
		for k, v := range m {
			b, _ := json.Marshal(v)
			merged[k] = b
		}
		if len(merged) == 0 {
			o.del(g)
			continue
		}
		names := make([]string, 0, len(merged))
		for k := range merged {
			names = append(names, k)
		}
		sort.Strings(names)
		sub := &ordered{keys: names, vals: merged}
		b, err := sub.marshal("", "")
		if err != nil {
			return nil, err
		}
		o.set(g, b)
	}
	ind := indentOf(data)
	out, err := o.marshal(ind, "")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	if !strings.HasPrefix(string(out), "{") {
		return nil, errors.New("internal: bad output")
	}
	return out, nil
}
