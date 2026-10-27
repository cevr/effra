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
rewrite, with a resolver separate from the rewriter's, and fails the run when one changes
declaration or a qualifier is doubled.

Contract: a measurement aid, not a compiler. It fails closed: a rewrite it cannot show to keep
every binding keeps the written row, or the run stops with a diagnostic naming the construct.
The normative rules for #196 are the amendments in the note, not this script.

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
        self.param_names = []           # names the parameter list declares, as the parser reads it

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
        self.ifaces = {}                # package import path -> declared interface (capability) names
        self.packages = []              # the import path of every package clause, in order
        self.imports = []               # (alias or None, path) of each import spec
        self.package_path = ""          # import path of the current package: Go's `// import "path"` comment, else its name

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
                sig.param_names.append(self.t(it[0]).text)
                self.parse_type(it[1], sig)
            elif len(it) == 1 and self.t(it[0]).kind == "ident" and (any_named or sig.kind == "literal"):
                if not any_named:
                    sig.untyped_params = True
                sig.param_names.append(self.t(it[0]).text)
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
                line_end = self.src.find("\n", tok.start)
                comment = re.search(r'//\s*import\s+"([^"]+)"', self.src[tok.start:line_end if line_end >= 0 else len(self.src)])
                self.package_path = comment.group(1) if comment else self.package
                self.packages.append(self.package_path)
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
                        self.ifaces.setdefault(self.package_path, set()).add(name_tok.text)
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


# ---- name resolution --------------------------------------------------------
# Contract. This script is a measurement aid, not a compiler. It fails closed: when it cannot
# show that a rewrite keeps every capability reference bound to the same declaration, it keeps
# the written row (name_plan), or the run exits with a diagnostic that names the construct
# (binding_check, c1_edits). It never emits a guessed rewrite. The normative statement of the
# rules is amendment 3 of the note, for #196; this code only measures them on the corpus.
#
# Identity. A package is identified by its full import path; an import's name (alias or last
# path segment) is only its spelling. A capability is (import path, name). A same-package
# declaration is (the package's own path, name).

def binds(sig):
    """Bare names a `uses` clause brings into scope for its body: {X: written package} for `uses pkg.X`."""
    out = {}
    for u in sig.uses:
        if "." in u:
            pkg, name = u.split(".", 1)
            out[name] = pkg
    return out


def receiver_names(p, open_, close):
    """The name a method receiver declares: `(p T)`, `(p *T)`, `(r TxRef[T])`; `(T)` declares none."""
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
    return [p.t(it[0]).text for it in items if len(it) >= 2 and p.t(it[0]).kind == "ident"]


def show(cap):
    return f"{cap[0]}.{cap[1]}"


