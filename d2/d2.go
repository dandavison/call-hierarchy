package d2

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"call-hierarchy/gopls"
)

type Edge struct {
	From gopls.CallSite
	To   gopls.Function
}

type Graph struct {
	Edges []Edge
}

const CRLF = "&#013;&#010;"

func (g Graph) Write(linkPrefix string) {
	fns := map[gopls.Function]string{}
	for _, e := range g.Edges {
		fns[e.From.Function] = ""
		fns[e.To] = ""
	}
	fmt.Println()
	for _, e := range g.Edges {
		fmt.Printf(
			"%s.%s -> %s.%s # %s\n",
			relDir(e.From.Function.Location.File),
			e.From.Function.Name,
			relDir(e.To.Location.File),
			e.To.Name,
			e.From.Locations[0].String(),
		)
		fns[e.From.Function] += fmt.Sprintf("=> %s: %s%s", e.To.Name, e.From.Locations[0].String(), CRLF)
	}
	writeFileGroup(fns, linkPrefix)
}

func writeFileGroup(fns map[gopls.Function]string, linkPrefix string) {
	for f, s := range fns {
		if len(s) == 0 {
			s = "[leaf]"
		}
		fmt.Printf(`
%s.%s: |md
	[`+"`%s`"+`](%s/%s?line=%d "%s")
|`, relDir(f.Location.File), f.Name, f.Name, linkPrefix, relPath(f.Location.File), f.Location.Line, s)
	}
}

func relDir(path string) string {
	dir := filepath.Dir(relPath(path))
	if dir == "." {
		dir = "root"
	}
	return strings.ReplaceAll(dir, "/", "\\.")
}

func relPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		return path
	}
	return rel
}
