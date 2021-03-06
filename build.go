package dojoBuilder

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"text/template"
)

const profileTemplate = `var profile = {{.}};`

type BuildConfig struct {
	RemoveUncompressed    bool `json:"removeUncompressed,omitempty"` // Remove uncompressed js files after build
	RemoveConsoleStripped bool `json:"removeConsoleStripped,omitempty"`

	BasePath    string           `json:"basePath"`
	ReleaseDir  string           `json:"releaseDir"`
	ReleaseName string           `json:"releaseName,omitempty"`
	Action      string           `json:"action"`
	Packages    []Package        `json:"packages"`
	Layers      map[string]Layer `json:"layers"`

	LayerOptimize     string             `json:"layerOptimize,omitempty"`
	Optimize          string             `json:"optimize,omitempty"`
	CssOptimize       string             `json:"cssOptimize,omitempty"`
	Mini              bool               `json:"mini,omitempty"`
	StripConsole      string             `json:"stripConsole,omitempty"`
	SelectorEngine    string             `json:"selectorEngine,omitempty"`
	StaticHasFeatures map[string]Feature `json:"staticHasFeatures,omitempty"`
	UseSourceMaps     bool               `json:"useSourceMaps"` // Build generate source maps
}

type Package struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

type Layer struct {
	Boot       bool     `json:"boot"`
	CustomBase bool     `json:"customBase"`
	Include    []string `json:"include,omitempty"`
	Exclude    []string `json:"exclude,omitempty"`
}

type Feature bool

func (f Feature) MarshalJSON() ([]byte, error) {
	var v uint8 = 0
	if bool(f) {
		v = 1
	}
	return json.Marshal(v)
}

var (
	buildExcludeFunc ExcludeFunc = func(path string, f os.FileInfo) (bool, error) {
		return false, nil
	}

	// DefaultBuildExcludeFunc skips uncompressed and consoleStripped js files
	DefaultBuildExcludeFunc = func(path string, f os.FileInfo) (bool, error) {
		var skippedFilesPatterns []string = []string{`.*\.js\.(uncompressed|consoleStripped)\.js`}
		var skippedDirsPatterns []string = []string{}

		if f.IsDir() {
			return IsMatchSliceMember(skippedDirsPatterns, path)
		}

		return IsMatchSliceMember(skippedFilesPatterns, path)

	}
)

func SetBuildExcludeFunc(exFunc ExcludeFunc) { buildExcludeFunc = exFunc }

func (c *Config) generateBuildProfile(name string) (profileFullPath string, err error) {
	bc, ok := c.BuildConfigs[name]
	if !ok {
		return "", errors.New("No build config found with name '" + name + "'")
	}

	if bc.Action == "" {
		bc.Action = "release"
	}

	profilePath := c.SrcDir + "/profiles/"
	os.MkdirAll(profilePath, 0754)

	profileFullPath = profilePath + name + ".profile.js"

	bc.BasePath = ".."

	bc.ReleaseDir = c.DestDir + "/dojoBuilderTMP"

	j, err := json.Marshal(bc)
	if err != nil {
		return "", err
	}

	f, err := os.OpenFile(profileFullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0664)
	if err != nil {
		return "", err
	}

	t := template.Must(template.New("profileTemplate").Parse(profileTemplate))
	err = t.Execute(f, string(j))

	return profileFullPath, err
}

func (c *Config) build(names []string) (err error) {
	var profilePath string

	if len(names) == 0 {
		for n, _ := range c.BuildConfigs {
			names = append(names, n)
		}
	}

	for _, n := range names {
		fmt.Printf("Generating %s build\n", n)

		profilePath, err = c.generateBuildProfile(n)
		if err != nil {
			return
		}

		if err = c.executeBuildProfile(profilePath); err != nil {
			return
		}

		bc, _ := c.BuildConfigs[n]
		bc.ReleaseDir = c.DestDir + "/dojoBuilderTMP"

		err = filepath.Walk(bc.ReleaseDir, func(path string, f os.FileInfo, err error) (_err error) {
			if path == bc.ReleaseDir {
				return
			}

			isDir := f.IsDir()
			dest := c.DestDir + path[len(bc.ReleaseDir):]

			if skip, err := buildExcludeFunc(path, f); err != nil {
				return err
			} else if skip {
				if isDir {
					return filepath.SkipDir
				}
				return
			} else if isDir {
				if _err = os.Mkdir(dest, 0754); _err != nil {
					return
				}
			} else if _err = CopyFile(path, dest); _err != nil {
				return
			}

			st := f.Sys().(*syscall.Stat_t)

			os.Chown(dest, int(st.Uid), int(st.Gid))

			return
		})

		os.RemoveAll(bc.ReleaseDir)
	}

	return
}

func (c *Config) executeBuildProfile(profilePath string) (err error) {
	buildScriptPath := c.SrcDir + "/util/buildscripts/build.sh"

	args := []string{"--profile", profilePath}

	if c.Bin != "" {
		args = append(args, "--bin", c.Bin)
	}

	cmd := exec.Command(buildScriptPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(stdout)
	go func() {
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	err = cmd.Run()
	if err != nil {
		return errors.New("Build command failed")
	}

	return
}
