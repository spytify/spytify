# Behaviour gate (ci.yml). Reads the player's names out of
# `spytify-cli info --json`, hides them in the job's log from here on and
# keeps them for the scrub. Usage: names.py <info.json> <names file>
import json
import re
import sys

COMMON = {"com", "org", "net", "app", "client", "desktop", "player", "music",
          "www", "mpris", "mediaplayer2"}


def names_of(target):
    found = []
    for v in (target.get("process"), target.get("mpris")):
        for part in re.split(r"[,;]", str(v or "")):
            part = part.strip()
            if part:
                found += [part, re.sub(r"\.exe$", "", part, flags=re.I)]
    # A bundle id or a bus name is the name between words every app has.
    for v in (target.get("bundle_id"), target.get("mpris")):
        for part in re.split(r"[.,;]", str(v or "")):
            part = part.strip()
            if part and part.lower() not in COMMON:
                found.append(part)
    seen, out = set(), []
    for n in found:
        if len(n) >= 4 and n.lower() not in seen:
            seen.add(n.lower())
            out.append(n)
    return sorted(out, key=len, reverse=True)


def main():
    info, out = sys.argv[1], sys.argv[2]
    try:
        with open(info, encoding="utf-8") as f:
            target = json.load(f)["target"]
    except Exception as e:
        print(f"could not read the player's names: {type(e).__name__}")
        return 1
    names = names_of(target or {})
    for n in names:
        for v in dict.fromkeys([n, n.lower(), n.upper(), n[:1].upper() + n[1:].lower()]):
            print(f"::add-mask::{v}")
    with open(out, "w", encoding="utf-8") as f:
        f.write("\n".join(names) + "\n" if names else "#none\n")
    print(f"{len(names)} player name(s) kept for the scrub")
    return 0


if __name__ == "__main__":
    sys.exit(main())
