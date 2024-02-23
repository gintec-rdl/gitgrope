package gitgrope

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/google/go-github/v59/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type AssetGlob struct {
	Globber glob.Glob
	Pattern string
}

type TimeSeconds struct {
	time.Duration
}

func (ts *TimeSeconds) UnmarshalYAML(value *yaml.Node) error {
	var intValue int = 0
	err := value.Decode(&intValue)
	if err != nil {
		return err
	}
	ts.Duration = time.Second * time.Duration(intValue)
	return nil
}

func (ag *AssetGlob) UnmarshalYAML(value *yaml.Node) error {
	err := value.Decode(&ag.Pattern)
	if err != nil {
		return err
	}
	ag.Globber, err = glob.Compile(ag.Pattern)
	if err != nil {
		return errors.Wrapf(err, "invalid asset glob pattern: %s", ag.Pattern)
	}
	return nil
}

type RepoSettings struct {
	ReleaseDirectory string `yaml:"release_dir"`
	AccessToken      string `yaml:"access_token"`
	Log              *logrus.Logger
}

type Repository struct {
	RepoSettings    `yaml:",inline"`
	Name            string `yaml:"name"` // owner/repo
	Owner           string
	Repo            string
	AssetsGlobs     []AssetGlob `yaml:"assets"`
	Tasks           []*Task     `yaml:"tasks"`
	GropeEverything bool        `yaml:"grope_everything"`
	Client          *github.Client
}

func (r *Repository) GropeAsset(ctx context.Context, asset *github.ReleaseAsset, dst string) error {
	rc, _, err := r.Client.Repositories.DownloadReleaseAsset(ctx, r.Owner, r.Repo, asset.GetID(), http.DefaultClient)
	if err != nil {
		return errors.Wrapf(err, "%s.%s: asset groping failed", r.Name, asset.GetName())
	}
	defer rc.Close()
	dstfd, err := os.Create(dst)
	if err != nil {
		return errors.Wrapf(err, "%s.%s: asset groping failed", r.Name, asset.GetName())
	}
	defer dstfd.Close()

	_, err = io.Copy(dstfd, rc)
	if err != nil {
		return errors.Wrapf(err, "%s.%s: asset groping failed", r.Name, asset.GetName())
	}

	return nil
}

func (r *Repository) FeelAndGrope(ctx context.Context) {
	release, _, err := r.Client.Repositories.GetLatestRelease(ctx, r.Owner, r.Repo)
	if err != nil {
		r.Log.WithError(err).Errorf("%s: error checking for latest releases", r.Name)
		return
	}

	if release.GetPrerelease() || release.GetDraft() {
		r.Log.Infof("%s.%s: skipped draft/pre-release", r.Name, release.GetTagName())
		return
	}

	// check if .release file exists
	releaseFile := path.Join(r.ReleaseDirectory, release.GetTagName()+".release")
	_, err = os.Stat(releaseFile)

	// base directory
	basedir := path.Join(r.ReleaseDirectory, release.GetTagName())

	if err := os.MkdirAll(basedir, os.ModePerm); err != nil {
		r.Log.WithError(err).Errorf("%s.%s: error creating download destination", r.Name, release.GetTagName())
		return
	}

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.Log.Infof("%s.%s: not found locally.", r.Name, release.GetTagName())

			assets := make([]*github.ReleaseAsset, 0)
			if r.GropeEverything {
				r.Log.Infof("%s.%s: will grope everything", r.Name, release.GetTagName())
				assets = append(assets, release.Assets...)
			} else {
				for _, assetGlob := range r.AssetsGlobs {
					for _, releaseAsset := range release.Assets {
						if assetGlob.Globber.Match(releaseAsset.GetName()) {
							assets = append(assets, releaseAsset)
						}
					}
				}
			}

			//
			r.Log.Infof("%s.%s: groping %d assets...", r.Name, release.GetTagName(), len(assets))
			successfullyGroped := true

			for _, asset := range assets {
				assetDst := path.Join(basedir, asset.GetName())
				r.Log.Infof("%s.%s: groping %s to %s", r.Name, release.GetTagName(), asset.GetName(), assetDst)

				err := r.GropeAsset(ctx, asset, assetDst)
				if err != nil {
					r.Log.WithError(err).Error()
					successfullyGroped = false
				}
			}

			if successfullyGroped {
				r.Log.Infof("%s.%s: running tasks", r.Name, release.GetTagName())
				assetNames := []string{}
				for _, asset := range assets {
					assetNames = append(assetNames, asset.GetName())
				}
				assetNameList := strings.Join(assetNames, ";")
				for _, task := range r.Tasks {
					if !task.ExecuteFor(r, release, basedir, assetNameList) {
						successfullyGroped = false
						break
					}
				}
				if successfullyGroped {
					// register release
					r.Log.Infof("%s.%s: save release info", r.Name, release.GetTagName())

					if err := os.WriteFile(releaseFile, []byte{}, os.ModePerm); err != nil {
						r.Log.WithError(err).Errorf("%s.%s: error saving release info", r.Name, release.GetTagName())
					}
				}
			}
		} else {
			r.Log.WithError(err).Errorf("%s: error checking for local release", r.Name)
		}
		return
	} else {
		r.Log.Infof("%s.%s exists locally", r.Name, release.GetTagName())
	}
}