class Lexical:
    """The rewriter's resolver: lexical frames of one unit, from the parser's records.

    Frames are function and literal bodies (holding their parameters and their written `uses`
    bindings) and the reach of each local declaration (`:=`, `var`, `catch e`) to the end of its
    block. A bare name at token k resolves to the innermost frame that declares it; a local and a
    `uses` binding of one name in the same frame is a conflict the script does not decide
    ('unsure'). Below every frame come the declarations of the unit's package."""

    def __init__(self, unit):
        p = self.p = unit.parser
        n = len(p.toks)
        self.scopes = sorted((s for s in p.sigs if s.kind in ("func", "method", "literal") and s.body),
                             key=lambda s: s.body[0])
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
        # Local frames: name -> [(from token, to token)]. Parameters reach the whole body.
        self.locals = {}
        for s in self.scopes:
            names = list(s.param_names) + (receiver_names(p, *s.recv) if s.recv else [])
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
        # Import spellings -> full import path. One spelling for two paths is unresolvable.
        self.imports, self.clashes = {}, set()
        for alias, path in p.imports:
            if alias == "_":
                continue
            spelling = path.rsplit("/", 1)[-1] if alias in (None, "go") else alias
            if spelling in self.imports and self.imports[spelling] != path:
                self.clashes.add(spelling)
            self.imports[spelling] = path
        self.has_imports = bool(p.imports)
        self.own_path = p.package_path or p.package

    def path(self, spelling):
        """The import path a qualifier spells, or None. In a fragment with no import
        declarations the spelling stands for itself."""
        if spelling in self.clashes:
            return None
        if spelling in self.imports:
            return self.imports[spelling]
        return None if self.has_imports else spelling

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
        """('local',) | ('cap', (path, X), written package, owner Sig) | ('pkg', (path, X))
        | ('unsure', why) | None. Innermost frame first; an elided body's `uses` binds nothing."""
        cands = [(a, "local", None) for a, b in self.locals.get(name, ()) if a <= k < b]
        for s in self.chain(k):
            if id(s) not in elided and name in binds(s):
                cands.append((s.body[0], "cap", s))
        if cands:
            top = max(c[0] for c in cands)
            inner = [c for c in cands if c[0] == top]
            if len({c[1] for c in inner}) > 1:
                return ("unsure", f"a local and a `uses` binding named {name} share one frame")
            kind, s = inner[0][1], inner[0][2]
            if kind == "local":
                return ("local",)
            written = binds(s)[name]
            path = self.path(written)
            if path is None:
                return ("unsure", f"`{written}` names no single import")
            return ("cap", (path, name), written, s)
        if name in self.p.declared.get(self.p.package, ()):
            return ("pkg", (self.own_path, name))
        return None

    def qualifier(self, written, path, k):
        """A spelling of the package at `path` that no local hides at k, or None."""
        names = [written] + sorted(n for n, c in self.imports.items() if c == path and n != written)
        for n in names:
            if n not in self.clashes and self.path(n) == path and not self.shadowed(n, k):
                return n
        return None


def lexical(unit):
    if not hasattr(unit, "_lex"):
        unit._lex = Lexical(unit)
    return unit._lex


def name_plan(unit, variant):
    """Which functions of `variant` lose their rows, and how each capability reference their
    `uses` clause bound is respelled (option (a)) or would resolve (option (b)).

    Each reference is resolved to its binding and owner before any edit. Functions are decided
    outer body first, so a reference owned by an elided body falls back to the bindings that
    stay written around it. A reference that still resolves to the same declaration needs no
    edit. Any other is qualified once with a spelling of its package that no local hides. The
    owning function keeps its rows (fail closed) when:
      - a reference in its body has a binding the script cannot decide ('unsure');
      - a reference would fall back to a local value of the same name in an enclosing frame
        (rows rule 5: the prototype lets the capability win, the design rejects the program);
      - every spelling of the package is hidden by a local (it needs an import alias)."""
    plans = unit.__dict__.setdefault("_plans", {})
    if variant in plans:
        return plans[variant]
    lex = lexical(unit)
    p = unit.parser
    names = {n for s in lex.scopes for n in binds(s)}
    before, unsure = {}, []
    for k in range(len(p.toks)):
        if lex.is_ref(k) and p.t(k).text in names:
            r = lex.resolve(p.t(k).text, k)
            if r and r[0] == "cap":
                before[k] = r
            elif r and r[0] == "unsure":
                unsure.append((k, r[1]))
    owned = {}
    for k, r in before.items():
        owned.setdefault(id(r[3]), []).append(k)
    elided, blocked, refs = set(), {}, []
    for s in lex.scopes:    # outer first
        if not (entries(s) and c1_base(s, variant, unit)):
            continue
        doubt = [why for k, why in unsure if s.body[0] < k < s.body[1] and p.t(k).text in binds(s)]
        if doubt:
            blocked[id(s)] = doubt[0]
            continue
        trial = elided | {id(s)}
        mine = []
        for k in owned.get(id(s), ()):
            cap, written = before[k][1], before[k][2]
            after = lex.resolve(p.t(k).text, k, trial)
            if after and after[0] == "cap" and after[1] == cap:
                continue
            reason, q = None, None
            if after and after[0] == "local":
                reason = "a local value of that name in an enclosing frame would capture it"
            elif after and after[0] == "unsure":
                reason = after[1]
            else:
                q = lex.qualifier(written, cap[0], k)
                if q is None:
                    reason = "a local hides every spelling of the package (needs an import alias)"
            mine.append({"tok": k, "name": p.t(k).text, "cap": cap, "after": after,
                         "qualifier": q, "reason": reason, "owner": s})
        reasons = [r["reason"] for r in mine if r["reason"]]
        if reasons:
            blocked[id(s)] = reasons[0]
        else:
            elided = trial
        refs += mine
    plans[variant] = {"elided": elided, "blocked": blocked, "refs": refs}
    return plans[variant]


