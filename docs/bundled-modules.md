# Compiler-distributed modules

Effra admits explicit portable imports from a finite compiler distribution.
`import Fns "effra/functions"` resolves the ordinary `Fns.call` forwarding
function, the pure `Fns.identity` function, and the managed callback forwarding
function `Fns.forwardFile`. Go package imports keep their
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

Each selected closure has a compiler-private versioned summary. Its strict
decoder requires all operational fields, including false, empty and absent
alternatives, and rejects incompatible schemas, stale bytes, invalid owners,
dangling references and cyclic callback graphs. Cached transport is immutable;
each check reconstructs occurrences and callback relations in its own arena.
Nominal/native identities must already have checked signature owners. Printed
names cannot admit another nominal or host type. Unsupported transient regions
and layouts produce an interface diagnostic.

Results distinguish source input, producer ABI, interface content hash, and a
hash of target function snippets generated in a deterministic isolated module
context. The implementation hash describes that generation context, not the
combined executable or runtime snapshot; final Go temporary numbering can also
depend on functions emitted earlier in the combined program.
Bodies still pass through the ordinary checker for emission, and their finalized
summaries must match the admitted transport. Borrowing and acquisition remain
distinct through `Fns.forwardFile`; callback forwarding does not grant an escape
from a closing scope. This boundary makes no cache-performance claim.

Imported template data and initialized conversion witnesses are subsequent
implementation units. Public inspection JSON is explanatory output and is never
admitted as executable ownership proof.
