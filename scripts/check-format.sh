#!/bin/sh
set -eu

for tool in rg gofmt xargs mktemp rm; do
    command -v "$tool" >/dev/null 2>&1 || {
        printf 'Formatting requires %s\n' "$tool" >&2
        exit 1
    }
done

# Finish discovery before formatting; neither failure may look like an empty
# successful result. NUL delimiters preserve paths containing whitespace.
file_list=$(mktemp "${TMPDIR:-/tmp}/bridge-fmt.XXXXXX")
trap 'rm -f -- "$file_list"' 0
trap 'exit 1' HUP INT TERM
rg --files -0 -g '*.go' >"$file_list"
[ -s "$file_list" ] || { printf 'No Go source files found\n' >&2; exit 1; }
unformatted=$(xargs -0 gofmt -l <"$file_list")
[ -z "$unformatted" ] || {
    printf 'Run gofmt on:\n%s\n' "$unformatted" >&2
    exit 1
}
