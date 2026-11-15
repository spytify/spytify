# Behaviour gate (ci.yml). Compares what the security tools saw against
# expected.json: an alert that is not listed fails the job, and so does a
# listed alert that no longer fires, so the PR that fixes one removes its
# line and its own CI run proves the alert is gone.
# Usage: check.py windows|linux|files <report dir>
import collections
import glob
import json
import ntpath
import os
import re
import sys

# Programs a Spytify program must not start: shells, script hosts and the
# system tools malware leans on.
WATCHED = {"powershell.exe", "pwsh.exe", "cmd.exe", "rundll32.exe", "wscript.exe", "cscript.exe", "mshta.exe",
           "regsvr32.exe", "schtasks.exe", "reg.exe", "sc.exe", "net.exe", "certutil.exe", "bitsadmin.exe",
           "curl.exe", "wmic.exe", "msiexec.exe"}
# Hayabusa's names for plain event types, which detect nothing.
EVENT_LABELS = {"Proc Access", "Proc Exec", "Proc Terminated", "File Created", "File Deleted", "Pipe Created",
                "Pipe Conn", "Net Conn", "DNS Query", "Reg Key Create", "Reg Key Value Set", "Image Loaded"}


def jsonl(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                try:
                    yield json.loads(line)
                except Exception:
                    pass
    except OSError:
        return


def windows(report):
    app = open(os.path.join(report, "app-dir.txt"), encoding="utf-8-sig").read().strip().lower()
    found = set()
    events = list(jsonl(os.path.join(report, "events", "sysmon.jsonl")))
    if not events:
        raise SystemExit("no Sysmon events: the gate cannot judge")
    procs, kids = {}, collections.defaultdict(list)
    for e in events:
        if e["id"] == 1:
            d = e["data"]
            procs[d.get("ProcessGuid")] = d
            kids[d.get("ParentProcessGuid")].append(d.get("ProcessGuid"))
    family = {g for g, d in procs.items() if (d.get("Image") or "").lower().startswith(app)}
    stack = list(family)
    while stack:
        for c in kids.get(stack.pop(), []):
            if c not in family:
                family.add(c)
                stack.append(c)
    for g in family:
        d = procs[g]
        image = ntpath.basename(d.get("Image") or "").lower()
        parent = procs.get(d.get("ParentProcessGuid"), {})
        if image in WATCHED and (parent.get("Image") or d.get("ParentImage") or "").lower().startswith(app):
            found.add(f"child: {ntpath.basename(d.get('ParentImage') or '').lower()} starts {image}")
    for e in events:
        if e["id"] == 22 and e["data"].get("ProcessGuid") in family:
            found.add(f"dns: {ntpath.basename(e['data'].get('Image') or '').lower()} looks up {e['data'].get('QueryName')}")
    for e in jsonl(os.path.join(report, "events", "defender.jsonl")):
        d, blob = e["data"], json.dumps(e["data"]).lower()
        if app not in blob.replace("\\\\", "\\"):
            continue
        if e["id"] in (1116, 1117, 1006, 1015):
            found.add(f"defender: {d.get('Threat Name')}")
        elif e["id"] in (1121, 1122):
            found.add(f"asr: {d.get('ID', '').lower()} on {ntpath.basename(d.get('Path') or '').lower()}")
        elif e["id"] in (1123, 1124):
            found.add(f"cfa: {ntpath.basename(d.get('Process Name') or '').lower()} writes under a protected folder")
    hits = list(jsonl(os.path.join(report, "hayabusa.jsonl")))
    if not hits:
        raise SystemExit("no Hayabusa timeline: the gate cannot judge")
    for h in hits:
        d = h.get("Details")
        if not isinstance(d, dict) or h.get("Level") == "info":
            continue
        # A process access counts when a Spytify program opens another,
        # not when Windows or the scan opens one of them.
        if "SrcProc" in d:
            actor = str(d.get("SrcProc", "")).lower()
        else:
            actor = " ".join(str(d.get(k, "")) for k in ("Proc", "ParentCmdline")).lower()
        if app in actor and h.get("RuleTitle") not in EVENT_LABELS:
            found.add(f"sigma: {h.get('RuleTitle')}")
    return found


def linux(report):
    path = os.path.join(report, "falco", "falco_events.json")
    if not os.path.exists(path):
        raise SystemExit("no Falco events file: the gate cannot judge")
    found = set()
    for e in jsonl(path):
        blob = (json.dumps(e.get("output_fields", {})) + (e.get("output") or "")).lower()
        if "spytify" in blob:
            found.add(f"falco: {e.get('rule')}")
    return found


def files(report):
    found = set()
    try:
        for line in open(os.path.join(report, "clamscan.txt"), encoding="utf-8", errors="replace"):
            m = re.match(r"(.*): (\S+) FOUND$", line.strip())
            if m:
                found.add(f"clamav: {m.group(2)} on {os.path.basename(m.group(1))}")
    except OSError:
        raise SystemExit("no ClamAV result: the gate cannot judge")
    for p in glob.glob(os.path.join(report, "yara-*.txt")):
        for line in open(p, encoding="utf-8", errors="replace"):
            parts = line.split()
            if len(parts) >= 2:
                found.add(f"yara: {parts[0]} on {os.path.basename(parts[-1])}")
    return found


def main():
    kind, report = sys.argv[1], sys.argv[2]
    with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "expected.json"), encoding="utf-8") as f:
        expected = {a["alert"]: a for a in json.load(f)[kind]}
    found = {"windows": windows, "linux": linux, "files": files}[kind](report)
    new = sorted(found - expected.keys())
    gone = sorted(a for a in expected.keys() - found if not expected[a].get("may_not_fire"))
    for a in sorted(found & expected.keys()):
        issue = expected[a].get("issue")
        print(f"known   {a}" + (f" (#{issue})" if issue else ""))
    for a in new:
        print(f"NEW     {a}")
    for a in gone:
        print(f"GONE    {a}: it no longer fires; remove it from scripts/behaviour/expected.json")
    if new:
        print(f"\n{len(new)} alert(s) security software did not raise before this change. "
              "Fix the cause, or, when it cannot be fixed in code, add it to expected.json with its issue.")
    return 1 if new or gone else 0


if __name__ == "__main__":
    sys.exit(main())