def export_inventory(units):
    """Import path -> exported capability names, from the library reconstruction and the stubs
    file. A package with a section there has an inventory (possibly empty); any other has none."""
    inv = {}
    for u in units:
        if u.group not in ("library", "exports"):
            continue
        for path in u.parser.packages:
            inv.setdefault(path, set())
        for path, names in u.parser.ifaces.items():
            inv.setdefault(path, set()).update(names)
    return inv


def option_b(unit, ref, inventory):
    """How a reference that lost its binding resolves under option (b): frames and same-package
    declarations first, then exactly one imported package (by import path) exporting the name."""
    lex = lexical(unit)
    after = ref["after"]
    if after is not None and after[0] in ("cap", "pkg", "local"):
        return "lexical"        # an outer written `uses`, a local or a same-package declaration captures it
    if after is not None:
        return "unresolved"     # the script cannot decide the binding
    if not lex.has_imports:
        return "unresolved"     # a fragment with no import declarations
    paths = set(lex.imports.values())
    go_marked = {path for alias, path in unit.parser.imports if alias == "go"}
    known = {pa for pa in paths - go_marked if ref["name"] in inventory.get(pa, ())}
    unknown = {pa for pa in paths if pa not in inventory or pa in go_marked}
    if len(known) > 1:
        return "ambiguous"
    if unknown:
        return "unresolved"
    if known == {ref["cap"][0]}:
        return "unique"
    return "unbound" if not known else "wrong"


B_OUTCOMES = ("unique", "ambiguous", "lexical", "unresolved", "unbound", "wrong")


def names_summary(units, inventory, variant="landing"):
    """Option (a) and option (b) over the references that elision of `variant` unbinds."""
    out = {"uses": 0, "by_capability": {}, "by_position": {"selector": 0, "wire or replace key": 0, "other": 0},
           "qualified": 0, "kept": 0, "kept_functions": 0, "kept_reasons": {}, "functions": 0,
           "b": {k: 0 for k in B_OUTCOMES}, "equal_calls": 0, "list": []}
    for u in units:
        plan = name_plan(u, variant)
        p = u.parser
        out["kept_functions"] += len(plan["blocked"])
        for why in plan["blocked"].values():
            out["kept_reasons"][why] = out["kept_reasons"].get(why, 0) + 1
        out["functions"] += len({id(r["owner"]) for r in plan["refs"]})
        for r in plan["refs"]:
            k = r["tok"]
            out["uses"] += 1
            out["by_capability"][show(r["cap"])] = out["by_capability"].get(show(r["cap"]), 0) + 1
            nxt = p.t(k + 1).text
            pos = "selector" if nxt == "." else "wire or replace key" if nxt == "=" else "other"
            out["by_position"][pos] += 1
            b = option_b(u, r, inventory)
            out["b"][b] += 1
            if id(r["owner"]) in plan["blocked"]:
                out["kept"] += 1
                spelled = "row kept"
            else:
                out["qualified"] += 1
                spelled = f"{r['qualifier']}.{r['name']}"
                if r["cap"] == ("testing", "T") and nxt == "." and p.t(k + 2).text == "Equal" and p.t(k + 3).text == "(":
                    out["equal_calls"] += 1
            out["list"].append(f"{u.rel}:{p.line_of(p.t(k).start)} {r['name']} ({pos}) -> {spelled}; (b) {b}")
    out["by_capability"] = dict(sorted(out["by_capability"].items()))
    out["tokens_a"] = 2 * out["qualified"]
    out["tokens_a_testing_Equal"] = out["tokens_a"] - 2 * out["equal_calls"]
    out["tokens_b"] = 2 * sum(out["b"][k] for k in ("ambiguous", "lexical", "unbound", "wrong"))
    return out


