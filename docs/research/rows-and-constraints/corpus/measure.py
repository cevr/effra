#!/usr/bin/env python3
"""Measure the #135 candidates against the sketch corpus.

Usage, from the repository root:

    python3 -I docs/research/rows-and-constraints/corpus/measure.py           # print the tables
    python3 -I docs/research/rows-and-constraints/corpus/measure.py --write   # regenerate after/, counts.json and the note's tables
    python3 -I docs/research/rows-and-constraints/corpus/measure.py --check   # fail when any of them is stale

Standard library only. The script reads:
  - corpus/before/: copies of sketch-eval-8/five, sketch-eval-9, sketch-eval-9/loops,
    sketch 10 and the matched Go controls, pinned by before.sha256;
  - corpus/before/library/implied.go.sketch: the library signatures the corpus calls;
  - docs/spec/*.md (fenced `go` blocks) and prototypes/sketch9/testdata (in place);
  - corpus/exports.go.sketch: export metadata for name-resolution option (b), not counted;
  - corpus/controls/before and controls/after: hand-written name-resolution controls, not
    counted; the landing rewrite of each must equal its after file.
The binding check resolves every capability reference before and after each elision
rewrite and fails the run when one changes capability or a qualifier is doubled.

It parses every signature with one small parser, applies each candidate as a
mechanical rewrite, writes the rewritten corpus to corpus/after/<candidate>/,
parses the rewritten text again and counts from that parse. Every number in
docs/research/rows-and-constraints.md between `measure.py` markers comes from here.
"""
import argparse
import hashlib
import json
import re
import sys
from bisect import bisect_right
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
BEFORE = HERE / "before"
AFTER = HERE / "after"
NOTE = ROOT / "docs" / "research" / "rows-and-constraints.md"
COUNTS = HERE / "counts.json"

LIBRARY_PACKAGES = {
    "tasks", "timeout", "retry", "build", "sync", "json", "http", "slices",
    "cmp", "builtin", "options", "batch", "testing",
}
# docs/spec shows two library functions, types.md Map and runtime.md timeout.After; every other example is user code.
SPEC_LIBRARY_NAMES = {"Map", "After"}
# Candidate 2: the method entry of the #116/#125 spelling and the named constraint that replaces it.
WHERE_TO_CONSTRAINT = {"Compare": "cmp.Ordered", "Equal": "comparable", "FindMany": "Batched[K, V]"}
TPARAM = re.compile(r"^[A-Z][0-9]*$")
# Each S101/S102 negative fixture of the prototype, classified by reading it: what keeps
# it failing once unexported rows may be inferred. A judgement per file, recorded here so
# that the counts in the note come from this script.
#   written: a row written on an unexported function, not main, is too small;
#   main:    main's written row is too small (the diagnostic moves if main's rows are inferred);
#   absent:  a function with no written row is relied on to have an empty row;
#   wire:    a closed named wire, a rule about wires, not function rows.
FIXTURE_AUDIT = {
    "prototypes/sketch9/testdata/reject/undeclared-error.sketch": ("written", "pay declares GatewayDown only"),
    "prototypes/sketch9/testdata/reject/missing-capability.sketch": ("written", "pay declares uses Orders only"),
    "prototypes/sketch9/testdata/reject/unprovided.sketch": ("main", "main uses Console only, calls pay"),
    "prototypes/sketch9/testdata/probes/wire-initializer-error.sketch": ("main", "try fail() escapes to main, which has no row"),
    "prototypes/sketch9/testdata/probes/wire-initializer.sketch": ("main", "an inline provider needs Store in main"),
    "prototypes/sketch9/testdata/probes/wire-sibling.sketch": ("main", "an inline provider needs Store in main"),
    "prototypes/sketch9/testdata/probes/function-uses-control.sketch": ("main", "main calls get, which uses Other"),
    "prototypes/sketch9/testdata/probes/error-as-value.sketch": ("absent", "identity returns a Fault value with no row"),
    "prototypes/sketch9/testdata/probes/named-wire-failure.sketch": ("wire", "a named wire lets a failure escape"),
    "prototypes/sketch9/testdata/probes/named-wire-requirement.sketch": ("wire", "a named wire needs Store from its caller"),
}

TOKEN = re.compile(r"""
 (?P<nl>\n)
|(?P<ws>[ \t\r\f]+)
|(?P<lcomment>//[^\n]*)
|(?P<bcomment>/\*.*?\*/)
|(?P<raw>`[^`]*`)
|(?P<str>"(?:\\.|[^"\\\n])*")
|(?P<rune>'(?:\\.|[^'\\\n])+')
|(?P<num>[0-9][0-9A-Za-z_.]*)
|(?P<ident>[^\W\d]\w*)
|(?P<op>\.\.\.|<-|:=|==|!=|<=|>=|&&|\|\||\+\+|--|\+=|-=|\*=|/=)
|(?P<punct>.)
""", re.S | re.X)


class Tok:
    __slots__ = ("kind", "text", "start", "end")

    def __init__(self, kind, text, start, end):
        self.kind, self.text, self.start, self.end = kind, text, start, end

    def __repr__(self):
        return f"{self.kind}:{self.text!r}"


def tokenize(src):
    out = []
    for m in TOKEN.finditer(src):
        kind = m.lastgroup
        if kind in ("ws", "lcomment", "bcomment"):
            continue
        out.append(Tok(kind, m.group(), m.start(), m.end()))
    out.append(Tok("eof", "", len(src), len(src)))
    return out


class Sig:
    """One signature: a declaration, a method spec, a literal, a function type or a type declaration."""

    def __init__(self, **kw):
        self.kind = kw["kind"]          # func, method, iface, literal, functype, typedecl, wire
        self.name = kw.get("name", "")
        self.package = kw.get("package", "")
        self.parent = kw.get("parent")
        self.start = kw.get("start", 0)
        self.line = kw.get("line", 0)
        self.name_end = kw.get("name_end")
        self.brackets = []              # (name, constraint text or "")
        self.where = []                 # (subject, method, type text)
        self.where_span = None
        self.params_end = None
        self.tail_end = None
        self.result_kind = "none"       # none, type, error, tuple
        self.value_text = None
        self.errors = []
        self.uses = []
        self.idents = []                # (name, position) with position type, error or uses
        self.children = []
        self.machine = False
        self.step = None                # a machine type's step function name
        self.untyped_params = False
        self.child_role = "param"       # for a nested function type: param, result, where or other
        self.bodiless = False           # a declaration with no body
        self.foreign = False            # a bodiless declaration under an //ef:go directive
        self.body = None                # (start, end) token indexes of the body braces
        self.uses_span = None           # (start, end) token indexes of the `uses` clause
        self.params_open = None         # token index of the parameter list's '('
        self.recv = None                # (start, end) token indexes of a method receiver's parentheses
        self.iface = False              # a type declaration of an interface (a capability)

    def exported(self):
        return bool(self.name) and self.name[0].isupper()

    def all_sigs(self):
        yield self
        for c in self.children:
            yield from c.all_sigs()


