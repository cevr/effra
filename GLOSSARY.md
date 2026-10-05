# Effra

Effra describes server programs using explicit, inspectable effect contracts.

## Language

**Effect contract**: The success value, named failures, and required services of a deferred Effra program.

**Failure row**: The unordered set of nominal failures admitted by an effect contract.

**Requirement row**: The unordered set of nominal services needed to execute an effect.

**Target provider**: An implementation of a service on a particular execution target.

**Semantic revision**: The identity of the source snapshot described by inspection or diagnostics.

**Managed fiber**: An execution of an Effra effect with an owner and a completion result. Its cancellation request and completed shutdown are distinct states.

**Owning scope**: The lifetime that owns managed fibers and resource releases. Its closure establishes their completed shutdown and cleanup.

**Host declaration**: A Go or TypeScript declaration supplying the native shape and identity of an imported value or callable.

**Binding contract**: Supplemental behavioral facts attached to a host declaration, including its cancellation, failure, resource, and trust policy.
