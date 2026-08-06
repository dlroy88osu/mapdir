package main

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const defaultConfig = `# mapConfig.toml - stuff listed here is NOT shown or counted.
#
# usage:
#   mapdir [path]              		print the tree
#   mapdir -c / --counts			print the tree with a rows of code table
#   mapdir -i [path]        		write this file
#   mapdir -r / --readme [path]     inject into README.md
#   mapdir -r / --readme="DOCS.md" 	[path] inject into a different file
#
# -r rewrites whatever sits between these two markers, so drop them
# into the target file first (they're HTML comments, invisible when
# rendered):
#
#   <!-- mapDir: start -->
#   <!-- mapDir: end -->
#
# Everything between them is replaced on every run; the markers
# themselves stay put. Anything outside them is left alone.
#
# .gitignore is read automatically (including nested ones) - the rules
# below stack on top of it.

# gitignore-style globs, relative to the mapped root.
# leading "/" anchors to root, trailing "/" matches dirs only,
# leading "!" re-includes something an earlier rule dropped.
ignore = [
    "*.png",
    "*.svg",
    "*.ico",
    "*.icns",
]

# bare extensions, dot optional. shorthand for "*.ext" in ignore.
ignore_exts = [
	".env",
	"lock",
	"exe",
]
`

const rmStart = "<!-- mapDir: start -->"
const rmEnd = "<!-- mapDir: end -->"

/***************************************************************************************************
* [ Parse mapConfig ]
**************************************************************************************************/
type rule struct {
	base     string
	segments []string
	negate   bool
	dirOnly  bool
	anchored bool
}

func loadConfig(root string) []rule {
	b, err := os.ReadFile(filepath.Join(root, "mapConfig.toml"))
	if err != nil {
		return nil
	}
	arrays := map[string][]string{}
	key, inArr := "", false
	for _, raw := range strings.Split(string(b), "\n") {
		ln := stripComment(raw)
		if !inArr {
			i := strings.Index(ln, "=")
			if i < 0 {
				continue
			}
			k := strings.TrimSpace(ln[:i])
			v := strings.TrimSpace(ln[i+1:])
			if !strings.HasPrefix(v, "[") {
				continue
			}
			key, inArr = k, true
			ln = v[1:]
		}
		if j := strings.Index(ln, "]"); j >= 0 {
			arrays[key] = append(arrays[key], grabStrings(ln[:j])...)
			inArr = false
			continue
		}
		arrays[key] = append(arrays[key], grabStrings(ln)...)
	}

	var out []rule
	for _, p := range arrays["ignore"] {
		if r, ok := parseRule("", p); ok {
			out = append(out, r)
		}
	}
	for _, e := range arrays["ignore_exts"] {
		e = strings.TrimPrefix(strings.TrimSpace(e), ".")
		if e == "" {
			continue
		}
		if r, ok := parseRule("", "*."+e); ok {
			out = append(out, r)
		}
	}
	return out
}

func stripComment(s string) string {
	q := false
	for i, c := range s {
		switch c {
		case '"':
			q = !q
		case '#':
			if !q {
				return s[:i]
			}
		}
	}
	return s
}

func grabStrings(s string) []string {
	var out []string
	for {
		i := strings.IndexAny(s, `"'`)
		if i < 0 {
			return out
		}
		q := s[i]
		j := strings.IndexByte(s[i+1:], q)
		if j < 0 {
			return out
		}
		out = append(out, s[i+1:i+1+j])
		s = s[i+2+j:]
	}
}

func parseRule(base, line string) (rule, bool) {
	p := strings.TrimSpace(line)
	if p == "" || strings.HasPrefix(p, "#") {
		return rule{}, false
	}
	r := rule{base: base}
	if strings.HasPrefix(p, "!") {
		r.negate = true
		p = p[1:]
	}
	if strings.HasSuffix(p, "/") {
		r.dirOnly = true
		p = strings.TrimSuffix(p, "/")
	}
	if strings.HasPrefix(p, "/") {
		r.anchored = true
		p = strings.TrimPrefix(p, "/")
	} else if strings.Contains(p, "/") {
		r.anchored = true
	}
	if strings.HasPrefix(p, "**/") {
		r.anchored = false
		p = strings.TrimPrefix(p, "**/")
	}
	if p == "" {
		return rule{}, false
	}
	r.segments = strings.Split(p, "/")
	return r, true
}