class Parser:
    def __init__(self, src, source):
        self.src = src
        self.source = source
        self.toks = tokenize(src)
        self.nl_offsets = [i for i, ch in enumerate(src) if ch == "\n"]
        self.package = ""
        self.sigs = []                  # top-level records, children hang off them
        self.declared = {}              # package -> declared type, capability and wire names
        self.ifaces = {}                # package -> declared interface (capability) names
        self.packages = []              # every package clause, in order
        self.imports = []               # (alias or None, path) of each import spec

    def line_of(self, off):
        return bisect_right(self.nl_offsets, off - 1) + 1

    def t(self, i):
        return self.toks[min(i, len(self.toks) - 1)]

    def skip_nl(self, i):
        while self.t(i).kind == "nl":
            i += 1
        return i

    def balanced(self, i, open_, close):
        """i at the opening token; returns the index after the matching close."""
        depth = 0
        while True:
            tok = self.t(i)
            if tok.kind == "eof":
                return i
            if tok.text == open_:
                depth += 1
            elif tok.text == close:
                depth -= 1
                if depth == 0:
                    return i + 1
            i += 1

    # ---- types -------------------------------------------------------
    def parse_type(self, i, sig, pos="type", in_group=True):
        i = self.skip_nl(i) if in_group else i
        tok = self.t(i)
        if tok.text in ("*", "...", "&"):
            return self.parse_type(i + 1, sig, pos, in_group)
        if tok.text == "<-":
            return self.parse_type(i + 1, sig, pos, in_group)
        if tok.text == "[":
            j = i + 1
            while self.t(j).text != "]" and self.t(j).kind != "eof":
                j += 1
            return self.parse_type(j + 1, sig, pos, in_group)
        if tok.text == "map":
            j = self.parse_type(i + 2, sig, pos)
            j = self.skip_nl(j)
            return self.parse_type(j + 1, sig, pos, in_group)
        if tok.text == "chan":
            j = i + 1
            if self.t(j).text == "<-":
                j += 1
            return self.parse_type(j, sig, pos, in_group)
        if tok.text == "func":
            child = Sig(kind="functype", package=self.package, parent=sig, start=tok.start, line=self.line_of(tok.start))
            j = self.parse_sig_rest(i + 1, child, in_params=True)
            sig.children.append(child)
            return j
        if tok.text in ("struct", "interface") and self.t(i + 1).text == "{":
            return self.balanced(i + 1, "{", "}")
        if tok.text == "(":
            end = self.balanced(i, "(", ")")
            return end
        if tok.kind == "ident":
            name = tok.text
            j = i + 1
            if self.t(j).text == "." and self.t(j + 1).kind == "ident":
                name = name + "." + self.t(j + 1).text
                j += 2
            sig.idents.append((name, pos))
            if self.t(j).text == "[" and self.t(j + 1).text != "]":
                j = self.parse_type_args(j, sig, "error" if name == "error" else pos)
            return j
        return i + 1

    def parse_type_args(self, i, sig, pos):
        """i at '['; type arguments separated by ',' and '|'."""
        j = i + 1
        while True:
            j = self.parse_type(j, sig, pos)
            j = self.skip_nl(j)
            if self.t(j).text in (",", "|"):
                j += 1
                continue
            if self.t(j).text == "]":
                return j + 1
            if self.t(j).kind == "eof":
                return j
            j += 1

    def parse_error_row(self, i, sig, outer):
        """i at 'error', next is '['. Returns the index after ']'."""
        j = i + 2
        entries = []
        while True:
            j = self.skip_nl(j)
            start = self.t(j).start
            k = self.parse_type(j, sig, "error")
            entries.append(self.src[start:self.t(k - 1).end].strip())
            j = self.skip_nl(k)
            if self.t(j).text == "|":
                j += 1
                continue
            break
        if self.t(j).text == "]":
            j += 1
        if outer:
            sig.errors.extend(e for e in entries if e)
        return j

    # ---- signatures --------------------------------------------------
    def parse_params(self, i, sig):
        """i at '('. Returns index after ')'."""
        end = self.balanced(i, "(", ")")
        items, cur, depth = [], [], 0
        for k in range(i + 1, end - 1):
            tok = self.t(k)
            if tok.kind == "nl":
                continue
            if tok.text in "([{":
                depth += 1
            elif tok.text in ")]}":
                depth -= 1
            if tok.text == "," and depth == 0:
                items.append(cur)
                cur = []
            else:
                cur.append(k)
        if cur:
            items.append(cur)

        def named(item):
            if len(item) < 2 or self.t(item[0]).kind != "ident":
                return False
            second = self.t(item[1]).text
            if second == ".":
                return False
            if second == "[":
                return len(item) > 2 and (self.t(item[2]).text == "]" or self.t(item[2]).kind == "num")
            return True

        any_named = any(named(it) for it in items)
        for it in items:
            if named(it):
                self.parse_type(it[1], sig)
            elif len(it) == 1 and self.t(it[0]).kind == "ident" and (any_named or sig.kind == "literal"):
                if not any_named:
                    sig.untyped_params = True
                continue  # a grouped name, or a literal parameter whose type is inferred
            elif it:
                self.parse_type(it[0], sig)
        return end

    def parse_uses(self, i, sig, in_params):
        """i at 'uses'."""
        j = i + 1
        sig.uses_span = (i, i + 1)
        while True:
            tok = self.t(j)
            if tok.kind != "ident":
                break
            name, k = tok.text, j + 1
            if self.t(k).text == "." and self.t(k + 1).kind == "ident":
                name += "." + self.t(k + 1).text
                k += 2
            sig.uses.append(name)
            sig.idents.append((name, "uses"))
            j = k
            if self.t(j).text != ",":
                break
            # In a parameter list, a comma may end the clause: `uses R, next T`.
            nxt, after = self.t(j + 1), self.t(j + 2)
            if in_params and nxt.kind == "ident" and after.text not in (",", ")", "{", ".") and after.kind != "nl":
                break
            j += 1
        sig.uses_span = (i, j)
        return j

    def parse_where(self, i, sig):
        """i at 'where'. Entries `X.m: Type` separated by ','; ends at a newline or '{'."""
        start = self.t(i).start
        j = i + 1
        while self.t(j).kind == "ident" and self.t(j + 1).text == "." and self.t(j + 3).text == ":":
            subject, method = self.t(j).text, self.t(j + 2).text
            k = self.parse_type(j + 4, sig, in_group=False)
            sig.where.append((subject, method, self.src[self.t(j + 4).start:self.t(k - 1).end]))
            sig.idents.append((subject, "type"))
            j = k
            if self.t(j).text == ",":
                j += 1
                continue
            break
        sig.where_span = (start, self.t(j - 1).end)
        return j

    def parse_sig_rest(self, i, sig, in_params=False):
        """i at '(' of the parameters (after any receiver, name and bracket list)."""
        if self.t(i).text != "(":
            sig.params_end = self.t(i).start
            sig.tail_end = sig.params_end
            return i
        sig.params_open = i
        mark = len(sig.children)
        j = self.parse_params(i, sig)
        for c in sig.children[mark:]:
            c.child_role = "param"
        mark = len(sig.children)
        sig.params_end = self.t(j - 1).end
        last = j - 1
        tok = self.t(j)
        stops = {"{", ",", ")", "]", "}", ";", "=", ""}
        if tok.kind == "ident" and tok.text == "error" and self.t(j + 1).text == "[":
            sig.result_kind = "error"
            j = self.parse_error_row(j, sig, outer=True)
            last = j - 1
        elif tok.text == "(":
            end = self.balanced(j, "(", ")")
            parts, cur, depth = [], [], 0
            for k in range(j + 1, end - 1):
                tk = self.t(k)
                if tk.kind == "nl":
                    continue
                if tk.text in "([{":
                    depth += 1
                elif tk.text in ")]}":
                    depth -= 1
                if tk.text == "," and depth == 0:
                    parts.append(cur)
                    cur = []
                else:
                    cur.append(k)
            if cur:
                parts.append(cur)
            is_err = lambda p: self.t(p[0]).text == "error" and len(p) > 1 and self.t(p[1]).text == "["
            if len(parts) == 2 and is_err(parts[1]):
                sig.result_kind = "tuple"
                self.parse_type(parts[0][0], sig)
                sig.value_text = self.src[self.t(parts[0][0]).start:self.t(parts[0][-1]).end]
                self.parse_error_row(parts[1][0], sig, outer=True)
            else:
                sig.result_kind = "type"
                for p in parts:
                    self.parse_type(p[0], sig)
                sig.value_text = self.src[tok.start:self.t(end - 1).end]
            j = end
            last = j - 1
        elif tok.kind != "nl" and tok.text not in stops and tok.text not in ("uses", "where") and tok.kind != "eof":
            sig.result_kind = "type"
            k = self.parse_type(j, sig, in_group=False)
            sig.value_text = self.src[tok.start:self.t(k - 1).end]
            j = k
            last = j - 1
        for c in sig.children[mark:]:
            c.child_role = "result"
        mark = len(sig.children)
        if self.t(j).text == "uses":
            j = self.parse_uses(j, sig, in_params)
            last = j - 1
        if self.t(j).text == "where":
            j = self.parse_where(j, sig)
            last = j - 1
            for c in sig.children[mark:]:
                c.child_role = "where"
        sig.tail_end = self.t(last).end
        return j

    def parse_bracket_list(self, i, sig):
        """i at '['; `[T C, U]`."""
        end = self.balanced(i, "[", "]")
        k = i + 1
        while k < end - 1:
            name = self.t(k).text
            c_start = k + 1
            depth = 0
            m = c_start
            while m < end - 1 and not (self.t(m).text == "," and depth == 0):
                if self.t(m).text == "[":
                    depth += 1
                elif self.t(m).text == "]":
                    depth -= 1
                m += 1
            constraint = self.src[self.t(c_start).start:self.t(m - 1).end] if m > c_start else ""
            sig.brackets.append((name, constraint))
            k = m + 1
        return end

    # ---- the file ----------------------------------------------------
    def run(self):
        stack = []          # ("struct"|"iface"|"block", typedecl Sig or None)
        i = 0
        prev_sig = "nl"
        while self.t(i).kind != "eof":
            tok = self.t(i)
            text = tok.text
            if tok.kind == "nl":
                prev_sig = "nl"
                i += 1
                continue
            if text == "package" and not stack and self.t(i + 1).kind == "ident":
                self.package = self.t(i + 1).text
                self.packages.append(self.package)
                i += 2
                continue
            if text == "import" and not stack:
                j = i + 1
                end = self.balanced(j, "(", ")") if self.t(j).text == "(" else j + 1 + (self.t(j).kind == "ident")
                k = j + 1 if self.t(j).text == "(" else j
                while k < end:
                    if self.t(k).kind == "str":
                        alias = self.t(k - 1).text if self.t(k - 1).kind == "ident" and self.t(k - 1).text != "import" else None
                        self.imports.append((alias, self.t(k).text.strip('"')))
                    k += 1
                i = end
                continue
            if text == "type" and self.t(i + 1).kind == "ident" and (not stack or stack[-1][0] == "block"):
                name_tok = self.t(i + 1)
                if not stack:
                    self.declared.setdefault(self.package, set()).add(name_tok.text)
                td = Sig(kind="typedecl", name=name_tok.text, package=self.package, start=tok.start,
                         line=self.line_of(tok.start), name_end=name_tok.end)
                j = i + 2
                if self.t(j).text == "[" and self.t(j + 1).kind == "ident" and self.t(j + 2).text in ("]", ",", ")") or (
                        self.t(j).text == "[" and self.t(j + 1).kind == "ident" and self.t(j + 2).kind == "ident"):
                    j = self.parse_bracket_list(j, td)
                if not stack:
                    self.sigs.append(td)
                if self.t(j).text == "interface" and self.t(j + 1).text == "{":
                    td.iface = True
                    if not stack:
                        self.ifaces.setdefault(self.package, set()).add(name_tok.text)
                if self.t(j).text in ("struct", "interface") and self.t(j + 1).text == "{":
                    stack.append(("struct" if self.t(j).text == "struct" else "iface", td))
                    i = j + 2
                    prev_sig = "{"
                    continue
                if self.t(j).text == "machine":
                    td.machine = True
                j = self.parse_type(j, td, in_group=False)
                td.tail_end = self.t(j - 1).end
                if td.machine and self.t(j).text == "{":
                    end = self.balanced(j, "{", "}")
                    for k in range(j + 1, end - 1):
                        if self.t(k).text == "step" and self.t(k + 1).kind == "ident":
                            td.step = self.t(k + 1).text
                for c in td.children:
                    c.child_role = "other"
                i = j
                continue
            if text == "wire" and not stack and self.t(i + 1).kind == "ident":
                name_tok = self.t(i + 1)
                self.declared.setdefault(self.package, set()).add(name_tok.text)
                j = i + 2
                if self.t(j).text == "uses":
                    w = Sig(kind="wire", name=name_tok.text, package=self.package, start=tok.start, line=self.line_of(tok.start))
                    j = self.parse_uses(j, w, False)
                    w.tail_end = self.t(j - 1).end
                    self.sigs.append(w)
                i = j
                continue
            if stack and stack[-1][0] == "iface" and tok.kind == "ident" and prev_sig in ("nl", "{", ";") and self.t(i + 1).text == "(":
                m = Sig(kind="iface", name=text, package=self.package, parent=stack[-1][1], start=tok.start,
                        line=self.line_of(tok.start), name_end=tok.end)
                j = self.parse_sig_rest(i + 1, m)
                stack[-1][1].children.append(m)
                i = j
                prev_sig = "sig"
                continue
            if text == "func":
                if not stack:
                    sig = Sig(kind="func", package=self.package, start=tok.start, line=self.line_of(tok.start))
                    j = i + 1
                    if self.t(j).text == "(":
                        sig.kind = "method"
                        rend = self.balanced(j, "(", ")")
                        sig.recv = (j, rend)
                        inner =[self.t(k) for k in range(j + 1, rend - 1)]
                        if "[" in [x.text for x in inner]:
                            b = [x.text for x in inner]
                            open_at = b.index("[")
                            for x in inner[open_at + 1:]:
                                if x.kind == "ident":
                                    sig.brackets.append((x.text, ""))
                        j = rend
                    if self.t(j).kind == "ident":
                        sig.name = self.t(j).text
                        sig.name_end = self.t(j).end
                        j += 1
                    if self.t(j).text == "[":
                        j = self.parse_bracket_list(j, sig)
                    j = self.parse_sig_rest(j, sig)
                    if self.t(j).text == "{":
                        sig.body = (j, self.balanced(j, "{", "}"))
                    else:
                        sig.bodiless = True
                        line_start = self.src.rfind("\n", 0, sig.start)
                        prev_line_start = self.src.rfind("\n", 0, max(line_start, 0))
                        sig.foreign = self.src[prev_line_start + 1:line_start].strip().startswith("//ef:go")
                    self.sigs.append(sig)
                    i = j
                    continue
                sig = Sig(kind="literal", package=self.package, start=tok.start, line=self.line_of(tok.start))
                parent = stack[-1][1] if stack[-1][0] == "struct" else None
                j = self.parse_sig_rest(i + 1, sig)
                if self.t(j).text != "{":
                    sig.kind = "functype"
                    sig.untyped_params = False
                    sig.child_role = "field" if parent is not None else "other"
                else:
                    sig.body = (j, self.balanced(j, "{", "}"))
                if parent is not None and sig.kind == "functype":
                    sig.parent = parent
                    parent.children.append(sig)
                else:
                    self.sigs.append(sig)
                i = j
                continue
            if text == "{":
                stack.append(("block", None))
            elif text == "}":
                if stack:
                    stack.pop()
            prev_sig = text
            i += 1
        return self


