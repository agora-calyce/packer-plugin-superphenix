#!/usr/bin/env python3
"""Validate an autounattend.xml, or an answer-file ISO that contains one, before using it as `cd_url`.

Usage:
  validate-answer-iso.py answers.iso|autounattend.xml [--username packer]
  WINRM_PASSWORD=... validate-answer-iso.py answers.iso --username packer

Checks the file offline (an ISO needs xorriso): it must be well-formed, have the passes and settings the
Packer build relies on (UEFI disk layout, an image to install, EULA, a build account, a way to enable WinRM)
and, when given, match the username/password of your template (`winrm_username`, `winrm_password`).
It cannot prove that Windows will accept the file: only a build does. Exit status 1 on any ERROR.
"""
import argparse, os, re, subprocess, sys, tempfile, urllib.request
import xml.etree.ElementTree as ET

NS = {"u": "urn:schemas-microsoft-com:unattend"}
errors, warnings = [], []
err = lambda m: errors.append(m)
warn = lambda m: warnings.append(m)


def load(path):
    if path.lower().endswith(".xml"):
        return open(path, encoding="utf-8-sig").read()
    with tempfile.TemporaryDirectory() as d:
        try:
            ls = subprocess.run(["xorriso", "-indev", path, "-find", "/", "-maxdepth", "1", "-type", "f"],
                                capture_output=True, text=True, check=True).stdout
        except FileNotFoundError:
            sys.exit("xorriso is needed to read an ISO (apt install xorriso), or pass autounattend.xml directly")
        except subprocess.CalledProcessError as e:
            sys.exit("cannot read %s as an ISO: %s" % (path, e.stderr.strip()[-200:]))
        names = re.findall(r"^'/(.+)'$", ls, re.M)
        match = [n for n in names if n.lower() == "autounattend.xml"]
        if not match:
            err("no autounattend.xml at the root of the ISO (found: %s)" % (", ".join(names) or "nothing"))
            return None
        if match[0] != "autounattend.xml":
            warn("file is named %s; use the exact name autounattend.xml" % match[0])
        out = os.path.join(d, "a.xml")
        subprocess.run(["xorriso", "-osirrox", "on", "-indev", path, "-extract", "/" + match[0], out],
                       capture_output=True, check=True)
        return open(out, encoding="utf-8-sig").read()


def comp(root, pass_, name):
    for s in root.findall("u:settings", NS):
        if s.get("pass") == pass_:
            for c in s.findall("u:component", NS):
                if c.get("name") == name:
                    return c
    return None


