<div align="center">
  <a href="https://vale.sh">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://vale.sh/brand/vale-logo-white.svg">
      <img src="https://vale.sh/brand/vale-logo.svg" alt="Vale" width="200">
    </picture>
  </a>
  <h3>Your style, our editor.</h3>
  <p>
    A command-line linter for prose. Turn your team's writing guidelines into checks that run<br>
    in your editor, in CI, and alongside your code&mdash;on macOS, Windows, and Linux, entirely offline.
  </p>
  <p>
    <a href="https://github.com/vale-cli/vale/actions/workflows/test.yml"><img alt="Test" src="https://github.com/vale-cli/vale/actions/workflows/test.yml/badge.svg?branch=v3"></a>
    <a href="https://github.com/vale-cli/vale/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/vale-cli/vale?label=release&color=62A527"></a>
    <a href="https://github.com/vale-cli/vale/releases"><img alt="GitHub downloads" src="https://img.shields.io/github/downloads/vale-cli/vale/total?logo=GitHub&label=downloads&color=333"></a>
    <a href="https://hub.docker.com/r/jdkato/vale"><img alt="Docker pulls" src="https://img.shields.io/docker/pulls/jdkato/vale?logo=docker&logoColor=white&color=1D63ED"></a>
    <a href="https://formulae.brew.sh/formula/vale"><img alt="Homebrew installs" src="https://img.shields.io/homebrew/installs/dy/vale?logo=homebrew&label=homebrew&color=FBB040"></a>
    <a href="https://community.chocolatey.org/packages/vale"><img alt="Chocolatey downloads" src="https://img.shields.io/chocolatey/dt/vale?logo=chocolatey&label=chocolatey&color=80B5E3"></a>
  </p>
  <p>
    <a href="https://docs.vale.sh">Docs</a> ·
    <a href="https://docs.vale.sh/topics/quickstart">Quickstart</a> ·
    <a href="https://vale.sh/explorer">Package Explorer</a> ·
    <a href="https://cms.vale.sh">Vale CMS</a> ·
    <a href="https://vale.sh/blog">Blog</a> ·
    <a href="https://discord.gg/NTX6Xggryj">Discord</a>
  </p>
</div>

<p align="center">
  <img width="80%" alt="Vale reporting alerts for a directory of Markdown files in a terminal." src="https://vale.sh/media/mac.png">
</p>

Vale doesn't ship opinions of its own. It's a framework for enforcing _your_ style: a published guide like Microsoft's or Google's, an in-house set of terms, or both. It's run by teams at AWS, NVIDIA, Microsoft, GitLab, and Red Hat, [among others who publish their configs](https://vale.sh/adopters).

```console
$ vale sync
 SUCCESS  Synced 2 package(s) to 'styles'.

$ vale docs/

 docs/configure.md
 3:7   suggestion  Consider using 'use' instead of 'utilize'.              Microsoft.Wordiness
 3:44  suggestion  'are loaded' looks like passive voice.                  Microsoft.Passive
 9:12  error       Use 'Vale CLI' instead of 'Vale cli'.                   Docs.Terms

 docs/install.md
 3:40  warning     Use 'select' instead of the input-specific verb 'Click'. Microsoft.UIVerbs
 4:28  error       Did you really mean 'existant'?                         Vale.Spelling

✖ 2 errors, 1 warning and 2 suggestions in 2 files.
```

## :heart: Sponsors