# ---- corpus --------------------------------------------------------------
def md_blocks(text):
    """Yield (index, block text, offset) for each fenced go block."""
    for n, m in enumerate(re.finditer(r"^```go\n(.*?)^```", text, re.S | re.M)):
        yield n, m.group(1), m.start(1)


class Unit:
    """One parsed source unit: a file, or one fenced block of a Markdown file."""

    def __init__(self, group, rel, text, kind="file", block=None, negative=False):
        self.group, self.rel, self.text, self.kind, self.block, self.negative = group, rel, text, kind, block, negative
        self.parser = Parser(text, rel).run()

    def audience(self, sig):
        if self.group == "fixtures":
            return "fixture"
        if sig.package in LIBRARY_PACKAGES or self.group == "library":
            return "library"
        if self.group == "spec" and top(sig).name in SPEC_LIBRARY_NAMES:
            return "library"
        return "user"


def top(sig):
    while sig.parent is not None:
        sig = sig.parent
    return sig


def load_units():
    units = []
    for group in ("eval8-five", "eval9", "eval9-loops", "library"):
        for path in sorted((BEFORE / group).glob("*.go.sketch")):
            units.append(Unit(group, f"{group}/{path.name}", path.read_text()))
    s10 = BEFORE / "sketch10" / "sketch-10.md"
    for n, block, _ in md_blocks(s10.read_text()):
        units.append(Unit("sketch10", f"sketch10/sketch-10.md#block{n}", block, "block", n))
    for path in sorted((ROOT / "docs" / "spec").glob("*.md")):
        for n, block, _ in md_blocks(path.read_text()):
            units.append(Unit("spec", f"docs/spec/{path.name}#block{n}", block, "block", n))
    tdir = ROOT / "prototypes" / "sketch9" / "testdata"
    for path in sorted(tdir.rglob("*.sketch")):
        text = path.read_text()
        units.append(Unit("fixtures", str(path.relative_to(ROOT)), text, negative="// want" in text))
    return units


def verify_before():
    manifest = HERE / "before.sha256"
    bad = []
    for line in manifest.read_text().splitlines():
        digest, rel = line.split(maxsplit=1)
        path = BEFORE / rel
        if not path.exists() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            bad.append(rel)
    return bad


# ---- classification ------------------------------------------------------
def role(sig):
    if sig.kind == "literal":
        return "literal"
    if sig.kind in ("iface",):
        return "interface method"
    if sig.kind == "functype":
        return "function type"
    if sig.kind == "typedecl":
        return "type declaration"
    if sig.kind == "wire":
        return "wire exposure"
    if sig.kind == "method":
        return "method"
    if sig.name == "main":
        return "main"
    if re.match(r"^Test[A-Z0-9_]", sig.name):
        return "test"
    if re.match(r"^Law[A-Z0-9_]", sig.name):
        return "law"
    return "exported function" if sig.exported() else "unexported function"


def declared_names(unit, sig):
    return unit.parser.declared.get(sig.package, set())


def tparams(unit, sig):
    """Type and row parameters of a signature and its nested function types: name -> kind."""
    declared = declared_names(unit, sig)
    out = {}
    for s in sig.all_sigs():
        for name, constraint in s.brackets:
            out.setdefault(name, "error" if constraint == "error" else "type")
        for name, pos in s.idents:
            if TPARAM.match(name) and name not in declared:
                if pos in ("error", "uses") or name not in out:
                    out[name] = pos if pos in ("error", "uses") else out.get(name, "type")
    return out


def row_vars(unit, sig):
    declared = declared_names(unit, sig)
    return {e for e in sig.errors + sig.uses if TPARAM.match(e) and e not in declared}


def entries(sig):
    return len(sig.errors) + len(sig.uses)


def total_entries(sig):
    """Row entries of a signature and of the function types nested in it."""
    return sum(entries(s) for s in sig.all_sigs())


def counted(sig):
    """Signatures listed in the inventory: every function-shaped record, and type declarations
    that are generic, machines, or hold a function-typed field."""
    if sig.kind == "typedecl":
        return bool(sig.brackets) or sig.machine or any(c.kind == "functype" for c in sig.children)
    return True


def sig_tokens(unit, sig):
    end = sig.tail_end if sig.tail_end is not None else sig.start
    return sum(1 for t in tokenize(unit.text[sig.start:end]) if t.kind not in ("nl", "eof"))


def iter_records(units):
    """Every top-level record and every interface method; nested function types stay with their parent."""
    for u in units:
        for s in u.parser.sigs:
            if s.kind == "typedecl":
                if counted(s):
                    yield u, s
                for c in s.children:
                    if c.kind == "iface":
                        yield u, c
            else:
                yield u, s


# ---- candidates ----------------------------------------------------------
C1_VARIANTS = ("unexported", "unguarded", "landing", "main")


def guarded_steps(unit):
    """Step functions named by an exported machine type in the same unit: they keep their rows."""
    return {s.step for s in unit.parser.sigs if s.kind == "typedecl" and s.machine and s.exported() and s.step}


def c1_elides(sig, variant, unit):
    """Candidate 1: rows of these signatures are inferred and need not be written.

    Variants:
      unexported: unexported functions and function literals;
      unguarded:  plus TestX, with no export guard (the round-1 total; leaks rows, comparison only);
      landing:    plus TestX, but a step named by an exported machine type and a bodiless
                  foreign (//ef:go) declaration keep their rows;
      main:       the landing rule plus main (measured and refused).
    In every variant, a function keeps its rows when one of the capability references its
    `uses` clause binds cannot be qualified because a local hides every import name of the
    package (name_plan: it needs an import alias)."""
    if not c1_base(sig, variant, unit):
        return False
    if sig.kind in ("func", "method", "literal") and sig.body and entries(sig):
        return id(sig) not in name_plan(unit, variant)["blocked"]
    return True


def c1_base(sig, variant, unit):
    r = role(sig)
    guarded = variant in ("landing", "main")
    if guarded and sig.kind == "func" and (sig.foreign or sig.name in guarded_steps(unit)):
        return False
    if r in ("literal", "unexported function"):
        return True
    if r == "test":
        return variant != "unexported"
    if r == "main":
        return variant == "main"
    return False


# ---- name resolution (M4 of round 1, M1 and M2 of round 2) ----------------
def binds(sig):
    """Bare names a `uses` clause brings into scope for its body: {X: written package} for `uses pkg.X`."""
    out = {}
    for u in sig.uses:
        if "." in u:
            pkg, name = u.split(".", 1)
            out[name] = pkg
    return out


def param_names(p, open_, close):
    """Names declared by a parameter or receiver list; open_ at '(' and close after ')'."""
    items, cur, depth = [], [], 0
    for k in range(open_ + 1, close - 1):
        tok = p.t(k)
        if tok.kind == "nl":
            continue
        if tok.text in "([{":
            depth += 1
        elif tok.text in ")]}":
            depth -= 1
        if tok.text == "," and depth == 0:
            items.append(cur)
            cur = []
        else:
            cur.append(k)
    if cur:
        items.append(cur)
    named = [it for it in items if len(it) >= 2 and p.t(it[0]).kind == "ident" and p.t(it[1]).text not in (".", "[")]
    singles = [it for it in items if len(it) == 1 and p.t(it[0]).kind == "ident"]
    return [p.t(it[0]).text for it in named] + ([p.t(it[0]).text for it in singles] if named else [])


class Lexical:
    """Lexical scopes of one unit: function bodies, the `uses` bindings they carry, local
    declarations (parameters, `:=`, `var`, `catch e`) and the import names.

    A bare name X at token k resolves, innermost first, to: a local declaration in effect; the
    nearest enclosing body whose (written, not elided) `uses pkg.X` binds it; a declaration of
    X in the unit's package; otherwise nothing. A qualified `p.X` resolves through the import
    whose name is p, unless a local named p is in effect."""

    def __init__(self, unit):
        p = self.p = unit.parser
        n = len(p.toks)
        self.scopes = sorted((s for s in p.sigs if s.kind in ("func", "method", "literal") and s.body),
                             key=lambda s: s.body[0])
        # Innermost scope of each token, and each scope's enclosing scope.
        self.scope_of = [None] * n
        self.parent = {}
        stack = []
        for s in self.scopes:
            while stack and stack[-1].body[1] <= s.body[0]:
                stack.pop()
            self.parent[id(s)] = stack[-1] if stack else None
            stack.append(s)
        for s in self.scopes:   # outer first, so inner scopes overwrite
            for k in range(s.body[0] + 1, s.body[1] - 1):
                self.scope_of[k] = s
        # Row clauses that elision can remove are not references.
        self.in_row = [False] * n
        for s in p.sigs:
            if s.kind in ("func", "method", "literal") and s.uses_span:
                for k in range(*s.uses_span):
                    self.in_row[k] = True
        # Block extents, for the reach of a local declaration.
        close_of, stack = {}, []
        for k, t in enumerate(p.toks):
            if t.kind == "punct" and t.text == "{":
                stack.append(k)
            elif t.kind == "punct" and t.text == "}" and stack:
                close_of[stack.pop()] = k
        self.block_end, stack = [n] * n, []
        for k, t in enumerate(p.toks):
            if t.kind == "punct" and t.text == "}" and stack:
                stack.pop()
            self.block_end[k] = close_of.get(stack[-1], n) if stack else n
            if t.kind == "punct" and t.text == "{":
                stack.append(k)
        self.locals = {}    # name -> [(from token, to token)]
        for s in self.scopes:
            names = param_names(p, s.params_open, self.after_params(s)) if s.params_open is not None else []
            if s.recv:
                names += param_names(p, *s.recv)
            for name in names:
                self.locals.setdefault(name, []).append(s.body)
        for k, t in enumerate(p.toks):
            if t.kind != "ident" or self.scope_of[k] is None or p.t(k - 1).text == ".":
                continue
            j = k
            while p.t(j).kind == "ident" and p.t(j + 1).text == ",":
                j += 2
            if p.t(k - 1).text in ("var", "catch") or (p.t(j).kind == "ident" and p.t(j + 1).text == ":="):
                self.locals.setdefault(t.text, []).append((k, self.block_end[k]))
        # Import names: name -> canonical package (the last path segment).
        self.imports = {}
        for alias, path in p.imports:
            seg = path.rsplit("/", 1)[-1]
            self.imports[seg if alias in (None, "go", "_") else alias] = seg
        self.has_imports = bool(p.imports)

    def after_params(self, s):
        return self.p.balanced(s.params_open, "(", ")")

    def canon(self, pkg):
        return self.imports.get(pkg, pkg)

    def shadowed(self, name, k):
        return any(a <= k < b for a, b in self.locals.get(name, ()))

    def chain(self, k):
        s = self.scope_of[k]
        while s is not None:
            yield s
            s = self.parent[id(s)]

    def is_ref(self, k):
        """A bare identifier in a body that names something: not a selector's member, not a
        composite-literal key or label, not a row clause."""
        p = self.p
        return (p.t(k).kind == "ident" and self.scope_of[k] is not None and not self.in_row[k]
                and p.t(k - 1).text != "." and p.t(k + 1).text != ":")

    def resolve(self, name, k, elided=frozenset()):
        """('local',) | ('cap', canonical pkg.X, written pkg, owner Sig) | ('pkg', package.X) | None."""
        if self.shadowed(name, k):
            return ("local",)
        for s in self.chain(k):
            if id(s) in elided:
                continue
            b = binds(s)
            if name in b:
                return ("cap", f"{self.canon(b[name])}.{name}", b[name], s)
        if name in self.p.declared.get(self.p.package, ()):
            return ("pkg", f"{self.p.package}.{name}")
        return None

    def qualifier(self, written, canonical_pkg, k):
        """An import name for the package that no local hides at k, or None (needs an alias)."""
        names = [written] + sorted(n for n, c in self.imports.items() if c == canonical_pkg and n != written)
        for n in names:
            if not self.shadowed(n, k):
                return n
        return None

    def refs(self, caps, names):
        """Capability references in body order, as (token, canonical capability or outcome):
        qualified `p.X` with canonical p.X in caps, and bare X for X in names."""
        p, out = self.p, []
        for k in range(len(p.toks)):
            if not self.is_ref(k):
                continue
            text = p.t(k).text
            if p.t(k + 1).text == "." and p.t(k + 2).kind == "ident" and not self.shadowed(text, k):
                q = f"{self.canon(text)}.{p.t(k + 2).text}"
                if q in caps:
                    out.append((k, q))
                    continue
            if text in names:
                r = self.resolve(text, k)
                out.append((k, r[1] if r and r[0] in ("cap", "pkg") else (r[0] if r else None)))
        return out