def text(el, path):
    e = el.find(path, NS) if el is not None else None
    return (e.text or "").strip() if e is not None else ""


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("file")
    ap.add_argument("--username", help="winrm_username of the template")
    a = ap.parse_args()
    password = os.environ.get("WINRM_PASSWORD")

    raw = load(a.file)
    if raw is None:
        return report()
    for pat, msg in ((r"@[A-Z_]+@", "unreplaced placeholder"), (r"\$\{|%\{", "unrendered template syntax")):
        m = re.search(pat, raw)
        if m:
            err("%s: %s" % (msg, m.group(0)))
    try:
        root = ET.fromstring(raw.encode())
    except ET.ParseError as e:
        err("XML is not well-formed: %s" % e)
        return report()
    if root.tag != "{%s}unattend" % NS["u"]:
        err("root element must be <unattend xmlns=\"urn:schemas-microsoft-com:unattend\">, got %s" % root.tag)
        return report()

    setup = comp(root, "windowsPE", "Microsoft-Windows-Setup")
    if setup is None:
        err("windowsPE pass: Microsoft-Windows-Setup component is missing")
    else:
        if text(setup, "u:UserData/u:AcceptEula").lower() != "true":
            err("UserData/AcceptEula must be true, or Setup stops at the licence page")
        if not text(setup, "u:ImageInstall/u:OSImage/u:InstallFrom/u:MetaData/u:Value"):
            err("ImageInstall/OSImage/InstallFrom/MetaData is missing: Setup will wait at the edition prompt")
        parts = setup.findall("u:DiskConfiguration/u:Disk/u:CreatePartitions/u:CreatePartition", NS)
        if not parts:
            err("DiskConfiguration/CreatePartitions is missing: Setup will wait at the disk prompt")
        else:
            if not any(text(p, "u:Type").upper() == "EFI" for p in parts):
                err("no EFI partition: the VM boots with UEFI, so the disk needs an EFI system partition")
            ids = [text(p, "u:Order") for p in parts]
            target = text(setup, "u:ImageInstall/u:OSImage/u:InstallTo/u:PartitionID")
            if target and target not in ids:
                err("InstallTo PartitionID %s matches no partition order (%s)" % (target, ", ".join(ids)))
        for key in ("u:DiskConfiguration/u:Disk/u:DiskID", "u:ImageInstall/u:OSImage/u:InstallTo/u:DiskID"):
            if text(setup, key) not in ("", "0"):
                warn("%s is %s: the build disk is the only disk, normally 0" % (key.split("/")[-1], text(setup, key)))

    oobe = comp(root, "oobeSystem", "Microsoft-Windows-Shell-Setup")
    if oobe is None:
        err("oobeSystem pass: Microsoft-Windows-Shell-Setup component is missing")
    else:
        accounts = oobe.findall("u:UserAccounts/u:LocalAccounts/u:LocalAccount", NS)
        names = [text(x, "u:Name") for x in accounts]
        pwds = {text(x, "u:Name"): text(x, "u:Password/u:Value") for x in accounts}
        admin_pwd = text(oobe, "u:UserAccounts/u:AdministratorPassword/u:Value")
        if not accounts and not admin_pwd:
            err("no build account: define a LocalAccount or an AdministratorPassword")
        auto = oobe.find("u:AutoLogon", NS)
        if auto is None or text(auto, "u:Enabled").lower() != "true":
            err("AutoLogon must be enabled: FirstLogonCommands (the WinRM bootstrap) only run after a logon")
        else:
            if names and text(auto, "u:Username") not in names + ["Administrator", "packer"]:
                warn("AutoLogon user %s is not one of the LocalAccounts (%s)" % (text(auto, "u:Username"), ", ".join(names)))
        if oobe.find("u:OOBE", NS) is None:
            warn("no <OOBE> block: Windows may stop at the first-run screens (set HideEULAPage, ProtectYourPC, ...)")
        cmds = [text(c, "u:CommandLine") for c in oobe.findall("u:FirstLogonCommands/u:SynchronousCommand", NS)]
        if not cmds:
            err("no FirstLogonCommands: nothing enables WinRM, Packer would wait for it until it times out")
        elif not any(re.search(r"winrm|PSRemoting|windows-initialisation", c, re.I) for c in cmds):
            warn("no FirstLogonCommand mentions WinRM: make sure something enables it (and opens the firewall)")
        for p in re.findall(r"<PlainText>(\w+)</PlainText>", raw):
            if p.lower() == "true":
                warn("passwords are stored in clear text: fine for a throwaway build password (the plugin's winrm_final_username replaces the account), never put a real one in the ISO")
                break
        if a.username and a.username not in names + (["Administrator", "packer"] if admin_pwd else []):
            err("winrm_username %s is not a LocalAccount of the file (%s)" % (a.username, ", ".join(names) or "none"))
        if password:
            known = [v for v in list(pwds.values()) + [admin_pwd, text(auto, "u:Password/u:Value") if auto is not None else ""] if v]
            if a.username and pwds.get(a.username) not in (None, password):
                err("WINRM_PASSWORD differs from the password of account %s in the file" % a.username)
            elif not a.username and password not in known:
                err("WINRM_PASSWORD matches no password in the file")

    return report()


def report():
    for w in warnings:
        print("WARNING:", w)
    for e in errors:
        print("ERROR:", e)
    print("%d error(s), %d warning(s)" % (len(errors), len(warnings)))
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