/***************************************************************************************************
 * [ Take a walk ]
 **************************************************************************************************/
type node struct {
	name  string
	isDir bool
	kids  []*node
	keep  bool // dir holds a .gitkeep
}

type stat struct{ count, rows int }

var stats = map[string]*stat{}

func countLines(p string) int {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}

func walk(dir, rel string, rules []rule) *node {
	n := &node{name: filepath.Base(dir), isDir: true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return n
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Name()), strings.ToLower(entries[j].Name())
		if a != b {
			return a < b
		}
		return entries[i].Name() < entries[j].Name()
	})

	if local := loadGitignore(dir, rel); local != nil {
		rules = append(append([]rule{}, rules...), local...)
	}

	var dirs, files []*node
	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		if name == ".gitkeep" {
			n.keep = true
			continue
		}
		child := name
		if rel != "" {
			child = rel + "/" + name
		}
		if ignored(rules, child, e.IsDir()) {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, walk(filepath.Join(dir, name), child, rules))
			continue
		}
		files = append(files, &node{name: name})

		ext := strings.ToLower(filepath.Ext(name))
		if ext == "" || ext == name { // "" or a dotfile like .gitignore
			ext = "(none)"
		}
		s := stats[ext]
		if s == nil {
			s = &stat{}
			stats[ext] = s
		}
		s.count++
		s.rows += countLines(filepath.Join(dir, name))
	}

	for _, d := range dirs {
		if len(d.kids) > 0 || d.keep {
			n.kids = append(n.kids, d)
		}
	}
	n.kids = append(n.kids, files...)
	return n
}

func table(md bool) []string {
	if len(stats) == 0 {
		return []string{"No tracked files found.", ""}
	}
	hExt, hCnt, hRow := "File Type", "File Count", "Total Rows"
	wExt, wCnt, wRow := len(hExt), len(hCnt), len(hRow)
	for ext, s := range stats {
		wExt = max(wExt, len(ext)+2)
		wCnt = max(wCnt, len(fmt.Sprint(s.count)))
		wRow = max(wRow, len(fmt.Sprint(s.rows)))
	}

	keys := make([]string, 0, len(stats))
	totC, totR := 0, 0
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		totC, totR = totC+stats[k].count, totR+stats[k].rows
	}

	if md {
		sep := "|:" + strings.Repeat("-", wExt+1) + "|" + strings.Repeat("-", wCnt+1) + ":|" + strings.Repeat("-", wRow+1) + ":|"
		rows := []string{
			fmt.Sprintf("| %-*s | %*s | %*s |", wExt, hExt, wCnt, hCnt, wRow, hRow), sep}
		for _, k := range keys {
			s := stats[k]
			rows = append(rows, fmt.Sprintf("| %-*s | %*d | %*d |", wExt, k, wCnt, s.count, wRow, s.rows))
		}
		return append(rows,
			fmt.Sprintf("| %-*s | %*d | %*d |", wExt, "**TOTAL**", wCnt, totC, wRow, totR), "")
	}

	// box-drawing rule: left, mid, right junction chars
	bar := func(l, m, r string) string {
		return l + strings.Repeat("─", wExt+2) + m + strings.Repeat("─", wCnt+2) +
			m + strings.Repeat("─", wRow+2) + r
	}
	line := func(a string, b, c any) string {
		return fmt.Sprintf("│ %-*s │ %*v │ %*v │", wExt, a, wCnt, b, wRow, c)
	}

	rows := []string{bar("┌", "┬", "┐"), line(hExt, hCnt, hRow), bar("├", "┼", "┤")}
	for _, k := range keys {
		s := stats[k]
		rows = append(rows, line(k, s.count, s.rows))
	}
	return append(rows, bar("├", "┼", "┤"), line("TOTAL", totC, totR), bar("└", "┴", "┘"), "")
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