def lexical(unit):
    if not hasattr(unit, "_lex"):
        unit._lex = Lexical(unit)
    return unit._lex


def name_plan(unit, variant):
    """Which functions of `variant` lose their rows, and how each capability reference their
    `uses` clause bound is respelled (option (a)) or would resolve (option (b)).

    Each reference is resolved to its binding and owner before any edit. Functions are decided
    outer body first, so a reference owned by an elided body falls back to the bindings that
    stay written around it. A reference that still resolves to the same capability needs no
    edit; any other is qualified once with an import name no local hides; when every import
    name is hidden, the owning function keeps its rows (it needs an import alias)."""
    plans = unit.__dict__.setdefault("_plans", {})
    if variant in plans:
        return plans[variant]
    lex = lexical(unit)
    p = unit.parser
    names = {n for s in lex.scopes for n in binds(s)}
    before = {}
    for k in range(len(p.toks)):
        if lex.is_ref(k) and p.t(k).text in names:
            r = lex.resolve(p.t(k).text, k)
            if r and r[0] == "cap":
                before[k] = r
    owned = {}
    for k, r in before.items():
        owned.setdefault(id(r[3]), []).append(k)
    elided, blocked, refs = set(), set(), []
    for s in lex.scopes:    # outer first
        if not (entries(s) and c1_base(s, variant, unit)):
            continue
        trial = elided | {id(s)}
        mine = []
        for k in owned.get(id(s), ()):
            cap, written = before[k][1], before[k][2]
            after = lex.resolve(p.t(k).text, k, trial)
            if after and after[0] == "cap" and after[1] == cap:
                continue
            mine.append({"tok": k, "name": p.t(k).text, "cap": cap, "after": after,
                         "qualifier": lex.qualifier(written, cap.split(".")[0], k), "owner": s})
        if any(r["qualifier"] is None for r in mine):
            blocked.add(id(s))
        else:
            elided = trial
        refs += mine
    plans[variant] = {"elided": elided, "blocked": blocked, "refs": refs}
    return plans[variant]


def export_inventory(units):
    """Package -> exported capability names, from the library reconstruction and the stubs file.
    A package with a section there has an inventory (possibly empty); any other has none."""
    inv = {}
    for u in units:
        if u.group not in ("library", "exports"):
            continue
        for pkg in u.parser.packages:
            inv.setdefault(pkg, set())
        for pkg, names in u.parser.ifaces.items():
            inv.setdefault(pkg, set()).update(names)
    return inv


def option_b(unit, ref, inventory):
    """How a reference that lost its binding resolves under option (b): lexical and same-package
    bindings first, then exactly one imported package exporting a capability of that name."""
    lex = lexical(unit)
    after = ref["after"]
    if after is not None:
        return "lexical"        # an outer written `uses` or a same-package declaration captures it
    if not lex.has_imports:
        return "unresolved"     # a fragment with no import declarations
    imported = {c for n, c in lex.imports.items()}
    go_marked = {path.rsplit("/", 1)[-1] for alias, path in unit.parser.imports if alias == "go"}
    known = {pkg for pkg in imported - go_marked if ref["name"] in inventory.get(pkg, ())}
    unknown = {pkg for pkg in imported if pkg not in inventory or pkg in go_marked}
    if len(known) > 1:
        return "ambiguous"
    if unknown:
        return "unresolved"
    if known == {ref["cap"].split(".")[0]}:
        return "unique"
    return "unbound" if not known else "wrong"


B_OUTCOMES = ("unique", "ambiguous", "lexical", "unresolved", "unbound", "wrong")


def names_summary(units, inventory, variant="landing"):
    """Option (a) and option (b) over the references that elision of `variant` unbinds."""
    out = {"uses": 0, "by_capability": {}, "by_position": {"selector": 0, "wire or replace key": 0, "other": 0},
           "qualified": 0, "needs_alias": 0, "needs_alias_functions": 0, "functions": 0,
           "b": {k: 0 for k in B_OUTCOMES}, "equal_calls": 0, "list": []}
    for u in units:
        plan = name_plan(u, variant)
        p = u.parser
        out["needs_alias_functions"] += len(plan["blocked"])
        out["functions"] += len({id(r["owner"]) for r in plan["refs"]})
        for r in plan["refs"]:
            k = r["tok"]
            out["uses"] += 1
            out["by_capability"][r["cap"]] = out["by_capability"].get(r["cap"], 0) + 1
            nxt = p.t(k + 1).text
            pos = "selector" if nxt == "." else "wire or replace key" if nxt == "=" else "other"
            out["by_position"][pos] += 1
            b = option_b(u, r, inventory)
            out["b"][b] += 1
            if id(r["owner"]) in plan["blocked"]:
                out["needs_alias"] += 1
                spelled = "keeps its row (needs an import alias)"
            else:
                out["qualified"] += 1
                spelled = f"{r['qualifier']}.{r['name']}"
                if r["cap"] == "testing.T" and nxt == "." and p.t(k + 2).text == "Equal" and p.t(k + 3).text == "(":
                    out["equal_calls"] += 1
            out["list"].append(f"{u.rel}:{p.line_of(p.t(k).start)} {r['name']} ({pos}) -> {spelled}; (b) {b}")
    out["by_capability"] = dict(sorted(out["by_capability"].items()))
    out["tokens_a"] = 2 * out["qualified"]
    out["tokens_a_testing_Equal"] = out["tokens_a"] - 2 * out["equal_calls"]
    out["tokens_b"] = 2 * sum(out["b"][k] for k in ("ambiguous", "lexical", "unbound", "wrong"))
    return out


def capability_universe(units, inventory):
    """Canonical names of every capability: the export inventory plus every qualified `uses` entry."""
    caps = {f"{pkg}.{n}" for pkg, names in inventory.items() for n in names}
    for u in units:
        lex = lexical(u)
        for top_sig in u.parser.sigs:
            for s in top_sig.all_sigs():
                for e in s.uses:
                    if "." in e:
                        pkg, name = e.split(".", 1)
                        caps.add(f"{lex.canon(pkg)}.{name}")
    return caps


def user_unit(u):
    """A unit of user code: not a fixture, not the library, and every top-level record user-facing."""
    return u.group not in ("fixtures", "library") and all(u.audience(s) == "user" for s in u.parser.sigs)


def unit_tokens(u):
    return sum(1 for t in tokenize(u.text) if t.kind not in ("nl", "eof"))


def load_exports():
    return Unit("exports", "exports.go.sketch", (HERE / "exports.go.sketch").read_text())


def load_controls():
    return [Unit("controls", f"controls/before/{path.name}", path.read_text())
            for path in sorted((HERE / "controls" / "before").glob("*.go.sketch"))]


def control_results(controls, inventory, caps):
    """Rewrite each control with the landing rule; compare with its hand-written after/ file and
    with the outcomes its `// expect` lines state. Returns (rows, problems)."""
    rows, problems = [], []
    for u in controls:
        name = u.rel.rsplit("/", 1)[-1]
        new = apply_edits(u.text, c1_edits(u, "landing"))
        expected_path = HERE / "controls" / "after" / name
        if not expected_path.exists():
            problems.append(f"{u.rel}: no controls/after/{name}")
        elif expected_path.read_text() != new:
            problems.append(f"{u.rel}: the landing rewrite differs from controls/after/{name}")
        au = Unit("controls", f"controls/after/{name}", new)
        problems += binding_check(u, au, caps)
        s = names_summary([u], inventory)
        got_a = f"qualified {s['qualified']}, needs alias {s['needs_alias']}"
        got_b = ", ".join(f"{k} {s['b'][k]}" for k in ("unique", "ambiguous", "lexical", "unresolved"))
        want = dict(re.findall(r"^// expect \((a|b)\): (.*)$", u.text, re.M))
        if want.get("a") != got_a or want.get("b") != got_b:
            problems.append(f"{u.rel}: measured (a) {got_a}; (b) {got_b}; the file expects (a) {want.get('a')}; (b) {want.get('b')}")
        about = re.search(r"^// control: (.*)$", u.text, re.M)
        rows.append({"control": name.removesuffix(".go.sketch"), "about": about.group(1) if about else "",
                     "a": got_a, "b": got_b, "tokens_a": s["tokens_a"], "b_counts": s["b"]})
    return rows, problems


# Known-bad rewrites the binding check must reject: (control, text in its after/ file, bad spelling).
CHECKER_MUTANTS = (
    ("nested-literal-same-row", "http.Client.Ping", "http.http.Client.Ping"),  # round 2: qualified twice
    ("inline-wire-key", "wire{ http.Client =", "wire{ Client ="),              # round 2: bare wire key
    ("replace-key", "replace http.Client", "replace Client"),                 # round 2: bare replace target
    ("package-name-shadowed-alias", "web.Client", "http.Client"),             # captured by the local `http`
)


