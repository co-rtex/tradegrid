# Contributing to TradeGrid

TradeGrid is built by students with different levels of programming experience. These rules keep the project approachable and the codebase healthy.

## Core Rules

1. **Do not push directly to `main`.**
2. Every feature, fix, or documentation change should begin with a GitHub issue.
3. Use one branch per issue whenever practical.
4. Keep pull requests focused. Avoid unrelated changes.
5. Link the issue in your PR using `Closes #<issue number>`.
6. Test your work before requesting review.
7. Resolve review conversations before merge.
8. A maintainer should approve the PR before it is merged.
9. If you are blocked for more than 24 hours, ask in the TradeGrid help chat.
10. If you cannot finish an issue, tell a maintainer. Reassignment is normal and is not a failure.

## Branch Naming

Use:

```text
feature/<issue>-short-description
fix/<issue>-short-description
docs/<issue>-short-description
test/<issue>-short-description
```

Examples:

```text
feature/23-order-validation
docs/12-add-contributor-profile
test/31-matching-tests
```

## Commit Messages

Use short, descriptive messages:

```text
Add quantity validation
Create portfolio empty state
Document matching rules
```

Perfect commit history is not expected from new contributors. Clear intent matters more.

## Pull Request Checklist

Before requesting review:

- [ ] My PR addresses one primary issue
- [ ] I linked the issue
- [ ] I tested the change where applicable
- [ ] I did not include unrelated files
- [ ] I can explain what my change does
- [ ] I am ready for feedback

## Continuous Integration

Pull requests targeting `main` and pushes to `main` run backend formatting, vet,
and test checks plus frontend lint, typecheck, and build checks through GitHub
Actions. A PR should pass CI before merge.

## Reviews

Reviews are collaborative. Comments are about improving the code, not judging the contributor.

For significant architecture changes, discuss the approach with a maintainer before implementation.
