# go-cloud-lib

## Guidelines

### Keep the repo map current

`.claude/CLAUDE.md` is the map every session reads before touching anything, and its
whole value is accuracy. A stale map routes the next session to the wrong file
with full confidence — worse than no map at all.

It carries a one-line purpose for each top-level file and directory, the facts
that would otherwise cost a search to recover (that `deploy/`, `clusterize/`,
`join/`, `protocol/`, `scale_down/` and `bash_functions/` emit bash as Go string
literals assembled with `fmt.Sprintf` + `dedent.Dedent`, so positional `%s`
ordering is load-bearing; which `functions_def.FunctionName` each generator
injects; that `connectors` and `functions_def` are interfaces implemented by the
per-cloud repos, so a signature change is breaking for callers; the Go version
those callers build against), and the conventions that live in no single file.

Update it in the same change that invalidates it — a file moved, a module added,
a default changed, a convention shifted. Never as a follow-up commit.

Keep it navigational. It says where to look; it does not restate what the file
already says, and it does not grow a section for every detail.

### Agentic flow

The main agent is an orchestrator. It should delegate work via the Task tool and
minimize direct tool use — direct tool use is acceptable only for 1-2 quick
checks to orient. Always set the model explicitly on tasks/subagents.

Delegation model, in order:

1. **Haiku task** — all codebase exploration, investigation, searching, and
   reading files. Even for complex debugging: haiku can read and trace code
   paths, and it is 10-20x cheaper than opus.
2. **Sonnet task** — code edits, test runs, build verification, deploy flows.
3. **Opus task** — only for complex plan generation that needs deep understanding
   of the codebase, and only on top of context a haiku task already gathered, or
   when sonnet is struggling with execution.

### Scope of a change

Fix what was asked, and only that. Unrelated problems you notice along the way
stay untouched — even obvious ones, even one-line ones. Mention them and offer
to open a ticket instead.

A change is in scope only if the requested fix does not work without it.

### After any code change

Run `/simplify` once the change is complete and working. Not after each edit —
after the change as a whole.

Then run `go build ./...`, `go vet ./...`, and `go test ./...`. There is no
Makefile and no CI in this repo; these three are the only automated checks that
exist.

### Verify before reporting done

Run the checks above and read the output before saying a change works.
"Should work" is not a result, and neither is a run you launched but never
looked at.

When a check fails, say so and show the real output. When you skipped one —
actually running the generated bash on a cloud instance, no credentials, not
reproducible locally — name which and why. Silence reads as "it passed".

### Commit messages

One line. No body, no bullet list, no trailing explanation — if the change needs
more words than a single subject line, those words belong in the PR description,
the code, or a comment, not in the commit.

Existing history is the bar: `fix: read weka CLI output via -o/--no-header instead of positional parsing`.

### Comments

Keep them minimal. Write a comment only when the code cannot be made obvious on
its own: a non-obvious WEKA CLI, cloud-provider, or upstream-library constraint,
a deliberate omission, a workaround whose reason isn't visible in the diff. Do
not comment what the code already says.

`lib/jrpc/jrpc.go:309` is the bar — it carries information the reader
cannot recover from the code.

The test is what happens when the code is wrong. Comment what fails *silently*,
at apply time, or in only one environment — those cost a cycle to rediscover.
Say nothing about syntax, types, or API shape: `go vet ./...` and the compiler
(neither of which reads the bash inside string literals) rejects those
instantly and loudly, so the comment buys nothing even when it is accurate.

### Shell function names, in two places, must match

The generated bash calls these functions as bare words inside Go string
literals, so the compiler never sees them and no test covers the generators —
rename a constant and `go build ./...` stays green while the script dies on the
instance.

Currently paired: the `FunctionName` constants in `functions_def/functions_def.go`
(`clusterize`, `clusterize_finalization`, `deploy`, `protect`, `report`, `join`,
`join_finalization`, `join_nfs_finalization`, `setup_nfs`, `fetch`, `status`)
and their call sites in the script literals under `clusterize/`, `deploy/`,
`join/`, `protocol/`, and `scale_down/`.

Change one, change the other in the same commit.
