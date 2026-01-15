---
title: Contributing
description: How to contribute to dnsweaver development
icon: material/heart
---

# Contributing to dnsweaver

Thank you for your interest in contributing to dnsweaver! This section provides everything you need to get started.

## Ways to Contribute

<div class="grid cards" markdown>

-   :material-bug:{ .lg .middle } **Report Bugs**

    ---

    File issues with clear reproduction steps on GitHub.

    [:octicons-arrow-right-24: Open an Issue](https://github.com/maxfield-allison/dnsweaver/issues)

-   :material-lightbulb:{ .lg .middle } **Suggest Features**

    ---

    Open discussions for new ideas and enhancements.

    [:octicons-arrow-right-24: Discussions](https://github.com/maxfield-allison/dnsweaver/discussions)

-   :material-code-tags:{ .lg .middle } **Submit Code**

    ---

    Fix bugs, add features, improve performance.

    [:octicons-arrow-right-24: Development Setup](development.md)

-   :material-dns:{ .lg .middle } **Add Providers**

    ---

    Integrate new DNS services.

    [:octicons-arrow-right-24: Provider Guide](adding-provider.md)

</div>

## Quick Links

| Guide | Description |
| :---- | :---------- |
| [Development Setup](development.md) | Set up your local environment |
| [Adding a Provider](adding-provider.md) | Implement a new DNS provider |
| [Adding a Source](adding-source.md) | Implement a new hostname source |

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork** locally
3. **Create a branch** for your changes

```bash
git clone https://github.com/your-username/dnsweaver.git
cd dnsweaver
git checkout -b feature/your-feature-name
```

## Pull Request Process

1. **Create a focused PR** — One feature or fix per PR
2. **Write descriptive commits** — Follow conventional commit format
3. **Update documentation** — If your change affects user-facing behavior
4. **Add tests** — For new functionality
5. **Ensure CI passes** — All checks must be green

### Commit Message Format

```text
type(scope): description

[optional body]

[optional footer]
```

**Types:** `feat`, `fix`, `docs`, `test`, `refactor`, `chore`

**Examples:**
```
feat(provider): add AdGuard Home provider support
fix(docker): handle container restart events correctly
docs: update Technitium configuration examples
```

## Reporting Issues

When filing an issue, please include:

- **Bug reports**: Logs (redacted), configuration, steps to reproduce
- **Feature requests**: Use case description and proposed solution
- **Questions**: Check the [FAQ](../faq.md) first

## Code of Conduct

Be respectful, inclusive, and constructive. We're all here to build something useful together.

## License

By contributing, you agree that your contributions will be licensed under the project's [MIT License](https://github.com/maxfield-allison/dnsweaver/blob/main/LICENSE).
