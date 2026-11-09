#!/usr/bin/env bash
# Usage: compile-to-webdocs.sh <repo> <rendered-docs> <web-docs> <namespace>
# Converts the rendered docs (renderdocs) into the .web-docs/ structure read by the Packer integrations library.
set -euo pipefail
src="$2"; dst="$3"

rm -rf "$dst/components"
mkdir -p "$dst/components"
cp "$src/README.md" "$dst/README.md"

for kind in builders post-processors; do
  type="${kind%s}" # builder | post-processor
  for f in "$src/$kind"/*.mdx; do
    [ -e "$f" ] || continue
    name="$(basename "$f" .mdx)"
    mkdir -p "$dst/components/$type/$name"
    # strip the YAML front matter; the library rebuilds it from metadata.hcl
    awk 'NR==1 && /^---$/ {fm=1; next} fm && /^---$/ {fm=0; next} !fm' "$f" >"$dst/components/$type/$name/README.md"
  done
done
