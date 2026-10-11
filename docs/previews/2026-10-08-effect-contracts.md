# 2026-10-08: explicit failure and service contracts

Label: **Checked** (the sketch called it "Available baseline"). Source: the Effra snippet in the 2026-10-08 language-evolution sketch, copied without changes. Checked again on 2026-10-11 with `ef check <file>` at commit `6b349af8`: zero diagnostics.

```rust
error NotFound
service Users { effect fn get(id: string) -> string raises { NotFound } }
effect fn greeting(id: string) -> string raises { NotFound } uses { Users } {
  let name = run Users.get(id)
  "Hello, " + name
}
```

What it shows:

- `greeting` names its failure alternatives in `raises { NotFound }` and the services it needs in `uses { Users }`. Both rows are part of its contract.
- Calling an effect function builds a recipe. `run` is the visible point inside an effect body where a recipe executes and its failures and requirements propagate.

What it does not claim: generalized `run` over other types, JSX pragmas, a user-selected effect runtime, or any performance result. The sketch's TypeScript/Effect and Go comparisons were source explanations rather than Effra fixtures, and are not recorded here.

Supersedes: none (first preview).
