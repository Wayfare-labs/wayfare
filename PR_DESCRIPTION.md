commit: docs: add workflow permissions and blast radius section

pull request:

## Summary

- Adds `docs/workflow-permissions.md`, documenting the write permissions and
  supply-chain path for the two writing workflows — `measure` and `auto-merge` —
  and listing CI for contrast as the read-only workflow.
- For each workflow: events, declared permissions, what it can and cannot do,
  why it is a supply-chain path for published figures, and how a compromise would
  show up.

## Reasoning

The measure and auto-merge workflows can write to the repository, and their
output is embedded into the deployed binary. That makes them a supply-chain path
for published figures. This section records that path on the project's own
terms — permissions, scope, and blast radius — so a reader does not have to
infer it from the workflow files.

## Accepted criteria

- [x] Section added.
- [x] Each workflow's permissions listed.
- [x] Permission and blast radius documented per workflow, with CI listed for
      contrast.

## Files changed

- `docs/workflow-permissions.md` — new file
