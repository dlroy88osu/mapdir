# MapDir

Zero dependencies, single file directory mapping. Prints a tree with a
file-type breakdown, or injects a linked version straight into your README.

Respects `.gitignore` (including nested ones) out of the box.

## Build it

```
go build -o mapdir.exe .
go install .
```

## Use it

```
mapdir.exe                          # map current dir
mapdir.exe C:\GitHub\GoLoadThis     # map target dir
mapdir.exe -i                       # drop a starter mapConfig.toml
mapdir.exe -c                       # adds counts table to see rows of code
mapdir.exe -r                       # inject into README.md
mapdir.exe -r="DOCS.md"             # inject into a different file
mapdir.exe -r C:\GitHub\GoLoadThis  # flags and path combine freely
mapdir.exe -h                       # usage
```

Long forms `--init`, `--counts `, `--readme`, `--readme=NAME`, and `--help` all work too.
> Note that `-init` writes the config and exits — it won't also map the directory in the same run.

## Actual Output (mapdir.exe -r -c)

<!-- mapDir: start -->
- mapdir/
- ├── <a href="./anotherDir/">anotherDir/</a>
- │&nbsp;&nbsp;&nbsp;└── <a href="./anotherDir/xyz.py">xyz.py</a>
- ├── <a href="./someDir/">someDir/</a>
- │&nbsp;&nbsp;&nbsp;└── <a href="./someDir/abc.py">abc.py</a>
- ├── <a href="./.gitignore">.gitignore</a>
- ├── <a href="./go.mod">go.mod</a>
- ├── <a href="./main.go">main.go</a>
- ├── <a href="./mapConfig.toml">mapConfig.toml</a>
- ├── <a href="./oddReadme.md">oddReadme.md</a>
- └── <a href="./README.md">README.md</a>

| File Type | File Count | Total Rows |
|:----------|-----------:|-----------:|
| (none)    |          1 |          3 |
| .go       |          1 |        662 |
| .md       |          2 |        156 |
| .mod      |          1 |          3 |
| .py       |          2 |          0 |
| .toml     |          1 |         37 |
| **TOTAL** |          8 |        861 |

<!-- mapDir: end -->

## Injecting into a README

Paste these two markers wherever you want the map to land. They're HTML
comments, so they stay invisible once rendered:

```
(remove '\\' for use, but cant demo without escaping (lol))
\\<!-- mapDir: start -->
\\<!-- mapDir: end -->
```

Every `-r` run replaces whatever sits between them. The markers themselves
stick around, and everything outside them is left alone — so it's safe to
run on every commit. Injected trees are rendered as relative Markdown links,
so each entry is clickable on GitHub.

If the markers are missing, MapDir tells you and exits without touching the
file.

## Filtering

Three layers stack, in this order:

1. `.git` and `.gitkeep` are always skipped. A directory holding a
   `.gitkeep` is kept in the tree even when it's otherwise empty.
2. `.gitignore` files, read per-directory as the walk descends.
3. `mapConfig.toml` in the mapped root, if present.

Later layers win, and within a layer the last matching rule wins — same as
git. That means a `!pattern` line can pull something back in that an earlier
rule dropped.

Empty directories are pruned from the output unless they hold a `.gitkeep`.

### mapConfig.toml

`-init` writes a commented starter file. Two keys, both arrays of strings:

```toml
# mapConfig.toml - stuff listed here is NOT shown or counted.
#
# usage:
#   mapdir [path]              		print the tree
#   mapdir -h / --help			    prints the help docs
#   mapdir -c / --counts			print the tree with a rows of code table
#   mapdir -i / --init [path]       write this file
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
```

Glob syntax matches `.gitignore`: a leading `/` anchors to the root, a
trailing `/` matches directories only, a leading `!` re-includes, and `**`
spans any number of path segments.
