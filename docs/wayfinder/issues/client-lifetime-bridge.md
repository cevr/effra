<!-- {"id": "client-lifetime-bridge", "title": "Owned client lifetime bridge for view and foreign components (VW4)", "status": "open", "labels": ["wayfinder:task", "implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["typescript-declarations", "host-interop"]} -->
# Owned client lifetime bridge for view and foreign components (VW4)

Define and implement client-profile lifetime semantics through a user-selected pragma and lawful runtime/provider boundary, with React, Solid 2 and solid-yield as host validation targets. The [lawful runtime contract](../../research/lawful-runtime-contract.md) supplies the shared law-obligation categories and completed-shutdown evidence boundary. A provider may distinguish inert server markup from a client element with deferred code, retained state and host callbacks under a mount lifetime, but the compiler does not mandate `View.Node`/`Client.Element` names or a bespoke framework adapter API. JSX notation and a source gallery do not establish this bridge.

## Acceptance

- Keep the selected provider's server/client representations distinct in types, profiles and inspection. A client constructor/runtime owns mount, update, error and disposal lifetimes; callbacks and subscriptions cannot escape that owner.
- Preserve Effra rows and cancellation through the selected host runtime with explicit Pending/Failed/Ready or equivalent state, stale-response fencing, disconnect/unmount cleanup and error-boundary behavior. Preserve declared services and failures through the bridge; no unchecked `any`, hidden provider or compiler-hardcoded framework adapter.
- Exercise one React, one Solid 2 and one pinned solid-yield fixture with actual mount/update/dispose behavior through the selected pragma/runtime boundary, plus negative controls for retained callbacks, unresolved requirements and late responses. Record framework/runtime versions and source hashes.
- Keep hydration, streaming, SSR parity and external transactional guarantees separate. Full gate and independent review are required before a framework row becomes verified.

This is VW4 acceptance work; the current gallery and source sketches remain reference/design evidence.
