# oh

**oh** is a coding harness.

- Secure: agents get unrestricted bash because the Linux kernel's sandboxing features are awesome.
- Robust: a plethora of golden and non-golden tests ensure that changes can be trusted.
- Pleasant: no unexpected repaints so you're not forcibly scrolled to the bottom while you're trying to read.
- Refreshing: configuration options and UI segments reload automatically.
- Modern: inline images, hyperlinks, window titles, synced repaints, and paste support.
- Consistent: a replayed session renders byte-identically to a live session.
- Instant: ready in 50ms, guaranteed to stay below the HCI threshold of 100ms.
- Efficient: chunked incremental markdown rendering keeps long messages generating smoothly.
- Relaxing: line-based streaming makes it easy to follow the endless river of prose.
- Integrated: provider usage data and limits allow dynamically swapping out based on availability.
- Configurable: arrange segments in any layout, and configure custom providers.

## Code

The code is in [crdx/io](https://github.com/crdx/io), under [cmd/oh](https://github.com/crdx/io/tree/main/cmd/oh#oh).

While I'm rapidly iterating it lives with the primitives it uses. Eventually it'll be separated out and moved here.

## Demo

Pick _`Simulation`_ when prompted.

```sh
go run crdx.org/io/cmd/oh@latest
```

## Installation

```sh
go install crdx.org/io/cmd/oh@latest
```
