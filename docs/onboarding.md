# TradeGrid Onboarding

Welcome to TradeGrid.

This guide is intentionally short. You do not need to know advanced Git to contribute.

## Before the Workshop

Install:

- Git
- VS Code or another editor
- A GitHub account

Verify Git:

```bash
git --version
```

## Your First Contribution

### 1. Clone the repository

```bash
git clone https://github.com/co-rtex/tradegrid.git
cd tradegrid
```

### 2. Create or claim a GitHub issue

For the first workshop, use the **First Contribution** issue template.

### 3. Create a branch

```bash
git checkout -b docs/ISSUE-your-github-username
```

Replace `ISSUE` with your issue number.

### 4. Create your contributor file

Create:

```text
contributors/<your-github-username>.md
```

Use the template in `contributors/README.md`.

### 5. Check your changes

```bash
git status
```

### 6. Stage and commit

```bash
git add .
git commit -m "Add <name> contributor profile"
```

### 7. Push your branch

```bash
git push -u origin docs/ISSUE-your-github-username
```

### 8. Open a pull request

On GitHub, choose **Compare & pull request**.

In the PR description include:

```text
Closes #ISSUE
```

### 9. Request review

A peer can review your change, then a maintainer will give final approval.

## The Workflow to Remember

```text
Issue -> Branch -> Change -> Commit -> Push -> Pull Request -> Review -> Merge
```

## Need Help?

Do not stay stuck silently. Ask in the TradeGrid help chat and include:

- What you were trying to do
- What command/action you used
- The exact error message
- A screenshot if useful
