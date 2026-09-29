#!/usr/bin/env python3
"""Summarise a replay run: python3 summarise.py <out-dir> [verdicts.json].

<out-dir> holds one folder per PR written by replay.sh. Prints, per tier, the
hits it reached first (tier attribution as in the report), their hand verdicts,
and the cumulative recall over the 21 behaviour requirements, then tokens.
"""
import json, math, os, statistics, sys

out = sys.argv[1]
here = os.path.dirname(os.path.abspath(__file__))
verd = json.load(open(sys.argv[2] if len(sys.argv) > 2 else os.path.join(here, "verdicts.json")))
B, E = verd["behaviour"], verd["evidence"]
PRS = ["P1", "P2", "P3", "P4", "P5", "P6", "P7", "S1", "S2", "S3", "S4", "S5"]
CTRL = ["C1", "C2"]


def report(pr, tag):
    return json.load(open(f"{out}/{pr}/{tag}.1.json"))["report"]


def verdict(pr, ref):
    if ref in B.get(pr, []):
        return "B"
    if ref in E.get(pr, []):
        return "E"
    return "U"


def text_tokens(pr, tag):
    return math.ceil(len(open(f"{out}/{pr}/{tag}.txt", "rb").read()) / 3)


def med(xs):
    return statistics.median(xs)


print("## hits reached first by each tier (t123 run), verdicts B/E/U")
tot = {1: [0, 0, 0], 2: [0, 0, 0], 3: [0, 0, 0]}
found = {1: set(), 2: set(), 3: set()}
for pr in PRS + CTRL:
    r = report(pr, "t123")
    row = {1: [0, 0, 0], 2: [0, 0, 0], 3: [0, 0, 0]}
    for h in r["hits"]:
        v = verdict(pr, h["ref"])
        row[h["tier"]]["BEU".index(v)] += 1
        tot[h["tier"]]["BEU".index(v)] += 1
        if v == "B":
            found[h["tier"]].add((pr, h["ref"]))
    print(pr, {t: "/".join(map(str, row[t])) for t in row},
          "; ".join(f"{t['tier']}:{t['status']}" + (f" ({t['message']})" if t.get("message") else "") for t in r["tiers"] if t["status"] != "ok"))
print("tier totals B/E/U (12 PRs + 2 controls):", {t: tot[t] for t in tot})
truth = {(pr, ref) for pr in PRS for ref in B[pr]}
cum = set()
for t in (1, 2, 3):
    cum |= found[t] & truth
    print(f"cumulative recall after tier {t}: {len(cum)}/{len(truth)}")

print("\n## tier 3 alone (t3 run), verdicts B/E/U over the 8 candidates")
t3 = [0, 0, 0]
maxscore = 0
for pr in PRS + CTRL:
    r = report(pr, "t3")
    c = [0, 0, 0]
    for h in r["hits"]:
        c["BEU".index(verdict(pr, h["ref"]))] += 1
        maxscore = max(maxscore, h.get("score", 0))
    for i in range(3):
        t3[i] += c[i]
    print(pr, "/".join(map(str, c)))
print("tier 3 alone total B/E/U:", t3, "max score", maxscore)

print("\n## tokens (report 'tokens' = MCP JSON size; text = ceil(bytes/3) of the CLI text)")
for tag in ("t1", "t12", "t123", "t3"):
    js = [report(pr, tag)["tokens"] for pr in PRS]
    tx = [text_tokens(pr, tag) for pr in PRS]
    ctl = [report(pr, tag)["tokens"] for pr in CTRL]
    print(f"{tag}: JSON median {med(js)} worst {max(js)} controls {ctl}; text median {med(tx)} worst {max(tx)}")
for tag in ("t1", "t12", "t123", "t3"):
    print(tag, "per PR JSON tokens:", {pr: report(pr, tag)["tokens"] for pr in PRS + CTRL})

print("\n## determinism (byte-identical pairs) and latency")
same = sum(open(f"{out}/{pr}/{t}.cmp").read().strip() == "same" for pr in PRS + CTRL for t in ("t1", "t12", "t123", "t3"))
print("identical pairs:", same, "of", 4 * len(PRS + CTRL))
for tag in ("t1", "t12", "t123", "t3"):
    s = [float(open(f"{out}/{pr}/{tag}.1.secs").read()) for pr in PRS]
    print(tag, "median s", round(med(s), 2), "max s", round(max(s), 2))
