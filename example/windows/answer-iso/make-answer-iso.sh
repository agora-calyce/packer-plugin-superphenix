#!/bin/sh
# Generates the answer-file ISO (autounattend.xml) to host, then pass as the builder's `cd_url`.
# Usage : ./make-answer-iso.sh out.iso   (every variable below is optional)
# The ISO does not depend on the runner: the WinRM bootstrap (windows-initialisation.ps1) is on the ISO itself.
# Variables: LOCALE (fr-FR), WIN_USERNAME (packer), PASSWORD (Packer-build-1: a throwaway build password, not a secret;
#   the plugin's winrm_final_username/winrm_final_password replace the account at the end of the build),
#   WIN_HOSTNAME (windows-build), TIMEZONE (Romance Standard Time), IMAGE_KEY (/IMAGE/NAME or /IMAGE/INDEX),
#   IMAGE_VALUE (Windows Server 2025 SERVERSTANDARD), PRODUCT_KEY (empty = no key).
# Requires xorriso and python3.
set -eu
out=${1:?usage: make-answer-iso.sh out.iso}
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
python3 - "$here/autounattend.xml.in" "$tmp/autounattend.xml" <<'PY'
import os, sys
from xml.sax.saxutils import escape
e = os.environ
key = e.get("PRODUCT_KEY", "")
vals = {
    "LOCALE": e.get("LOCALE", "fr-FR"), "USERNAME": e.get("WIN_USERNAME", "packer"), "PASSWORD": e.get("PASSWORD", "Packer-build-1"),
    "HOSTNAME": e.get("WIN_HOSTNAME", "windows-build"), "TIMEZONE": e.get("TIMEZONE", "Romance Standard Time"),
    "IMAGE_KEY": e.get("IMAGE_KEY", "/IMAGE/NAME"), "IMAGE_VALUE": e.get("IMAGE_VALUE", "Windows Server 2025 SERVERSTANDARD"),
    "PRODUCT_KEY_XML": "<ProductKey><Key>%s</Key><WillShowUI>OnError</WillShowUI></ProductKey>" % escape(key) if key else "",
}
s = open(sys.argv[1]).read()
for k, v in vals.items():
    s = s.replace("@%s@" % k, v if k == "PRODUCT_KEY_XML" else escape(v))
open(sys.argv[2], "w").write(s)
PY
xorriso -as genisoimage -V ANSWERS -J -joliet-long -r -o "$out" "$tmp/autounattend.xml" "$here/../scripts/windows-initialisation.ps1"
echo "wrote $out"
