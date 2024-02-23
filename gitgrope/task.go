package gitgrope

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/google/go-github/v59/github"
	"github.com/sirupsen/logrus"
)

type Task struct {
	Run        string `yaml:"run"`
	Name       string `yaml:"name"`
	Shell      string
	WaitSwitch string
	Log        *logrus.Logger
}

func (t *Task) ExecuteFor(repo *Repository, release *github.RepositoryRelease, dir, assetNameList string) bool {
	envs := os.Environ()

	moreenvs := []string{
		fmt.Sprintf("GITHUB_RELEASE_REPO=%s", repo.Name),
		fmt.Sprintf("GITHUB_RELEASE_URL=%s", release.GetURL()),
		fmt.Sprintf("GITHUB_RELEASE_ASSETS=%s", assetNameList),
		fmt.Sprintf("GITHUB_RELEASE_TAG=%s", release.GetTagName()),
		fmt.Sprintf("GITHUB_RELEASE_COMMITSH=%s", release.GetTargetCommitish()),
	}

	envs = append(envs, moreenvs...)

	proc := exec.Command(t.Shell, t.WaitSwitch, t.Run)

	proc.Env = envs
	proc.Dir = dir
	proc.Stdout = t.Log.WriterLevel(logrus.InfoLevel)
	proc.Stderr = t.Log.WriterLevel(logrus.ErrorLevel)

	err := proc.Run()
	if err != nil {
		switch e := err.(type) {
		case *exec.ExitError:
			t.Log.WithError(e).Errorf("%s.%s.%s: task process exited with %d", repo.Name, release.GetName(), t.Name, e.ExitCode())
		default:
			t.Log.WithError(e).Errorf("%s.%s.%s: task process not successful", repo.Name, release.GetName(), t.Name)
		}
		return false
	}
	return true
}
