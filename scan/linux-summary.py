# Throwaway (scan-behaviour branch). Turns the Linux report into tables:
# the programs started, connections, writes, /proc reads, signals, and the
# Falco rules that fired. Writes summary.txt; never fails the job.
# Usage: linux-summary.py <report dir> [names file]
import collections
import glob
import json
import os
import re
import sys

lines = []
SENSITIVE = re.compile(r"/etc/(shadow|sudoers|passwd|machine-id)|/var/lib/dbus/machine-id|\.ssh/|\.gnupg/|keyring|\.mozilla|google-chrome|/proc/\d+/(mem|maps|environ)|/dev/(input|uinput|mem)|/sys/class/input|ld\.so\.preload|/etc/cron|autostart|systemd/user")


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


def norm(s):
    s = re.sub(r"/tmp/[^/\s\"]*\d[^/\s\"]*", "/tmp/<x>", s)
    s = re.sub(r"/home/runner/work/_temp", "<temp>", s)
    s = re.sub(r"/proc/\d+/", "/proc/<pid>/", s)
    s = re.sub(r"\b\d{3,}\b", "<n>", s)
    s = re.sub(r"[0-9a-f]{16,}", "<hex>", s)
    return s


CALL = re.compile(r"^(\d+) (\S+) (\w+)\((.*)\)\s+=\s+(-?\d+|\?)(.*)$")


def traces(report):
    for path in sorted(glob.glob(os.path.join(report, "linux-*", "trace", "*.txt"))):
        phase = os.path.basename(path)[:-4]
        comm = {}
        with open(path, encoding="utf-8", errors="replace") as f:
            for l in f:
                m = CALL.match(l.rstrip("\n"))
                if not m:
                    continue
                pid, t, call, args, ret, rest = m.groups()
                if call == "execve" and ret == "0":
                    a = re.match(r'"([^"]*)"', args)
                    if a:
                        comm[pid] = os.path.basename(a.group(1))
                yield phase, pid, comm.get(pid, "?"), call, args, ret, rest


def summarize_strace(report):
    execs = collections.Counter()
    conns = collections.Counter()
    writes = collections.Counter()
    procs = collections.Counter()
    sens = collections.Counter()
    danger = collections.Counter()
    signals = collections.Counter()
    unlinks = collections.Counter()
    watches = collections.Counter()
    for phase, pid, comm, call, args, ret, rest in traces(report):
        if call in ("execve", "execveat"):
            argv = re.search(r"\[(.*?)\]", args)
            execs[(phase, norm(argv.group(1) if argv else args)[:300], ret)] += 1
        elif call in ("connect", "bind"):
            target = re.search(r'sun_path="([^"]*)"|sin6?_addr=[^,]*\("?([^")]*)"?\)|sin6?_port=htons\((\d+)\)', args)
            path = re.findall(r'sun_path="([^"]*)"|inet_addr\("([^"]*)"\)|htons\((\d+)\)|inet_pton\(AF_INET6, "([^"]*)"', args)
            flat = " ".join(x for t in path for x in t if x)
            conns[(comm, call, norm(flat) or norm(args)[:120], ret if ret.startswith("-") else "ok")] += 1
        elif call == "openat":
            p = re.search(r'"([^"]*)"', args)
            p = p.group(1) if p else ""
            if re.search(r"O_WRONLY|O_RDWR|O_CREAT", args):
                writes[(comm, norm(os.path.dirname(p)))] += 1
            if p.startswith("/proc/") and not p.startswith("/proc/self"):
                procs[(comm, norm(p))] += 1
            if SENSITIVE.search(p):
                sens[(comm, p, ret if ret.startswith("-") else "ok")] += 1
        elif call in ("ptrace", "process_vm_readv", "process_vm_writev", "memfd_create", "setuid", "setgid", "setns", "unshare", "mount", "bpf", "perf_event_open", "init_module", "finit_module", "fanotify_init", "fanotify_mark"):
            danger[(comm, call, norm(args)[:120])] += 1
        elif call in ("kill", "tkill", "tgkill"):
            signals[(comm, norm(args)[:80])] += 1
        elif call in ("unlink", "unlinkat", "rename", "renameat", "renameat2"):
            p = re.findall(r'"([^"]*)"', args)
            unlinks[(comm, call, norm(os.path.dirname(p[0])) if p else "?")] += 1
        elif call == "inotify_add_watch":
            p = re.search(r'"([^"]*)"', args)
            watches[(comm, norm(p.group(1) if p else args))] += 1
        elif call in ("chmod", "fchmod", "fchmodat") and re.search(r"0[0-7]*[1357]\b", args):
            danger[(comm, call + " +x", norm(args)[:120])] += 1
    table("Programs started (phase, argv, result)", [f"{n}x [{p}] {a} = {r}" for (p, a, r), n in execs.most_common()], 150)
    table("Connections and listeners (program, call, target, result)", [f"{n}x {c} {k} {t} {r}" for (c, k, t, r), n in conns.most_common()], 80)
    table("Files opened for writing (program, folder)", [f"{n}x {c}: {d}" for (c, d), n in writes.most_common()], 80)
    table("Other processes' /proc files read", [f"{n}x {c}: {p}" for (c, p), n in procs.most_common()], 40)
    table("Sensitive paths opened", [f"{n}x {c}: {p} {r}" for (c, p, r), n in sens.most_common()], 40)
    table("Tracing, injection, privilege and kernel calls", [f"{n}x {c} {k} {a}" for (c, k, a), n in danger.most_common()], 40)
    table("Signals sent", [f"{n}x {c}: {a}" for (c, a), n in signals.most_common()], 40)
    table("Deletes and renames (program, call, folder)", [f"{n}x {c} {k}: {d}" for (c, k, d), n in unlinks.most_common()], 40)
    table("Folders watched", [f"{n}x {c}: {p}" for (c, p), n in watches.most_common()], 40)