def inventory_caps(inventory):
    return {(path, n) for path, names in inventory.items() for n in names}


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
        problems += binding_check(u.text, new, f"controls/after/{name}", caps)
        s = names_summary([u], inventory)
        got_a = f"qualified {s['qualified']}, row kept {s['kept']}"
        got_b = ", ".join(f"{k} {s['b'][k]}" for k in ("unique", "ambiguous", "lexical", "unresolved"))
        want = dict(re.findall(r"^// expect \((a|b)\): (.*)$", u.text, re.M))
        if want.get("a") != got_a or want.get("b") != got_b:
            problems.append(f"{u.rel}: measured (a) {got_a}; (b) {got_b}; the file expects (a) {want.get('a')}; (b) {want.get('b')}")
        about = re.search(r"^// control: (.*)$", u.text, re.M)
        rows.append({"control": name.removesuffix(".go.sketch"), "about": about.group(1) if about else "",
                     "a": got_a, "b": got_b, "tokens_a": s["tokens_a"], "b_counts": s["b"]})
    return rows, problems


# Known-bad rewrites the binding check must reject: (control, [(text in its after/ file, bad text)], what).
CHECKER_MUTANTS = (
    ("nested-literal-same-row", [("http.Client.Ping", "http.http.Client.Ping")], "qualified twice (round 2)"),
    ("inline-wire-key", [("wire{ http.Client =", "wire{ Client =")], "bare wire key (round 2)"),
    ("replace-key", [("replace http.Client", "replace Client")], "bare replace target (round 2)"),
    ("package-name-shadowed-alias", [("web.Client", "http.Client")], "qualifier captured by a local (round 2)"),
    ("slice-parameter-named-http", [("func work(http []string) uses http.Client, Console {", "func work(http []string) {"),
                                    ("\tClient.Ping()", "\thttp.Client.Ping()")], "qualifier captured by a slice parameter (round 3)"),
    ("outer-value-inner-capability", [("func() uses http.Client { Client.Ping() }", "func() { Client.Ping() }")],
     "bare call captured by an outer value (round 3)"),
    ("same-leaf-distinct-paths", [("{ fresh.Client.Ping() }", "{ Client.Ping() }")],
     "bare call bound to the outer provider of another package (round 3)"),
)


def checker_self_test(caps):
    """Each mutant must draw at least one binding-check problem. Returns the mutants that pass unseen."""
    unseen = []
    for name, pairs, what in CHECKER_MUTANTS:
        before = (HERE / "controls" / "before" / f"{name}.go.sketch").read_text()
        text = (HERE / "controls" / "after" / f"{name}.go.sketch").read_text()
        for good, bad in pairs:
            if good not in text:
                unseen.append(f"{name}: {good!r} is not in its after/ file")
                break
            text = text.replace(good, bad, 1)
        else:
            if not binding_check(before, text, f"mutant/{name}", caps):
                unseen.append(f"{name}: the binding check accepts a known-bad rewrite ({what})")
    return unseen


