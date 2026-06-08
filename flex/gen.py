#!/usr/bin/env python3
"""Generate Go structs for the IBKR Flex report from the sample XML.

Reads testdata/sample.xml and writes flex_sections.go. The committed sample is
synthetic and schema-complete enough to guard generated-code drift in CI.
"""
import re
import xml.etree.ElementTree as ET
from collections import defaultdict, Counter

SRC = "testdata/sample.xml"
OUT = "flex_sections.go"

INITIALISMS = {
    "ACL","API","ASCII","CPU","CSS","DNS","EOF","GUID","HTML","HTTP","HTTPS",
    "ID","IP","JSON","LHS","QPS","RAM","RHS","RPC","SLA","SMTP","SQL","SSH",
    "TCP","TLS","TTL","UDP","UI","UID","UUID","URI","URL","UTF8","VM","XML",
}

def words(attr):
    s = re.sub(r'(.)([A-Z][a-z]+)', r'\1 \2', attr)
    s = re.sub(r'([a-z0-9])([A-Z])', r'\1 \2', s)
    return s.split()

def go_name(attr):
    out = []
    for w in words(attr):
        out.append(w.upper() if w.upper() in INITIALISMS else w[:1].upper() + w[1:])
    name = ''.join(out)
    if not name or not name[0].isalpha():
        name = 'X' + name
    return name

DENY_EXPLICIT = {"cusip", "isin", "figi", "sedol", "conid"}

def keep_string(attr):
    l = attr.lower()
    if l in DENY_EXPLICIT:
        return True
    if "postal" in l or "zip" in l:
        return True
    if l.endswith("id") or l.endswith("code") or l.endswith("number"):
        return True
    if re.search(r"(date|time|expiry|when|period|maturity|month|year|quarter)", l):
        return True
    return False

# ---- parse ----------------------------------------------------------------
root = ET.parse(SRC).getroot()
attrs = defaultdict(set)            # tag -> set(attr)
values = defaultdict(set)           # attr -> set(value) (global)
child_card = defaultdict(int)       # (parent, child) -> max children per instance
counts = Counter()

def walk(e):
    counts[e.tag] += 1
    for k, v in e.attrib.items():
        attrs[e.tag].add(k)
        if len(values[k]) < 200:
            values[k].add(v)
    cc = Counter(c.tag for c in e)
    for ct, n in cc.items():
        if n > child_card[(e.tag, ct)]:
            child_card[(e.tag, ct)] = n
    for c in e:
        walk(c)

walk(root)

SKIP = {"FlexQueryResponse", "FlexStatements"}
tags = [t for t in counts if t not in SKIP]

# self_nesting: tags that wrap rows of the same tag name (e.g. <OptionEAE> holds
# <OptionEAE> rows). These are modeled as a single row struct, referenced from
# the parent via a nested "Tag>Tag" XML path.
self_nesting = {p for (p, c) in child_card if p == c}

# A tag is a container (section wrapper) if it has children of a *different*
# tag, or it carries no attributes of its own. Self-nesting tags are rows.
def real_children(t):
    return sorted({c for (p, c) in child_card if p == t and c != t})

def is_container(t):
    if t in self_nesting:
        return False
    return bool(real_children(t)) or len(attrs[t]) == 0

containers = [t for t in tags if is_container(t)]
leaves = [t for t in tags if not is_container(t)]
children = {t: real_children(t) for t in containers}

# ---- group leaves by identical attribute set ------------------------------
groups = defaultdict(list)
for t in leaves:
    groups[frozenset(attrs[t])].append(t)

leaf_struct = {}     # leaf tag -> struct name
emitted = {}         # struct name -> attr set
for key, members in groups.items():
    ms = set(members)
    if {"Trade", "Lot", "Order"} <= ms:
        name = "Trade"
    elif {"FxLot", "FxPosition"} <= ms:
        name = "FxLot"
    elif len(members) == 1:
        name = go_name(members[0])
    else:
        name = go_name(sorted(members)[0]) + "Element"
    for m in members:
        leaf_struct[m] = name
    emitted[name] = sorted(key)

def struct_for(tag):
    return leaf_struct[tag] if tag in leaf_struct else go_name(tag)

def field_type(attr):
    if keep_string(attr):
        return "string"
    ne = [v for v in values[attr] if v != ""]
    if not ne:
        return "string"
    for v in ne:
        try:
            float(v)
        except ValueError:
            return "string"
    return "Float"

def emit_attr_fields(attr_list, used):
    lines = []
    for a in sorted(attr_list):
        fn = go_name(a)
        while fn in used:
            fn += "_"
        used.add(fn)
        lines.append(f'\t{fn} {field_type(a)} `xml:"{a},attr"`')
    return lines

structs = {}  # name -> list of field lines

# leaf structs
for name, attr_list in emitted.items():
    used = set()
    structs[name] = emit_attr_fields(attr_list, used)

# container structs
for t in containers:
    name = "Statement" if t == "FlexStatement" else go_name(t)
    used = set()
    lines = emit_attr_fields(sorted(attrs[t]), used)
    kids = children.get(t, [])
    for c in kids:
        fn = go_name(c)
        while fn in used:
            fn += "_"
        used.add(fn)
        typ = struct_for(c)
        if c in self_nesting:
            # <c> wrapper holding <c> rows: reach the rows by nested path.
            lines.append(f'\t{fn} []{typ} `xml:"{c}>{c}"`')
        elif is_container(c):
            # One sub-container per parent.
            lines.append(f'\t{fn} {typ} `xml:"{c}"`')
        else:
            # Row lists are always slices: a section may hold any number of rows,
            # regardless of how many appeared in the sample used to scaffold this.
            lines.append(f'\t{fn} []{typ} `xml:"{c}"`')
    # Self-nesting sections (e.g. OptionEAE) are reached from their parent, so a
    # leaf appearing as a container's child via nested path needs no field here.
    if not kids and len(attrs[t]) == 0:
        # Empty section in this sample. Capture anything that appears later so
        # data is never silently dropped.
        lines.append('\tUnmodeled []RawElement `xml:",any"`')
    structs[name] = lines

# ---- write ----------------------------------------------------------------
with open(OUT, "w") as f:
    f.write("// Code generated by gen.py from testdata/sample.xml; DO NOT EDIT.\n\n")
    f.write("package flex\n\n")
    f.write("// Each struct mirrors one element of the Flex report XML. Fields bind by\n")
    f.write("// attribute name, so a query may select any subset of columns; unselected\n")
    f.write("// columns and unmodeled sections are simply absent. Numeric attributes use\n")
    f.write("// Float, which decodes an empty attribute as 0. Dates, identifiers, and codes\n")
    f.write("// are kept as strings because their formats are query-configurable. Several\n")
    f.write("// elements (trade detail levels; FX position and lot) share an identical\n")
    f.write("// attribute set and therefore a single Go type. To regenerate, run\n")
    f.write("// 'go generate ./flex' after refreshing testdata/sample.xml.\n\n")
    for name in sorted(structs):
        f.write(f"type {name} struct {{\n")
        f.write("\n".join(structs[name]) + "\n")
        f.write("}\n\n")

print(f"wrote {OUT}: {len(structs)} structs")
print("leaf groups:")
for name in sorted(emitted):
    members = [t for t, n in leaf_struct.items() if n == name]
    print(f"  {name}: {sorted(members)}")
print("containers:", sorted(go_name(t) for t in containers))