def summarize_falco(report):
    path = os.path.join(report, "falco", "falco_events.json")
    ours = collections.Counter()
    other = collections.Counter()
    samples = {}
    try:
        f = open(path, encoding="utf-8", errors="replace")
    except OSError:
        table("Falco", ["no events file"])
        return
    with f:
        for l in f:
            try:
                e = json.loads(l)
            except Exception:
                continue
            blob = json.dumps(e.get("output_fields", {})).lower() + (e.get("output") or "").lower()
            key = (e.get("priority"), e.get("rule"))
            if "spytify" in blob or "fakeplayer" in blob:
                ours[key] += 1
                samples.setdefault(key, norm(e.get("output") or "")[:400])
            else:
                other[key] += 1
    try:
        ran = open(os.path.join(report, "falco", "falco-ran.txt")).read().strip()
    except OSError:
        ran = "?"
    table(f"Falco rules on the app ({ran})", [f"{n}x {p} {r}\n      {samples[(p, r)]}" for (p, r), n in ours.most_common()], 60)
    table("Falco rules on the rest of the runner", [f"{n}x {p} {r}" for (p, r), n in other.most_common()], 40)


def main():
    report = sys.argv[1]
    names, known = [], False
    if len(sys.argv) > 2 and os.path.exists(sys.argv[2]):
        ls = [l.strip() for l in open(sys.argv[2], encoding="utf-8") if l.strip()]
        known = bool(ls)
        names = [l for l in ls if not l.startswith("#")]
    out("# Behaviour scan, Linux")
    for mode in ("strace", "plain"):
        try:
            table(f"Marks ({mode})", open(os.path.join(report, f"linux-{mode}", "marks.txt"), encoding="utf-8").read().splitlines())
        except OSError:
            pass
    summarize_strace(report)
    summarize_falco(report)
    for f in ("systemd-security.txt", "spytifyd.service", "dmesg.txt"):
        try:
            table(f, open(os.path.join(report, "linux-strace", f), encoding="utf-8", errors="replace").read().splitlines(), 80)
        except OSError:
            pass
    sink = []
    for p in glob.glob(os.path.join(report, "linux-*", "sentry-sink.jsonl")):
        sink += [json.loads(l) for l in open(p, encoding="utf-8") if l.strip()]
    kinds = collections.Counter((it.get("header") or {}).get("type") or "envelope" for r in sink for it in r.get("items", []))
    table("Bug tracker requests (to the local sink)", [f"{len(sink)} requests"] + [f"{k}: {n}" for k, n in kinds.most_common()])
    text = "\n".join(lines) + "\n"
    for n in names:
        text = re.sub(re.escape(n), "[player]", text, flags=re.I)
    with open(os.path.join(report, "summary.txt"), "w", encoding="utf-8") as f:
        f.write(text)
    print(text if known else f"summary written ({len(lines)} lines); not printed: the player's names are unknown")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"summary failed: {type(e).__name__}: {e}")
    sys.exit(0)
