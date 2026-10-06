<!-- {"id": "foundation-providers", "title": "Dependency-capturing provider composition", "status": "open", "labels": ["implementation:task"], "parent": "foundation-spec", "assignee": null, "blocked_by": ["foundation-data"]} -->
# Dependency-capturing provider composition

## Question

Implement checked provider dependencies/configuration, lexical capture and truthful dependency graph relationships per [the foundation spec](../../specs/production-foundations.md). Caller runtime ownership remains current; provider captures do not retain a closed construction fiber.
