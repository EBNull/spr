package git

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ejoffe/spr/config"
	"github.com/rs/zerolog/log"
)

// GetLocalBranchName returns the current local git branch
func GetLocalBranchName(gitcmd GitInterface) string {
	var output string
	err := gitcmd.Git("branch --no-color", &output)
	check(err)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "* ") {
			return line[2:]
		}
	}
	panic("cannot determine local git branch name")
}

func BranchNameFromCommit(cfg *config.Config, commit Commit) string {
	remoteBranchName := cfg.Repo.GitHubBranch
	// TODO(eb): Make the branch prefix configurable, perhaps based on the commit description (ticket/bug id?)
	branchPrefix := "ebnull"
	elms := []string{"spr", remoteBranchName, commit.CommitID}
	if branchPrefix != "" {
		elms = append([]string{elms[0], branchPrefix}, elms[1:]...)
	}
	return strings.Join(elms, "/")
}

var BranchNameRegex = regexp.MustCompile(`spr/([a-zA-Z0-9_\-/\.]+/)?([a-zA-Z0-9_\-/\.]+)/([a-f0-9]{8})$`)

// GetLocalTopCommit returns the top unmerged commit in the stack
//
// return nil if there are no unmerged commits in the stack
func GetLocalTopCommit(cfg *config.Config, gitcmd GitInterface) *Commit {
	commits := GetLocalCommitStack(cfg, gitcmd)
	if len(commits) == 0 {
		return nil
	}
	return &commits[len(commits)-1]
}

// GetLocalCommitStack returns a list of unmerged commits
//
//	the list is ordered with the bottom commit in the stack first
func GetLocalCommitStack(cfg *config.Config, gitcmd GitInterface) []Commit {
	var commitLog string
	logCommand := fmt.Sprintf("log --format=medium --no-color %s/%s..HEAD",
		cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)
	gitcmd.MustGit(logCommand, &commitLog)
	commits, valid := parseLocalCommitStack(commitLog, true) // Allow patchIds (which papers over missing `commit-id` in descriptions)
	if !valid {
		// TODO(eb): Record bad commits (ones with no id) and match them up with good commits (ones with an id)
		//           Can probably use `git diff-tree HEAD -p | git patch-id` since we wouldn't be changing the content
		//           of the commit, only the message (and thus the hash).
		//           Using this patch id would let us tie in with `git branchless` and automatically `obsolete`
		//           the "bad" commit in favor of the "good" commit.
		panic("A commit in your patch stack is missing a `commit-id:xxxxxxxx` line.")
		// if not valid - run rebase to add commit ids
		rewordPath, err := exec.LookPath("spr_reword_helper")
		check(err)
		rebaseCommand := fmt.Sprintf("rebase %s/%s -i --autosquash --autostash",
			cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)
		gitcmd.GitWithEditor(rebaseCommand, nil, rewordPath)

		gitcmd.MustGit(logCommand, &commitLog)
		commits, valid = parseLocalCommitStack(commitLog, true)
		if !valid {
			// if still not valid - panic
			errMsg := "unable to fetch local commits\n"
			errMsg += " most likely this is an issue with missing commit-id in the commit body\n"
			panic(errMsg)
		}
	}
	return commits
}

// patchIdForCommit returns a patch ID, which is a "fuzzy" inexact identifier of a tree's contents
//
// While this ID is not stable when a commit's description is modified (such as by adding a commit-id),
// it is a useful approximation for a commit-id on the local system (and commits without a real commit-id
// should not be pushed).
//
// See https://git-scm.com/docs/git-diff-tree and https://git-scm.com/docs/git-patch-id for more details.
func patchIdForCommit(gitcmd GitInterface, commitHash string) (string, error) {
	// TODO(eb): Implement this - since the commit never leaves the local system, the commit hash works too
	return commitHash, nil
}

func parseLocalCommitStack(commitLog string, patchIdOk bool) ([]Commit, bool) {
	var commits []Commit

	commitHashRegex := regexp.MustCompile(`^commit ([a-f0-9]{40})`)
	commitIDRegex := regexp.MustCompile(`commit-id\:([a-f0-9]{8})`)

	// The list of commits from the command line actually starts at the
	//  most recent commit. In order to reverse the list we use a
	//  custom prepend function instead of append
	prepend := func(l []Commit, c Commit) []Commit {
		l = append(l, Commit{})
		copy(l[1:], l)
		l[0] = c
		return l
	}

	// commitScanOn is set to true when the commit hash is matched
	//  and turns false when the commit-id is matched.
	//  Commit messages always start with a hash and end with a commit-id.
	//  The commit subject and body are always between the hash the commit-id.
	commitScanOn := false

	subjectIndex := 0
	var scannedCommit Commit

	lines := strings.Split(commitLog, "\n")
	log.Debug().Int("lines", len(lines)).Msg("parseLocalCommitStack")
	for index, line := range lines {

		// match commit hash : start of a new commit
		matches := commitHashRegex.FindStringSubmatch(line)
		if matches != nil {
			log.Debug().Interface("matches", matches).Msg("parseLocalCommitStack :: commitHashMatch")
			if commitScanOn {
				// missing the commit-id of previous commit
				if !patchIdOk {
					log.Debug().Msg("parseLocalCommitStack :: missing commit id")
					return nil, false
				}
				// ah, but we can get a patchId instead
				patchId, err := patchIdForCommit(nil, scannedCommit.CommitHash)
				if err != nil {
					log.Debug().Msg(fmt.Sprintf("parseLocalCommitStack :: missing commit id and could not get patch id :: %s", err))
					return nil, false
				}
				log.Debug().Msg("parseLocalCommitStack :: but has patch ID; using that and marking commit WIP")
				// TODO: refactor, the next two lines are repeated in the "last thing in the commit" section below
				scannedCommit.CommitID = patchId
				scannedCommit.Body = strings.TrimSpace(scannedCommit.Body)

				scannedCommit.WIP = true // All commits using patchId must be marked WIP because we can never upload them

				commits = prepend(commits, scannedCommit)
			}
			commitScanOn = true
			scannedCommit = Commit{
				CommitHash: matches[1],
			}
			subjectIndex = index + 4
		}

		// match commit id : last thing in the commit
		matches = commitIDRegex.FindStringSubmatch(line)
		if matches != nil {
			log.Debug().Interface("matches", matches).Msg("parseLocalCommitStack :: commitIdMatch")
			scannedCommit.CommitID = matches[1]
			scannedCommit.Body = strings.TrimSpace(scannedCommit.Body)

			if strings.HasPrefix(scannedCommit.Subject, "WIP") {
				scannedCommit.WIP = true
			}

			commits = prepend(commits, scannedCommit)
			commitScanOn = false
		}

		// look for subject and body
		if commitScanOn {
			if index == subjectIndex {
				scannedCommit.Subject = strings.TrimSpace(line)
			} else if index == (subjectIndex+1) && line != "\n" {
				scannedCommit.Body += strings.TrimSpace(line) + "\n"
			} else if index > (subjectIndex + 1) {
				scannedCommit.Body += strings.TrimSpace(line) + "\n"
			}
		}

	}

	// if commitScanOn is true here it means there was a commit without
	//  a commit-id
	if commitScanOn {
		// missing the commit-id
		log.Debug().Msg("parseLocalCommitStack :: missing last commit id")
		return nil, false
	}

	log.Debug().Interface("commits", commits).Msg("parseLocalCommitStack")
	return commits, true
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
