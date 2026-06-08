#!/usr/bin/env python3
"""Produce a synthetic, schema-complete Flex sample from a real report.

This is the manual "refresh the schema" tool. Given a real Activity Flex Query
report (which contains private account data), it writes testdata/sample.xml: a
synthetic report that has the same element/attribute *names* and nesting, but no
real values. gen.py then generates flex_sections.go from that sample.

It does NOT redact the input. It reads the real report only to learn, per
element, the set of attribute names, the parent->child element graph, and one
type bit per attribute (numeric / string / empty, derived from real values but
never a value itself). It then builds a fresh tree whose every value comes from
the fixed synthetic vocabulary below. No real value is ever copied to the
output, so the result is safe by construction rather than by scrubbing.

The complete output vocabulary is:

    "", "1.5", "SAMPLE", "SAMPLE DATA", "VOO", "USD", "STK", "BUY",
    "0000", "X", "20240101", "20240101;120000"

TestSampleHasNoPrivateData (sample_test.go) asserts the committed sample
contains only these values; keep the two in sync.

Usage (run from the flex/ directory):

    python3 sanitize.py --input /path/to/real-report.xml
    go generate ./...    # regenerate flex_sections.go from the new sample
"""
import argparse
import os
import re
import xml.etree.ElementTree as ET
from collections import defaultdict

DENY_EXPLICIT = {"cusip", "isin", "figi", "sedol", "conid"}

# Readable synthetic values for a few common string columns; reusing security
# names is fine. These stay non-numeric so type inference is unchanged.
NICE = {
    "symbol": "VOO", "underlyingSymbol": "VOO", "currency": "USD",
    "ibCommissionCurrency": "USD", "fxCurrency": "USD", "functionalCurrency": "USD",
    "assetCategory": "STK", "description": "SAMPLE DATA", "buySell": "BUY",
}


def denylisted(attr):
    l = attr.lower()
    if l in DENY_EXPLICIT:
        return True
    if l.endswith("id") or l.endswith("code") or l.endswith("number"):
        return True
    if re.search(r"(date|time|expiry|when|period|maturity|month|year|quarter)", l):
        return True
    return False


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--input", "-i", required=True,
                    help="path to a real Activity Flex Query XML report")
    ap.add_argument("--output", "-o", default="testdata/sample.xml",
                    help="output path (default: testdata/sample.xml)")
    args = ap.parse_args()

    real = ET.parse(args.input).getroot()

    attr_union = defaultdict(set)
    attr_vals = defaultdict(set)
    children_of = defaultdict(set)

    def walk(e):
        for k, v in e.attrib.items():
            attr_union[e.tag].add(k)
            if len(attr_vals[k]) < 500:
                attr_vals[k].add(v)
        for c in e:
            children_of[e.tag].add(c.tag)
            walk(c)

    walk(real)

    def category(attr):
        ne = [v for v in attr_vals[attr] if v != ""]
        if not ne:
            return "empty"
        for v in ne:
            try:
                float(v)
            except ValueError:
                return "str"
        return "num"

    def synth(attr):
        cat = category(attr)
        if cat == "empty":
            return ""
        if attr in NICE:
            return NICE[attr]
        if denylisted(attr):
            l = attr.lower()
            if "time" in l:
                return "20240101;120000"
            if re.search(r"(date|expiry)", l):
                return "20240101"
            if l.endswith("id"):
                return "0000"
            return "X"
        return "1.5" if cat == "num" else "SAMPLE"

    def build(tag, path):
        attrs = {a: synth(a) for a in sorted(attr_union[tag])}
        out = ET.Element(tag, attrs)
        for ct in sorted(children_of.get(tag, ())):
            if ct == tag and tag in path:
                continue  # self-nesting (e.g. OptionEAE): emit only one level
            out.append(build(ct, path | {tag}))
        return out

    sample = build("FlexQueryResponse", set())
    try:
        ET.indent(sample)
    except AttributeError:
        pass
    os.makedirs(os.path.dirname(args.output) or ".", exist_ok=True)
    ET.ElementTree(sample).write(args.output, encoding="unicode")
    print(f"wrote {args.output}")


if __name__ == "__main__":
    main()
