# oh

**oh** is a command-line coding harness with a focus on security, speed, and reliability.

I started with Claude Code and OpenCode back in the day, and they were good. Then I discovered [pi](https://pi.dev), and, as they say, [the rest is history](https://textplain.org/pi).

I built a lot of extensions. When I couldn't do what I wanted to do with extensions anymore, I created patches against pi core and applied them to my custom build. When the number of patches became unwieldy, I [soft-forked pi](https://github.com/crdx/pi-io) at v0.62.0, with the intention of continuing to port in upstream changes.

Soon after, upstream began the major client/server refactor, and the situation became untenable. It became a hard fork. I had already toyed with the idea of my own harness, and this was when I started seriously thinking about it.

Finally, I had the idea to build a repo of primitives (wire formats, providers, agent loop) to play with agent world-building, or at least that was the intention. I started building a simple harness to test the primitives out, and then a month passed, and now I have a harness called oh, apparently.

## Security

Agent sandboxing does not have to be reserved for the cloud. The right security approach is not to throw your harness in a container and call it a day, because that treats both the harness and the model as untrusted. Security should be a first-class harness feature, not _just_ because it's very important to get right.

The Linux kernel is full of great sandboxing features we can use to restrict a model:

- Landlock LSM for filesystem restriction
- User namespaces so unprivileged code can build the environment
- PID and mount namespaces for the virtual _seams_ of the landscape it sees
- Network namespaces to cut it off from the rest of the world

These are the big ones. Others include `no_new_privs`, dropping the capability bounding set, a seccomp filter over socket families, resource limits, Go's `os.Root` for the file tools, read-only mount refinements (since Landlock cannot), and denial mounts via mode `000`.

## Terminal Features

The terminal emulator is full of features we can use to enhance the experience. Some of these are not new at all, but support remains fragmented, so they may be new to you.

- Kitty graphics protocol to render model-read images, and the usage bar pacing gauges
- OSC 66 text sizing so we can see our session mascot in all its glory
- ?2026 synchronised output for tear-free repainting
- OSC 9;4 progress indicators so you know when work is happening
- OSC 99 desktop notifications so you know when work is done
- ?1004 focus reporting so desktop notifications don't send pointlessly
- OSC 8 hyperlinks for everything linkable, including out-of-band tool results
- OSC 133 prompt marks for easy navigation through the past conversation
- OSC 52 for copying via in-band signalling, so it works over ssh
- ?5522 for pasting via in-band signalling, for pasting images
- OSC 2 window titles that follow the session and mark write access, with the title stack so yours comes back
- Kitty keyboard protocol to tell a bare escape from a fragmented sequence, so escape stops a turn at once
- Bracketed paste so a multi-line paste arrives as one dedented edit

## Install

```sh
go install crdx.org/oh@latest
```

## Run Demo

This will drop you in the simulator, where you won't need to spend real tokens.

The simulator is a fake provider that handles basic tool calls, and falls back to ELIZA for anything else.

```sh
go run crdx.org/oh@latest --demo
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

Additional information about these can be found in [pkg/README.md](pkg/README.md).

## Licence

[GPLv3](LICENCE).
