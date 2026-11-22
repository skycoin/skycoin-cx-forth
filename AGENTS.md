# AGENTS.md

Rules for anyone changing this repository, people and coding agents alike.

## Naming

Every name describes itself. Names are hierarchical and lexicographically
prefixed, and they use Java type name style (UpperCamelCase).

- The most general part comes first, and each later part narrows it:
  `Struct`, `StructDefine`, `StructDefineFieldAdd`.
- Related names share a prefix, so they sort next to each other:
  `RegisterClear`, `RegisterFocusLeft`, `RegisterTypeSet`.
- A name that does not say what it is is invalid. That includes single
  letters, `Tmp`, `Data`, `Info`, `Handler`, `Do`, `Get`, and the method
  names `Error`, `Unwrap`, `Code`, `Is` and `As`.
- Booleans take no `is` or `has` prefix.
- Abbreviations are allowed only where they are the usual name, such as `ID`,
  `UDP` or `JSON`.

Forth words follow the same rule, for example `StructCursorNext` rather than
`NEXT`. Existing short words are renamed in their own change, with the old
name kept as an alias only until the CX IR translator stops emitting it.

## Changes

- `go test ./...` and `go vet ./...` pass before every push.
- One branch and one pull request per change, merged into `main`.
- No coding tool attribution in commits or pull requests.
