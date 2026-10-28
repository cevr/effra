# Language server

`ergo lsp` is unavailable until it is ported to the new front end ([#203](https://github.com/cevr/ergo/issues/203)). It prints `ergo lsp: unavailable until ported to the new front end (#203)` to stderr and exits 1.

The language server read the retired `.ef` compiler's model, so [#205](https://github.com/cevr/ergo/issues/205) removed it with that compiler. The [retirement record](receipts/retire-ef.md#what-203-must-restore) lists the methods #203 restores and where the removed implementation is. Its last documentation is [`docs/lsp.md` at `dbf135bf`](https://github.com/cevr/ergo/blob/dbf135bff09901e85385945593113f35b300199c/docs/lsp.md).
