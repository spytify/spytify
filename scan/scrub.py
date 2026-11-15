# Throwaway (scan-behaviour branch). Takes the player's names, audio,
# pictures, binaries and tokens out of the report before it is uploaded.
# Fails (exit 1) when the names are unknown or a name is left.
# Usage: scrub.py <report dir> <names file>
import os
import re
import sys

DROP = {".wav", ".mp3", ".flac", ".m4a", ".ogg", ".opus", ".aac", ".part", ".evtx",
        ".png", ".jpg", ".jpeg", ".raw", ".pcm", ".zip", ".exe", ".dll", ".so", ".deb",
        ".rpm", ".dmg", ".gz", ".xz", ".7z"}
TOKEN = re.compile(rb"(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})")


def load_names(path):
    try:
        with open(path, encoding="utf-8") as f:
            lines = [l.strip() for l in f if l.strip()]
    except OSError:
        return None
    if not lines:
        return None
    return [l for l in lines if not l.startswith("#")]


def patterns(names):
    out = []
    for n in names:
        esc = re.escape(n)
        out.append((re.compile(esc.encode("utf-8"), re.I), b"[player]"))
        # UTF-16LE: each ASCII character followed by a zero byte.
        u16 = b"".join(re.escape(c.encode("utf-8")) + b"\\x00" for c in n)
        out.append((re.compile(u16, re.I), "[player]".encode("utf-16-le")))
    return out


def fix_name(s, names):
    for n in names:
        s = re.sub(re.escape(n), "[player]", s, flags=re.I)
    return s


def main():
    report, names_file = sys.argv[1], sys.argv[2]
    names = load_names(names_file)
    if names is None:
        print("the player's names are unknown: nothing is uploaded")
        return 1
    if not os.path.isdir(report):
        print("no report")
        return 1
    pats = patterns(names)
    dropped = changed = renamed = tokens = 0
    for root, dirs, files in os.walk(report, topdown=False):
        for f in files:
            p = os.path.join(root, f)
            if os.path.islink(p) or os.path.splitext(f)[1].lower() in DROP:
                os.remove(p)
                dropped += 1
                continue
            with open(p, "rb") as fh:
                data = fh.read()
            new = data
            for pat, rep in pats:
                new = pat.sub(rep, new)
            new, k = TOKEN.subn(b"[token]", new)
            tokens += k
            if new != data:
                with open(p, "wb") as fh:
                    fh.write(new)
                changed += 1
        for d in files + dirs:
            fixed = fix_name(d, names)
            if fixed != d:
                os.rename(os.path.join(root, d), os.path.join(root, fixed))
                renamed += 1
    left = 0
    for root, dirs, files in os.walk(report):
        for x in dirs + files:
            if any(n.lower() in x.lower() for n in names):
                left += 1
        for f in files:
            with open(os.path.join(root, f), "rb") as fh:
                data = fh.read()
            if any(p.search(data) for p, _ in pats) or TOKEN.search(data):
                left += 1
    print(f"scrub: {len(names)} name(s), {changed} file(s) changed, {renamed} renamed, "
          f"{dropped} dropped, {tokens} token(s), {left} left")
    return 1 if left else 0


if __name__ == "__main__":
    sys.exit(main())
