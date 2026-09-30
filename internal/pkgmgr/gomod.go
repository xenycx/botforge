package pkgmgr

import (
	"fmt"

	"golang.org/x/mod/modfile"
)

var goGroups = []string{"direct", "indirect"}

var gomod = &Ecosystem{ID: "gomod", File: "go.mod", Groups: goGroups, Parse: parseGoMod, Apply: applyGoMod}

func parseGoMod(data []byte) ([]Dependency, error) {
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, &ParseError{"go.mod", err.Error()}
	}
	var out []Dependency
	for _, r := range f.Require {
		g := "direct"
		if r.Indirect {
			g = "indirect"
		}
		out = append(out, Dependency{Name: r.Mod.Path, Spec: r.Mod.Version, Group: g, Editable: true})
	}
	return out, nil
}

func applyGoMod(data []byte, ops []Op) ([]byte, error) {
	if err := checkOps("gomod", ops, goGroups, goPath, goVersion, false); err != nil {
		return nil, err
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, &ParseError{"go.mod", err.Error()}
	}
	have := map[string]bool{}
	for _, r := range f.Require {
		have[r.Mod.Path] = true
	}
	for _, op := range ops {
		switch op.Action {
		case "add":
			if have[op.Name] {
				return nil, fmt.Errorf("%s is already required", op.Name)
			}
			if err := f.AddRequire(op.Name, op.Spec); err != nil {
				return nil, err
			}
			have[op.Name] = true
		case "update":
			if !have[op.Name] {
				return nil, fmt.Errorf("%s is not required", op.Name)
			}
			if err := f.AddRequire(op.Name, op.Spec); err != nil { // sets the version of an existing requirement
				return nil, err
			}
		case "remove":
			if !have[op.Name] {
				return nil, fmt.Errorf("%s is not required", op.Name)
			}
			if err := f.DropRequire(op.Name); err != nil {
				return nil, err
			}
			delete(have, op.Name)
		}
	}
	f.Cleanup()
	return f.Format()
}
