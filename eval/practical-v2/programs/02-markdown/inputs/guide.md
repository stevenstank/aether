Project *Aurora* Guide
======================

Aurora is a **fast**, _friendly_ tool for building `static sites`.
See the [docs][docs] or the [FAQ](https://example.com/faq "Frequently asked").  
This line follows a hard break.

## Installation

1. Download the archive.
2. Unpack it:

   ```sh
   tar xzf aurora.tar.gz && cd aurora
   ```

3. Run `./aurora --help`.

## Features & Limits

- [x] Markdown with *nested **strong** text*
- [ ] Plugins (planned)
- Tables, ~~deprecated~~ strikethrough, and <https://aurora.example.org/path?q=1&r=2>
  - nested item one
  - nested item two

> **Note:** quoted text can contain
> - lists
> - and `code`
>
> > even nested quotes

| Option | Default | Description |
|:-------|:-------:|------------:|
| `--out` | `public/` | Output directory |
| `--watch` | false | Rebuild on change \| fast |
| `--minify` | true |

---

    indented code block
    with <html> & entities

Setext two
----------

### Edge cases ###

Escapes: \*not emphasis\*, 5 \< 6, AT&amp;T, a & b, snake_case_name, 2*3*4.
Unclosed **strong and [unclosed link and `unclosed code.
Image: ![Logo *bold*](img/logo.png "The logo"), bad link [x](javascript:alert(1)).
Mail: <team@aurora.example.org> and <not a link>.
Unicode: café — 日本語 — emoji 🎉.

<div class="raw">
  <p>Raw HTML passes through.</p>
</div>

[docs]: https://aurora.example.org/docs 'Documentation'
[Unused Ref]: /nowhere
