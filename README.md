# gitgrope
Serverside GitHub deployment manager

### What

The goal of `gitgrope` is to provide the simplest way known to mankind of getting your repository onto your server, without too many complexities. 
It works by polling your GitHub repositories for latest releaases (via tags) and downloading the releases to the server. 

That's that. The rest is up to you.

### How
1. Create an access token for your private epository. If your repository is public there is no need for an access token.
2. Install, configure, and run `gitgrope` on your server as a service.
3. Expect your releases to be deployed after the amount of polling time configured.


### Configuration file fields

The following example lists all possible configuration fields


```yaml
access_token: "my_global_token" 
log_file: "logs/gitgrope.log" # Log file (default: logs to stdout. Path doesn't need to exist)
release_dir: "test" # where to download files
poll_seconds: 15 # polling timeout (seconds)
http_timeout: 30 # http client timeout (seconds: default 30 seconds)
task_shell: /bin/sh # Shell for running tasks (default is platform dependent)
repos:
  - name: "SharkFourSix/gitrope" # Name of repository (required)
    grope_everything: true # Download all release assets (default: false)
    tasks: # Tasks to run after successfuly downloading assets
      - name: info
        run: 'echo "Commit hash: $GITHUB_RELEASE_COMMITSH"'
      - name: "Unzip"
        run: unzip gitgrope_releases.zip
  - name: SharkFourSix/gompare
    # access_token: # Repository level access token
    assets:
       - gompare_*linux*.zip # Using glob patterns to match assets
```

The following environment variables are available to the tasks:

1. `GITHUB_RELEASE_REPO` - Repository
2. `GITHUB_RELEASE_URL` - Release URL
3. `GITHUB_RELEASE_TAG` - Release tag
4. `GITHUB_RELEASE_COMMITSH` - Commit hash branch/hash
5. `GITHUB_RELEASE_ASSETS` - Contains a semi colon-separated list of release assets, that matched the configured glob.

### Usage


Specify a configuration file

```shell
gitgrope [-config-file optional/path/to/config/file ]
```

... or run without a configuration file (defaults to `.grope.yaml`)

```shell
gitgrope
```