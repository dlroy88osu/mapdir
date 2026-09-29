/*
GOBIN="$HOME/.local/bin" go install -trimpath -ldflags="-s -w" .
*/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

/***************************************************************************************************
 * [ Consts & help ]
 **************************************************************************************************/

const help = `
╔═══════════════════════════════════════════════════════════════════════════╗
║                                  MAPDIR                                   ║
╠═══════════════════════════════════════════════════════════════════════════╣
║                                                                           ║
║ Usage                                                                     ║
║   mapdir      Print directory map to the console                          ║
║   mapdir -h   Print help                                                  ║
║   mapdir -i   Write a config file in the current directory                ║
║   mapdir -p   Print only; do not inject into README ( or target file )    ║
║                                                                           ║
╠═══════════════════════════════════════════════════════════════════════════╣
║                                                                           ║
║ README injection                                                          ║
║   mapdir looks for README.md ( or target file ) and replaces content      ║
║   between these markers:                                                  ║
║                                                                           ║
║     <!-- mapDir: start -->                                                ║
║     <!-- mapDir: end -->                                                  ║
║                                                                           ║
║   Keep both markers in the target file. If either is missing, mapdir      ║
║   prints the result instead. Everything between them is replaced on       ║
║   every run. That is the warning.                                         ║
║                                                                           ║
╠═══════════════════════════════════════════════════════════════════════════╣
║                                                                           ║
║ Ignore rules                                                              ║
║   .gitignore is respected. Configured ignore rules are applied on top.    ║
║                                                                           ║
╚═══════════════════════════════════════════════════════════════════════════╝
`

const config = `{
    "insert_into": "README.md",
    "gitignore_style_globs_ignore": [
        "*.exe",
        "*.md",
        "*.gitignore",
        "*mapDir.json"
    ]
}
`
const cfgPath = "mapDir.json"
const red = "\x1b[38;5;203m"
const res = "\x1b[0m"
const ptr = "\x1b[38;5;75m\x1b[0m"

/***************************************************************************************************
 * [ Config ]
 **************************************************************************************************/
type Config struct {
	InsertInto string   `json:"insert_into"`
	Ignore     []string `json:"gitignore_style_globs_ignore"`
}

type rule struct {
	base     string
	segments []string
	negate   bool
	dirOnly  bool
	anchored bool
}

func loadConfig() (string, []rule) {
	cfg := Config{
		InsertInto: "README.md",
		Ignore:     []string{},
	}
	var rules []rule

	data, err := os.ReadFile(cfgPath)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg.InsertInto, rules
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
		os.Exit(1)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
		os.Exit(1)
	}

	for _, p := range cfg.Ignore {
		if r, ok := parseRule("", p); ok {
			rules = append(rules, r)
		}
	}

	fmt.Printf("%s Config loaded!", ptr)
	return cfg.InsertInto, rules
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

func walk(dir, rel string, rules []rule) *node {
	n := &node{name: filepath.Base(dir), isDir: true}
    entries, err := os.ReadDir(dir)
    if err != nil {
        fmt.Fprintf(os.Stderr, "%s Could not read %s: %s%s\n", red, dir, err, res)
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
	}

	for _, d := range dirs {
		if len(d.kids) > 0 || d.keep {
			n.kids = append(n.kids, d)
		}
	}
	n.kids = append(n.kids, files...)
	return n
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
 * [ Inject ]
 **************************************************************************************************/

// const (
// 	indBar   = "│&nbsp;&nbsp;&nbsp;"
// 	indBlank = "&nbsp;&nbsp;&nbsp;&nbsp;"
// )

func renderLinks(n *node, indent, dir string, out []string) []string {
	for _, k := range n.kids {
		rel := k.name
		if dir != "" {
			rel = dir + "/" + k.name
		}

		label := k.name
		if k.isDir {
			label += "/"
		}

		out = append(out, indent+"- "+mdLink(label, rel, k.isDir))

		if k.isDir {
			out = renderLinks(k, indent+"  ", rel, out)
		}
	}

	return out
}

func mdLink(label, rel string, isDir bool) string {
	target := "./" + rel
	if isDir {
		target += "/"
	}

	return `<a href="` + html.EscapeString(urlEscape(target)) + `">` +
		html.EscapeString(label) +
		`</a>`
}

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

func injection(root string, tree *node, rmName string) {
	p := filepath.Join(root, rmName)
	if _, err := os.Stat(p); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
		os.Exit(1)
	}

    out := []string{
        "- " + mdLink(filepath.Base(root)+"/", "", true),
    }
    out = renderLinks(tree, "  ", "", out)
    out = append(out, "")

	b, err := os.ReadFile(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
		os.Exit(1)
	}
	src := string(b)

	rmStart := "<!-- mapDir: start -->"
	rmEnd := "<!-- mapDir: end -->"

	i := strings.Index(src, rmStart)
	if i < 0 {
		fmt.Printf("%s`%s` marker not in %s%s\n", red, rmStart, rmName, res)
		return
	}

    if i > 0 && strings.TrimSpace(src[i-1:i]) != "" {
        fmt.Printf("%s Tag commented or impeded by `%s`; ignoring.\n", ptr, src[i-1:i])
        return
    }

	j := strings.Index(src[i:], rmEnd)
	if j < 0 {
		fmt.Fprintf(os.Stderr, "%s`%s` not found after start in %s%s\n", red, rmEnd, rmName, res)
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
		return
	}
	if err := os.WriteFile(p, []byte(next), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
		os.Exit(1)
	}

	fmt.Printf("%s %s has been updated!\n", ptr, rmName)
}

func printIt(root string, tree *node) {
	out := []string{filepath.Base(root) + "/"}
	render(tree, "", &out)
	out = append(out, "")
	fmt.Println()
	for _, l := range out {
		fmt.Println(l)
	}
}

/***************************************************************************************************
 * [ main ]
 **************************************************************************************************/

func main() {
	start := time.Now()
	printOnly := false

	args := os.Args[1:]
	if len(args) == 0 {
		printOnly = true
	}

	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			fmt.Print(help)
			os.Exit(0)

        case "-i", "--init":
            if _, err := os.Stat(cfgPath); err == nil {
                fmt.Printf("%s %s already exists, leaving it alone!\n", ptr, cfgPath)
                os.Exit(0)
            } else if !errors.Is(err, fs.ErrNotExist) {
                fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
                os.Exit(1)
            }

            if err := os.WriteFile(cfgPath, []byte(config), 0o644); err != nil {
                fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
                os.Exit(1)
            }

            fmt.Printf("%s Boom config! %s\n", ptr, cfgPath)
            os.Exit(0)

		case "-p", "--print":
			printOnly = true

		default:
			fmt.Fprintf(os.Stderr, "%s`%s` is an unknown argument, seek help!%s", red, arg, res)
			fmt.Print(help)
			os.Exit(1)
		}
	}

	root, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s%s\n", red, err, res)
		os.Exit(1)
	}

	tgt, cfg := loadConfig()
	tree := walk(root, "", cfg)

	if !printOnly {
		injection(root, tree, tgt)
	}

	printIt(root, tree)

	elapsed := time.Since(start)
	fmt.Printf("%s Walk finished in %s\n", ptr, elapsed)
}