class Rescan:
    """The binding check's resolver, written apart from Lexical and from the parser's signature
    reader so that one resolver cannot certify its own error. One left-to-right pass over the
    tokens keeps a stack of frames: a function or literal body opens a frame holding its
    parameters and its `uses` bindings; any other brace opens a block frame; `x :=`, `var x`
    and `catch e` add x to the current frame from that point on. A bare name resolves in the
    innermost frame that has it; a frame holding it both as a local and as a binding is
    'unsure'. A qualified `a.X` is a package reference only when `a` is no local and is an
    import spelling; its identity is (import path, X)."""

    def __init__(self, text, names=None):
        self.toks = tokenize(text)
        self.n = len(self.toks)
        self.imports, self.has_imports, self.decls = {}, False, set()
        self.bodies, self.row, self.declared_at = {}, set(), set()
        self.uses_caps = set()
        self.header_scan()
        self.names = names if names is not None else {x for hdr in self.bodies.values() for x in hdr["binds"]}
        self.refs, self.doubled = [], []

    def tok(self, i):
        return self.toks[min(i, self.n - 1)]

    def match(self, i):
        """i at an opening bracket; the index after its partner."""
        pairs = {"(": ")", "[": "]", "{": "}"}
        open_, close, depth = self.tok(i).text, pairs[self.tok(i).text], 0
        while i < self.n:
            t = self.toks[i]
            if t.kind == "punct" and t.text == open_:
                depth += 1
            elif t.kind == "punct" and t.text == close:
                depth -= 1
                if depth == 0:
                    return i + 1
            i += 1
        return self.n

    def header_scan(self):
        depth = 0
        i = 0
        while i < self.n:
            t = self.toks[i]
            if t.kind == "punct" and t.text == "{":
                depth += 1
            elif t.kind == "punct" and t.text == "}":
                depth -= 1
            elif depth == 0 and t.text == "import" and t.kind == "ident":
                self.has_imports = True
                end = self.match(i + 1) if self.tok(i + 1).text == "(" else i + 3
                for k in range(i + 1, min(end, self.n)):
                    if self.toks[k].kind == "str":
                        prev = self.toks[k - 1]
                        path = self.toks[k].text.strip('"')
                        alias = prev.text if prev.kind == "ident" and prev.text != "import" else None
                        if alias != "_":
                            spelling = alias if alias not in (None, "go") else path.rsplit("/", 1)[-1]
                            self.imports[spelling] = None if spelling in self.imports and self.imports[spelling] != path else path
                i = end
                continue
            elif depth == 0 and t.text in ("type", "wire") and self.tok(i + 1).kind == "ident":
                self.decls.add(self.tok(i + 1).text)
            if t.kind == "ident" and t.text == "func":
                self.header(i)
            i += 1

    def header(self, i):
        j = i + 1
        recv = None
        if self.tok(j).text == "(":
            close = self.match(j)
            if self.tok(close).kind == "ident" and self.tok(close + 1).text in ("(", "["):
                recv, j = (j, close), close
        named = False
        if self.tok(j).kind == "ident":
            named, j = True, j + 1
        if self.tok(j).text == "[":
            j = self.match(j)
        if self.tok(j).text != "(":
            return
        params = (j, self.match(j))
        j = params[1]
        binds, body, depth, row_span = {}, None, 0, None
        while j < self.n:
            t = self.toks[j]
            if t.kind == "eof":
                break
            if depth == 0 and (t.kind == "nl" or t.text in (",", ")", "]", ";", "=", "}")):
                break
            if t.kind == "punct" and t.text == "{":
                if self.tok(j - 1).text in ("struct", "interface"):
                    j = self.match(j)
                    continue
                if depth == 0:
                    body = j
                    break
            if t.kind == "punct" and t.text in "([":
                depth += 1
            elif t.kind == "punct" and t.text in ")]":
                depth -= 1
            if depth == 0 and t.text == "uses":
                k, start = j + 1, j
                while self.tok(k).kind == "ident":
                    a = self.tok(k).text
                    if self.tok(k + 1).text == "." and self.tok(k + 2).kind == "ident":
                        binds[self.tok(k + 2).text] = a
                        k += 3
                    else:
                        k += 1
                    nxt = self.tok(k + 2)
                    if self.tok(k).text == "," and self.tok(k + 1).kind == "ident" and (
                            nxt.text in (",", "{", ".", ")", "where") or nxt.kind in ("nl", "eof")):
                        k += 1
                        continue
                    break
                row_span = (start, k)
                j = k
                continue
            j += 1
        if body is None or body in self.bodies:     # a function type, or a result type sharing its function's brace
            return
        if row_span:
            self.row.update(range(*row_span))
        names = self.param_list(*params, literal=not named and recv is None)
        if recv:
            names += self.param_list(*recv, literal=False, receiver=True)
        self.declared_at.update(k for _, k in names)
        self.bodies[body] = {"params": {n for n, _ in names}, "binds": binds}

    def param_list(self, open_, close, literal, receiver=False):
        """[(name, token)] a parameter list declares, by Go's grammar: when any entry is
        `name Type`, every leading identifier is a name; a literal's bare entries are names."""
        items, cur, depth = [], [], 0
        for k in range(open_ + 1, close - 1):
            t = self.toks[k]
            if t.kind == "nl":
                continue
            if t.kind == "punct" and t.text in "([{":
                depth += 1
            elif t.kind == "punct" and t.text in ")]}":
                depth -= 1
            if t.kind == "punct" and t.text == "," and depth == 0:
                items.append(cur)
                cur = []
            else:
                cur.append(k)
        if cur:
            items.append(cur)

        def has_name(it):
            if len(it) < 2 or self.toks[it[0]].kind != "ident":
                return False
            second = self.toks[it[1]]
            if second.text == ".":
                return False
            if second.text == "[":      # `xs []T` and `xs [4]T` are named; `Box[T]` is a type
                third = self.toks[it[2]] if len(it) > 2 else None
                return third is not None and (third.text == "]" or third.kind == "num")
            return True

        with_names = [it for it in items if has_name(it)]
        if receiver:
            return [(self.toks[it[0]].text, it[0]) for it in with_names]
        singles = [it for it in items if len(it) == 1 and self.toks[it[0]].kind == "ident"]
        if with_names:
            return [(self.toks[it[0]].text, it[0]) for it in with_names + singles]
        return [(self.toks[it[0]].text, it[0]) for it in singles] if literal else []

    def run(self, caps):
        frames = [{"kind": "file", "locals": set(), "binds": {}}]
        for i in range(self.n):
            t = self.toks[i]
            if t.kind == "punct" and t.text == "{":
                hdr = self.bodies.get(i)
                frames.append({"kind": "func", "locals": set(hdr["params"]), "binds": dict(hdr["binds"])} if hdr
                              else {"kind": "block", "locals": set(), "binds": {}})
                continue
            if t.kind == "punct" and t.text == "}":
                if len(frames) > 1:
                    frames.pop()
                continue
            if t.kind != "ident" or i in self.row or i in self.declared_at:
                continue
            prev, nxt = self.tok(i - 1), self.tok(i + 1)
            if prev.text == ".":
                continue
            j = i
            while self.tok(j).kind == "ident" and self.tok(j + 1).text == ",":
                j += 2
            if prev.text in ("var", "catch") or (self.tok(j).kind == "ident" and self.tok(j + 1).text == ":="):
                frames[-1]["locals"].add(t.text)
                continue
            if nxt.text == ":" or not any(f["kind"] == "func" for f in frames):
                continue
            here = self.lookup(t.text, frames)
            if nxt.text == "." and self.tok(i + 2).kind == "ident" and here is None:
                path = self.imports.get(t.text) if self.has_imports else t.text
                member = self.tok(i + 2).text
                if path is not None and (path, member) in caps:
                    self.refs.append((path, member))
                if path is not None and t.text in self.imports and member in self.imports:
                    self.doubled.append(f"{t.text}.{member}")
                if path is not None or t.text not in self.names:
                    continue
            if t.text in self.names:
                self.refs.append(self.resolve(t.text, here))
        return self

    def lookup(self, name, frames):
        for f in reversed(frames):
            loc, cap = name in f["locals"], name in f["binds"]
            if loc and cap:
                return ("unsure",)
            if loc:
                return ("local",)
            if cap:
                written = f["binds"][name]
                path = self.imports.get(written) if self.has_imports else written
                return ("cap", path) if path else ("unsure",)
        return None

    def resolve(self, name, here):
        if here is None:
            return ("pkg", name) if name in self.decls else None
        if here[0] == "cap":
            return (here[1], name)
        return here


