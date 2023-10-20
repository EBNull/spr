# Fork

This is a fork motivated by three things:

- To control when origin is fetched from
  - This almost works with `noRebase`, but then it isn't used for adding the `commit-id` and `status` fails silently
- To control when commits are rewritten
  - `status` should not automatically add `commit-id`, that should only happen on `update`
- To play nicely with git-branchless
  - When `git spr` rebases and updates a non-main local branch, `main` gets out of sync with `origin/main`.
    This means that `git sl` now thinks that just-pulled [`public` commits](https://github.com/arxanas/git-branchless/wiki/Command:-git-smartlog#public-commits)
    are now considered `draft` commits.
  - When `git spr` rewrites a local commit to add the `commit-id`, the old commit is still visible in `git sl`.
    - Thus, `git spr` should `git hide` / [_obsolete_](https://github.com/arxanas/git-branchless/blob/13b72512df612429be537601ab97ba71be323472/git-branchless-lib/src/core/eventlog.rs#L143) the commit

Stretch goals:

- Move the commit-id out of commit descriptions and into local refs and remote branch names
  - However, this adds local state (local refs) - perhaps we can cut them out during the final rebase / merge?
- Contribute back to upstream
