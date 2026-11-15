# Throwaway (scan-behaviour branch). Turns the Windows report into tables:
# what Defender did, what the app's programs started, reached, wrote and
# loaded, and which Sigma rules fired on them. Writes summary.txt and the
# event logs as JSON lines; never fails the job.
# Usage: windows-summary.py <report dir> [names file]
import collections
import glob
import json
import ntpath
import os
import re
import sys
import xml.etree.ElementTree as ET

ASR = {
    "56a863a9-875e-4185-98a7-b882c64b5ce5": "vulnerable signed drivers",
    "7674ba52-37eb-4a4f-a9a1-f0f9a1619a2c": "Adobe Reader child processes",
    "d4f940ab-401b-4efc-aadc-ad5f3c50688a": "Office child processes",
    "9e6c4e1f-7d60-472f-ba1a-a39ef669e4b2": "lsass credential stealing",
    "be9ba2d9-53ea-4cdc-84e5-9b1eeee46550": "executable content from email",
    "01443614-cd74-433a-b99e-2ecdc07bfc25": "executables unless prevalence, age or trusted list",
    "5beb7efe-fd9a-4556-801d-275e5ffc04cc": "obfuscated scripts",
    "d3e037e1-3eb8-44c8-a917-57927947596d": "JS/VBS launching downloaded executables",
    "3b576869-a4ec-4529-8536-b80a7769e899": "Office creating executables",
    "75668c1f-73b5-4cf0-bb93-3ecf5cb7cc84": "Office injection",
    "26190899-1602-49e8-8b27-eb1d0a1ce869": "Office communication child processes",
    "e6db77e5-3df2-4cf1-b95a-636979351e5b": "WMI persistence",
    "d1e49aac-8f56-4280-b9ba-993a6d77406c": "PSExec/WMI process creation",
    "33ddedf1-c6e0-47cb-833e-de6133960387": "Safe Mode reboot",
    "b2b3f03d-6a65-4f7b-a9c7-1c7ef74a9ba4": "unsigned from USB",
    "c0033c00-d16d-4114-a5a0-dc9b3a7d2ceb": "copied or impersonated system tools",
    "a8f5898e-1dc8-49a9-9878-85004b8a61e6": "webshell creation",
    "92e97fa1-2edf-4476-bdd6-9dd0b4dddc7b": "Win32 API from Office macros",
    "c1db55ab-c21a-4637-bb3f-a12568109d35": "advanced ransomware protection",
}
lines = []


def out(s=""):
    lines.append(s)


def table(title, rows, limit=60):
    out()
    out(f"## {title}")
    if not rows:
        out("  (none)")
        return
    for r in rows[:limit]:
        out("  " + r)
    if len(rows) > limit:
        out(f"  ... {len(rows) - limit} more")


def read_jsonl(path):
    rows = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for l in f:
                l = l.strip()
                if l:
                    try:
                        rows.append(json.loads(l))
                    except Exception:
                        pass
    except OSError:
        pass
    return rows


def read_json(path):
    try:
        with open(path, encoding="utf-8-sig", errors="replace") as f:
            return json.load(f)
    except Exception:
        return None


def strip_tag(t):
    return t.split("}", 1)[-1]


def parse_events(report):
    """events/<name>.xml (wevtutil) → events/<name>.jsonl."""
    out_counts = {}
    for path in glob.glob(os.path.join(report, "events", "*.xml")):
        raw = open(path, "rb").read()
        if raw[:2] in (b"\xff\xfe", b"\xfe\xff"):
            text = raw.decode("utf-16", errors="replace")
        else:
            text = raw.decode("utf-8", errors="replace")
        text = re.sub(r"""\sxmlns(:\w+)?=(["'])[^"']*\2""", "", text)
        text = re.sub(r"&#x?0*([0-8bcef]|1[0-9a-f]);", "", text, flags=re.I)
        good = bad = 0
        dest = path[:-4] + ".jsonl"
        with open(dest, "w", encoding="utf-8") as f:
            for m in re.finditer(r"<Event[ >].*?</Event>", text, re.S):
                try:
                    e = ET.fromstring(m.group(0))
                except Exception:
                    bad += 1
                    continue
                sysn = e.find("System")
                row = {"provider": None, "id": None, "time": None, "level": None, "pid": None, "channel": None, "data": {}}
                if sysn is not None:
                    p = sysn.find("Provider")
                    row["provider"] = p.get("Name") if p is not None else None
                    row["id"] = int((sysn.findtext("EventID") or "0").strip() or 0)
                    tc = sysn.find("TimeCreated")
                    row["time"] = tc.get("SystemTime") if tc is not None else None
                    row["level"] = sysn.findtext("Level")
                    ex = sysn.find("Execution")
                    row["pid"] = ex.get("ProcessID") if ex is not None else None
                    row["channel"] = sysn.findtext("Channel")
                for part in ("EventData", "UserData"):
                    node = e.find(part)
                    if node is None:
                        continue
                    for d in node.iter():
                        if d is node:
                            continue
                        name = d.get("Name") or strip_tag(d.tag)
                        if d.text and d.text.strip():
                            if name == "Data":
                                row["data"].setdefault("text", []).append(d.text.strip())
                            else:
                                row["data"][name] = d.text.strip()
                f.write(json.dumps(row) + "\n")
                good += 1
        out_counts[os.path.basename(path)[:-4]] = (good, bad)
    return out_counts


