package gitinterface

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/jonboulle/clockwork"
)

const (
	RefPrefix       = "refs/"
	BranchRefPrefix = "refs/heads/"
	TagRefPrefix    = "refs/tags/"
	RemoteRefPrefix = "refs/remotes/"
)

var (
	ErrReferenceNotFound = plumbing.ErrReferenceNotFound

	clock = clockwork.NewRealClock()
)

// GetTip returns the hash of the tip of the specified ref.
func GetTip(repo *git.Repository, refName string) (plumbing.Hash, error) {
	ref, err := repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return ref.Hash(), nil
}

// ResetCommit sets a Git reference with the name refName to the commit
// specified by its hash as commitID. Note that the commit must already be in
// the repository's object store.
func ResetCommit(repo *git.Repository, refName string, commitID plumbing.Hash) error {
	currentHEAD, err := repo.Head()
	if err != nil {
		return err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	if err := wt.Checkout(&git.CheckoutOptions{Branch: plumbing.ReferenceName(refName)}); err != nil {
		return err
	}

	if err := wt.Reset(&git.ResetOptions{Commit: commitID, Mode: git.MergeReset}); err != nil {
		return err
	}

	if currentHEAD.Type() == plumbing.HashReference {
		return wt.Checkout(&git.CheckoutOptions{Hash: currentHEAD.Hash()})
	}

	return wt.Checkout(&git.CheckoutOptions{Branch: currentHEAD.Name()})
}

// ResetDueToError is a helper used to reverse a change applied to a ref due to
// an error encountered after the change but part of the same operation. This
// ensures that gittuf operations are atomic. Otherwise, a repository may enter
// a violation state where a ref is updated without accompanying RSL entries or
// other metadata changes.
func ResetDueToError(cause error, repo *git.Repository, refName string, commitID plumbing.Hash) error {
	if err := ResetCommit(repo, refName, commitID); err != nil {
		return fmt.Errorf("unable to reset %s to %s, caused by following error: %w", refName, commitID.String(), cause)
	}
	return cause
}

// AbsoluteReference returns the fully qualified reference path for the provided
// Git ref.
func AbsoluteReference(repo *git.Repository, target string) (string, error) {
	if strings.HasPrefix(target, RefPrefix) {
		return target, nil
	}

	// Check if branch
	refName := plumbing.NewBranchReferenceName(target)
	_, err := repo.Reference(refName, false)
	if err == nil {
		return string(refName), nil
	}
	if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", err
	}

	// Check if tag
	refName = plumbing.NewTagReferenceName(target)
	_, err = repo.Reference(refName, false)
	if err == nil {
		return string(refName), nil
	}
	if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", err
	}

	return "", ErrReferenceNotFound
}

// RefSpec creates a Git refspec for the specified ref.  For more information on
// the Git refspec, please consult:
// https://git-scm.com/book/en/v2/Git-Internals-The-Refspec.
func RefSpec(repo *git.Repository, refName, remoteName string, fastForwardOnly bool) (config.RefSpec, error) {
	var (
		refPath string
		err     error
	)

	refPath = refName
	if !strings.HasPrefix(refPath, RefPrefix) {
		refPath, err = AbsoluteReference(repo, refName)
		if err != nil {
			return "", err
		}
	}

	if strings.HasPrefix(refPath, TagRefPrefix) {
		// TODO: check if this is correct, AFAICT tags aren't tracked in the
		// remotes namespace.
		fastForwardOnly = true
	}

	// local is always refPath, destination depends on trackRemote
	localPath := refPath
	var remotePath string
	if len(remoteName) > 0 {
		if strings.HasPrefix(refPath, BranchRefPrefix) {
			// refs/heads/<path> -> refs/remotes/<remote>/<path>
			rest := strings.TrimPrefix(refPath, BranchRefPrefix)
			remotePath = path.Join(RemoteRefPrefix, remoteName, rest)
		} else if strings.HasPrefix(refPath, TagRefPrefix) {
			// refs/tags/<path> -> refs/tags/<path>
			remotePath = refPath
		} else {
			// refs/<path> -> refs/remotes/<remote>/<path>
			rest := strings.TrimPrefix(refPath, RefPrefix)
			remotePath = path.Join(RemoteRefPrefix, remoteName, rest)
		}
	} else {
		remotePath = refPath
	}

	refSpecString := fmt.Sprintf("%s:%s", localPath, remotePath)
	if !fastForwardOnly {
		refSpecString = fmt.Sprintf("+%s", refSpecString)
	}

	return config.RefSpec(refSpecString), nil
}
