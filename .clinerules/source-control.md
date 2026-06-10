# Source Control & Git Rules

1. **Automatic Commits:** Automatically stage and commit changes to Git after successfully implementing and testing a requested feature, updating documentation, or resolving a bug.
2. **Conventional Commits:** Always use standard conventional commit messages for all commits (e.g., `feat:`, `fix:`, `refactor:`, `docs:`, `chore:`).
3. **Commit Pacing:** Do not bundle massive, unrelated changes into a single commit. Commit logically separated units of work immediately after they are verified.
4. **Branching Strategy:** Do not create new branches automatically. Commit all changes directly to the current working** branch. However, you should suggest creating a new branch if a requested feature is complex, experimental, or risky. Always wait for explicit user approval before creating or switching branches.**
5. **Version Tagging:** After completing a meaningful batch of feature work or a bug-fix release, tag the current commit with a semantic version (e.g. `git tag v0.1.0 && git push origin v0.1.0`). Use `vMAJOR.MINOR.PATCH` — bump MAJOR for breaking changes, MINOR for new features, PATCH for bug fixes. The CI pipeline automatically builds binaries and Docker images when a tag is pushed.