> Hi there! I'm [@jdkato](https://github.com/jdkato), the sole developer of Vale. If you'd like to help me dedicate more time to _developing_, _documenting_, and _supporting_ Vale, feel free to donate through [GitHub Sponsors](https://github.com/sponsors/jdkato) or [Open Collective](https://opencollective.com/vale). Any donation&mdash;big, small, one-time, or recurring&mdash;is greatly appreciated!

### Spotlights

<div align="center">
<table>
<tr>
<td align="center" width="50%">
  <a href="https://vale.sh/sponsors/mintlify">
    <img src="https://github.com/mintlify.png?size=200" width="72" alt="Mintlify">
  </a>
  <br><br>
  <a href="https://vale.sh/sponsors/mintlify"><b>Mintlify</b></a>
  <br>
  <sub>An AI-native documentation platform built for developers.</sub>
  <br>
  <sub>Ships Vale as a built-in CI check.</sub>
</td>
<td align="center" width="50%">
  <a href="https://vale.sh/sponsors/promptless">
    <img src="https://github.com/Promptless.png?size=200" width="72" alt="Promptless">
  </a>
  <br><br>
  <a href="https://vale.sh/sponsors/promptless"><b>Promptless</b></a>
  <br>
  <sub>Suggests doc updates when your product changes.</sub>
  <br>
  <sub>Runs Vale on every doc its agents write.</sub>
</td>
</tr>
</table>
</div>

> Sponsors at $1,000 and above get a dedicated page on [vale.sh](https://vale.sh/sponsor).

### Grants

Vale received a $10,000 grant from [FLOSS/fund](https://floss.fund/blog/second-tranche-2025-anniversary/) in its second 2025 tranche, alongside FFmpeg, Zig, Wireshark, and Blender.

### Organizations

<a href="https://opencollective.com/vale"><img src="https://opencollective.com/vale/organizations.svg?width=890" alt="Organizations sponsoring Vale on Open Collective"></a>

Everyone who funds Vale, individuals included, is listed on the [sponsors page](https://vale.sh/sponsor).

### Infrastructure

Thanks to [Fyra Stack](https://dash.fyrastack.com/) for hosting [Vale Studio](https://studio.vale.sh), and to [GitBook](https://www.gitbook.com/solutions/open-source) for hosting the [documentation](https://docs.vale.sh).

<p>
  <a href="https://dash.fyrastack.com/"><img src="https://img.shields.io/badge/Studio%20hosted%20by-Fyra%20Stack-FE6565?style=for-the-badge" alt="Studio hosted by Fyra Stack" height="40"></a>
  &nbsp;
  <a href="https://docs.vale.sh"><img src="https://img.shields.io/badge/Docs%20hosted%20by-GitBook-3884FF?style=for-the-badge&logo=gitbook&logoColor=white" alt="Docs hosted by GitBook" height="40"></a>
</p>

## Why Vale

Most tools see text. Vale sees a document.

- **It parses your markup instead of guessing at it.** Markdown, AsciiDoc, reStructuredText, HTML, and [every other format](https://docs.vale.sh/formats) go through a real parser. A rule can target headings, lists, or table cells, and code spans, URLs, and fenced blocks are skipped before a rule ever runs. See [Scopes](https://docs.vale.sh/topics/scopes).
- **Your comments are documentation too.** Vale lifts comments and docstrings out of source code with tree-sitter grammars, so a comment marker inside a string literal stays code. The Markdown in a Rust doc comment, or the reStructuredText in a Python docstring, is linted as though it were its own file. See [Code](https://docs.vale.sh/formats/code).
- **It finds prose in files that aren't prose.** A [View](https://docs.vale.sh/topics/views) says where the writing is in an OpenAPI description, a notebook cell, or a commit message, so Vale lints that and passes over the rest.
- **Rules are files, and they read grammar.** [Extension points](https://docs.vale.sh/checks/existence) in YAML run from a token list to part-of-speech patterns, cross-file relationships, readability formulas, and scripts. A rule can [carry its own fix](https://docs.vale.sh/topics/actions), which an editor applies with one keystroke and an agent applies without deciding anything.
- **One binary, nothing alongside it.** Written in Go, with no runtime to install and files linted in parallel. See [the benchmark](https://vale.sh/features/speed) on GitLab's documentation.
- **Private by design.** Nothing leaves your machine: no account, no upload, and no training on your writing.

## Install

| | |
| :-- | :-- |
| **macOS** | `brew install vale` |
| **Windows** | `choco install vale` &nbsp;·&nbsp; `winget install -e --id errata-ai.Vale` |
| **Linux** | `sudo snap install vale` &nbsp;·&nbsp; `sudo pacman -S vale` &nbsp;·&nbsp; `sudo apt install vale` |
| **Docker** | `docker pull jdkato/vale` |
| **Go** | `go install github.com/vale-cli/vale/v3/cmd/vale@latest` |
| **Browser** | `vale_<version>_wasm.tar.gz` from a release, loaded as a [WebAssembly module](https://docs.vale.sh/topics/wasm) |

Every release also ships [prebuilt binaries](https://github.com/vale-cli/vale/releases) for each platform. Scoop, MacPorts, FreeBSD ports, conda-forge, the APT archive, and the rest are on the [installation page](https://docs.vale.sh/topics/installation).

## Quickstart

Vale needs a configuration file that says where to keep styles and which to apply. Create one at the root of your project:

```ini
StylesPath = styles
MinAlertLevel = suggestion

Packages = Microsoft

[*.md]
BasedOnStyles = Vale, Microsoft
```

Then download the styles and lint something:

```bash
vale sync
vale README.md
```

The [Quickstart](https://docs.vale.sh/topics/quickstart) walks through each step. To skip the reading, open a chat already pointed at [vale.sh/AGENTS.md](https://vale.sh/AGENTS.md) and have an assistant set it up.

## Styles

A style is a folder of YAML rules. Start from one that's published&mdash;Microsoft, Google, Red Hat, and more are one line in your config away&mdash;and browse them all in the [Package Explorer](https://vale.sh/explorer). Or write your own:

```yaml
# styles/Docs/Terms.yml
extends: substitution
message: "Use '%s' instead of '%s'."
level: error
swap:
  'Vale cli|vale-cli': Vale CLI
```

That's the whole rule. See [Styles](https://docs.vale.sh/topics/styles) for the rest, and [Vale CMS](https://cms.vale.sh) to author a full project&mdash;config, rules, vocabularies, and tests&mdash;in the browser, with the real engine linting live.

## Contributing

Bug reports, feature requests, documentation fixes, and pull requests are all welcome. Start with the [contributing guide](.github/CONTRIBUTING.md), which covers setting up a development environment, testing, benchmarking, and the [contributor license agreement](.github/CLA.md). This project follows a [code of conduct](.github/CODE_OF_CONDUCT.md).

## License

[MIT](LICENSE)
