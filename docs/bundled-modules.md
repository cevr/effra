# Compiler-distributed modules

Effra admits explicit portable imports from a finite compiler distribution.
`import Fns "effra/functions"` resolves the ordinary `Fns.call` forwarding
function and the pure `Fns.identity` function. Go package imports keep their
separate `import go` form. User package loading and version selection remain
unsupported and produce diagnostics without searching the filesystem or network.

`Fns.call` accepts a named effect callback from string to string. Its declared
row parameters preserve the callback's failures and requirements; execution
remains visible through `run`. The function body is ordinary checked `.ef`
source. Different import aliases refer to the same declaration identity.

The compiler indexes the available declarations, then loads only referenced
members and their declared dependency closure. Bundle source offsets belong to
separate source IDs, never to an injected user prelude. Check results include
the loaded bindings, exact source digests and a declared checker producer ABI.
This producer value identifies compatibility, not a reproducible binary hash.
User byte-offset queries continue to address the user's source only.

Referenced bodies have distinct target emission names and pass through the same
function, row and ownership checker as local helpers. Merely importing a module
does not select every implementation. The existing finite builtin prelude is
unchanged. Current native snapshots still contain all admitted runtime sources;
this module boundary makes no binary-size or compilation-performance claim.

Stored interface summaries, imported nominal/template data and initialized
conversion witnesses are subsequent implementation units. Public inspection
JSON is explanatory output and is never admitted as executable ownership proof.