def checker_self_test(controls, caps):
    """Each mutant must draw at least one binding-check problem. Returns the mutants that pass unseen."""
    by_name = {u.rel.rsplit("/", 1)[-1].removesuffix(".go.sketch"): u for u in controls}
    unseen = []
    for name, good, bad in CHECKER_MUTANTS:
        before = by_name[name]
        text = (HERE / "controls" / "after" / f"{name}.go.sketch").read_text()
        if good not in text:
            unseen.append(f"{name}: {good!r} is not in its after/ file")
            continue
        if not binding_check(before, Unit("controls", f"mutant/{name}", text.replace(good, bad, 1)), caps):
            unseen.append(f"{name}: the binding check accepts {bad!r}")
    return unseen


def binding_check(before, after, caps):
    """The rewrite keeps every capability reference bound to the same capability, and writes no
    doubled qualifier. Returns a list of problems."""
    problems = []
    lb, la = lexical(before), lexical(after)
    names = {n for s in lb.scopes for n in binds(s)}
    rb = [c for _, c in lb.refs(caps, names)]
    ra = [c for _, c in la.refs(caps, names)]
    if rb != ra:
        for i, (x, y) in enumerate(zip(rb, ra)):
            if x != y:
                problems.append(f"{after.rel}: reference {i + 1} resolves to {y!r}, was {x!r}")
                break
        else:
            problems.append(f"{after.rel}: {len(rb)} capability references before, {len(ra)} after")
    pkgs = set(la.imports) | {c.split(".")[0] for c in caps}
    p = after.parser
    for k in range(len(p.toks) - 2):
        t = p.t(k)
        if t.kind == "ident" and t.text in pkgs and p.t(k + 1).text == "." and p.t(k + 2).text == t.text:
            problems.append(f"{after.rel}:{p.line_of(t.start)}: doubled qualifier {t.text}.{t.text}")
    return problems


def tail_text(sig, keep_err=lambda e: True, keep_use=lambda u: True, keep_where=True):
    parts = []
    errs = [e for e in sig.errors if keep_err(e)]
    if sig.result_kind == "tuple":
        parts.append(f"({sig.value_text}, error[{' | '.join(errs)}])" if errs else sig.value_text)
    elif sig.result_kind == "error":
        if errs:
            parts.append(f"error[{' | '.join(errs)}]")
    elif sig.result_kind == "type":
        parts.append(sig.value_text)
    us = [u for u in sig.uses if keep_use(u)]
    if us:
        parts.append("uses " + ", ".join(us))
    if sig.where and keep_where:
        parts.append("where " + ", ".join(f"{s}.{m}: {t}" for s, m, t in sig.where))
    return (" " + " ".join(parts)) if parts else ""


def edit_tail(sig, **kw):
    return (sig.params_end, sig.tail_end, tail_text(sig, **kw))


def c1_edits(unit, variant):
    """Remove the rows, and qualify each bare capability name the removed `uses` clause bound
    (name-resolution option (a): `Client.Get` becomes `http.Client.Get`)."""
    edits = []
    for _, s in iter_records([unit]):
        if s.kind in ("func", "method", "literal") and c1_elides(s, variant, unit) and entries(s):
            edits.append(edit_tail(s, keep_err=lambda e: False, keep_use=lambda u: False))
    tails = list(edits)
    for r in name_plan(unit, variant)["refs"]:
        if id(r["owner"]) in name_plan(unit, variant)["blocked"]:
            continue
        off = unit.parser.t(r["tok"]).start
        if any(a <= off < b for a, b, _ in tails):
            raise SystemExit(f"{unit.rel}: a reference to {r['cap']} lies in a removed row clause")
        edits.append((off, off, r["qualifier"] + "."))
    return edits


def c2_edits(unit):
    edits = []
    for _, s in iter_records([unit]):
        if not s.where:
            continue
        cons = []
        for subject, method, _ in s.where:
            c = WHERE_TO_CONSTRAINT[method]
            if s.package == "cmp" and c.startswith("cmp."):
                c = c[len("cmp."):]
            cons.append(f"{subject} {c}")
        edits.append((s.name_end, s.name_end, "[" + ", ".join(cons) + "]"))
        edits.append(edit_tail(s, keep_where=False))
    return edits


def passthrough(unit, sig):
    """Row variables of function-typed parameters that also appear in the declaration's own rows."""
    own = row_vars(unit, sig)
    child_vars = set()
    for c in sig.children:
        child_vars |= row_vars(unit, c)
    return own & child_vars, child_vars - own


def c3_edits(unit):
    """Candidate 3 as an implicit-callback rule: drop pass-through row variables everywhere they are written."""
    edits = []
    for _, s in iter_records([unit]):
        if s.kind not in ("func", "method", "iface"):
            continue
        through, _ = passthrough(unit, s)
        if not through:
            continue
        keep = lambda e, through=through: e not in through
        for c in s.children:
            if row_vars(unit, c) & through:
                edits.append(edit_tail(c, keep_err=keep, keep_use=keep))
        edits.append(edit_tail(s, keep_err=keep, keep_use=keep))
    return edits


def apply_edits(text, edits):
    for start, end, new in sorted(edits, key=lambda e: e[0], reverse=True):
        text = text[:start] + new + text[end:]
    return text


CANDIDATES = {
    "row-elision": lambda u: c1_edits(u, "landing"),
    "row-elision-unguarded": lambda u: c1_edits(u, "unguarded"),
    "named-constraints": c2_edits,
    "function-type-rows": c3_edits,
}


def rewrite(units):
    """Return {candidate: [Unit after]} and the files to write."""
    out, files = {}, {}
    for cand, fn in CANDIDATES.items():
        after_units = []
        md_out = {}
        for u in units:
            new = apply_edits(u.text, fn(u)) if u.group != "fixtures" else u.text
            au = Unit(u.group, u.rel, new, u.kind, u.block, u.negative)
            after_units.append(au)
            if u.group in ("fixtures", "spec") or new == u.text:
                if u.group == "spec" and new != u.text:
                    stem = u.rel.split("/")[-1].replace(".md#", "-")
                    files[f"{cand}/spec/{stem}.go.sketch"] = f"// {u.rel}, rewritten by measure.py for candidate {cand}\n" + new
                continue
            if u.kind == "block":
                md_out.setdefault(u.rel.split("#")[0], {})[u.block] = new
            else:
                files[f"{cand}/{u.rel}"] = new
        for rel, blocks in md_out.items():
            src = (BEFORE / rel).read_text()
            pieces, last = [], 0
            for n, block, off in md_blocks(src):
                pieces.append(src[last:off])
                pieces.append(blocks.get(n, block))
                last = off + len(block)
            pieces.append(src[last:])
            files[f"{cand}/{rel}"] = "".join(pieces)
        out[cand] = after_units
    return out, files


# ---- measurements ----------------------------------------------------------
def inventory(units):
    rows = []
    for u, s in iter_records(units):
        tp = tparams(u, s)
        rows.append({
            "unit": u.rel, "line": s.line, "group": u.group, "audience": u.audience(s),
            "negative": u.negative, "role": role(s), "name": s.name or "(literal)" if s.kind == "literal" else s.name or "(anonymous)",
            "exported": s.exported(), "entries": total_entries(s), "own": entries(s),
            "tparams": sorted(tp), "tparam_kinds": tp, "brackets": [b for b, _ in s.brackets],
            "constraints": [c for _, c in s.brackets if c] + [f"{x}.{m}" for x, m, _ in s.where],
            "machine": s.machine, "tokens": sig_tokens(u, s),
        })
    return rows


def summarize(rows, pred):
    sel = [r for r in rows if pred(r)]
    return {
        "signatures": len(sel),
        "with_rows": sum(1 for r in sel if r["entries"]),
        "with_tparams": sum(1 for r in sel if r["tparams"]),
        "row_entries": sum(r["entries"] for r in sel),
        "tokens": sum(r["tokens"] for r in sel),
    }


def corpus_pred(r):
    return r["group"] != "fixtures"