def load_events(report, name):
    return read_jsonl(os.path.join(report, "events", name + ".jsonl"))


class Norm:
    def __init__(self, app):
        self.app = app.lower() if app else None
        self.home = os.environ.get("USERPROFILE", "").lower()

    def __call__(self, s):
        if not s:
            return ""
        t = s
        if self.app:
            t = re.sub(re.escape(self.app), "<app>", t, flags=re.I)
        t = re.sub(r"[A-Z]:\\Users\\[^\\]+\\AppData\\Local\\Temp", "<temp>", t, flags=re.I)
        t = re.sub(r"[A-Z]:\\a\\_temp", "<runner-temp>", t, flags=re.I)
        t = re.sub(r"[A-Z]:\\Users\\[^\\]+", "<home>", t, flags=re.I)
        t = re.sub(r"--pid \d+", "--pid N", t)
        t = re.sub(r"\{?[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\}?", "<guid>", t, flags=re.I)
        t = re.sub(r"\b[0-9a-f]{16,}\b", "<hex>", t, flags=re.I)
        t = re.sub(r"\d{6,}", "<n>", t)
        return t


def main():
    report = sys.argv[1]
    names_file = sys.argv[2] if len(sys.argv) > 2 else None
    names = []
    known = False
    if names_file and os.path.exists(names_file):
        with open(names_file, encoding="utf-8") as f:
            ls = [l.strip() for l in f if l.strip()]
        known = bool(ls)
        names = [l for l in ls if not l.startswith("#")]
    app = None
    try:
        app = open(os.path.join(report, "app-dir.txt"), encoding="utf-8-sig").read().strip()
    except OSError:
        pass
    norm = Norm(app)

    counts = parse_events(report)
    out("# Behaviour scan, Windows")
    table("Event logs read (events, unreadable)", [f"{k}: {g}, {b}" for k, (g, b) in sorted(counts.items())])

    # Defender
    pc = read_json(os.path.join(report, "defender", "positive-control.json"))
    st = read_json(os.path.join(report, "defender", "status-after-on.json")) or {}
    rows = [f"positive controls: {json.dumps(pc)}"]
    for k in ("AMRunningMode", "AntivirusEnabled", "RealTimeProtectionEnabled", "BehaviorMonitorEnabled",
              "IoavProtectionEnabled", "OnAccessProtectionEnabled", "IsTamperProtected", "AntivirusSignatureVersion"):
        rows.append(f"{k}: {st.get(k)}")
    failed = [r for r in read_jsonl(os.path.join(report, "defender", "settings.jsonl")) if not r.get("ok")]
    rows += [f"setting FAILED: {r.get('change')}: {r.get('error')}" for r in failed]
    table("Defender state", rows)

    dev = load_events(report, "defender")
    byid = collections.Counter(e["id"] for e in dev)
    table("Defender events by id", [f"{i}: {n}" for i, n in sorted(byid.items())])
    det = []
    for e in dev:
        if e["id"] in (1006, 1007, 1008, 1009, 1011, 1015, 1116, 1117, 1118, 1119, 1121, 1122, 1123, 1124, 1125, 1126, 1127, 1128, 2050, 1150):
            d = e["data"]
            asr = d.get("ID", "").lower()
            what = ASR.get(asr, "")
            det.append(f"{e['time']} {e['id']} {d.get('Threat Name') or what} | {norm(d.get('Path') or d.get('Process Name') or '')} "
                       f"| {norm(d.get('Process Name') or '')} | {d.get('Action Name') or ''} {d.get('Detection Origin Name') or ''}")
    table("Defender detections, audits and blocks", det, 200)
    for f in ("detections", "threats"):
        j = read_json(os.path.join(report, "defender", f + ".json"))
        if j:
            j = j if isinstance(j, list) else [j]
            table(f"Get-Mp{f}", [norm(json.dumps({k: x.get(k) for k in ('ThreatName', 'ThreatID', 'Resources', 'ActionSuccess', 'InitialDetectionTime', 'ProcessName', 'DetectionSourceTypeID')}, default=str)) for x in j])

    # Sysmon: the app family
    sy = load_events(report, "sysmon")
    procs = {}
    children = collections.defaultdict(list)
    for e in sy:
        if e["id"] == 1:
            d = e["data"]
            procs[d.get("ProcessGuid")] = d
            children[d.get("ParentProcessGuid")].append(d.get("ProcessGuid"))
    fam = set()
    if app:
        for g, d in procs.items():
            if (d.get("Image") or "").lower().startswith(app.lower()):
                fam.add(g)
        stack = list(fam)
        while stack:
            g = stack.pop()
            for c in children.get(g, []):
                if c not in fam:
                    fam.add(c)
                    stack.append(c)
    fam_pids = {procs[g].get("ProcessId") for g in fam}

    edges = collections.Counter()
    for g in fam:
        d = procs[g]
        parent = procs.get(d.get("ParentProcessGuid"), {})
        pimg = ntpath.basename(d.get("ParentImage") or parent.get("Image") or "?")
        edges[(pimg, norm(d.get("CommandLine")), d.get("IntegrityLevel"))] += 1
    table("Processes of the app family (parent -> command line, integrity, count)",
          [f"{n}x {p} -> {c} [{i}]" for (p, c, i), n in edges.most_common()], 120)

    harness = collections.Counter()
    for g, d in procs.items():
        if g in fam:
            continue
        img = ntpath.basename(d.get("Image") or "").lower()
        if img in ("powershell.exe", "cmd.exe", "rundll32.exe", "conhost.exe", "werfault.exe", "explorer.exe", "smartscreen.exe"):
            harness[(img, norm(d.get("CommandLine"))[:200])] += 1
    table("Same programs outside the app family (harness and system)", [f"{n}x {i}: {c}" for (i, c), n in harness.most_common()], 60)

    marks = read_jsonl(os.path.join(report, "marks.jsonl"))
    taps = [d for g, d in procs.items() if g in fam and (d.get("Image") or "").lower().endswith("spytify-tap.exe")]
    if taps:
        ts = sorted(d.get("UtcTime") for d in taps)
        table("Capture helper starts", [f"{len(taps)} starts from {ts[0]} to {ts[-1]}"])

    def fam_ev(eid, key="Image"):
        rows = []
        for e in sy:
            if e["id"] == eid:
                d = e["data"]
                if d.get("ProcessGuid") in fam or (app and (d.get(key) or "").lower().startswith(app.lower())):
                    rows.append(d)
        return rows

    net = collections.Counter((ntpath.basename(d.get("Image") or ""), d.get("DestinationHostname") or d.get("DestinationIp"), d.get("DestinationPort")) for d in fam_ev(3))
    table("Network connections (image, host, port)", [f"{n}x {a} -> {b}:{c}" for (a, b, c), n in net.most_common()])
    dns = collections.Counter((ntpath.basename(d.get("Image") or ""), d.get("QueryName")) for d in fam_ev(22))
    table("DNS queries", [f"{n}x {a}: {b}" for (a, b), n in dns.most_common()])
    fc = collections.Counter((ntpath.basename(d.get("Image") or ""), norm(ntpath.dirname(d.get("TargetFilename") or "")), ntpath.splitext(d.get("TargetFilename") or "")[1]) for d in fam_ev(11))
    table("Files created (image, folder, extension)", [f"{n}x {a}: {b} *{c}" for (a, b, c), n in fc.most_common()], 100)
    reg = collections.Counter((ntpath.basename(d.get("Image") or ""), d.get("EventType"), norm(d.get("TargetObject"))) for d in fam_ev(12) + fam_ev(13) + fam_ev(14))
    table("Registry changes", [f"{n}x {a} {b}: {c}" for (a, b, c), n in reg.most_common()], 100)
    dll = collections.Counter()
    for d in fam_ev(7):
        loaded = d.get("ImageLoaded") or ""
        if not loaded.lower().startswith("c:\\windows") or d.get("Signed") != "true":
            dll[(ntpath.basename(d.get("Image") or ""), norm(loaded), d.get("Signed"), d.get("SignatureStatus"))] += 1
    table("Modules from outside Windows, or unsigned", [f"{n}x {a}: {b} signed={c} {s}" for (a, b, c, s), n in dll.most_common()], 100)
    acc = collections.Counter()
    for e in sy:
        if e["id"] == 10:
            d = e["data"]
            acc[(norm(d.get("SourceImage")), norm(d.get("TargetImage")), d.get("GrantedAccess"))] += 1
    table("Process access (source -> target, rights)", [f"{n}x {a} -> {b} {c}" for (a, b, c), n in acc.most_common()], 80)
    for eid, title in ((8, "Remote threads"), (25, "Process tampering"), (17, "Pipes created"), (18, "Pipes connected"), (15, "Stream hashes"), (9, "Raw disk reads"), (26, "Files deleted"), (29, "Executables written")):
        rows = [norm(json.dumps({k: v for k, v in e["data"].items() if k not in ("RuleName", "UtcTime", "ProcessGuid", "Hashes", "Hash")})) for e in sy if e["id"] == eid
                and (eid in (8, 25) or e["data"].get("ProcessGuid") in fam or (app and app.lower() in json.dumps(e["data"]).lower()))]
        table(title, sorted(set(rows)), 40)

    # PowerShell script blocks
    ps = load_events(report, "powershell")
    blocks = collections.Counter()
    for e in ps:
        if e["id"] == 4104:
            text = e["data"].get("ScriptBlockText") or ""
            who = "app" if e.get("pid") in fam_pids else "other"
            blocks[(who, norm(text)[:300].replace("\n", " "))] += 1
    table("PowerShell script blocks (who, start of the text)", [f"{n}x [{w}] {t}" for (w, t), n in blocks.most_common()], 40)

    ci = load_events(report, "codeintegrity")
    table("Code integrity events", [f"{e['time']} {e['id']} {norm(json.dumps(e['data']))[:300]}" for e in ci], 40)
    ss = load_events(report, "smartscreen")
    table("SmartScreen events", [f"{e['time']} {e['id']} {norm(json.dumps(e['data']))[:300]}" for e in ss], 40)
    apps = [e for e in load_events(report, "application") if e["id"] in (1000, 1001, 1002) and "spytify" in json.dumps(e["data"]).lower()]
    table("Application crashes and hangs", [f"{e['time']} {e['id']} {norm(json.dumps(e['data']))[:300]}" for e in apps], 40)

    # The harness's own record
    runs = read_jsonl(os.path.join(report, "runs.jsonl"))
    table("Runs", [f"{r.get('n')} {r.get('name')} {norm(r.get('args'))}: exit {r.get('exit')}"
                   f"{' timed out' if r.get('timed_out') else ''}{' stopped' if r.get('stopped') else ''}"
                   f"{' NOT STARTED ' + str(r.get('start_error')) if r.get('start_error') else ''}" for r in runs], 200)
    table("Marks", [f"{m.get('t')} {m.get('mark')} {norm(m.get('detail'))}" for m in marks], 200)
    table("Phase errors", [f"{m.get('phase')}: {m.get('error')}" for m in read_jsonl(os.path.join(report, 'phase-errors.jsonl'))])

    before = {f["path"]: f for f in (read_json(os.path.join(report, "files-before.json")) or [])}
    after = {f["path"]: f for f in (read_json(os.path.join(report, "files-after.json")) or [])}
    diff = [f"gone: {p}" for p in before if p not in after] + [f"new: {p}" for p in after if p not in before]
    diff += [f"changed: {p}" for p in before if p in after and before[p].get("sha256") != after[p].get("sha256")]
    table("App folder, before and after", diff + [f"{len(before)} files before, {len(after)} after"])

    sink = read_jsonl(os.path.join(report, "sentry-sink.jsonl"))
    kinds = collections.Counter()
    for r in sink:
        for it in r.get("items", []):
            kinds[(it.get("header") or {}).get("type") or "envelope"] += 1
    table("Bug tracker requests (to the local sink)", [f"{len(sink)} requests"] + [f"{k}: {n}" for k, n in kinds.most_common()])

    hb = read_jsonl(os.path.join(report, "hayabusa.jsonl"))
    rules = collections.Counter()
    for h in hb:
        blob = json.dumps(h).lower()
        who = "app" if (app and app.lower() in blob) or "spytify" in blob else "other"
        rules[(who, h.get("Level"), h.get("RuleTitle"))] += 1
    table("Sigma rules (Hayabusa) that fired on the app", [f"{n}x {l} {t}" for (w, l, t), n in rules.most_common() if w == "app"], 100)
    table("Sigma rules (Hayabusa) on the rest of the machine", [f"{n}x {l} {t}" for (w, l, t), n in rules.most_common() if w == "other"], 60)

    text = "\n".join(lines) + "\n"
    for n in names:
        text = re.sub(re.escape(n), "[player]", text, flags=re.I)
    with open(os.path.join(report, "summary.txt"), "w", encoding="utf-8") as f:
        f.write(text)
    if known:
        print(text)
    else:
        print(f"summary written ({len(lines)} lines); not printed: the player's names are unknown")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"summary failed: {type(e).__name__}: {e}")
    sys.exit(0)
