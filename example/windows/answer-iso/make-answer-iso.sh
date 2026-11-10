#!/bin/sh
# Generates the answer-file ISO (autounattend.xml) to host, then pass as the builder's `cd_url`.
# Usage : PASSWORD=... BOOTSTRAP_URL=http://RUNNER_IP:8080 ./make-answer-iso.sh out.iso
# Variables: LOCALE (fr-FR), USERNAME (Administrateur), PASSWORD (required, throwaway: reuse it in winrm_password),
#   HOSTNAME (windows-build), TIMEZONE (Romance Standard Time), IMAGE_KEY (/IMAGE/NAME or /IMAGE/INDEX),
#   IMAGE_VALUE (Windows Server 2025 SERVERSTANDARD), PRODUCT_KEY (empty = no key),
#   BOOTSTRAP_URL (required: Packer's HTTP server, http://RUNNER_IP:8080 with the runner's private IP in
#   your subnet and the template's http_port_min).
# Requires xorriso and python3.
set -eu
out=${1:?usage: make-answer-iso.sh out.iso}
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
python3 - "$here/autounattend.xml.in" "$tmp/autounattend.xml" <<'PY'
import os, sys
from xml.sax.saxutils import escape
e = os.environ
def need(k):
    if not e.get(k): sys.exit(k + " is required")
    return e[k]
key = e.get("PRODUCT_KEY", "")
vals = {
    "LOCALE": e.get("LOCALE", "fr-FR"), "USERNAME": e.get("USERNAME", "Administrateur"), "PASSWORD": need("PASSWORD"),
    "HOSTNAME": e.get("HOSTNAME", "windows-build"), "TIMEZONE": e.get("TIMEZONE", "Romance Standard Time"),
    "IMAGE_KEY": e.get("IMAGE_KEY", "/IMAGE/NAME"), "IMAGE_VALUE": e.get("IMAGE_VALUE", "Windows Server 2025 SERVERSTANDARD"),
    "BOOTSTRAP_URL": need("BOOTSTRAP_URL"),
    "PRODUCT_KEY_XML": "<ProductKey><Key>%s</Key><WillShowUI>OnError</WillShowUI></ProductKey>" % escape(key) if key else "",
}
s = open(sys.argv[1]).read()
for k, v in vals.items():
    s = s.replace("@%s@" % k, v if k == "PRODUCT_KEY_XML" else escape(v))
open(sys.argv[2], "w").write(s)
PY
xorriso -as genisoimage -V ANSWERS -J -joliet-long -r -o "$out" "$tmp/autounattend.xml"
echo "wrote $out"