def measure(units, after):
    inv = inventory(units)
    res = {"inventory": {}, "candidates": {}, "gap23": {}, "honesty": {}, "go": {}}

    # Inventory by audience and role.
    by = {}
    for r in inv:
        if r["negative"]:
            key = ("fixture (negative)", r["role"])
        else:
            key = (r["audience"], r["role"])
        by.setdefault(key, []).append(r)
    res["inventory"]["by_audience_role"] = {
        f"{a} / {ro}": summarize(v, lambda r: True) for (a, ro), v in sorted(by.items())
    }
    for aud in ("user", "library", "fixture"):
        res["inventory"][aud] = summarize(inv, lambda r, aud=aud: r["audience"] == aud and not r["negative"])
    res["inventory"]["fixture_negative"] = summarize(inv, lambda r: r["negative"])
    res["inventory"]["user_by_group"] = {
        g: summarize(inv, lambda r, g=g: r["group"] == g and r["audience"] == "user")
        for g in ("eval8-five", "eval9", "eval9-loops", "sketch10", "spec")
    }
    res["inventory"]["tparam_signatures"] = [
        {"unit": r["unit"], "line": r["line"], "name": r["name"], "audience": r["audience"],
         "role": r["role"], "tparams": r["tparam_kinds"], "brackets": r["brackets"], "constraints": r["constraints"]}
        for r in inv if r["tparams"] and corpus_pred(r)
    ]

    # Candidate 1, step by step, on the corpus (fixtures excluded).
    c1 = {"guarded": [], "foreign": []}
    for variant in C1_VARIANTS:
        removed = {"user": 0, "library": 0}
        sigs = {"user": 0, "library": 0}
        for u, s in iter_records(units):
            if u.group == "fixtures" or not entries(s):
                continue
            if s.kind in ("func", "method", "literal") and c1_elides(s, variant, u):
                removed[u.audience(s)] += entries(s)
                sigs[u.audience(s)] += 1
        c1[variant] = {"removed_user": removed["user"], "removed_library": removed["library"],
                       "signatures_user": sigs["user"], "signatures_library": sigs["library"]}
    for u, s in iter_records(units):
        if u.group == "fixtures" or s.kind != "func" or not entries(s):
            continue
        if c1_elides(s, "unguarded", u) and not c1_elides(s, "landing", u):
            key = "foreign" if s.foreign else "guarded"
            c1[key].append(f"{u.rel}: {s.name} ({entries(s)} entries)")

    # Name resolution once a `uses` clause is elided (M4): option (a) qualifies, option (b) looks up imports.
    exports_inv = export_inventory(units + [load_exports()])
    controls = load_controls()
    caps = capability_universe(units + controls, exports_inv)
    problems, checked, refs_checked = [], 0, 0
    for cand in ("row-elision", "row-elision-unguarded"):
        for b, a in zip(units, after[cand]):
            if b.group == "fixtures":
                continue
            problems += binding_check(b, a, caps)
            checked += 1
            if cand == "row-elision":
                refs_checked += len(lexical(b).refs(caps, {n for s in lexical(b).scopes for n in binds(s)}))
    control_rows, control_problems = control_results(controls, exports_inv, caps)
    res["controls"] = control_rows
    unseen = checker_self_test(controls, caps)
    res["binding_check"] = {"units": checked, "references": refs_checked, "mutants": len(CHECKER_MUTANTS),
                            "problems": problems + control_problems + unseen}
    no_inventory = sorted({c for u in units if u.group != "fixtures" for c in lexical(u).imports.values()} - set(exports_inv))
    res["inventory"]["imports_without_export_inventory"] = no_inventory
    c1["names"] = names_summary([u for u in units if u.group != "fixtures"], exports_inv)
    # Resolution reads one unit at a time; count references a sibling file of the same package could capture.
    c1["names"]["sibling_declarations"] = sum(
        1 for u in units if u.group != "fixtures" for r in name_plan(u, "landing")["refs"]
        for v in units if v is not u and v.group == u.group and v.kind == "file" and u.kind == "file"
        and r["name"] in v.parser.declared.get(u.parser.package, ()))
    c1["names"]["tokens_a_loops"] = 2 * sum(
        1 for u in units if u.group == "eval9-loops" for r in name_plan(u, "landing")["refs"]
        if id(r["owner"]) not in name_plan(u, "landing")["blocked"])
    user_units = [(b, a) for b, a in zip(units, after["row-elision"]) if user_unit(b)]
    c1["source_tokens"] = {"before": sum(unit_tokens(b) for b, _ in user_units),
                           "after": sum(unit_tokens(a) for _, a in user_units),
                           "units": len(user_units)}
    after_inv = inventory(after["row-elision"])
    before_user = summarize(inv, lambda r: corpus_pred(r) and r["audience"] == "user")
    after_user = summarize(after_inv, lambda r: corpus_pred(r) and r["audience"] == "user")
    c1["before_user"], c1["after_user"] = before_user, after_user
    c1["after_user_by_role"] = {}
    for ro in sorted({r["role"] for r in after_inv if corpus_pred(r) and r["audience"] == "user"}):
        b = summarize(inv, lambda r, ro=ro: corpus_pred(r) and r["audience"] == "user" and r["role"] == ro)
        a = summarize(after_inv, lambda r, ro=ro: corpus_pred(r) and r["audience"] == "user" and r["role"] == ro)
        c1["after_user_by_role"][ro] = {"before": b["row_entries"], "after": a["row_entries"], "signatures": b["signatures"]}
    res["candidates"]["row-elision"] = c1

    # Candidate 2: constrained signatures, before and after, and the Gleam alternative.
    c2 = {"user_constrained": 0, "library_constrained": 0, "where_entries": 0, "bracket_constraints_after": 0,
          "tokens_before": 0, "tokens_after": 0, "gleam_params_added": 0, "list": []}
    after2 = {(u.rel, s.line, s.name): (u, s) for u, s in iter_records(after["named-constraints"])}
    for u, s in iter_records(units):
        if u.group == "fixtures":
            continue
        if s.where or any(c for _, c in s.brackets if c != "error"):
            aud = u.audience(s)
            c2[f"{aud}_constrained"] += 1
            c2["where_entries"] += len(s.where)
            au, as_ = after2[(u.rel, s.line, s.name)]
            c2["bracket_constraints_after"] += sum(1 for _, c in as_.brackets if c)
            c2["tokens_before"] += sig_tokens(u, s)
            c2["tokens_after"] += sig_tokens(au, as_)
            # Gleam: no constraint; each method entry becomes an explicit function parameter.
            c2["gleam_params_added"] += len(s.where)
            c2["list"].append({"unit": u.rel, "name": s.name, "before": u.text[s.start:s.tail_end],
                               "after": au.text[as_.start:as_.tail_end]})
    # Equality on a non-comparable type: T.Equal on []int in poller_test.
    eq_noncomparable = 0
    for u in units:
        if u.group == "fixtures":
            continue
        eq_noncomparable += len(re.findall(r"T\.Equal\([^\n]*\[\]int\{", u.text))
    c2["equal_on_slices"] = eq_noncomparable
    res["candidates"]["named-constraints"] = c2

    # Candidate 3: pass-through rows of function-typed parameters.
    c3 = {"decls_with_row_vars": 0, "user_decls_with_row_vars": 0, "row_var_occurrences": 0,
          "removed_by_implicit": 0, "absorbed": 0, "absorbed_list": [], "error_uses_pairs": 0,
          "saved_by_combined": 0, "empty_row_callbacks": 0, "empty_row_list": [],
          "functype_multi_uses": 0, "functype_with_rows": 0, "user_functype_with_row_vars": 0, "list": []}
    for u, s in iter_records(units):
        if u.group == "fixtures":
            continue
        for x in s.all_sigs():
            if x.kind == "functype" and entries(x):
                c3["functype_with_rows"] += 1
                if len(x.uses) > 1:
                    c3["functype_multi_uses"] += 1
                if row_vars(u, x) and u.audience(s) == "user":
                    c3["user_functype_with_row_vars"] += 1
        if s.kind not in ("func", "method", "iface", "typedecl"):
            continue
        occ = sum(1 for x in s.all_sigs() for e in x.errors + x.uses if e in row_vars(u, x))
        if occ:
            c3["decls_with_row_vars"] += 1
            c3["row_var_occurrences"] += occ
            if u.audience(s) == "user":
                c3["user_decls_with_row_vars"] += 1
            c3["list"].append({"unit": u.rel, "name": s.name, "occurrences": occ, "audience": u.audience(s)})
        if s.kind == "typedecl":
            continue
        through, absorbed = passthrough(u, s)
        c3["removed_by_implicit"] += sum(1 for x in s.all_sigs() for e in x.errors + x.uses if e in through)
        if absorbed:
            c3["absorbed"] += len(absorbed)
            c3["absorbed_list"].append(f"{s.package}.{s.name}: {', '.join(sorted(absorbed))}")
        err_through = {v for v in through if any(v in x.errors for x in s.all_sigs())}
        use_through = {v for v in through if any(v in x.uses for x in s.all_sigs())}
        pairs = min(len(err_through), len(use_through))
        c3["error_uses_pairs"] += pairs
        if pairs:
            c3["saved_by_combined"] += sum(1 for x in s.all_sigs() for e in x.uses if e in use_through)
        if any(x.kind == "functype" for x in s.children) and tparams(u, s):
            for c in s.children:
                if c.kind == "functype" and c.child_role == "param" and not entries(c):
                    c3["empty_row_callbacks"] += 1
                    c3["empty_row_list"].append(f"{s.package}.{s.name}: {u.text[c.start:c.tail_end]}")
    after3 = inventory(after["function-type-rows"])
    c3["library_entries_before"] = summarize(inv, lambda r: corpus_pred(r) and r["audience"] == "library")["row_entries"]
    c3["library_entries_after"] = summarize(after3, lambda r: corpus_pred(r) and r["audience"] == "library")["row_entries"]
    c3["user_entries_before"] = before_user["row_entries"]
    c3["user_entries_after"] = summarize(after3, lambda r: corpus_pred(r) and r["audience"] == "user")["row_entries"]
    res["candidates"]["function-type-rows"] = c3

    # Gap 23: which names are parameters.
    single_declared = sorted({(u.rel, n) for u in units if u.group != "fixtures"
                              for p, names in u.parser.declared.items() for n in names if TPARAM.match(n)})
    names = {}
    for r in inv:
        if corpus_pred(r):
            for n, k in r["tparam_kinds"].items():
                names.setdefault(f"{n} ({k})", 0)
                names[f"{n} ({k})"] += 1
    bare_t = sum(len(re.findall(r"(?<![\w.])T\.Equal\(", u.text)) for u in units if u.group != "fixtures")
    explicit_inst = 0
    for u in units:
        if u.group == "fixtures":
            continue
        explicit_inst += len(re.findall(r"\b(?:json\.Decode|http\.ReadJSON|retry\.On)\[", u.text))
    # A one-letter name in a signature that resolves to a declaration in scope, not to a parameter (M3).
    bound = []
    for u, s in iter_records(units):
        if u.group == "fixtures":
            continue
        declared = declared_names(u, s)
        for x in s.all_sigs():
            for n, pos in x.idents:
                if TPARAM.match(n) and n in declared:
                    bound.append(f"{u.rel}: {s.name} ({pos} {n})")
    tests_qualified = sum(1 for u, s in iter_records(units)
                          if u.group != "fixtures" and role(s) == "test" and "testing.T" in s.uses)
    res["gap23"] = {
        "bound_one_letter_names": bound,
        "tests_using_testing_T": tests_qualified,
        "single_letter_declarations": [f"{a}: {b}" for a, b in single_declared],
        "parameter_names": dict(sorted(names.items())),
        "bare_T_capability_calls": bare_t,
        "explicit_instantiation_calls": explicit_inst,
        "bracket_lists": sum(1 for r in inv if corpus_pred(r) and r["brackets"]),
    }

    # Honesty checks.
    leaks = []
    for u, s in iter_records(units):
        if u.group != "fixtures" and s.kind == "typedecl" and s.machine and s.exported():
            leaks.append(f"{u.rel}: type {s.name}")
    neg = [u.rel for u in units if u.group == "fixtures" and u.negative and re.search(r"// want S10[12]\b", u.text)]
    missing = sorted(set(neg) - set(FIXTURE_AUDIT)) + sorted(set(FIXTURE_AUDIT) - set(neg))
    if missing:
        raise SystemExit(f"FIXTURE_AUDIT is out of date: {missing}")
    audit = {}
    for rel in neg:
        audit.setdefault(FIXTURE_AUDIT[rel][0], []).append(rel.split("/")[-1].removesuffix(".sketch"))
    laws = [f"{u.rel}: {s.name} ({entries(s)} entries)" for u, s in iter_records(units)
            if u.group != "fixtures" and role(s) == "law"]
    provider_methods = summarize(inv, lambda r: corpus_pred(r) and r["audience"] == "user" and r["role"] == "method")
    res["honesty"] = {
        "exported_machine_types": leaks,
        "negative_fixtures_S101_S102": {k: sorted(v) for k, v in sorted(audit.items())},
        "negative_fixtures_S101_S102_total": len(neg),
        "laws": laws,
        "user_methods": provider_methods,
        "wire_exposures": [f"{u.rel}: wire {s.name} uses {', '.join(s.uses)}" for u, s in iter_records(units)
                           if s.kind == "wire" and u.group != "fixtures"],
        "derived_codec_rows": sum(u.text.count("// DERIVED") for u in units if u.group == "library"),
    }

    # Sources.
    src = {}
    for u in units:
        k = "fixtures (negative)" if u.negative else u.group
        src.setdefault(k, {"units": 0, "lines": 0})
        src[k]["units"] += 1
        src[k]["lines"] += u.text.count("\n")
    res["sources"] = src

    # Go controls: signature tokens of matched functions.
    pairs = {"fetchall-main": "eval9-loops/fetchall.go.sketch", "ledger-main": "eval9-loops/ledger.go.sketch",
             "poller-poller": "eval9-loops/poller.go.sketch", "poller-poller_test": "eval9-loops/poller_test.go.sketch",
             "wordcount-main": "eval9-loops/wordcount.go.sketch"}
    eff_before = {u.rel: u for u in units}
    eff_after = {u.rel: u for u in after["row-elision"]}
    table = []
    tot = {"go": 0, "before": 0, "after": 0}
    for go_name, rel in pairs.items():
        gu = Unit("go", f"go-control/{go_name}.go.txt", (BEFORE / "go-control" / f"{go_name}.go.txt").read_text())
        gsigs = {s.name.lower(): s for s in gu.parser.sigs if s.kind == "func"}
        bsigs = {s.name.lower(): s for s in eff_before[rel].parser.sigs if s.kind == "func"}
        asigs = {s.name.lower(): s for s in eff_after[rel].parser.sigs if s.kind == "func"}
        for name in sorted(set(gsigs) & set(bsigs)):
            row = {"program": rel.split("/")[-1], "function": bsigs[name].name,
                   "go": sig_tokens(gu, gsigs[name]), "before": sig_tokens(eff_before[rel], bsigs[name]),
                   "after": sig_tokens(eff_after[rel], asigs[name]),
                   "go_text": gu.text[gsigs[name].start:gsigs[name].tail_end],
                   "before_text": eff_before[rel].text[bsigs[name].start:bsigs[name].tail_end],
                   "after_text": eff_after[rel].text[asigs[name].start:asigs[name].tail_end]}
            table.append(row)
            for k in tot:
                tot[k] += row[k]
    res["go"] = {"matched": table, "totals": tot}
    return res, inv


