# MapDir

MapDir is a zero-dependency Go CLI that maps the current directory. It prints a
plain terminal tree and, injects a hyperlinked file map into a target Markdown file.

It respects `.gitignore`, including nested `.gitignore` files. Additional
ignore rules can be supplied in `mapDir.json`.

## Build

```powershell
go build -ldflags="-s -w" -o mapdir.exe .
go install .
```

## Usage

```text
mapdir              Inject into README.md, then print the terminal tree
mapdir -p           Print only; do not inject into a Markdown file
mapdir -i           Create mapDir.json in the current directory
mapdir -h           Print help
```

Long forms work too: `--print`, `--init`, and `--help`.

`-i` creates the starter config and exits. It respects existing `mapDir.json`.

## Markdown injection

By default, MapDir targets `README.md`. Set `insert_into` in the config to target a different file.

Place both markers in the target Markdown file:

```html
<!-- mapDir: start -->
<!-- mapDir: end -->
```

If the start is absent, MapDir leaves the file alone but still prints the tree. If
the end marker is missing after a start marker, it explodes throws error.

## Config

Run `mapdir -i` to create `mapDir.json` or paste the following: (All keys are optional)

```json
{
  "insert_into": "README.md",
  "gitignore_style_globs_ignore": [
    "*.exe",
    "*.md",
    "*.gitignore",
    "*mapDir.json"
  ]
}
```

Configured ignore rules are applied after `.gitignore` rules. Within the
combined rule list, the last matching rule wins; prefix a rule with `!` to
re-include a matching path.

Supported rule behavior:

- A leading `/` anchors the pattern to its config or `.gitignore` directory.
- A trailing `/` matches directories only.
- `**/` can match at any directory depth.
- Standard glob matching such as `*.exe` is supported.

`.git` is always skipped. `.gitkeep` is not rendered, but keeps an otherwise
empty directory visible in the map.

## Example injected output

<!-- mapDir: start -->
- <a href=".//">mapdir/</a>
  - <a href="./anotherDir/">anotherDir/</a>
    - <a href="./anotherDir/main%20copy.go">main copy.go</a>
    - <a href="./anotherDir/xyz.py">xyz.py</a>
  - <a href="./someDir/">someDir/</a>
    - <a href="./someDir/abc.py">abc.py</a>
  - <a href="./.gitignore">.gitignore</a>
  - <a href="./go.mod">go.mod</a>
  - <a href="./LICENSE">LICENSE</a>
  - <a href="./main.go">main.go</a>
  - <a href="./README.md">README.md</a>

<!-- mapDir: end -->