def binding_check(before_text, after_text, label, caps):
    """The rewrite keeps every capability reference bound to the same declaration and writes no
    doubled qualifier, as Rescan resolves both texts from scratch. Returns a list of problems."""
    rb = Rescan(before_text)
    ra = Rescan(after_text, names=rb.names)
    caps = caps | {(rb.imports.get(a) if rb.has_imports else a, x)
                   for hdr in rb.bodies.values() for x, a in hdr["binds"].items()}
    rb.run(caps)
    ra.run(caps)
    problems = [f"{label}: doubled qualifier {d}" for d in ra.doubled]
    if rb.refs != ra.refs:
        for i, (x, y) in enumerate(zip(rb.refs, ra.refs)):
            if x != y:
                problems.append(f"{label}: capability reference {i + 1} resolves to {y!r}, was {x!r}")
                break
        else:
            problems.append(f"{label}: {len(rb.refs)} capability references before, {len(ra.refs)} after")
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
    caps = inventory_caps(exports_inv)
    problems, checked, refs_checked = [], 0, 0
    for cand in ("row-elision", "row-elision-unguarded"):
        for b, a in zip(units, after[cand]):
            if b.group == "fixtures":
                continue
            problems += binding_check(b.text, a.text, a.rel, caps)
            checked += 1
    for b in units:
        if b.group != "fixtures":
            rb = Rescan(b.text)
            refs_checked += len(rb.run(caps | {(rb.imports.get(x) if rb.has_imports else x, n)
                                               for hdr in rb.bodies.values() for n, x in hdr["binds"].items()}).refs)
    control_rows, control_problems = control_results(controls, exports_inv, caps)
    res["controls"] = control_rows
    unseen = checker_self_test(caps)
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
         ["functions that keep their rows (fail closed: binding not provable after elision)", nm["kept_functions"], 0],
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
        f"**Binding check.** A second resolver, `Rescan`, shares only the tokenizer with the rewriter's resolver and the parser's "
        f"signature reader: one left-to-right pass over the tokens with a stack of lexical frames. It resolves every "
        f"capability reference of each unit from scratch, before and after both elision rewrites ({bc['units']} unit "
        f"rewrites; {bc['references']} references in the corpus before the landing rewrite) and of each control, and the "
        f"run fails when a reference resolves to another declaration (by full import path) or to nothing, or when a "
        f"qualifier that resolves to an import is followed by an import name (`http.http.Client`; `http.http` through a "
        f"value stays legal). Each control's rewrite must also equal its hand-written `controls/after/` file and the "
        f"outcomes its `// expect` lines state. The check must reject {bc['mutants']} known-bad rewrites of the controls "
        f"(`CHECKER_MUTANTS`): a doubled qualifier, a bare wire key, a bare `replace` target, a qualifier captured by a "
        f"local, a qualifier captured by a slice parameter, a call captured by an outer value, and a call bound to the "
        f"outer provider of another package with the same last path segment. Problems found: {len(bc['problems'])}.")
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
         f"({nm['tokens_a_testing_Equal']} with `testing.Equal`); {nm['kept_functions']} functions keep their rows (fail closed)",
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
