# oh

**oh** is a command-line coding harness with a focus on security, speed, and reliability.

<!-- The user, model, and environment must have the same knowledge. -->

- Secure: agents get unrestricted bash because the Linux kernel's sandboxing features are awesome.
- Robust: a plethora of golden and non-golden tests ensure that changes can be trusted.
- Pleasant: no unexpected repaints so you're not forcibly scrolled to the bottom while you're trying to read.
- Refreshing: configuration options and UI segments reload automatically.
- Modern: inline images, hyperlinks, window titles, synced repaints, and paste support.
- Consistent: a replayed session renders byte-identically to a live session.
- Efficient: chunked incremental markdown rendering keeps long messages generating smoothly.
- Relaxing: line-based streaming makes it easy to follow the endless river of prose.
- Integrated: provider usage data and limits allow dynamically swapping out based on availability.

## Install

```sh
go install crdx.org/oh@latest
```

## Run Demo

```sh
go run crdx.org/oh --demo
```

## Primitives

- `pkg/agent`: conversation loop, with streaming, batching, and cancellation
- `pkg/ask`: confirmations and choices mediated by an interface
- `pkg/tool`: tools, schemas, middleware, and concurrency
- `pkg/toolbox`: implementation of standard tools, plus extras
- `pkg/session`: session saving and resumption as an append-only journal
- `pkg/wire/anthropic/messages`: the Anthropic Messages protocol
- `pkg/wire/openai/chatcompletions`: the OpenAI-compatible Chat Completions protocol
- `pkg/wire/openai/responses`: the OpenAI Responses protocol
- `pkg/provider/anthropic`: the Claude subscription service
- `pkg/provider/codex`: the ChatGPT Codex service
- `pkg/provider/opencodego`: the OpenCode Go service
- `pkg/provider/ollama`: local and network Ollama servers

Additional information about these can be found in [pkg/README.md][pkg/README.md].

## Contributions

Open an [issue](https://github.com/crdx/oh/issues) or send a [pull request](https://github.com/crdx/oh/pulls).

## Licence

[GPLv3](LICENCE).