func (r rule) match(rel string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	if r.base != "" {
		if !strings.HasPrefix(rel, r.base+"/") {
			return false
		}
		rel = rel[len(r.base)+1:]
	}
	segments := strings.Split(rel, "/")
	if r.anchored {
		return matchSegments(r.segments, segments)
	}
	for i := range segments {
		if matchSegments(r.segments, segments[i:]) {
			return true
		}
	}
	return false
}

// last matching rule wins, same as git
func ignored(rules []rule, rel string, isDir bool) bool {
	out := false
	for _, r := range rules {
		if r.match(rel, isDir) {
			out = !r.negate
		}
	}
	return out
}

func loadGitignore(dir, base string) []rule {
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	var out []rule
	for _, ln := range strings.Split(string(b), "\n") {
		if r, ok := parseRule(base, strings.TrimRight(ln, "\r")); ok {
			out = append(out, r)
		}
	}
	return out
}

/***************************************************************************************************
 * [ Print Results ]
 **************************************************************************************************/
func render(n *node, prefix string, out *[]string) {
	for i, k := range n.kids {
		last := i == len(n.kids)-1
		conn := "├── "
		if last {
			conn = "└── "
		}
		name := k.name
		if k.isDir {
			name += "/"
		}
		*out = append(*out, prefix+conn+name)
		if k.isDir {
			next := prefix + "│   "
			if last {
				next = prefix + "    "
			}
			render(k, next, out)
		}
	}
}

/***************************************************************************************************
 * [ Inject ]
 **************************************************************************************************/
const (
	indBar   = "│&nbsp;&nbsp;&nbsp;"
	indBlank = "&nbsp;&nbsp;&nbsp;&nbsp;"
)

func renderLinks(n *node, prefix, dir string, out *[]string) {
	for i, k := range n.kids {
		last := i == len(n.kids)-1
		conn := "├── "
		if last {
			conn = "└── "
		}

		rel := k.name
		if dir != "" {
			rel = dir + "/" + k.name
		}

		label := k.name
		if k.isDir {
			label += "/"
		}

		*out = append(*out, "- "+prefix+conn+mdLink(label, rel, k.isDir))

		if k.isDir {
			next := prefix + indBar
			if last {
				next = prefix + indBlank
			}
			renderLinks(k, next, rel, out)
		}
	}
}

func mdLink(label, rel string, isDir bool) string {
	target := "./" + rel
	if isDir {
		target += "/"
	}
	return `<a href="` + htmlEscape(urlEscape(target)) + `">` + htmlEscape(label) + `</a>`
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
	).Replace(s)
}

// urlEscape percent-encodes per path segment, leaving separators intact.
func urlEscape(s string) string {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		parts[i] = (&url.URL{Path: p}).EscapedPath()
	}
	return strings.Join(parts, "/")
}

/***************************************************************************************************
 * [ Add to readme ]
 **************************************************************************************************/
func injection(root string, tree *node, readmeName string, withCounts bool) {
	p := filepath.Join(root, readmeName)
	if _, err := os.Stat(p); err != nil {
		fmt.Printf("    > unable to locate '%s': %v\n", readmeName, err)
		os.Exit(1)
	}

	out := []string{"- " + filepath.Base(root) + "/"}
	renderLinks(tree, "", "", &out)
	out = append(out, "")
	if withCounts {
		out = append(out, table(true)...)
	}

	b, err := os.ReadFile(p)
	if err != nil {
		fmt.Printf("    > Unable to read '%s': %v\n", readmeName, err)
		os.Exit(1)
	}
	src := string(b)

	i := strings.Index(src, rmStart)
	if i < 0 {
		fmt.Printf("    > No '%s' marker in '%s'\n", rmStart, readmeName)
		os.Exit(1)
	}
	j := strings.Index(src[i:], rmEnd)
	if j < 0 {
		fmt.Printf("    > No '%s' marker after start in '%s'\n", rmEnd, readmeName)
		os.Exit(1)
	}
	j += i

	nl := "\n"
	if strings.Contains(src, "\r\n") {
		nl = "\r\n"
	}

	body := strings.Join(out, nl)
	next := src[:i+len(rmStart)] + nl + body + nl + src[j:]

	if next == src {
		fmt.Println("    > Already up to date")
		return
	}
	if err := os.WriteFile(p, []byte(next), 0644); err != nil {
		fmt.Printf("    > Unable to write '%s': %v\n", readmeName, err)
		os.Exit(1)
	}
	fmt.Printf("    > Injected %d lines into '%s'\n", len(out), readmeName)
}

