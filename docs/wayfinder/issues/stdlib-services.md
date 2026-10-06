<!-- {"id": "stdlib-services", "title": "Fallible providers, configuration and structured logs", "status": "open", "labels": ["implementation:task"], "parent": "map", "assignee": null, "blocked_by": ["stdlib-owned-core", "layer-construction"]} -->
# Fallible providers, configuration and structured logs

Implement Services in [bundled library capability contracts](../../specs/standard-library-capabilities.md): owned constructor effects with completed rollback, explicit shared materialized values, dependency-path/cycle diagnostics, then configuration/redaction and explicit Log providers.

Graph assembly and acquisition are supplied by the [first-class layer task](language-layers.md), including checked replacement, concurrent sharing and construction/operation separation. This task adds configuration/logging policy through that shared seam, rather than rebuilding a second provider framework.

Gate partial acquisition and cleanup defects, Missing-only defaults, separate invalid/source failures, secret-safe display/inspection, completed buffered log flush and unused-module receipts. Redacted display does not silently define transport encoding. Existing pure configured providers remain supported.