# ---- rendering -------------------------------------------------------------
def md_table(header, rows):
    out = ["| " + " | ".join(header) + " |", "| " + " | ".join("---" for _ in header) + " |"]
    for r in rows:
        out.append("| " + " | ".join(str(x) for x in r) + " |")
    return "\n".join(out)


def render(res, inv):
    sec = {}
    sec["sources"] = md_table(["Group", "Files or fenced blocks", "Lines"],
                              [[k, v["units"], v["lines"]] for k, v in res["sources"].items()])
    inv_rows = []
    for key, v in res["inventory"]["by_audience_role"].items():
        inv_rows.append([key, v["signatures"], v["with_rows"], v["row_entries"], v["with_tparams"]])
    sec["inventory"] = md_table(["Audience / role", "Signatures", "With rows", "Row entries", "With type parameters"], inv_rows)

    g = res["inventory"]["user_by_group"]
    sec["inventory-groups"] = md_table(
        ["Source (user-facing)", "Signatures", "With rows", "Row entries", "With type parameters"],
        [[k, v["signatures"], v["with_rows"], v["row_entries"], v["with_tparams"]] for k, v in g.items()])

    tps = res["inventory"]["tparam_signatures"]
    sec["tparams"] = md_table(
        ["Signature", "Audience", "Role", "Parameters (kind)", "Bracket list", "Constraint"],
        [[f"`{t['name']}` ({t['unit']}:{t['line']})", t["audience"], t["role"],
          ", ".join(f"{n} ({k})" for n, k in sorted(t["tparams"].items())),
          ", ".join(t["brackets"]) or "none", ", ".join(t["constraints"]) or "none"] for t in tps])

    c1 = res["candidates"]["row-elision"]
    sec["c1"] = md_table(
        ["Step", "User signatures elided", "User row entries removed", "Library row entries removed"],
        [["unexported functions and literals, no guard", c1["unexported"]["signatures_user"], c1["unexported"]["removed_user"], c1["unexported"]["removed_library"]],
         ["plus tests, no guard (round 1; leaks rows, comparison only)", c1["unguarded"]["signatures_user"], c1["unguarded"]["removed_user"], c1["unguarded"]["removed_library"]],
         ["**plus tests, with the export guard and the foreign exemption (lands)**", c1["landing"]["signatures_user"], c1["landing"]["removed_user"], c1["landing"]["removed_library"]],
         ["landing rule plus `main` (refused)", c1["main"]["signatures_user"], c1["main"]["removed_user"], c1["main"]["removed_library"]]])
    sec["c1-kept"] = "\n".join(
        [f"- export guard (step of an exported machine): {x}" for x in c1["guarded"]]
        + [f"- foreign declaration (`//ef:go`, no body): {x}" for x in c1["foreign"]])
    nm = c1["names"]
    nb = nm["b"]
    no_inv = res["inventory"]["imports_without_export_inventory"]
    sec["names"] = md_table(
        ["Measure", "Option (a): qualify", "Option (b): unique import"],
        [["capability references whose binding the elided `uses` clause carried", nm["uses"], nm["uses"]],
         ["… by capability", ", ".join(f"{k}: {v}" for k, v in nm["by_capability"].items()), "same"],
         ["… by position", ", ".join(f"{k}: {v}" for k, v in nm["by_position"].items()), "same"],
         ["resolve with no edit (exactly one import exports the name)", 0, nb["unique"]],
         ["captured by an outer `uses` or a same-package declaration (need a qualifier)", nb["lexical"], nb["lexical"]],
         ["ambiguous: two imports export the name (need a qualifier)", 0, nb["ambiguous"]],
         ["unresolved: an import has no export inventory", 0, nb["unresolved"]],
         ["qualified once", nm["qualified"], nb["lexical"] + nb["ambiguous"]],
         ["functions that keep their rows because a local hides every import name", nm["needs_alias_functions"], 0],
         ["tokens added back", nm["tokens_a"],
          f"{nm['tokens_b']}, plus up to {2 * nb['unresolved']} if the unresolved references need a qualifier"],
         ["tokens added back if `T.Equal` is spelled with the library's `testing.Equal`", nm["tokens_a_testing_Equal"], nm["tokens_b"]]])
    sec["sibling-gap"] = (
        f"- Name resolution reads one unit at a time: a same-package declaration in a sibling file is not seen. "
        f"References whose name a sibling file of the same package declares: {nm['sibling_declarations']}.")
    sec["names-detail"] ="\n".join(f"- `{x}`" for x in nm["list"])
    sec["controls"] = md_table(
        ["Control (`corpus/controls/`)", "What it shows", "Option (a)", "Option (b)"],
        [[f"`{r['control']}`", r["about"], r["a"], r["b"]] for r in res["controls"]])
    bc = res["binding_check"]
    st = c1["source_tokens"]
    sec["binding-check"] = (
        f"**Binding check.** `measure.py` resolves every capability reference of each unit before and after both elision rewrites "
        f"({bc['units']} unit rewrites; {bc['references']} references in the corpus before the landing rewrite) and of each "
        f"control, and fails when a reference resolves to another capability or to nothing, or when a doubled qualifier "
        f"(`http.http.`) appears. Each control's rewrite must also equal its hand-written `controls/after/` file and the "
        f"outcomes its `// expect` lines state. The check must also reject {bc['mutants']} known-bad rewrites of the "
        f"controls (`CHECKER_MUTANTS`: a doubled qualifier, a bare wire key, a bare `replace` target and a qualifier "
        f"captured by a local). Problems found: {len(bc['problems'])}.")
    sec["source-tokens"] = (
        f"Whole-source tokens, qualifiers included, in the {st['units']} user-facing source units: {st['before']} before, "
        f"{st['after']} after the landing rewrite, {st['before'] - st['after']} saved. Option (a)'s qualifiers cost "
        f"{nm['tokens_a']} of them ({nm['tokens_a_loops']} in the loop programs). The Go comparison above counts "
        f"signature tokens only, so the qualifiers, which sit in bodies, are not in it.")
    sec["c1-roles"] = md_table(
        ["User role", "Signatures", "Row entries before", "Row entries after"],
        [[ro, v["signatures"], v["before"], v["after"]] for ro, v in c1["after_user_by_role"].items()]
        + [["**all user-facing**", c1["before_user"]["signatures"], c1["before_user"]["row_entries"], c1["after_user"]["row_entries"]]])

    c2 = res["candidates"]["named-constraints"]
    sec["c2"] = md_table(
        ["Measure", "Value"],
        [["user-facing signatures that need a constraint", c2["user_constrained"]],
         ["library signatures that need a constraint", c2["library_constrained"]],
         ["method entries written before (`where T.M: …`)", c2["where_entries"]],
         ["named constraints written after (`[T cmp.Ordered]`)", c2["bracket_constraints_after"]],
         ["signature tokens before / after", f"{c2['tokens_before']} / {c2['tokens_after']}"],
         ["Gleam alternative: function parameters added", c2["gleam_params_added"]],
         ["`T.Equal` calls on a non-comparable `[]int`", c2["equal_on_slices"]]])
    sec["c2-list"] = "\n".join(f"- `{x['before']}`\n  → `{x['after']}`" for x in c2["list"])

    c3 = res["candidates"]["function-type-rows"]
    sec["c3"] = md_table(
        ["Measure", "Value"],
        [["declarations with row variables / user-facing among them", f"{c3['decls_with_row_vars']} / {c3['user_decls_with_row_vars']}"],
         ["row-variable occurrences in them", c3["row_var_occurrences"]],
         ["function types with written rows / with more than one `uses` entry", f"{c3['functype_with_rows']} / {c3['functype_multi_uses']}"],
         ["user-facing function types with a row variable", c3["user_functype_with_row_vars"]],
         ["entries removed by implicit callback rows (3c)", c3["removed_by_implicit"]],
         ["library row entries before / after 3c", f"{c3['library_entries_before']} / {c3['library_entries_after']}"],
         ["user row entries before / after 3c", f"{c3['user_entries_before']} / {c3['user_entries_after']}"],
         ["entries saved by one combined row variable (3b)", c3["saved_by_combined"]],
         ["callback row variables absorbed, not passed through", f"{c3['absorbed']} ({'; '.join(c3['absorbed_list'])})"],
         ["empty-row callbacks in generic declarations (meaning flips under 3c)", c3["empty_row_callbacks"]]])
    sec["c3-empty"] = "\n".join(f"- `{x}`" for x in c3["empty_row_list"])

    g23 = res["gap23"]
    sec["gap23"] = md_table(
        ["Measure", "Value"],
        [["package-level declarations with a one-letter name", f"{len(g23['single_letter_declarations'])} ({'; '.join(g23['single_letter_declarations'])})"],
         ["one-letter names in signatures that resolve to a declaration, not a parameter", f"{len(g23['bound_one_letter_names'])} ({'; '.join(g23['bound_one_letter_names'])})"],
         ["tests that write `uses testing.T` (qualified; unaffected by the rule)", g23["tests_using_testing_T"]],
         ["parameter names used (kind: signatures)", ", ".join(f"{k}: {v}" for k, v in g23["parameter_names"].items())],
         ["bracket lists written (explicit instantiation or constraint)", g23["bracket_lists"]],
         ["calls with explicit type arguments (`json.Decode[…]`, `http.ReadJSON[…]`, `retry.On[…]`)", g23["explicit_instantiation_calls"]],
         ["bare `T.Equal` calls on the imported capability `testing.T`", g23["bare_T_capability_calls"]]])

    h = res["honesty"]
    sec["honesty"] = md_table(
        ["Check", "Value"],
        [["exported machine types whose rows come from their step", f"{len(h['exported_machine_types'])} ({'; '.join(h['exported_machine_types'])})"],
         ["laws (rows kept)", "; ".join(h["laws"])],
         ["user-facing methods (rows kept): signatures / row entries", f"{h['user_methods']['signatures']} / {h['user_methods']['row_entries']}"],
         ["prototype negative fixtures for S101 or S102", h["negative_fixtures_S101_S102_total"]]]
        + [[f"… that rely on: {k}", f"{len(v)} ({', '.join(v)})"] for k, v in h["negative_fixtures_S101_S102"].items()]
        + [["`wire … uses` exposure clauses (not in docs/spec)", "; ".join(h["wire_exposures"])],
           ["library signatures whose rows are derived from a type's codec (no candidate states them)", h["derived_codec_rows"]]])

    # Prose with numbers: generated, so that no count in the note is typed by hand.
    c1t, c1m, ub = c1["landing"], c1["main"], c1["before_user"]
    c1u = c1["unguarded"]
    by_role = c1["after_user_by_role"]
    kept_roles = ("exported function", "interface method", "method")
    tparam_users = [t for t in tps if t["audience"] == "user"]
    gt = res["go"]["totals"]
    sec["answer"] = "\n".join([
        f"- **Row elision lands** for unexported functions, function literals and `TestX` functions, except a step "
        f"named by an exported machine type and a bodiless foreign declaration. It removes "
        f"{c1t['removed_user']} of the {ub['row_entries']} row entries in user-facing signatures. An elided function "
        f"reaches an imported capability by its qualified name (`http.Client.Get`): {nm['qualified']} references, "
        f"{nm['tokens_a']} tokens added back. User source still shrinks by {st['before'] - st['after']} tokens. "
        f"In the Go-controlled loop programs, signature tokens fall from {gt['before']} to {gt['after']}, "
        f"against {gt['go']} for Go.",
        f"- **Named constraints land** in Go's bracket list, `func Max[T cmp.Ordered](a, b T) T`. A bracket list is "
        f"written only when it carries a constraint or fixes an order for explicit type arguments. "
        f"{c2['user_constrained']} user-facing signatures in the corpus need a constraint; {c2['library_constrained']} library signatures do.",
        f"- **Row parameters on function types stay explicit.** All {c3['row_var_occurrences']} row-variable occurrences "
        f"are in library code ({c3['user_decls_with_row_vars']} user-facing). Each simplification that would remove them "
        f"hides a failure from a reader or loses a resource rule.",
        "- **Refused:** row elision for `main`, for laws and for methods; constraints inferred from bodies; one combined "
        "row variable; implicit callback rows.",
    ])
    sec["inventory-notes"] = "\n".join([
        f"- Users write rows, not generics. {ub['row_entries']} row entries are in user-facing signatures; "
        f"{len(tparam_users)} user-facing declarations have a type parameter "
        f"({', '.join('`' + t['name'] + '`' for t in tparam_users)}, the generic structs of the retry recipe).",
        f"- Every row variable is in library code: {c3['decls_with_row_vars']} declarations, {c3['user_decls_with_row_vars']} of them user-facing.",
        f"- Unexported functions and tests hold {by_role['unexported function']['before'] + by_role['test']['before']} "
        f"of the {ub['row_entries']} user-facing entries; `main` holds {by_role['main']['before']}. Exported functions, "
        f"methods and capability methods hold {sum(by_role[r]['before'] for r in kept_roles)}.",
    ])
    hn = h["negative_fixtures_S101_S102"]
    sec["decision"] = md_table(["Candidate", "Decision", "Measured", "Cost"], [
        ["1. Infer rows of unexported functions, literals and `TestX`, with the export guard and the foreign exemption", "**Land**",
         f"{c1t['removed_user']} of {ub['row_entries']} user entries removed",
         "package-level row fixpoint; a missing row reads as \"inferred\" on unexported code; inlay hints needed"],
        ["1. The same with no guard (round 1)", "Refuse",
         f"{c1u['removed_user']} removed; {c1u['removed_user'] - c1t['removed_user']} of them leak into exported machine types or drop a foreign row",
         "public rows written nowhere; a foreign binding loses its stated row"],
        ["1. Steps named by an exported machine type keep their rows (export guard)", "**Land (new rule)**",
         f"{len(c1['guarded'])} steps kept ({len(h['exported_machine_types'])} exported machine types)", "one more rule in the checker"],
        ["1. Bodiless foreign declarations (`//ef:go`) keep their rows", "**Land (new rule)**",
         f"{len(c1['foreign'])} declaration kept", "none: there is no body to infer from"],
        ["1. Name resolution in elided functions: (a) qualified access", "**Land**",
         f"{nm['qualified']} references qualified once, {nm['tokens_a']} tokens added back "
         f"({nm['tokens_a_testing_Equal']} with `testing.Equal`); {nm['needs_alias_functions']} functions need an import alias",
         "two spellings of one capability call, by whether `uses` is written"],
        ["1. Name resolution in elided functions: (b) unique import", "Documented alternative",
         f"{nb['unique']} resolve, {nb['ambiguous']} ambiguous, {nb['lexical']} captured, {nb['unresolved']} unresolved "
         f"({len(no_inv)} imported packages have no export inventory); controls: {sum(r['b_counts']['ambiguous'] for r in res['controls'])} ambiguous",
         "a bare name's meaning depends on the import set; needs every import's export inventory"],
        ["1. Infer rows of `main`", "Refuse",
         f"would remove {c1m['removed_user'] - c1t['removed_user']} entries; {len(hn.get('main', []))} of "
         f"{h['negative_fixtures_S101_S102_total']} S101/S102 fixtures change, `wire-initializer-error` is accepted",
         "the host contract and exit failures become implicit"],
        ["1. Infer rows of `LawX`", "Refuse", f"{by_role['law']['before']} entry kept",
         "a provider law's `uses` selects the providers it is checked against"],
        ["1. Infer rows of methods on unexported types", "Refuse for v0", f"{by_role['method']['before']} entries kept",
         "needs export reachability; leaks rows when an exported function returns the type"],
        ["2. Named constraints in Go's bracket list", "**Land**",
         f"{c2['user_constrained']} user, {c2['library_constrained']} library signatures; {c2['tokens_before']} → {c2['tokens_after']} signature tokens",
         "two ways to introduce a parameter, limited to Go's cases"],
        ["2. Gleam: explicit functions, no constraints", "Refuse", f"{c2['gleam_params_added']} parameters added",
         "no `comparable`; callers pass compare functions"],
        ["2. Roc: constraints inferred", "Refuse", f"{c2['user_constrained']} user signatures to simplify",
         "a hidden requirement on exported generics"],
        ["2. Gap 23 naming rule and `[F error]`", "**Land**",
         f"one-letter names bound by a declaration: {len(g23['bound_one_letter_names'])} (library `testing.T`); "
         f"{g23['tests_using_testing_T']} tests unaffected; {g23['explicit_instantiation_calls']} calls with explicit type arguments",
         "a declared one-letter name shadows the parameter reading in its package"],
        ["3a. Explicit row parameters, kinds by position", "**Land (status quo)**",
         f"{c3['row_var_occurrences']} row-variable occurrences, {c3['user_functype_with_row_vars']} in user-facing function types",
         "library authors write `E` and `R`"],
        ["3b. One combined row variable", "Refuse", f"{c3['saved_by_combined']} library entries saved",
         "cannot state per-row transformations"],
        ["3c. Implicit callback rows", "Refuse",
         f"{c3['library_entries_before'] - c3['library_entries_after']} library entries saved, "
         f"{c3['user_entries_before'] - c3['user_entries_after']} user",
         f"hides failures; {c3['empty_row_callbacks']} empty-row callbacks flip, among them the infallible release; cannot absorb"],
        ["Grammar: `uses (A, B)` in a function-typed parameter", "**Land**",
         f"{c3['functype_multi_uses']} corpus occurrences", "none in the corpus"],
    ])

    go = res["go"]
    sec["go"] = md_table(
        ["Program", "Function", "Go tokens", "Sketch tokens before", "Sketch tokens after candidate 1"],
        [[r["program"], f"`{r['function']}`", r["go"], r["before"], r["after"]] for r in go["matched"]]
        + [["**total**", "", go["totals"]["go"], go["totals"]["before"], go["totals"]["after"]]])
    sec["go-text"] = "\n".join(
        f"- `{r['function']}`\n  - Go: `{r['go_text']}`\n  - before: `{r['before_text']}`\n  - after: `{r['after_text']}`"
        for r in go["matched"])
    return sec