/***************************************************************************************************
 * [ init ]
 **************************************************************************************************/
func set_root(target string) string {
	if target == "" {
		target = "."
	}

	root, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "not a directory: %s\n", root)
		os.Exit(1)
	}

	return root
}

func init_config(root string, initCfg bool) {
	if !initCfg {
		return
	}

	p := filepath.Join(root, "mapConfig.toml")
	if _, err := os.Stat(p); err == nil {
		fmt.Println("    > mapConfig.toml already exists, leaving it alone")
	} else if err := os.WriteFile(p, []byte(defaultConfig), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	} else {
		fmt.Println("    > Wrote", p)
	}

	os.Exit(0)
}

func usage() {
	fmt.Print(`mapdir - directory tree mapper

USAGE
  mapdir [flags] [path]

  path defaults to the current directory.

FLAGS
  -c, --counts           append a file-type / line-count table
  -r, --readme           inject the tree into README.md
  -r=NAME, --readme=NAME inject into NAME instead
  -init, --init          write a starter mapConfig.toml, then exit
  -h, --help             this text

EXAMPLES
  mapdir                      print tree for the current dir
  mapdir C:\GitHub\proj       print tree for another dir
  mapdir -c                   tree plus the counts table
  mapdir -r -c                inject tree + table into README.md
  mapdir -r=DOCS.md C:\proj   inject into proj\DOCS.md

INJECTING
  -r replaces whatever sits between these two markers. Put them in the
  target file first; they're HTML comments, so they stay invisible once
  rendered:

    <!-- mapDir: start -->
    <!-- mapDir: end -->

  Everything outside the markers is left alone, so it's safe to re-run.
  Injected entries are relative links, clickable on GitHub. If the
  markers are missing, mapdir says so and exits without writing.

FILTERING
  Three layers stack, later wins, and within a layer the last matching
  rule wins (same as git):

    1. .git and .gitkeep are always skipped. A dir holding a .gitkeep
       stays in the tree even when empty; other empty dirs are pruned.
    2. .gitignore files, read per-directory as the walk descends.
    3. mapConfig.toml in the mapped root, if present.

  See the comments in the generated mapConfig.toml for the two keys it
  understands.
`)
}

func main() {
	args := os.Args[1:]
	initCfg := false
	target := "."
	injectReadme := false
	readmeName := "README.md"
	withCounts := false

	for _, a := range args {
		switch {
		case a == "-i" || a == "--init":
			initCfg = true
		case a == "-c" || a == "--counts":
			withCounts = true
		case a == "-h" || a == "--help":
			usage()
			return
		case strings.HasPrefix(a, "-r=") || strings.HasPrefix(a, "--readme="):
			injectReadme = true
			if v := a[strings.Index(a, "=")+1:]; v != "" {
				readmeName = v
			} else {
				fmt.Println("    > Cannot have a null assignment, pass a name or drop the '='")
			}
		case a == "-r" || a == "--readme":
			injectReadme = true
		default:
			target = a
		}
	}

	root := set_root(target)
	init_config(root, initCfg)

	rules := loadConfig(root)
	tree := walk(root, "", rules)

	if injectReadme {
		injection(root, tree, readmeName, withCounts)
	} else {
		out := []string{filepath.Base(root) + "/"}
		render(tree, "", &out)
		out = append(out, "")
		if withCounts {
			out = append(out, table(false)...)
		}

		fmt.Println()
		for _, l := range out {
			fmt.Println(l)
		}
	}
}
