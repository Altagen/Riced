// Commitlint config for the CI commitlint job.
// We follow Conventional Commits + skip git's auto-generated "Merge ..."
// commit messages (these are produced by `git merge` and don't follow
// the conventional format by design).
module.exports = {
  extends: ['@commitlint/config-conventional'],
  ignores: [(message) => message.startsWith('Merge ')],
}