def splice_note(note, sec):
    def repl(m):
        name = m.group(1)
        if name not in sec:
            raise SystemExit(f"note names an unknown table {name!r}")
        return f"<!-- measure.py:begin {name} -->\n{sec[name]}\n<!-- measure.py:end {name} -->"
    return re.sub(r"<!-- measure\.py:begin ([\w-]+) -->.*?<!-- measure\.py:end \1 -->", repl, note, flags=re.S)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--write", action="store_true")
    ap.add_argument("--check", action="store_true")
    args = ap.parse_args()

    bad = verify_before()
    if bad:
        print("before/ differs from before.sha256:", ", ".join(bad))
        return 1
    units = load_units()
    after, files = rewrite(units)
    res, inv = measure(units, after)
    sec = render(res, inv)
    counts = json.dumps(res, indent=1, sort_keys=True) + "\n"
    if res["binding_check"]["problems"]:
        print("binding check failed:", *res["binding_check"]["problems"], sep="\n  ")
        return 1

    if args.write or args.check:
        stale = []
        expected = {AFTER / rel: text for rel, text in files.items()}
        existing = set(AFTER.rglob("*")) if AFTER.exists() else set()
        for path, text in expected.items():
            if not path.exists() or path.read_text() != text:
                stale.append(str(path.relative_to(HERE)))
        for path in existing:
            if path.is_file() and path not in expected:
                stale.append(f"{path.relative_to(HERE)} (not generated)")
        if not COUNTS.exists() or COUNTS.read_text() != counts:
            stale.append("counts.json")
        note = NOTE.read_text() if NOTE.exists() else ""
        new_note = splice_note(note, sec)
        if new_note != note:
            stale.append("rows-and-constraints.md tables")
        if args.check:
            if stale:
                print("stale:", *stale, sep="\n  ")
                return 1
            print("measure.py: counts, after/ and the note's tables are current")
            return 0
        for path, text in expected.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        COUNTS.write_text(counts)
        if note:
            NOTE.write_text(new_note)
        unexpected = [s for s in stale if s.endswith("(not generated)")]
        if unexpected:
            print("remove by hand, not generated:", *unexpected, sep="\n  ")
        print(f"wrote {len(expected)} after/ files, counts.json and the note's tables")
        return 0
    for name, text in sec.items():
        print(f"## {name}\n{text}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
